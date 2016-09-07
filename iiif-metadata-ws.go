package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/julienschmidt/httprouter"
	"github.com/lestrrat/go-libxml2"
	"github.com/lestrrat/go-libxml2/xpath"
	"github.com/spf13/viper"
)

var db *sql.DB // global variable to share it between main and the HTTP handler
var logger *log.Logger

const version = "1.2.1"

// Types used to generate the JSON response; masterFile and iiifData
type masterFile struct {
	PID           string
	Title         string
	Description   string
	Width         int
	Height        int
	Transcription string
}
type iiifData struct {
	IiifURL     string
	URL         string
	BiblPID     string
	Title       string
	Description string
	MasterFiles []masterFile
}

/**
 * Main entry point for the web service
 */
func main() {
	lf, _ := os.OpenFile("service.log", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0666)
	defer lf.Close()
	logger = log.New(lf, "service: ", log.LstdFlags)
	// use below to log to console....
	// logger = log.New(os.Stdout, "logger: ", log.LstdFlags)

	// Load cfg
	logger.Printf("===> iiif-metadata-ws staring up <===")
	logger.Printf("Load configuration...")
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	viper.AddConfigPath(".")
	viper.AddConfigPath(path.Dir(os.Args[0]))
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Printf("Unable to read config: %s", err.Error())
		os.Exit(1)
	}

	// Init DB connection
	logger.Printf("Init DB connection...")
	connectStr := fmt.Sprintf("%s:%s@tcp(%s)/%s?allowOldPasswords=%s", viper.GetString("db_user"), viper.GetString("db_pass"),
		viper.GetString("db_host"), viper.GetString("db_name"), viper.GetString("db_old_passwords"))
	db, err = sql.Open("mysql", connectStr)
	if err != nil {
		fmt.Printf("Database connection failed: %s", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	// Set routes and start server
	mux := httprouter.New()
	mux.GET("/", rootHandler)
	mux.GET("/:pid/manifest.json", iiifHandler)
	mux.GET("/:pid", iiifHandler)
	logger.Printf("Start service on port %s", viper.GetString("port"))
	http.ListenAndServe(":"+viper.GetString("port"), mux)
}

/**
 * Handle a request for /
 */
func rootHandler(rw http.ResponseWriter, req *http.Request, params httprouter.Params) {
	logger.Printf("%s %s", req.Method, req.RequestURI)
	fmt.Fprintf(rw, "IIIF metadata service version %s", version)
}

/**
 * Handle a request for IIIF metdata; returns json
 */
func iiifHandler(rw http.ResponseWriter, req *http.Request, params httprouter.Params) {
	logger.Printf("%s %s", req.Method, req.RequestURI)
	pid := params.ByName("pid")

	// initialize IIIF data struct
	var data iiifData
	data.URL = fmt.Sprintf("http://%s%s", req.Host, req.URL)
	data.IiifURL = viper.GetString("iiif_url")

	// handle different types of PID
	pidType := determinePidType(pid)
	if pidType == "bibl" {
		logger.Printf("%s is a bibl", pid)
		data.BiblPID = pid
		generateBiblMetadata(data, rw)
	} else if pidType == "master_file" {
		logger.Printf("%s is a masterfile", pid)
		generateMasterFileMetadata(pid, data, rw)
	} else if pidType == "item" {
		logger.Printf("%s is an item", pid)
		generateItemMetadata(pid, data, rw)
	} else if pidType == "component" {
		logger.Printf("%s is a component", pid)
		data.BiblPID = pid
		generateComponentMetadata(data, rw)
	} else {
		logger.Printf("Couldn't find %s", pid)
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(rw, "PID %s not found", pid)
	}
}

func determinePidType(pid string) (pidType string) {
	var cnt int
	pidType = "invalid"
	qs := "select count(*) as cnt from bibls b where pid=?"
	db.QueryRow(qs, pid).Scan(&cnt)
	if cnt == 1 {
		pidType = "bibl"
		return
	}

	qs = "select count(*) as cnt from master_files b where pid=?"
	db.QueryRow(qs, pid).Scan(&cnt)
	if cnt == 1 {
		pidType = "master_file"
		return
	}

	qs = "select count(*) as cnt from items b where pid=?"
	db.QueryRow(qs, pid).Scan(&cnt)
	if cnt == 1 {
		pidType = "item"
		return
	}

	qs = "select count(*) as cnt from components b where pid=?"
	db.QueryRow(qs, pid).Scan(&cnt)
	if cnt == 1 {
		pidType = "component"
		return
	}

	return
}

func generateComponentMetadata(data iiifData, rw http.ResponseWriter) {
	var compID int
	var label sql.NullString
	var title sql.NullString

	qs := "select c.id, c.title, c.label, c.content_desc from components c where pid=?"
	err := db.QueryRow(qs, data.BiblPID).Scan(&compID, &title, &label, &data.Description)
	if err != nil {
		logger.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}

	logger.Printf("BiblPID: %s, compID: %s\n title: %s, label: %s\n desc: %s", data.BiblPID, compID, title, label, data.Description)

	data.Title = ""
	if title.Valid == true {
		data.Title = data.Title + title.String
	} else if label.Valid == true {
		data.Title = data.Title + label.String
	} else {
		data.Title = data.BiblPID
	}

	// Get data for all master files from units associated with bibl
	qs = `select m.pid, m.title, m.description, m.transcription_text, t.width, t.height from master_files m
	      inner join image_tech_meta t on m.id=t.master_file_id where m.component_id = ? `
	rows, _ := db.Query(qs, compID)
	defer rows.Close()
	for rows.Next() {
		var mf masterFile
		var mfDesc sql.NullString
		var mfTrans sql.NullString
		err = rows.Scan(&mf.PID, &mf.Title, &mfDesc, &mfTrans, &mf.Width, &mf.Height)
		if err != nil {
			logger.Printf("Unable to retreive IIIF MasterFile metadata for %s: %s", data.BiblPID, err.Error())
			fmt.Fprintf(rw, "Unable to retreive IIIF MasterFile metadata: %s", err.Error())
			return
		}
		mf.Description = mfDesc.String
		if mfTrans.Valid {
			mf.Transcription = strings.Replace(mfTrans.String, "\n", "\\n", -1)
			mf.Transcription = strings.Replace(mf.Transcription, "\r", "", -1)
		}
		data.MasterFiles = append(data.MasterFiles, mf)
	}
	renderMetadata(data, rw)
}

func generateBiblMetadata(data iiifData, rw http.ResponseWriter) {
	var biblID int
	var desc sql.NullString
	qs := "select b.id, b.title, b.description from bibls b where pid=?"
	err := db.QueryRow(qs, data.BiblPID).Scan(&biblID, &data.Title, &desc)
	if err != nil {
		logger.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}

	// set description if it is available
	if desc.Valid == true {
		data.Description = desc.String
	}

	// Get data for all master files from units associated with bibl
	qs = `select m.pid, m.title, m.description, m.transcription_text, t.width, t.height from master_files m
	      inner join units u on u.id=m.unit_id
	      inner join image_tech_meta t on m.id=t.master_file_id where u.bibl_id = ? and u.include_in_dl = ?`
	rows, _ := db.Query(qs, biblID, 1)
	defer rows.Close()
	for rows.Next() {
		var mf masterFile
		var mfDesc sql.NullString
		var mfTrans sql.NullString
		err = rows.Scan(&mf.PID, &mf.Title, &mfDesc, &mfTrans, &mf.Width, &mf.Height)
		if err != nil {
			logger.Printf("Unable to retreive IIIF MasterFile metadata for %s: %s", data.BiblPID, err.Error())
			fmt.Fprintf(rw, "Unable to retreive IIIF MasterFile metadata: %s", err.Error())
			return
		}
		mf.Description = mfDesc.String
		if mfTrans.Valid {
			mf.Transcription = strings.Replace(mfTrans.String, "\n", "\\n", -1)
			mf.Transcription = strings.Replace(mf.Transcription, "\r", "", -1)
		}
		data.MasterFiles = append(data.MasterFiles, mf)
	}
	renderMetadata(data, rw)
}

func renderMetadata(data iiifData, rw http.ResponseWriter) {
	rw.Header().Set("Access-Control-Allow-Origin", "*")
	rw.Header().Set("content-type", "application/json; charset=utf-8")
	tmpl, _ := template.ParseFiles("iiif.json")
	err := tmpl.ExecuteTemplate(rw, "iiif.json", data)
	if err != nil {
		logger.Printf("Unable to render IIIF metadata for %s: %s", data.BiblPID, err.Error())
		fmt.Fprintf(rw, "Unable to render IIIF metadata: %s", err.Error())
		return
	}
	logger.Printf("IIIF Metadata generated for %s", data.BiblPID)
}

func generateItemMetadata(pid string, data iiifData, rw http.ResponseWriter) {
	// grab all of the masterfiles hooked to this item
	var extURI sql.NullString
	var unitID int
	var itemID int
	qs := `select id, external_uri, unit_id from items where pid=?`
	err := db.QueryRow(qs, pid).Scan(&itemID, &extURI, &unitID)
	if err != nil {
		logger.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}

	// Get data for all master files attached to this item
	qs = `select m.pid, m.title, m.description, m.transcription_text, m.desc_metadata, t.width, t.height from master_files m
	      inner join image_tech_meta t on m.id=t.master_file_id where m.item_id = ?`
	rows, _ := db.Query(qs, itemID)
	defer rows.Close()
	for rows.Next() {
		var mf masterFile
		var mfDesc sql.NullString
		var mfTrans sql.NullString
		var mfDescMetadata sql.NullString
		err = rows.Scan(&mf.PID, &mf.Title, &mfDesc, &mfTrans, &mfDescMetadata, &mf.Width, &mf.Height)
		if err != nil {
			logger.Printf("Unable to retreive IIIF MasterFile metadata for %s: %s", data.BiblPID, err.Error())
			fmt.Fprintf(rw, "Unable to retreive IIIF MasterFile metadata: %s", err.Error())
			return
		}
		mf.Description = mfDesc.String
		if mfTrans.Valid {
			mf.Transcription = strings.Replace(mfTrans.String, "\n", "\\n", -1)
			mf.Transcription = strings.Replace(mf.Transcription, "\r", "", -1)
		}
		// MODS desc metadata in record overrides title and desc
		if mfDescMetadata.Valid {
			parseMods(&mf, mfDescMetadata.String)
		}
		data.MasterFiles = append(data.MasterFiles, mf)
	}

	// TODO when extURI is populated, go scrape external system for metadata
	// since this will be blank for some time, it is being ignored and bibl is pulled

	qs = `select b.title, b.description from units u inner join bibls b on u.bibl_id=b.id where u.id=?`
	var desc sql.NullString
	err = db.QueryRow(qs, unitID).Scan(&data.Title, &desc)
	if err != nil {
		logger.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}
	data.Description = desc.String
	renderMetadata(data, rw)
}

func generateMasterFileMetadata(mfPid string, data iiifData, rw http.ResponseWriter) {
	var desc sql.NullString
	var mfTitle sql.NullString
	var mfDesc sql.NullString
	var mfDescMetadata sql.NullString
	var mf masterFile
	mf.PID = mfPid
	qs := `select b.pid, b.title, b.description, m.title, m.description, m.desc_metadata, t.width, t.height from master_files m
	      inner join units u on u.id=m.unit_id
         inner join bibls b on u.bibl_id=b.id
	      inner join image_tech_meta t on m.id=t.master_file_id where m.pid = ?`
	err := db.QueryRow(qs, mfPid).Scan(&data.BiblPID, &data.Title, &desc, &mfTitle, &mfDesc, &mfDescMetadata, &mf.Width, &mf.Height)
	if err != nil {
		logger.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}

	// set title and descriptions if available
	data.Description = desc.String
	mf.Description = mfDesc.String
	mf.Title = mfTitle.String

	// MODS desc metadata in record overrides title and desc
	if mfDescMetadata.Valid {
		parseMods(&mf, mfDescMetadata.String)
	}

	data.MasterFiles = append(data.MasterFiles, mf)

	renderMetadata(data, rw)
}

/**
 * Parse title and description from MODS string
 */
func parseMods(data *masterFile, mods string) {
	doc, err := libxml2.ParseString(mods)
	if err != nil {
		logger.Printf("Unable to parse MODS: %s; just using data from DB", err.Error())
		return
	}
	defer doc.Free()

	root, err := doc.DocumentElement()
	if err != nil {
		logger.Printf("Failed to fetch document element: %s", err)
		return
	}

	ctx, err := xpath.NewContext(root)
	if err != nil {
		logger.Printf("Failed to create xpath context: %s", err)
		return
	}
	defer ctx.Free()

	if err := ctx.RegisterNS("ns", "http://www.loc.gov/mods/v3"); err != nil {
		logger.Printf("Failed to register namespace: %s", err)
		return
	}
	title := xpath.String(ctx.Find("/ns:mods/ns:titleInfo/ns:title/text()"))
	if len(title) > 0 {
		data.Title = title
	}

	// first try <abstract displayLabel="Description">
	desc := xpath.String(ctx.Find("//ns:abstract[@displayLabel='Description']/text()"))
	if len(desc) > 0 {
		data.Description = desc
		return
	}

	// .. next try for a provenance note
	desc = xpath.String(ctx.Find("//ns:note[@type='provenance' and @displayLabel='staff']/text()"))
	if len(desc) > 0 {
		data.Description = fmt.Sprintf("Staff note: %s", desc)
	}
}

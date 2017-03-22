package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/julienschmidt/httprouter"
	"github.com/lestrrat/go-libxml2"
	"github.com/lestrrat/go-libxml2/xpath"
	"github.com/rs/cors"
	"github.com/spf13/viper"
)

var db *sql.DB // global variable to share it between main and the HTTP handler
var logger *log.Logger

const version = "1.5.1"

// Types used to generate the JSON response; masterFile and iiifData
type masterFile struct {
	PID         string
	Title       string
	Description string
	Width       int
	Height      int
}
type iiifData struct {
	IiifURL     string
	URL         string
	MetadataPID string
	Title       string
	Exemplar    int
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
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Printf("Unable to read config: %s", err.Error())
		os.Exit(1)
	}

	// Init DB connection
	logger.Printf("Init DB connection to %s...", viper.GetString("db_host"))
	connectStr := fmt.Sprintf("%s:%s@tcp(%s)/%s?allowOldPasswords=%s", viper.GetString("db_user"), viper.GetString("db_pass"),
		viper.GetString("db_host"), viper.GetString("db_name"), viper.GetString("db_old_passwords"))
	db, err = sql.Open("mysql", connectStr)
	if err != nil {
		fmt.Printf("Database connection failed: %s", err.Error())
		os.Exit(1)
	}
	_, err = db.Query("SELECT 1")
	if err != nil {
		fmt.Printf("Database query failed: %s", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	// Set routes and start server
	mux := httprouter.New()
	mux.GET("/", rootHandler)
	mux.GET("/:pid/manifest.json", iiifHandler)
	mux.GET("/:pid", iiifHandler)
	logger.Printf("Start service on port %s", viper.GetString("port"))

	if viper.GetBool("https") == true {
		crt := viper.GetString("ssl_crt")
		key := viper.GetString("ssl_key")
		log.Fatal(http.ListenAndServeTLS(":"+viper.GetString("port"), crt, key, cors.Default().Handler(mux)))
	} else {
		log.Fatal(http.ListenAndServe(":"+viper.GetString("port"), cors.Default().Handler(mux)))
	}
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
	if pidType == "metadata" {
		logger.Printf("%s is a metadata record", pid)
		data.MetadataPID = pid
		generateFromMetadataRecord(data, rw)
	} else if pidType == "component" {
		logger.Printf("%s is a component", pid)
		data.MetadataPID = pid
		generateFromComponent(pid, data, rw)
	} else {
		logger.Printf("Couldn't find %s", pid)
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(rw, "PID %s not found", pid)
	}
}

func determinePidType(pid string) (pidType string) {
	var cnt int
	pidType = "invalid"
	qs := "select count(*) as cnt from metadata b where pid=?"
	db.QueryRow(qs, pid).Scan(&cnt)
	if cnt == 1 {
		pidType = "metadata"
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

func generateFromMetadataRecord(data iiifData, rw http.ResponseWriter) {
	var metadataID int
	var exemplar sql.NullString
	var descMetadata sql.NullString
	var metadataType string
	qs := "select id, title, exemplar, type, desc_metadata from metadata where pid=?"
	err := db.QueryRow(qs, data.MetadataPID).Scan(&metadataID, &data.Title, &exemplar, &metadataType, &descMetadata)
	if err != nil {
		logger.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}

	// Get data for all master files from units associated with the metadata record
	qs = `select m.pid, m.filename, m.title, m.description, t.width, t.height from master_files m
	      inner join units u on u.id=m.unit_id
	      inner join image_tech_meta t on m.id=t.master_file_id where m.metadata_id = ? and u.include_in_dl = ? order by m.filename asc`
	rows, _ := db.Query(qs, metadataID, 1)
	defer rows.Close()
	pgNum := 0
	for rows.Next() {
		var mf masterFile
		var mfFilename string
		var mfTitle sql.NullString
		var mfDesc sql.NullString
		err = rows.Scan(&mf.PID, &mfFilename, &mfTitle, &mfDesc, &mf.Width, &mf.Height)
		if err != nil {
			logger.Printf("Unable to retreive IIIF MasterFile metadata for %s: %s", data.MetadataPID, err.Error())
			fmt.Fprintf(rw, "Unable to retreive IIIF MasterFile metadata: %s", err.Error())
			return
		}
		mf.Description = mfDesc.String
		mf.Title = mfTitle.String

		// If the metadata for this master file is XML, the MODS desc metadata in record overrides title and desc
		if descMetadata.Valid && strings.Compare("XmlMetadata", metadataType) == 0 {
			parseMods(&mf, descMetadata.String)
		}
		data.MasterFiles = append(data.MasterFiles, mf)

		// if exemplar is set, see if it matches the current master file filename
		// if it does, set the current page num as the start canvas
		if exemplar.Valid && strings.Compare(mfFilename, exemplar.String) == 0 {
			data.Exemplar = pgNum
			logger.Printf("Exemplar set to filename %s, page %d", mfFilename, data.Exemplar)
		}
		pgNum++
	}
	renderIiifMetadata(data, rw)
}

func generateFromComponent(pid string, data iiifData, rw http.ResponseWriter) {
	// grab all of the masterfiles hooked to this component
	var componentID int
	var exemplar sql.NullString
	var cTitle sql.NullString
	qs := `select id, title, exemplar from components where pid=?`
	err := db.QueryRow(qs, pid).Scan(&componentID, &cTitle, &exemplar)
	if err != nil {
		logger.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}
	data.Title = cTitle.String

	// Get data for all master files attached to this component
	pgNum := 0
	qs = `select m.pid, m.filename, m.title, m.description, b.desc_metadata, t.width, t.height from master_files m
         inner join metadata b on b.id = m.metadata_id
	      inner join image_tech_meta t on m.id=t.master_file_id where m.component_id = ? order by m.filename asc`
	rows, _ := db.Query(qs, componentID)
	defer rows.Close()
	for rows.Next() {
		var mf masterFile
		var mfFilename string
		var mfTitle sql.NullString
		var mfDesc sql.NullString
		var mfDescMetadata sql.NullString
		err = rows.Scan(&mf.PID, &mfFilename, &mfTitle, &mfDesc, &mfDescMetadata, &mf.Width, &mf.Height)
		if err != nil {
			logger.Printf("Unable to retreive IIIF MasterFile metadata for %s: %s", data.MetadataPID, err.Error())
			fmt.Fprintf(rw, "Unable to retreive IIIF MasterFile metadata: %s", err.Error())
			return
		}
		mf.Description = mfDesc.String
		mf.Title = mfTitle.String

		// MODS desc metadata in record overrides title and desc
		if mfDescMetadata.Valid {
			parseMods(&mf, mfDescMetadata.String)
		}

		data.MasterFiles = append(data.MasterFiles, mf)

		// if exemplar is set, see if it matches the current master file filename
		// if it does, set the current page num as the start canvas
		if exemplar.Valid && strings.Compare(mfFilename, exemplar.String) == 0 {
			data.Exemplar = pgNum
			logger.Printf("Exemplar set to filename %s, page %d", mfFilename, data.Exemplar)
		}
		pgNum++

	}
	renderIiifMetadata(data, rw)
}

func renderIiifMetadata(data iiifData, rw http.ResponseWriter) {
	// rw.Header().Set("content-type", "application/json; charset=utf-8")
	tmpl := template.Must(template.ParseFiles("iiif.json"))
	err := tmpl.ExecuteTemplate(rw, "iiif.json", data)
	if err != nil {
		logger.Printf("Unable to render IIIF metadata for %s: %s", data.MetadataPID, err.Error())
		fmt.Fprintf(rw, "Unable to render IIIF metadata: %s", err.Error())
		return
	}
	logger.Printf("IIIF Metadata generated for %s", data.MetadataPID)
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

package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/go-zoo/bone"
	"github.com/spf13/viper"
)

var db *sql.DB // global variable to share it between main and the HTTP handler
var logger *log.Logger

// Types used to generate the JSON response; masterFile and iiifData
type masterFile struct {
	PID    string
	Title  string
	Width  int
	Height int
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
	logger.Printf("Init DB connection...")
	connectStr := fmt.Sprintf("%s:%s@tcp(%s)/%s", viper.GetString("db_user"), viper.GetString("db_pass"),
		viper.GetString("db_host"), viper.GetString("db_name"))
	db, err = sql.Open("mysql", connectStr)
	if err != nil {
		fmt.Printf("Database connection failed: %s", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	// Set routes and start server
	mux := bone.New()
	mux.Get("/iiif/:pid/manifest.json", http.HandlerFunc(iiifHandler))
	mux.Get("/iiif/:pid", http.HandlerFunc(iiifHandler))
	mux.Get("/", http.HandlerFunc(rootHandler))
	logger.Printf("Start service on port %s", viper.GetString("port"))
	http.ListenAndServe(":"+viper.GetString("port"), mux)
}

/**
 * Handle a request for /
 */
func rootHandler(rw http.ResponseWriter, req *http.Request) {
	logger.Printf("%s %s", req.Method, req.RequestURI)
	fmt.Fprintf(rw, "IIIF metadata service. Usage: ./iiif/[pid]/manifest.json")
}

/**
 * Handle a request for IIIF metdata; returns json
 */
func iiifHandler(rw http.ResponseWriter, req *http.Request) {
	logger.Printf("%s %s", req.Method, req.RequestURI)
	pid := bone.GetValue(req, "pid")

	// init template data with request URL
	var data iiifData
	data.URL = fmt.Sprintf("http://%s%s", req.Host, req.URL)
	data.IiifURL = viper.GetString("iiif_url") // default to this; set to UVA only after bibl retrieved

	// Get BIBL data for the passed PID
	var availability sql.NullInt64
	var biblID int
	qs := "select b.id,b.title,b.description,b.pid,b.availability_policy_id from bibls b where pid=?"
	err := db.QueryRow(qs, pid).Scan(&biblID, &data.Title, &data.Description, &data.BiblPID, &availability)
	switch {
	case err == sql.ErrNoRows:
		logger.Printf("%s not found", pid)
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(rw, "%s not found", pid)
		return
	case err != nil:
		logger.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}

	// Must have availability set
	if availability.Valid == false {
		logger.Printf("%s not found", pid)
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(rw, "%s not found", pid)
		return
	}
	if availability.Int64 == 3 {
		data.IiifURL = viper.GetString("iiif_uvaonly_url")
	}

	// Get data for all master files from units associated with bibl
	qs = `select m.pid, m.title, t.width, t.height from master_files m
         inner join units u on u.id=m.unit_id
         inner join image_tech_meta t on m.id=t.master_file_id where u.bibl_id = ?`
	rows, _ := db.Query(qs, biblID)
	defer rows.Close()
	for rows.Next() {
		var mf masterFile
		err = rows.Scan(&mf.PID, &mf.Title, &mf.Width, &mf.Height)
		if err != nil {
			logger.Printf("Unable to retreive IIIF MasterFile metadata for %s: %s", pid, err.Error())
			fmt.Fprintf(rw, "Unable to retreive IIIF MasterFile metadata: %s", err.Error())
			return
		}
		data.MasterFiles = append(data.MasterFiles, mf)
	}

	// Render the json template with all of the data collected above
	tmpl, _ := template.ParseFiles("iiif.json")
	err = tmpl.ExecuteTemplate(rw, "iiif.json", data)
	if err != nil {
		logger.Printf("Unable to render IIIF metadata for %s: %s", pid, err.Error())
		fmt.Fprintf(rw, "Unable to render IIIF metadata: %s", err.Error())
		return
	}
	logger.Printf("IIIF Metadata generated for %s", pid)
}

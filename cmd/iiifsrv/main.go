package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/cors"
	"github.com/spf13/viper"
	"github.com/uvalib/iiif-metadata-ws/internal/models"
	"github.com/uvalib/iiif-metadata-ws/internal/parsers"
)

const version = "1.8.0"

// globals to share between main and the HTTP handler
var db *sql.DB

/**
 * Main entry point for the web service
 */
func main() {
	log.Printf("===> iiif-metadata-ws staring up <===")
	log.Printf("Load configuration...")
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Printf("Unable to read config: %s", err.Error())
		os.Exit(1)
	}

	// Init DB connection
	log.Printf("Init DB connection to %s...", viper.GetString("db_host"))
	connectStr := fmt.Sprintf("%s:%s@tcp(%s)/%s", viper.GetString("db_user"), viper.GetString("db_pass"),
		viper.GetString("db_host"), viper.GetString("db_name"))
	db, err = sql.Open("mysql", connectStr)
	if err != nil {
		fmt.Printf("Database connection failed: %s", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	// Set routes and start server
	mux := httprouter.New()
	mux.GET("/", loggingHandler(rootHandler))
	mux.GET("/:pid/manifest.json", loggingHandler(iiifHandler))
	mux.GET("/:pid", loggingHandler(iiifHandler))

	if viper.GetBool("https") == true {
		crt := viper.GetString("ssl_crt")
		key := viper.GetString("ssl_key")
		log.Printf("Start HTTPS service on port %s", viper.GetString("port"))
		log.Fatal(http.ListenAndServeTLS(":"+viper.GetString("port"), crt, key, cors.Default().Handler(mux)))
	} else {
		log.Printf("Start HTTP service on port %s", viper.GetString("port"))
		log.Fatal(http.ListenAndServe(":"+viper.GetString("port"), cors.Default().Handler(mux)))
	}
}

/**
 * Function Adapter for httprouter handlers that will log start and complete info.
 * A bit different than usual looking adapter because of the httprouter library used. Foun
 * this code here:
 *   https://stackoverflow.com/questions/43964461/how-to-use-middlewares-when-using-julienschmidt-httprouter-in-golang
 */
func loggingHandler(next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		start := time.Now()
		log.Printf("Started %s %s", req.Method, req.RequestURI)
		next(w, req, ps)
		log.Printf("Completed %s %s in %s", req.Method, req.RequestURI, time.Since(start))
	}
}

/**
 * Handle a request for /
 */
func rootHandler(rw http.ResponseWriter, req *http.Request, params httprouter.Params) {
	fmt.Fprintf(rw, "IIIF metadata service version %s", version)
}

/**
 * Handle a request for IIIF metdata; returns json
 */
func iiifHandler(rw http.ResponseWriter, req *http.Request, params httprouter.Params) {
	pid := params.ByName("pid")
	if strings.Compare(pid, "favicon.ico") == 0 {
		return
	}
	unitID, _ := strconv.Atoi(req.URL.Query().Get("unit"))

	// initialize IIIF data struct
	var data models.IIIF
	data.URL = fmt.Sprintf("http://%s%s", req.Host, req.URL)
	data.IiifURL = viper.GetString("iiif_url")
	data.VirgoURL = viper.GetString("virgo_url")
	data.MetadataPID = pid
	data.Metadata = make(map[string]string)

	// Tracksys is the system that tracks items that contain
	// masterfiles. All pids the arrive at the IIIF service should
	// refer to these items. Determine what type the PID is:
	pidURL := fmt.Sprintf("%s/pid/%s/type", viper.GetString("tracksys_api_url"), pid)
	pidType, err := getAPIResponse(pidURL)
	if err != nil {
		rw.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(rw, "Unable to connect with TrackSys to identify pid %s", pid)
		return
	}

	if pidType == "sirsi_metadata" {
		log.Printf("%s is a sirsi metadata record", pid)
		generateFromMetadataRecord(data, rw, unitID)
	} else if pidType == "xml_metadata" {
		log.Printf("%s is an xml metadata record", pid)
		generateFromXML(data, rw)
	} else if pidType == "apollo_metadata" {
		log.Printf("%s is an apollo metadata record", pid)
		generateFromApollo(data, rw)
	} else if pidType == "archivesspace_metadata" {
		// FIXME split logic
		log.Printf("%s is an as metadata record", pid)
		generateFromMetadataRecord(data, rw, unitID)
	} else if pidType == "component" {
		log.Printf("%s is a component", pid)
		generateFromComponent(pid, data, rw)
	} else {
		log.Printf("ERROR: Couldn't find %s", pid)
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(rw, "PID %s not found", pid)
	}
}

func getAPIResponse(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	respString := string(bodyBytes)
	if resp.StatusCode != 200 {
		return "", errors.New(respString)
	}
	return respString, nil
}

// Generate the IIIF manifest from data found in Apollo
func generateFromApollo(data models.IIIF, rw http.ResponseWriter) {
	// Get the Apollo PID
	PID := data.MetadataPID
	apolloURL := fmt.Sprintf("%s/legacy/lookup/%s", viper.GetString("apollo_api_url"), data.MetadataPID)
	respStr, err := getAPIResponse(apolloURL)
	if err == nil {
		PID = respStr
		log.Printf("Converted Tracksys PID %s to Apollo PID %s", data.MetadataPID, PID)
	}

	// Get some metadata about the collection from Apollo API...
	apolloURL = fmt.Sprintf("%s/items/%s", viper.GetString("apollo_api_url"), PID)
	respStr, err = getAPIResponse(apolloURL)
	if err != nil {
		log.Printf("Apollo Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(rw, "Unable communicate with Apollo: %s", err.Error())
		return
	}

	// Parse collection-level metadata from JSON response
	err = parsers.GetMetadataFromJSON(&data, respStr)
	if err != nil {
		log.Printf("Unable to parse Apollo response: %s", err.Error())
		rw.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprintf(rw, "Unable to parse Apollo Metadata: %s", err.Error())
		return
	}

	// Get masterFiles from TrackSys manifest API
	tsURL := fmt.Sprintf("%s/manifest/%s", viper.GetString("tracksys_api_url"), data.MetadataPID)
	respStr, err = getAPIResponse(tsURL)
	parsers.GetMasterFilesFromJSON(&data, "", respStr)

	renderIiifMetadata(data, rw)
}

// Generate the IIIF manifest from TrackSys XML Metadata
func generateFromXML(data models.IIIF, rw http.ResponseWriter) {
	tsURL := fmt.Sprintf("%s/metadata/%s?type=brief", viper.GetString("tracksys_api_url"), data.MetadataPID)
	respStr, err := getAPIResponse(tsURL)
	if err != nil {
		log.Printf("Tracksys metadata Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(rw, "Unable communicate with TrackSys: %s", err.Error())
		return
	}

	// unmarshall into struct
	var tsMetadata models.BriefMetadata
	json.Unmarshal([]byte(respStr), &tsMetadata)

	// Move ths data into the IIIF struct
	data.Title = tsMetadata.Title
	data.License = tsMetadata.Rights
	data.VirgoKey = data.MetadataPID
	if len(tsMetadata.CallNumber) > 0 {
		data.Metadata["Call Number"] = tsMetadata.CallNumber
	}
	if len(tsMetadata.CatalogKey) > 0 {
		data.VirgoKey = tsMetadata.CatalogKey
	}
	if len(tsMetadata.Creator) > 0 {
		data.Metadata["Author"] = tsMetadata.Creator
	}

	parsers.ParseSolrRecord(&data, "XmlMetadata")

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL = fmt.Sprintf("%s/manifest/%s", viper.GetString("tracksys_api_url"), data.MetadataPID)
	respStr, err = getAPIResponse(tsURL)
	parsers.GetMasterFilesFromJSON(&data, tsMetadata.Exemplar, respStr)
	renderIiifMetadata(data, rw)
}

// Generate the IIIF manifest for a METADATA record
func generateFromMetadataRecord(data models.IIIF, rw http.ResponseWriter, unitID int) {
	var exemplar sql.NullString
	var author sql.NullString
	var metadataType string
	var catalogKey sql.NullString
	var callNumber sql.NullString

	qs := `select m.title, creator_name, catalog_key, call_number, exemplar, type, u.uri from metadata m
          inner join use_rights u on u.id = use_right_id where pid=?`
	err := db.QueryRow(qs, data.MetadataPID).Scan(
		&data.Title, &author, &catalogKey, &callNumber, &exemplar, &metadataType, &data.License)
	if err != nil {
		log.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}

	if callNumber.Valid {
		data.Metadata["Call Number"] = callNumber.String
	}
	data.VirgoKey = data.MetadataPID
	if catalogKey.Valid {
		data.VirgoKey = catalogKey.String
	}

	// only take the author field from the DB for SirsiMetadata. For
	// XmlMetadata, the field needs to be pulled from the author_display of solr
	if author.Valid && strings.Compare(metadataType, "SirsiMetadata") == 0 {
		data.Metadata["Author"] = author.String
	}

	if strings.Compare(metadataType, "ExternalMetadata") != 0 {
		parsers.ParseSolrRecord(&data, metadataType)
	}

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/manifest/%s", viper.GetString("tracksys_api_url"), data.MetadataPID)
	if unitID > 0 {
		tsURL = fmt.Sprintf("%s?unit=%d", tsURL, unitID)
	}
	respStr, err := getAPIResponse(tsURL)
	parsers.GetMasterFilesFromJSON(&data, exemplar.String, respStr)
	renderIiifMetadata(data, rw)
}

func generateFromComponent(pid string, data models.IIIF, rw http.ResponseWriter) {
	// grab all of the masterfiles hooked to this component
	var exemplar sql.NullString
	var cTitle sql.NullString
	qs := `select title, exemplar from components where pid=?`
	err := db.QueryRow(qs, pid).Scan(&cTitle, &exemplar)
	if err != nil {
		log.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}
	data.Title = models.CleanString(cTitle.String)

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL := fmt.Sprintf("%s/manifest/%s", viper.GetString("tracksys_api_url"), pid)
	respStr, err := getAPIResponse(tsURL)
	parsers.GetMasterFilesFromJSON(&data, exemplar.String, respStr)
	renderIiifMetadata(data, rw)
}

func renderIiifMetadata(data models.IIIF, rw http.ResponseWriter) {
	rw.Header().Set("content-type", "application/json; charset=utf-8")
	tmpl := template.Must(template.ParseFiles("templates/iiif.json"))
	err := tmpl.ExecuteTemplate(rw, "iiif.json", data)
	if err != nil {
		log.Printf("Unable to render IIIF metadata for %s: %s", data.MetadataPID, err.Error())
		fmt.Fprintf(rw, "Unable to render IIIF metadata: %s", err.Error())
		return
	}
	log.Printf("IIIF Metadata generated for %s", data.MetadataPID)
}

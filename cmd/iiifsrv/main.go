package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
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

	// handle different types of PID
	pidType := determinePidType(pid)
	if pidType == "metadata" {
		log.Printf("%s is a metadata record", pid)
		generateFromMetadataRecord(data, rw, unitID)
	} else if pidType == "component" {
		log.Printf("%s is a component", pid)
		generateFromComponent(pid, data, rw)
	} else {
		// See if it is in Apollo...
		apolloURL := fmt.Sprintf("%s/legacy/lookup/%s", viper.GetString("apollo_api_url"), pid)
		apolloPID, err := getAPIResponse(apolloURL)
		if err == nil {
			log.Printf("%s was found in Apollo as %s", pid, apolloPID)
			generateFromApollo(pid, apolloPID, data, rw)
			return
		}

		log.Printf("ERROR: Couldn't find %s", pid)
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
func generateFromApollo(tsPID string, apolloPID string, data models.IIIF, rw http.ResponseWriter) {
	// Get some metadata about the collection from Apollo API...
	apolloURL := fmt.Sprintf("%s/items/%s", viper.GetString("apollo_api_url"), apolloPID)
	respStr, err := getAPIResponse(apolloURL)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusNotFound)
		return
	}

	// Parse collection-level metadata from JSON response
	err = parsers.GetMetadataFromJSON(&data, respStr)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	// Get metadata about masterfiles from the tracksys Manufest API
	tsURL := fmt.Sprintf("%s/items/%s", viper.GetString("tracksys_api_url"), tsPID)
	mfRespStr, err := getAPIResponse(tsURL)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusNotFound)
		return
	}

	err = parsers.GetMasterFilesFromJSON(&data, mfRespStr)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	fmt.Fprintf(rw, "hi")
}

/**
 * Generate the IIIF manifest for a METADATA record
 */
func generateFromMetadataRecord(data models.IIIF, rw http.ResponseWriter, unitID int) {
	var metadataID int
	var exemplar sql.NullString
	var descMetadata sql.NullString
	var author sql.NullString
	var metadataType string
	var catalogKey sql.NullString
	var callNumber sql.NullString

	qs := `select m.id, m.title, creator_name, catalog_key, call_number, exemplar, type, desc_metadata, u.uri from metadata m
          inner join use_rights u on u.id = use_right_id where pid=?`
	err := db.QueryRow(qs, data.MetadataPID).Scan(
		&metadataID, &data.Title, &author, &catalogKey, &callNumber, &exemplar, &metadataType, &descMetadata, &data.License)
	if err != nil {
		log.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}

	if callNumber.Valid {
		data.Metadata = append(data.Metadata, models.Metadata{"Call Number", callNumber.String})
	}
	data.VirgoKey = data.MetadataPID
	if catalogKey.Valid {
		data.VirgoKey = catalogKey.String
	}

	// only take the author field from the DB for SirsiMetadata. For
	// XmlMetadata, the field needs to be pulled from the author_display of solr
	if author.Valid && strings.Compare(metadataType, "SirsiMetadata") == 0 {
		data.Metadata = append(data.Metadata, models.Metadata{"Author", author.String})
	}

	if strings.Compare(metadataType, "ExternalMetadata") != 0 {
		parsers.ParseSolrRecord(&data, metadataType)
	}

	// Get data for all master files from units associated with the metadata record
	// The default query only gets master files for units that are in the DL. This
	// can be overridden if a unit ID was specified or of the metadata is external. In these
	// cases, don't care if unit is in DL or not
	var qsBuff bytes.Buffer
	qsBuff.WriteString("select m.pid, m.filename, m.title, m.description, t.width, t.height from master_files m")
	qsBuff.WriteString(" inner join units u on u.id = m.unit_id")
	qsBuff.WriteString(" inner join image_tech_meta t on m.id=t.master_file_id where m.metadata_id = ?")
	if unitID > 0 {
		log.Printf("Only including masterfiles from unit %d", unitID)
		qsBuff.WriteString(fmt.Sprintf(" and u.id=%d order by m.filename asc", unitID))
	} else if strings.Compare(metadataType, "ExternalMetadata") == 0 {
		log.Printf("This is External metadata; including all master files")
		qsBuff.WriteString(" order by m.filename asc")
	} else {
		log.Printf("Only including masterfiles from units in the DL")
		qsBuff.WriteString("  and u.include_in_dl = 1 order by m.filename asc")
	}
	rows, _ := db.Query(qsBuff.String(), metadataID)
	defer rows.Close()
	pgNum := 0
	for rows.Next() {
		var mf models.MasterFile
		var mfFilename string
		var mfTitle sql.NullString
		var mfDesc sql.NullString
		err = rows.Scan(&mf.PID, &mfFilename, &mfTitle, &mfDesc, &mf.Width, &mf.Height)
		if err != nil {
			log.Printf("Unable to retreive IIIF MasterFile metadata for %s: %s", data.MetadataPID, err.Error())
			fmt.Fprintf(rw, "Unable to retreive IIIF MasterFile metadata: %s", err.Error())
			return
		}
		mf.Description = models.CleanString(mfDesc.String)
		mf.Title = models.CleanString(mfTitle.String)

		// If the metadata for this master file is XML, the MODS desc metadata in record overrides title and desc
		if descMetadata.Valid && strings.Compare("XmlMetadata", metadataType) == 0 {
			parsers.ParseMODS(&mf, descMetadata.String)
		}
		data.MasterFiles = append(data.MasterFiles, mf)

		// if exemplar is set, see if it matches the current master file filename
		// if it does, set the current page num as the start canvas
		if exemplar.Valid && strings.Compare(mfFilename, exemplar.String) == 0 {
			data.StartPage = pgNum
			data.ExemplarPID = mf.PID
			log.Printf("Exemplar set to filename %s, page %d", mfFilename, data.StartPage)
		}
		pgNum++
	}
	renderIiifMetadata(data, rw)
}

func generateFromComponent(pid string, data models.IIIF, rw http.ResponseWriter) {
	// grab all of the masterfiles hooked to this component
	var componentID int
	var exemplar sql.NullString
	var cTitle sql.NullString
	qs := `select id, title, exemplar from components where pid=?`
	err := db.QueryRow(qs, pid).Scan(&componentID, &cTitle, &exemplar)
	if err != nil {
		log.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}
	data.Title = models.CleanString(cTitle.String)

	// Get data for all master files attached to this component
	pgNum := 0
	qs = `select m.pid, m.filename, m.title, m.description, b.desc_metadata, t.width, t.height from master_files m
         inner join metadata b on b.id = m.metadata_id
	      inner join image_tech_meta t on m.id=t.master_file_id where m.component_id = ? order by m.filename asc`
	rows, _ := db.Query(qs, componentID)
	defer rows.Close()
	for rows.Next() {
		var mf models.MasterFile
		var mfFilename string
		var mfTitle sql.NullString
		var mfDesc sql.NullString
		var mfDescMetadata sql.NullString
		err = rows.Scan(&mf.PID, &mfFilename, &mfTitle, &mfDesc, &mfDescMetadata, &mf.Width, &mf.Height)
		if err != nil {
			log.Printf("Unable to retreive IIIF MasterFile metadata for %s: %s", data.MetadataPID, err.Error())
			fmt.Fprintf(rw, "Unable to retreive IIIF MasterFile metadata: %s", err.Error())
			return
		}
		mf.Description = models.CleanString(mfDesc.String)
		mf.Title = models.CleanString(mfTitle.String)

		// MODS desc metadata in record overrides title and desc
		if mfDescMetadata.Valid {
			parsers.ParseMODS(&mf, mfDescMetadata.String)
		}

		data.MasterFiles = append(data.MasterFiles, mf)

		// if exemplar is set, see if it matches the current master file filename
		// if it does, set the current page num as the start canvas
		if exemplar.Valid && strings.Compare(mfFilename, exemplar.String) == 0 {
			data.StartPage = pgNum
			data.ExemplarPID = mf.PID
			log.Printf("Exemplar set to filename %s, page %d", mfFilename, data.StartPage)
		}
		pgNum++

	}
	renderIiifMetadata(data, rw)
}

func renderIiifMetadata(data models.IIIF, rw http.ResponseWriter) {
	// rw.Header().Set("content-type", "application/json; charset=utf-8")
	tmpl := template.Must(template.ParseFiles("templates/iiif.json"))
	err := tmpl.ExecuteTemplate(rw, "iiif.json", data)
	if err != nil {
		log.Printf("Unable to render IIIF metadata for %s: %s", data.MetadataPID, err.Error())
		fmt.Fprintf(rw, "Unable to render IIIF metadata: %s", err.Error())
		return
	}
	log.Printf("IIIF Metadata generated for %s", data.MetadataPID)
}

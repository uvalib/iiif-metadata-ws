package main

import (
	"bytes"
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

const version = "1.6.0"

// globals to share between main and the HTTP handler
var db *sql.DB
var logger *log.Logger

// Types used to generate the JSON response; masterFile and iiifData
type masterFile struct {
	PID         string
	Title       string
	Description string
	Width       int
	Height      int
}

type metadata struct {
	Name  string
	Value string
}

type iiifData struct {
	VirgoURL    string
	IiifURL     string
	URL         string
	VirgoKey    string
	MetadataPID string
	Title       string
	StartPage   int
	ExemplarPID string
	License     string
	Related     string
	Metadata    []metadata
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
	data.VirgoURL = viper.GetString("virgo_url")

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
	var author sql.NullString
	var metadataType string
	var catalogKey sql.NullString
	var callNumber sql.NullString

	qs := `select m.id, m.title, creator_name, catalog_key, call_number, exemplar, type, desc_metadata, u.uri from metadata m
          inner join use_rights u on u.id = use_right_id where pid=?`
	err := db.QueryRow(qs, data.MetadataPID).Scan(
		&metadataID, &data.Title, &author, &catalogKey, &callNumber, &exemplar, &metadataType, &descMetadata, &data.License)
	if err != nil {
		logger.Printf("Request failed: %s", err.Error())
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unable to retreive IIIF metadata: %s", err.Error())
		return
	}

	if author.Valid {
		data.Metadata = append(data.Metadata, metadata{"Author", author.String})
	}
	if callNumber.Valid {
		data.Metadata = append(data.Metadata, metadata{"Call Number", callNumber.String})
	}

	if catalogKey.Valid {
		data.VirgoKey = catalogKey.String
	} else {
		data.VirgoKey = data.MetadataPID
	}
	parseSolrRecord(&data)

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
			data.StartPage = pgNum
			data.ExemplarPID = mf.PID
			logger.Printf("Exemplar set to filename %s, page %d", mfFilename, data.StartPage)
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
			data.StartPage = pgNum
			data.ExemplarPID = mf.PID
			logger.Printf("Exemplar set to filename %s, page %d", mfFilename, data.StartPage)
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
 * Parse XML solr index for format_facet and published_display (sirsi) or year_display (xml)
 */
func parseSolrRecord(data *iiifData) {
	// request index record from solr...
	url := fmt.Sprintf("%s/select?q=id:\"%s\"", viper.GetString("virgo_solr_url"), data.VirgoKey)
	resp, err := http.Get(url)
	if err != nil {
		logger.Printf("Unable to get Solr index: %s", err.Error())
		return
	}
	defer resp.Body.Close()

	// parse the XML response into a document
	doc, err := libxml2.ParseReader(resp.Body)
	if err != nil {
		logger.Printf("Unable to parse Solr index: %s", err.Error())
		return
	}
	defer doc.Free()

	root, err := doc.DocumentElement()
	if err != nil {
		logger.Printf("Failed to fetch Solr document element: %s", err.Error())
		return
	}

	ctx, err := xpath.NewContext(root)
	if err != nil {
		logger.Printf("Failed to create Solr xpath context: %s", err.Error())
		return
	}
	defer ctx.Free()

	// Query for the data; format_facet. This has a bunch of <str> children that
	// need to be combined to make the final format string. Skip 'Online'
	nodes := xpath.NodeList(ctx.Find("//arr[@name='format_facet']/str"))
	var buffer bytes.Buffer
	for i := 0; i < len(nodes); i++ {
		val := nodes[i].NodeValue()
		if strings.Compare("Online", val) != 0 {
			if buffer.Len() > 0 {
				buffer.WriteString("; ")
			}
			buffer.WriteString(val)
		}
	}
	if buffer.Len() > 0 {
		data.Metadata = append(data.Metadata, metadata{"Format", buffer.String()})
	}

	// See if there is MARC data to parse for physical description
	marc := xpath.String(ctx.Find("//str[@name='marc_display']"))
	if len(marc) > 0 {
		parseMarc(data, marc)
	}

	// Try published_date_display (for sirsi records)
	date := xpath.String(ctx.Find("//arr[@name='published_date_display']/str"))
	if len(date) > 0 {
		data.Metadata = append(data.Metadata, metadata{"Date", date})
		return
	}

	// .. not found, try year_display (for XML records)
	date = xpath.String(ctx.Find("arr[@name='year_display']/str"))
	if len(date) > 0 {
		data.Metadata = append(data.Metadata, metadata{"Date", date})
	}
}

func parseMarc(data *iiifData, marc string) {
	doc, _ := libxml2.ParseString(marc)
	root, _ := doc.DocumentElement()
	ctx, _ := xpath.NewContext(root)
	defer doc.Free()
	defer ctx.Free()
	ctx.RegisterNS("ns", "http://www.loc.gov/MARC21/slim")

	nodes := xpath.NodeList(ctx.Find("//ns:datafield[@tag='300']/ns:subfield"))
	var buffer bytes.Buffer
	for i := 0; i < len(nodes); i++ {
		val := nodes[i].NodeValue()
		if buffer.Len() > 0 {
			buffer.WriteString(" ")
		}
		buffer.WriteString(val)
	}
	if buffer.Len() > 0 {
		data.Metadata = append(data.Metadata, metadata{"Physical Description", buffer.String()})
	}
}

/**
 * Parse title and description from MODS string
 */
func parseMods(mfData *masterFile, mods string) {
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
		mfData.Title = title
	}

	// first try <abstract displayLabel="Description">
	desc := xpath.String(ctx.Find("//ns:abstract[@displayLabel='Description']/text()"))
	if len(desc) > 0 {
		mfData.Description = desc
		return
	}

	// .. next try for a provenance note
	desc = xpath.String(ctx.Find("//ns:note[@type='provenance' and @displayLabel='staff']/text()"))
	if len(desc) > 0 {
		mfData.Description = fmt.Sprintf("Staff note: %s", desc)
	}
}

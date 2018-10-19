package main

import (
	// "encoding/json"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/uvalib/iiif-metadata-ws/internal/models"
	"github.com/uvalib/iiif-metadata-ws/internal/parsers"
)

// version of the service
const version = "2.3.0"

// configuratition data
type serviceConfig struct {
	port        int
	iiifURL     string
	virgoURL    string
	solrURL     string
	tracksysURL string
	apolloURL   string
}

var config = serviceConfig{}

/**
 * Main entry point for the web service
 */
func main() {
	log.Printf("===> iiif-metadata-ws staring up <===")

	flag.IntVar(&config.port, "port", 8080, "Port to offer service on (default 8080)")
	flag.StringVar(&config.iiifURL, "iiif", "https://iiif.lib.virginia.edu/iiif", "IIIF URL")
	flag.StringVar(&config.virgoURL, "virgo", "http://search.lib.virginia.edu/catalog", "Virgo URL")
	flag.StringVar(&config.tracksysURL, "tracksys", "http://tracksys.lib.virginia.edu/api", "Tracksys URL")
	flag.StringVar(&config.apolloURL, "apollo", "http://apollo.lib.virginia.edu/api", "Apollo URL")
	flag.StringVar(&config.solrURL, "solr", "http://solr.lib.virginia.edu:8082/solr/core", "Virgo Solr URL")
	flag.Parse()

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.Use(cors.Default())

	// Set routes and start server
	router.GET("/", rootHandler)
	router.GET("/:pid/manifest.json", iiifHandler)
	router.GET("/:pid", iiifHandler)

	portStr := fmt.Sprintf(":%d", config.port)
	log.Printf("Start HTTP service on port %s with CORS support enabled", portStr)
	log.Fatal(router.Run(portStr))
}

// rootHandler returns the version of the service
func rootHandler(c *gin.Context) {
	c.String(http.StatusOK, "IIIF metadata service version %s", version)
}

// iiifHandler processes a request for IIIF presentation metadata
func iiifHandler(c *gin.Context) {
	pid := c.Param("pid")
	if strings.Compare(pid, "favicon.ico") == 0 {
		return
	}

	// initialize IIIF data struct
	var data models.IIIF
	data.URL = fmt.Sprintf("http://%s%s", c.Request.Host, c.Request.URL)
	data.IiifURL = config.iiifURL
	data.VirgoURL = config.virgoURL
	data.MetadataPID = pid
	data.Metadata = make(map[string]string)

	// Tracksys is the system that tracks items that contain
	// masterfiles. All pids the arrive at the IIIF service should
	// refer to these items. Determine what type the PID is:
	pidURL := fmt.Sprintf("%s/pid/%s/type", config.tracksysURL, pid)
	pidType, err := getAPIResponse(pidURL)
	if err != nil {
		c.String(http.StatusServiceUnavailable, "Unable to connect with TrackSys to identify pid %s", pid)
		return
	}

	if pidType == "sirsi_metadata" {
		log.Printf("%s is a sirsi metadata record", pid)
		unitID, _ := strconv.Atoi(c.Query("unit"))
		generateFromSirsi(data, c, unitID)
	} else if pidType == "xml_metadata" {
		log.Printf("%s is an xml metadata record", pid)
		generateFromXML(data, c)
	} else if pidType == "apollo_metadata" {
		log.Printf("%s is an apollo metadata record", pid)
		generateFromApollo(data, c)
	} else if pidType == "archivesspace_metadata" {
		log.Printf("%s is an as metadata record", pid)
		generateFromExternal(data, c)
	} else if pidType == "component" {
		log.Printf("%s is a component", pid)
		generateFromComponent(pid, data, c)
	} else {
		log.Printf("ERROR: Couldn't find %s", pid)
		c.String(http.StatusNotFound, "PID %s not found", pid)
	}
}

func getAPIResponse(url string) (string, error) {
	timeout := time.Duration(5 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}
	resp, err := client.Get(url)
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

// generateFromApollo will Generate the IIIF manifest from data found in Apollo
func generateFromApollo(data models.IIIF, c *gin.Context) {
	// Get some metadata about the collection from Apollo API...
	PID := data.MetadataPID
	apolloURL := fmt.Sprintf("%s/items/%s", config.apolloURL, PID)
	respStr, err := getAPIResponse(apolloURL)
	if err != nil {
		log.Printf("Apollo Request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable communicate with Apollo: %s", err.Error())
		return
	}

	// Parse collection-level metadata from JSON response
	err = parsers.GetMetadataFromJSON(&data, respStr)
	if err != nil {
		log.Printf("Unable to parse Apollo response: %s", err.Error())
		c.String(http.StatusUnprocessableEntity, "Unable to parse Apollo Metadata: %s", err.Error())
		return
	}

	// Get masterFiles from TrackSys manifest API
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err = getAPIResponse(tsURL)
	parsers.GetMasterFilesFromJSON(&data, respStr)

	renderIiifMetadata(data, c)
}

func getTrackSysMetadata(data *models.IIIF) error {
	tsURL := fmt.Sprintf("%s/metadata/%s?type=brief", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL)
	if err != nil {
		return err
	}

	// unmarshall into struct
	var tsMetadata models.BriefMetadata
	json.Unmarshal([]byte(respStr), &tsMetadata)

	// Move ths data into the IIIF struct
	data.Title = models.CleanString(tsMetadata.Title)
	data.License = tsMetadata.Rights
	data.VirgoKey = data.MetadataPID
	if len(tsMetadata.CallNumber) > 0 {
		data.Metadata["Call Number"] = tsMetadata.CallNumber
	}
	if len(tsMetadata.CatalogKey) > 0 {
		data.VirgoKey = tsMetadata.CatalogKey
	}
	if len(tsMetadata.Creator) > 0 {
		data.Metadata["Author"] = models.CleanString(tsMetadata.Creator)
	}
	return nil
}

// generateFromXML wil generate the IIIF manifest from TrackSys XML Metadata
func generateFromXML(data models.IIIF, c *gin.Context) {
	err := getTrackSysMetadata(&data)
	if err != nil {
		log.Printf("Tracksys metadata Request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	parsers.ParseTracksysSolr(config.tracksysURL, &data)

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL)
	parsers.GetMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

// generateFromSirsi will generate the IIIF manifest for a SIRSI METADATA record
func generateFromSirsi(data models.IIIF, c *gin.Context, unitID int) {
	err := getTrackSysMetadata(&data)
	if err != nil {
		log.Printf("Tracksys metadata Request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	parsers.ParseVirgoSolr(config.solrURL, &data)

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	if unitID > 0 {
		tsURL = fmt.Sprintf("%s?unit=%d", tsURL, unitID)
	}
	respStr, err := getAPIResponse(tsURL)
	parsers.GetMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

// generateFromExternal will generate the IIIF manifest for an external record
func generateFromExternal(data models.IIIF, c *gin.Context) {
	err := getTrackSysMetadata(&data)
	if err != nil {
		log.Printf("Tracksys metadata Request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL)
	parsers.GetMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

func generateFromComponent(pid string, data models.IIIF, c *gin.Context) {
	err := getTrackSysMetadata(&data)
	if err != nil {
		log.Printf("Tracksys metadata Request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, pid)
	respStr, err := getAPIResponse(tsURL)
	parsers.GetMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

func renderIiifMetadata(data models.IIIF, c *gin.Context) {
	tmpl := template.Must(template.ParseFiles("templates/iiif.json"))
	var outBuffer bytes.Buffer
	err := tmpl.Execute(&outBuffer, data)
	if err != nil {
		log.Printf("Unable to render IIIF metadata for %s: %s", data.MetadataPID, err.Error())
		c.String(http.StatusInternalServerError, "Unable to render IIIF metadata: %s", err.Error())
		return
	}
	log.Printf("IIIF Metadata generated for %s", data.MetadataPID)
	c.Header("content-type", "application/json; charset=utf-8")
	c.String(http.StatusOK, outBuffer.String())
}

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
	"text/template"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// version of the service
const version = "3.3.1"

// configuratition data
type serviceConfig struct {
	port        int
	hostName    string
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
	flag.StringVar(&config.tracksysURL, "tracksys", "http://tracksys.lib.virginia.edu/api", "Tracksys URL")
	flag.StringVar(&config.apolloURL, "apollo", "http://apollo.lib.virginia.edu/api", "Apollo URL")
	flag.StringVar(&config.solrURL, "solr", "http://solr.lib.virginia.edu:8082/solr/core", "Virgo Solr URL")
	flag.StringVar(&config.hostName, "host", "iiifman.lib.virginia.edu", "Hostname for this service")
	flag.Parse()

	gin.SetMode(gin.ReleaseMode)
	gin.DisableConsoleColor()
	router := gin.Default()
	router.Use(cors.Default())

	// Set routes and start server
	router.GET("/favicon.ico", favHandler)
	router.GET("/version", versionHandler)
	router.GET("/healthcheck", healthCheckHandler)
	router.GET("/config", configHandler)
	router.GET("/pid/:pid", iiifHandler)
	router.GET("/pid/:pid/manifest.json", iiifHandler)
	router.GET("/pid/:pid/exist", existHandler)
	api := router.Group("/api")
	{
		api.GET("/aries", ariesPingHandler)
		api.GET("/aries/:id", ariesLookupHandler)
	}

	portStr := fmt.Sprintf(":%d", config.port)
	log.Printf("Start HTTP service on port %s with CORS support enabled", portStr)
	log.Fatal(router.Run(portStr))
}

// favHandler is a dummy handler to silence browser API requests that look for /favicon.ico
func favHandler(c *gin.Context) {
}

// configHandler dumps the current service config as json
func configHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service_host": config.hostName,
		"apollo":       config.apolloURL,
		"tracksys":     config.tracksysURL,
		"solr":         config.solrURL,
	})
}

// versionHandler returns the version of the service
func versionHandler(c *gin.Context) {
	c.String(http.StatusOK, "IIIF metadata service version %s", version)
}

func healthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"alive": "true"})
}

// existHandler checks if there is IIIF data available for a PID
func existHandler(c *gin.Context) {
	pid := c.Param("pid")
	pidURL := fmt.Sprintf("%s/pid/%s/type", config.tracksysURL, pid)
	_, err := getAPIResponse(pidURL)
	if err != nil {
		c.String(http.StatusNotFound, "IIIF Metadata does not exist for %s", pid)
		return
	}
	c.String(http.StatusOK, "IIIF Metadata exists for %s", pid)
}

// iiifHandler processes a request for IIIF presentation metadata
func iiifHandler(c *gin.Context) {
	pid := c.Param("pid")

	// initialize IIIF data struct
	var data IIIF
	data.URL = fmt.Sprintf("https://%s/pid/%s", config.hostName, pid)
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
	} else if pidType == "archivesspace_metadata" || pidType == "jstor_metadata" {
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
func generateFromApollo(data IIIF, c *gin.Context) {
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
	err = getMetadataFromJSON(&data, respStr)
	if err != nil {
		log.Printf("Unable to parse Apollo response: %s", err.Error())
		c.String(http.StatusUnprocessableEntity, "Unable to parse Apollo Metadata: %s", err.Error())
		return
	}

	// Get masterFiles from TrackSys manifest API
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err = getAPIResponse(tsURL)
	getMasterFilesFromJSON(&data, respStr)

	renderIiifMetadata(data, c)
}

func getTrackSysMetadata(data *IIIF) error {
	tsURL := fmt.Sprintf("%s/metadata/%s?type=brief", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL)
	if err != nil {
		return err
	}

	// unmarshall into struct
	var tsMetadata BriefMetadata
	json.Unmarshal([]byte(respStr), &tsMetadata)

	// Move ths data into the IIIF struct
	data.Title = cleanString(tsMetadata.Title)
	data.License = tsMetadata.Rights
	data.VirgoKey = data.MetadataPID
	if len(tsMetadata.CallNumber) > 0 {
		data.Metadata["Call Number"] = tsMetadata.CallNumber
	}
	if len(tsMetadata.CatalogKey) > 0 {
		data.VirgoKey = tsMetadata.CatalogKey
	}
	if len(tsMetadata.Creator) > 0 {
		data.Metadata["Author"] = cleanString(tsMetadata.Creator)
	}
	return nil
}

// generateFromXML wil generate the IIIF manifest from TrackSys XML Metadata
func generateFromXML(data IIIF, c *gin.Context) {
	err := getTrackSysMetadata(&data)
	if err != nil {
		log.Printf("Tracksys metadata Request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	parseTracksysSolr(config.tracksysURL, &data)

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL)
	getMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

// generateFromSirsi will generate the IIIF manifest for a SIRSI METADATA record
func generateFromSirsi(data IIIF, c *gin.Context, unitID int) {
	err := getTrackSysMetadata(&data)
	if err != nil {
		log.Printf("Tracksys metadata Request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	parseVirgoSolr(config.solrURL, &data)

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	if unitID > 0 {
		tsURL = fmt.Sprintf("%s?unit=%d", tsURL, unitID)
	}
	respStr, err := getAPIResponse(tsURL)
	getMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

// generateFromExternal will generate the IIIF manifest for an external record
func generateFromExternal(data IIIF, c *gin.Context) {
	err := getTrackSysMetadata(&data)
	if err != nil {
		log.Printf("Tracksys metadata Request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL)
	getMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

func generateFromComponent(pid string, data IIIF, c *gin.Context) {
	err := getTrackSysMetadata(&data)
	if err != nil {
		log.Printf("Tracksys metadata Request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, pid)
	respStr, err := getAPIResponse(tsURL)
	getMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

func renderIiifMetadata(data IIIF, c *gin.Context) {
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

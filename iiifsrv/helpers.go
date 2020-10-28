package main

import (
	// "encoding/json"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
)

var readTimeout = 45 // some of those Solr queries are realllllllyyyyy sloooowwwww
var connTimeout = 5

var httpClient = &http.Client{
	Timeout: time.Duration(readTimeout) * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(connTimeout) * time.Second,
			KeepAlive: 60 * time.Second,
		}).DialContext,
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 50,
		IdleConnTimeout:     90 * time.Second,
	},
}

func getAPIResponse(url string) (string, error) {

	resp, err := httpClient.Get(url)
	if err != nil {
		log.Printf("ERROR: issuing request: %s, %s", url, err.Error())
		return "", err
	}
	defer resp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ERROR: reading response: %s, %s", url, err.Error())
		return "", err
	}
	respString := string(bodyBytes)
	if resp.StatusCode != 200 {
		log.Printf("ERROR: bad response: status code %d, endpoint %s", resp.StatusCode, url)
		return "", errors.New(respString)
	}
	return respString, nil
}

// generateFromApollo will Generate the IIIF manifest from data found in Apollo
func generateFromApollo(config *serviceConfig, data IIIF, c *gin.Context) {
	// Get some metadata about the collection from Apollo API...
	PID := data.MetadataPID
	apolloURL := fmt.Sprintf("%s/items/%s", config.apolloURL, PID)
	respStr, err := getAPIResponse(apolloURL)
	if err != nil {
		log.Printf("ERROR: apollo request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable communicate with Apollo: %s", err.Error())
		return
	}

	// Parse collection-level metadata from JSON response
	err = getMetadataFromJSON(&data, respStr)
	if err != nil {
		log.Printf("ERROR: unable to parse apollo response: %s", err.Error())
		c.String(http.StatusUnprocessableEntity, "Unable to parse Apollo Metadata: %s", err.Error())
		return
	}

	// Get masterFiles from TrackSys manifest API
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err = getAPIResponse(tsURL)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return
	}

	getMasterFilesFromJSON(&data, respStr)

	renderIiifMetadata(data, c)
}

func getTrackSysMetadata(config *serviceConfig, data *IIIF) error {
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
func generateFromXML(config *serviceConfig, data IIIF, c *gin.Context) {
	err := getTrackSysMetadata(config, &data)
	if err != nil {
		log.Printf("ERROR: tracksys metadata request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	err = parseTracksysSolr(config.tracksysURL, &data)
	if err != nil {
		log.Printf("ERROR: solr data request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve solr data: %s", err.Error())
		return
	}

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return
	}

	getMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

// generateFromSirsi will generate the IIIF manifest for a SIRSI METADATA record
func generateFromSirsi(config *serviceConfig, data IIIF, c *gin.Context, unitID int) {
	err := getTrackSysMetadata(config, &data)
	if err != nil {
		log.Printf("ERROR: tracksys metadata request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	err = parseVirgoSolr(config.solrURL, &data)
	if err != nil {
		log.Printf("ERROR: solr data request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve solr data: %s", err.Error())
		return
	}

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	if unitID > 0 {
		tsURL = fmt.Sprintf("%s?unit=%d", tsURL, unitID)
	}
	respStr, err := getAPIResponse(tsURL)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return
	}

	getMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

// generateFromExternal will generate the IIIF manifest for an external record
func generateFromExternal(config *serviceConfig, data IIIF, c *gin.Context) {
	err := getTrackSysMetadata(config, &data)
	if err != nil {
		log.Printf("ERROR: tracksys metadata request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return
	}

	getMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

func generateFromComponent(config *serviceConfig, pid string, data IIIF, c *gin.Context) {
	err := getTrackSysMetadata(config, &data)
	if err != nil {
		log.Printf("ERROR: tracksys metadata request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return
	}

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL := fmt.Sprintf("%s/manifest/%s", config.tracksysURL, pid)
	respStr, err := getAPIResponse(tsURL)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return
	}

	getMasterFilesFromJSON(&data, respStr)
	renderIiifMetadata(data, c)
}

func renderIiifMetadata(data IIIF, c *gin.Context) {
	tmpl := template.Must(template.ParseFiles("templates/iiif.json"))
	var outBuffer bytes.Buffer
	err := tmpl.Execute(&outBuffer, data)
	if err != nil {
		log.Printf("ERROR: unable to render IIIF metadata for %s: %s", data.MetadataPID, err.Error())
		c.String(http.StatusInternalServerError, "Unable to render IIIF metadata: %s", err.Error())
		return
	}
	log.Printf("INFO: IIIF Metadata generated for %s", data.MetadataPID)
	c.Header("content-type", "application/json; charset=utf-8")
	c.String(http.StatusOK, outBuffer.String())
}

//
// end of file
//

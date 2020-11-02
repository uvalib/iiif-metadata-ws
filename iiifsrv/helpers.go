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
	"strings"
	"text/template"
	"time"
)

var readTimeout = 45 // some of those Solr queries are realllllllyyyyy sloooowwwww
var connTimeout = 5

//
// define our connection clients
//

// standard client used for normal requests
var standardHttpClient = &http.Client{
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

// used for healthcheck connections
var fastHttpClient = &http.Client{
	Timeout: time.Duration(5) * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(5) * time.Second,
			KeepAlive: 60 * time.Second,
		}).DialContext,
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 50,
		IdleConnTimeout:     90 * time.Second,
	},
}

// generateFromApollo will Generate the IIIF manifest from data found in Apollo
func generateFromApollo(config *serviceConfig, data IIIF) (string, int, string) {
	// Get some metadata about the collection from Apollo API...
	PID := data.MetadataPID
	apolloURL := fmt.Sprintf("%s/api/items/%s", config.apolloURL, PID)
	respStr, err := getAPIResponse(apolloURL, standardHttpClient)
	if err != nil {
		log.Printf("ERROR: apollo request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable communicate with Apollo: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable communicate with Apollo: %s", err.Error())
	}

	// Parse collection-level metadata from JSON response
	err = getMetadataFromJSON(&data, respStr)
	if err != nil {
		log.Printf("ERROR: unable to parse apollo response: %s", err.Error())
		//c.String(http.StatusUnprocessableEntity, "Unable to parse Apollo Metadata: %s", err.Error())
		return "", http.StatusUnprocessableEntity, fmt.Sprintf("Unable to parse Apollo Metadata: %s", err.Error())
	}

	// Get masterFiles from TrackSys manifest API
	tsURL := fmt.Sprintf("%s/api/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err = getAPIResponse(tsURL, standardHttpClient)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve manifest: %s", err.Error())
	}

	getMasterFilesFromJSON(&data, respStr)
	var metadata string
	metadata, err = renderIiifMetadata(data)
	if err != nil {
		//c.String(http.StatusInternalServerError, "Unable to render IIIF metadata: %s", err.Error())
		return "", http.StatusInternalServerError, fmt.Sprintf("Unable to render IIIF metadata: %s", err.Error())
	}

	// happy day
	return metadata, http.StatusOK, ""
}

// generateFromXML wil generate the IIIF manifest from TrackSys XML Metadata
func generateFromXML(config *serviceConfig, data IIIF) (string, int, string) {
	err := getTrackSysMetadata(config, &data)
	if err != nil {
		log.Printf("ERROR: tracksys metadata request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve metadata: %s", err.Error())
	}

	err = parseTracksysSolr(config.tracksysURL, &data)
	if err != nil {
		log.Printf("ERROR: solr data request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve solr data: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve solr data: %s", err.Error())
	}

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL := fmt.Sprintf("%s/api/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL, standardHttpClient)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve manifest: %s", err.Error())
	}

	getMasterFilesFromJSON(&data, respStr)
	var metadata string
	metadata, err = renderIiifMetadata(data)
	if err != nil {
		//c.String(http.StatusInternalServerError, "Unable to render IIIF metadata: %s", err.Error())
		return "", http.StatusInternalServerError, fmt.Sprintf("Unable to render IIIF metadata: %s", err.Error())
	}

	// happy day
	return metadata, http.StatusOK, ""
}

// generateFromSirsi will generate the IIIF manifest for a SIRSI METADATA record
func generateFromSirsi(config *serviceConfig, data IIIF, unitID int) (string, int, string) {
	err := getTrackSysMetadata(config, &data)
	if err != nil {
		log.Printf("ERROR: tracksys metadata request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve metadata: %s", err.Error())
	}

//	err = parseVirgoSolr(config.solrURL, &data)
//	if err != nil {
//		log.Printf("ERROR: solr data request failed: %s", err.Error())
//		//c.String(http.StatusServiceUnavailable, "Unable retrieve solr data: %s", err.Error())
//		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve solr data: %s", err.Error())
//	}

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/api/manifest/%s", config.tracksysURL, data.MetadataPID)
	if unitID > 0 {
		tsURL = fmt.Sprintf("%s?unit=%d", tsURL, unitID)
	}
	respStr, err := getAPIResponse(tsURL, standardHttpClient)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve manifest: %s", err.Error())
	}

	getMasterFilesFromJSON(&data, respStr)
	var metadata string
	metadata, err = renderIiifMetadata(data)
	if err != nil {
		//c.String(http.StatusInternalServerError, "Unable to render IIIF metadata: %s", err.Error())
		return "", http.StatusInternalServerError, fmt.Sprintf("Unable to render IIIF metadata: %s", err.Error())
	}

	// happy day
	return metadata, http.StatusOK, ""
}

// generateFromExternal will generate the IIIF manifest for an external record
func generateFromExternal(config *serviceConfig, data IIIF) (string, int, string) {
	err := getTrackSysMetadata(config, &data)
	if err != nil {
		log.Printf("ERROR: tracksys metadata request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve metadata: %s", err.Error())
	}

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/api/manifest/%s", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL, standardHttpClient)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve manifest: %s", err.Error())
	}

	getMasterFilesFromJSON(&data, respStr)
	var metadata string
	metadata, err = renderIiifMetadata(data)
	if err != nil {
		//c.String(http.StatusInternalServerError, "Unable to render IIIF metadata: %s", err.Error())
		return "", http.StatusInternalServerError, fmt.Sprintf("Unable to render IIIF metadata: %s", err.Error())
	}

	// happy day
	return metadata, http.StatusOK, ""
}

func generateFromComponent(config *serviceConfig, pid string, data IIIF) (string, int, string) {
	err := getTrackSysMetadata(config, &data)
	if err != nil {
		log.Printf("ERROR: tracksys metadata request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve metadata: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve metadata: %s", err.Error())
	}

	// Get masterFiles from TrackSys manifest API that are hooked to this component
	tsURL := fmt.Sprintf("%s/api/manifest/%s", config.tracksysURL, pid)
	respStr, err := getAPIResponse(tsURL, standardHttpClient)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve manifest: %s", err.Error())
	}

	getMasterFilesFromJSON(&data, respStr)
	var metadata string
	metadata, err = renderIiifMetadata(data)
	if err != nil {
		//c.String(http.StatusInternalServerError, "Unable to render IIIF metadata: %s", err.Error())
		return "", http.StatusInternalServerError, fmt.Sprintf("Unable to render IIIF metadata: %s", err.Error())
	}

	// happy day
	return metadata, http.StatusOK, ""
}

func getTrackSysMetadata(config *serviceConfig, data *IIIF) error {
	tsURL := fmt.Sprintf("%s/api/metadata/%s?type=brief", config.tracksysURL, data.MetadataPID)
	respStr, err := getAPIResponse(tsURL, standardHttpClient)
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

//
// shared call to handle API calls
//
func getAPIResponse(url string, httpClient *http.Client) (string, error) {

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

//
// render metadata from the template
//
func renderIiifMetadata(data IIIF) (string, error) {
	tmpl := template.Must(template.ParseFiles("templates/iiif.json"))
	var outBuffer bytes.Buffer
	err := tmpl.Execute(&outBuffer, data)
	if err != nil {
		log.Printf("ERROR: unable to render IIIF metadata for %s: %s", data.MetadataPID, err.Error())
		return "", err
	}
	log.Printf("INFO: IIIF Metadata generated for %s", data.MetadataPID)
	return outBuffer.String(), nil
}

//
// generate the cache key name
//
func cacheKey(path string, pid string, unit string) string {
	name := fmt.Sprintf("%s-%s", path, pid)
	if len(unit) != 0 {
		name = fmt.Sprintf("%s-%s", name, unit)
	}
	// cleanup any special characters
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")

	return name
}

//
// end of file
//

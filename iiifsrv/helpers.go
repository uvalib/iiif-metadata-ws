package main

import (
	// "encoding/json"
	"bytes"
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

var readTimeout = 15
var connTimeout = 5

//
// define our connection clients
//

// standard client used for normal requests
var standardHTTPClient = &http.Client{
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
var fastHTTPClient = &http.Client{
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

// generateFromApollo will generate the IIIF manifest from data found in Apollo
func generateFromApollo(config *serviceConfig, data IIIF) (string, int, string) {
	// Get some metadata about the collection from Apollo API...
	PID := data.MetadataPID
	apolloURL := fmt.Sprintf("%s/api/items/%s", config.apolloURL, PID)
	_, respStr, err := getAPIResponse(apolloURL, standardHTTPClient)
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
	_, respStr, err = getAPIResponse(tsURL, standardHTTPClient)
	if err != nil {
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		//c.String(http.StatusServiceUnavailable, "Unable retrieve manifest: %s", err.Error())
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable retrieve manifest: %s", err.Error())
	}

	err = getMasterFilesFromJSON(&data, respStr)
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Sprintf("ERROR: Unable to get masterfiles data for Apollo PID %s: %s", PID, err.Error())
	}
	var metadata string
	metadata, err = renderIiifMetadata(data)
	if err != nil {
		//c.String(http.StatusInternalServerError, "Unable to render IIIF metadata: %s", err.Error())
		return "", http.StatusInternalServerError, fmt.Sprintf("Unable to render IIIF metadata: %s", err.Error())
	}

	// happy day
	return metadata, http.StatusOK, ""
}

// generateFromTrackSys will generate the IIIF manifest from a TrackSys item
func generateFromTrackSys(config *serviceConfig, data IIIF, unitID int) (string, int, string) {
	// Get data for all master files from units associated with the metadata record. Include unit if specified
	tsURL := fmt.Sprintf("%s/api/manifest/%s", config.tracksysURL, data.MetadataPID)
	if unitID > 0 {
		tsURL = fmt.Sprintf("%s?unit=%d", tsURL, unitID)
	}
	respStatus, respStr, err := getAPIResponse(tsURL, standardHTTPClient)
	if err != nil {
		status := http.StatusServiceUnavailable
		if respStatus == http.StatusNotFound {
			status = http.StatusNotFound
		}
		log.Printf("ERROR: tracksys manifest request failed: %s", err.Error())
		return "", status, fmt.Sprintf("Unable retrieve manifest: %s", err.Error())
	}

	err = getMasterFilesFromJSON(&data, respStr)
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Sprintf("ERROR: Unable to get masterfiles data for TS PID %s: %s", data.MetadataPID, err.Error())
	}
	var metadata string
	metadata, err = renderIiifMetadata(data)
	if err != nil {
		//c.String(http.StatusInternalServerError, "Unable to render IIIF metadata: %s", err.Error())
		return "", http.StatusInternalServerError, fmt.Sprintf("Unable to render IIIF metadata: %s", err.Error())
	}

	// happy day
	return metadata, http.StatusOK, ""
}

//
// shared call to handle API calls
//
func getAPIResponse(url string, httpClient *http.Client) (int, string, error) {

	log.Printf("INFO: GET %s", url)
	resp, err := httpClient.Get(url)
	if err != nil {
		log.Printf("ERROR: issuing request: %s, %s", url, err.Error())
		return http.StatusServiceUnavailable, "", err
	}
	defer resp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ERROR: reading response: %s, %s", url, err.Error())
		return http.StatusServiceUnavailable, "", err
	}
	respString := string(bodyBytes)
	if resp.StatusCode != http.StatusOK {
		logLevel := "ERROR"
		// some errors are expected
		if resp.StatusCode == http.StatusNotFound {
			logLevel = "INFO"
		}
		log.Printf("%s: %s returns %d (%s)", logLevel, url, resp.StatusCode, respString)
		return resp.StatusCode, "", errors.New(respString)
	}
	return resp.StatusCode, respString, nil
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
func cacheKey(path string, pid string) string {
	name := fmt.Sprintf("%s-%s", path, pid)
	// cleanup any special characters
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")

	return name
}

//
// end of file
//

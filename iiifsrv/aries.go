package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ariesPingHandler handles requests to the aries endpoint with no params.
// Just returns and alive message
func ariesPingHandler(c *gin.Context) {
	c.String(http.StatusOK, "IIIF Manifest Service Aries API")
}

// ariesLookupHandler will query apollo for information on the supplied identifer
func ariesLookupHandler(c *gin.Context) {
	id := c.Param("id")
	pidURL := fmt.Sprintf("%s/pid/%s/type", config.tracksysURL, id)
	pidType, err := getAPIResponse(pidURL)
	if err != nil {
		log.Printf("Request to TrackSys %s failed: %s", config.tracksysURL, err.Error())
		c.String(http.StatusNotFound, "id %s not found", id)
		return
	}
	if pidType == "invalid" {
		c.String(http.StatusNotFound, "id %s not found", id)
		return
	}

	// TODO return Aries responce with link to manifest and identifier
	// TODO Fix build scripts to accept flags to control various URL and HostName parameters
	c.String(http.StatusOK, "IIIF Manifest Service Aries API Lookup %s", id)
}

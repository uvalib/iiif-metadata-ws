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

	svc := gin.H{"url": fmt.Sprintf("https://%s/pid/%s", config.hostName, id), "protocol": "iiif-presentation"}
	ids := []string{id}
	c.JSON(http.StatusOK, gin.H{
		"identifer":   ids,
		"service_url": []interface{}{svc},
	})
}

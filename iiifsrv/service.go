package main

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ServiceContext contains common data used by all handlers
type ServiceContext struct {
	config *serviceConfig
	//solr   ServiceSolr
}

// InitializeService will initialize the service context based on the config parameters.
func InitializeService(cfg *serviceConfig) *ServiceContext {

	log.Printf("INFO: initializing service")

	svc := ServiceContext{
		config: cfg,
	}
	return &svc
}

// IgnoreHandler is a dummy to handle certain browser requests without warnings (e.g. favicons)
func (svc *ServiceContext) IgnoreHandler(c *gin.Context) {
}

// FavHandler is a dummy handler to silence browser API requests that look for /favicon.ico
func (svc *ServiceContext) FavHandler(c *gin.Context) {
}

// ConfigHandler dumps the current service config as json
func (svc *ServiceContext) ConfigHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service_host": svc.config.hostName,
		"apollo":       svc.config.apolloURL,
		"tracksys":     svc.config.tracksysURL,
		"solr":         svc.config.solrURL,
		"iiif":         svc.config.iiifURL,
	})
}

// Handle a request for / and return version info
func (svc *ServiceContext) VersionHandler(c *gin.Context) {

	build := "unknown"

	// cos our CWD is the bin directory
	files, _ := filepath.Glob("../buildtag.*")
	if len(files) == 1 {
		build = strings.Replace(files[0], "../buildtag.", "", 1)
	}

	vMap := make(map[string]string)
	vMap["version"] = version
	vMap["build"] = build
	c.JSON(http.StatusOK, vMap)
}

func (svc *ServiceContext) HealthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"alive": "true"})
}

// ExistHandler checks if there is IIIF data available for a PID
func (svc *ServiceContext) ExistHandler(c *gin.Context) {
	pid := c.Param("pid")
	pidURL := fmt.Sprintf("%s/pid/%s/type", svc.config.tracksysURL, pid)
	resp, err := getAPIResponse(pidURL)
	if err != nil {
		c.String(http.StatusNotFound, "IIIF Metadata does not exist for %s", pid)
		return
	}
	if resp == "masterfile" {
		c.String(http.StatusNotFound, "IIIF Metadata does not exist for %s", pid)
		return
	}
	c.String(http.StatusOK, "IIIF Metadata exists for %s", pid)
}

// IiifHandler processes a request for IIIF presentation metadata
func (svc *ServiceContext) IiifHandler(c *gin.Context) {
	pid := c.Param("pid")

	// initialize IIIF data struct
	var data IIIF
	data.IiifURL = svc.config.iiifURL
	data.URL = fmt.Sprintf("https://%s/pid/%s", svc.config.hostName, pid)
	data.MetadataPID = pid
	data.Metadata = make(map[string]string)

	// Tracksys is the system that tracks items that contain
	// masterfiles. All pids the arrive at the IIIF service should
	// refer to these items. Determine what type the PID is:
	pidURL := fmt.Sprintf("%s/pid/%s/type", svc.config.tracksysURL, pid)
	pidType, err := getAPIResponse(pidURL)
	if err != nil {
		c.String(http.StatusServiceUnavailable, "Unable to connect with TrackSys to identify pid %s", pid)
		return
	}

	if pidType == "sirsi_metadata" {
		log.Printf("INFO: %s is a sirsi metadata record", pid)
		unitID, _ := strconv.Atoi(c.Query("unit"))
		generateFromSirsi(svc.config, data, c, unitID)
	} else if pidType == "xml_metadata" {
		log.Printf("INFO: %s is an xml metadata record", pid)
		generateFromXML(svc.config, data, c)
	} else if pidType == "apollo_metadata" {
		log.Printf("INFO: %s is an apollo metadata record", pid)
		generateFromApollo(svc.config, data, c)
	} else if pidType == "archivesspace_metadata" || pidType == "jstor_metadata" {
		log.Printf("INFO: %s is an as metadata record", pid)
		generateFromExternal(svc.config, data, c)
	} else if pidType == "component" {
		log.Printf("INFO: %s is a component", pid)
		generateFromComponent(svc.config, pid, data, c)
	} else {
		log.Printf("ERROR: couldn't find %s", pid)
		c.String(http.StatusNotFound, "PID %s not found", pid)
	}
}

// ariesPingHandler handles requests to the aries endpoint with no params.
// Just returns and alive message
func (svc *ServiceContext) AriesPingHandler(c *gin.Context) {
	c.String(http.StatusOK, "IIIF Manifest Service Aries API")
}

// ariesLookupHandler will query apollo for information on the supplied identifer
func (svc *ServiceContext) AriesLookupHandler(c *gin.Context) {

	id := c.Param("id")
	pidURL := fmt.Sprintf("%s/pid/%s/type", svc.config.tracksysURL, id)
	pidType, err := getAPIResponse(pidURL)
	if err != nil {
		log.Printf("ERROR: request to TrackSys %s failed: %s", svc.config.tracksysURL, err.Error())
		c.String(http.StatusNotFound, "id %s not found", id)
		return
	}
	if pidType == "invalid" || pidType == "masterfile" {
		c.String(http.StatusNotFound, "id %s not found", id)
		return
	}

	s := gin.H{"url": fmt.Sprintf("https://%s/pid/%s", svc.config.hostName, id), "protocol": "iiif-presentation"}
	ids := []string{id}
	c.JSON(http.StatusOK, gin.H{
		"identifier":  ids,
		"service_url": []interface{}{s},
	})
}

//
// end of file
//

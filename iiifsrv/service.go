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

// our content type definitions
var contentTypeHeader = "content-type"
var contentType = "application/json; charset=utf-8"

// ServiceContext contains common data used by all handlers
type ServiceContext struct {
	config *serviceConfig
	cache  *CacheProxy
}

// InitializeService will initialize the service context based on the config parameters.
func InitializeService(cfg *serviceConfig) *ServiceContext {

	log.Printf("INFO: initializing service")

	svc := ServiceContext{
		config: cfg,
		cache:  NewCacheProxy(cfg),
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
		//"solr":         svc.config.solrURL,
		"iiif":         svc.config.iiifURL,
	})
}

// VersionHandler returns service version information
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

// HealthCheckHandler returns service health information (dummy for now, FIXME)
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

// CacheHandler processes a request for cached IIIF presentation metadata
func (svc *ServiceContext) CacheHandler(c *gin.Context) {

	path := "pid"
	pid := c.Param("pid")
	unit := c.Query("unit")
	refresh := c.Query("refresh")
	key := cacheKey(path, pid, unit)
	cacheUrl := fmt.Sprintf("%s/%s/%s", svc.config.cacheRootUrl, svc.config.cacheBucket, key)

	// if the manifest is not in the cache or we recreating the cache explicitly
	if refresh == "true" || svc.cache.IsInCache(key) == false {

		// generate the manifest data as appropriate
		manifest, status, errorText := svc.generateManifest(cacheUrl, pid, unit)

		// error case
		if status != http.StatusOK {
			c.String(status, errorText)
			return
		}

		// write it to the cache
		err := svc.cache.WriteToCache(key, manifest)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("Error writing to cache: %s", err.Error()))
			return
		}
	}

	// happy day
	vMap := make(map[string]string)
	vMap["url"] = cacheUrl
	c.JSON(http.StatusOK, vMap)
}

// IiifHandler processes a request for IIIF presentation metadata
func (svc *ServiceContext) IiifHandler(c *gin.Context) {

	path := "pid"
	pid := c.Param("pid")
	unit := c.Query("unit")
	key := cacheKey(path, pid, unit)

	// if the manifest is in the cache
	if svc.cache.IsInCache(key) == true {

		// get it
		manifest, err := svc.cache.ReadFromCache(key)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("Error reading from cache: %s", err.Error()))
			return
		}

		// happy day
		c.Header(contentTypeHeader, contentType)
		c.String(http.StatusOK, manifest)

	} else {

		// generate the manifest data as appropriate
		cacheUrl := fmt.Sprintf("%s/%s/%s", svc.config.cacheRootUrl, svc.config.cacheBucket, key)
		manifest, status, errorText := svc.generateManifest(cacheUrl, pid, unit)

		// error case
		if status != http.StatusOK {
			c.String(status, errorText)
			return
		}

		// write it to the cache
		err := svc.cache.WriteToCache(key, manifest)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("Error writing to cache: %s", err.Error()))
			return
		}
		// happy day
		c.Header(contentTypeHeader, contentType)
		c.String(http.StatusOK, manifest)
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
// generate the manifest content and return it or the http status and an error message
//
func (svc *ServiceContext) generateManifest(url string, pid string, unit string) (string, int, string) {

	// initialize IIIF data struct
	var data IIIF
	data.IiifURL = svc.config.iiifURL
	data.URL = url
	data.MetadataPID = pid
	data.Metadata = make(map[string]string)

	// Tracksys is the system that tracks items that contain
	// masterfiles. All pids the arrive at the IIIF service should
	// refer to these items. Determine what type the PID is:
	pidURL := fmt.Sprintf("%s/pid/%s/type", svc.config.tracksysURL, pid)
	pidType, err := getAPIResponse(pidURL)
	if err != nil {
		return "", http.StatusServiceUnavailable, fmt.Sprintf("Unable to connect with TrackSys to identify pid %s", pid)
	}

	if pidType == "sirsi_metadata" {
		log.Printf("INFO: %s is a sirsi metadata record", pid)
		unitID, _ := strconv.Atoi(unit)
		return generateFromSirsi(svc.config, data, unitID)
	} else if pidType == "xml_metadata" {
		log.Printf("INFO: %s is an xml metadata record", pid)
		return generateFromXML(svc.config, data)
	} else if pidType == "apollo_metadata" {
		log.Printf("INFO: %s is an apollo metadata record", pid)
		return generateFromApollo(svc.config, data)
	} else if pidType == "archivesspace_metadata" || pidType == "jstor_metadata" {
		log.Printf("INFO: %s is an as metadata record", pid)
		return generateFromExternal(svc.config, data)
	} else if pidType == "component" {
		log.Printf("INFO: %s is a component", pid)
		return generateFromComponent(svc.config, pid, data)
	}
	log.Printf("ERROR: couldn't find %s", pid)
	return "", http.StatusNotFound, fmt.Sprintf("PID %s not found", pid)
}

//
// end of file
//

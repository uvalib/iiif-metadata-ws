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
	}
	if cfg.cacheDisabled == false {
		svc.cache = NewCacheProxy(cfg)
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

	type healthcheck struct {
		Healthy bool   `json:"healthy"`
		Message string `json:"message"`
	}

	//log.Printf("INFO: checking Tracksys...")
	//url := fmt.Sprintf("%s/api/pid/uva-lib:1157560/type", svc.config.tracksysURL)

	//tsStatus := healthcheck{true, ""}
	//_, _, err := getAPIResponse(url, fastHTTPClient)
	//if err != nil {
	//	tsStatus.Healthy = false
	//	tsStatus.Message = err.Error()
	//}

	// make sure apollo service is alive
	log.Printf("INFO: checking Apollo...")
	url := fmt.Sprintf("%s/version", svc.config.apolloURL)
	apolloStatus := healthcheck{true, ""}

	_, _, err := getAPIResponse(url, fastHTTPClient)
	if err != nil {
		apolloStatus.Healthy = false
		apolloStatus.Message = err.Error()
	}

	httpStatus := http.StatusOK
//	if tsStatus.Healthy == false || apolloStatus.Healthy == false {
	if apolloStatus.Healthy == false {
		httpStatus = http.StatusInternalServerError
	}

//	c.JSON(httpStatus, gin.H{"tracksys": tsStatus, "apollo": apolloStatus})
	c.JSON(httpStatus, gin.H{"apollo": apolloStatus})
}

// ExistHandler checks if there is IIIF data available for a PID
func (svc *ServiceContext) ExistHandler(c *gin.Context) {
	path := "pid"
	pid := c.Param("pid")
	unit := c.Query("unit")
	key := cacheKey(path, pid, unit)

	// if the manifest is already in the cache then return
	if svc.config.cacheDisabled == false {
		if svc.cache.IsInCache(key) == true {
			c.String(http.StatusOK, "Cached IIIF Metadata exists for %s", pid)
			return
		}
	} else {
		log.Printf("IIIF Cache is disabled")
	}

	// otherwise, check tracksys to see if it knows about this item
	pidURL := fmt.Sprintf("%s/api/pid/%s/type", svc.config.tracksysURL, pid)
	_, resp, err := getAPIResponse(pidURL, standardHTTPClient)
	if err != nil || resp == "masterfile" {
		c.String(http.StatusNotFound, "IIIF Metadata does not exist for %s", pid)
		return
	}

	log.Printf("IIIF Metadata exists for %s", pid)
	c.String(http.StatusOK, "IIIF Metadata exists for %s", pid)
}

// CacheHandler processes a request for cached IIIF presentation metadata
func (svc *ServiceContext) CacheHandler(c *gin.Context) {
	if svc.config.cacheDisabled == true {
		log.Printf("WARN: Request for cached item when cache is disabled")
		c.String(http.StatusFailedDependency, "cache is disabled")
		return
	}

	path := "pid"
	pid := c.Param("pid")
	unit := c.Query("unit")
	refresh := c.Query("refresh")
	key := cacheKey(path, pid, unit)
	cacheURL := fmt.Sprintf("%s/%s/%s", svc.config.cacheRootURL, svc.config.cacheBucket, key)

	// if the manifest is not in the cache or we recreating the cache explicitly
	if refresh == "true" || svc.cache.IsInCache(key) == false {

		// generate the manifest data as appropriate
		manifest, status, errorText := svc.generateManifest(cacheURL, pid, unit)

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
	vMap["url"] = cacheURL
	c.JSON(http.StatusOK, vMap)
}

// IiifHandler processes a request for IIIF presentation metadata
func (svc *ServiceContext) IiifHandler(c *gin.Context) {

	path := "pid"
	pid := c.Param("pid")
	unit := c.Query("unit")
	key := cacheKey(path, pid, unit)

	// if the manifest is in the cache
	if svc.config.cacheDisabled == false && svc.cache.IsInCache(key) == true {

		// get it
		manifest, err := svc.cache.ReadFromCache(key)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("Error reading from cache: %s", err.Error()))
			return
		}

		// happy day
		c.Header(contentTypeHeader, contentType)
		c.Header("Cache-Control", "no-store")
		c.String(http.StatusOK, manifest)

	} else {

		// generate the manifest data as appropriate
		cacheURL := fmt.Sprintf("%s/%s/%s", svc.config.cacheRootURL, svc.config.cacheBucket, key)
		manifest, status, errorText := svc.generateManifest(cacheURL, pid, unit)

		// error case
		if status != http.StatusOK {
			c.String(status, errorText)
			return
		}

		// write it to the cache
		if svc.config.cacheDisabled == false {
			err := svc.cache.WriteToCache(key, manifest)
			if err != nil {
				c.String(http.StatusInternalServerError, fmt.Sprintf("Error writing to cache: %s", err.Error()))
				return
			}
		}

		// happy day
		c.Header(contentTypeHeader, contentType)
		c.String(http.StatusOK, manifest)
	}
}

// AriesPingHandler handles requests to the aries endpoint with no params.
// Just returns and alive message
func (svc *ServiceContext) AriesPingHandler(c *gin.Context) {
	c.String(http.StatusOK, "IIIF Manifest Service Aries API")
}

// AriesLookupHandler will query apollo for information on the supplied identifer
func (svc *ServiceContext) AriesLookupHandler(c *gin.Context) {

	id := c.Param("id")
	pidURL := fmt.Sprintf("%s/api/pid/%s/type", svc.config.tracksysURL, id)
	_, pidType, err := getAPIResponse(pidURL, standardHTTPClient)
	if err != nil {
		log.Printf("ERROR: request to TrackSys %s failed: %s", pidURL, err.Error())
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

// generateManifest will generate the manifest content and return it or the http status and an error message
func (svc *ServiceContext) generateManifest(url string, pid string, unit string) (string, int, string) {
	var data IIIF
	data.IiifURL = svc.config.iiifURL
	data.URL = url
	data.MetadataPID = pid
	unitID, _ := strconv.Atoi(unit)
	return generateFromTrackSys(svc.config, data, unitID)
}

//
// end of file
//

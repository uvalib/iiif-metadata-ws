package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
)

// ServiceContext contains common data used by all handlers
type ServiceContext struct {
	HostName       string
	TracksysURL    string
	IIIFServerURL  string
	ManifestBucket string
	ManifestURL    string
	IIIFTemplate   *template.Template
	S3Client       *s3.Client
	HTTPClient     *http.Client
}

// RequestError contains error data from an HTTP API request
type RequestError struct {
	StatusCode int
	Message    string
}

func initializeService(cfg *serviceConfig) *ServiceContext {
	log.Printf("INFO: initializing service")
	ctx := ServiceContext{
		HostName:       cfg.hostName,
		TracksysURL:    cfg.tracksysURL,
		IIIFServerURL:  cfg.iiifURL,
		ManifestBucket: cfg.cacheBucket,
		ManifestURL:    cfg.cacheRootURL,
	}

	log.Printf("INFO: initialize s3 session...")
	awsCfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatal(fmt.Sprintf("unable to load s3 config: %s", err.Error()))
	}
	ctx.S3Client = s3.NewFromConfig(awsCfg)
	log.Printf("INFO: s3 session established")

	log.Printf("INFO: create http client...")
	defaultTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 60 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	ctx.HTTPClient = &http.Client{
		Transport: defaultTransport,
		Timeout:   15 * time.Second,
	}
	log.Printf("INFO: http client created")

	log.Printf("INFO: load iiif manifest template")
	ctx.IIIFTemplate, err = template.New("iiif.json").ParseFiles("./templates/iiif.json")
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("INFO: service initialized")
	return &ctx
}

// FavHandler is a dummy handler to silence browser API requests that look for /favicon.ico
func (svc *ServiceContext) favIconHandler(c *gin.Context) {
}

// ConfigHandler dumps the current service config as json
func (svc *ServiceContext) configHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service_host": svc.HostName,
		"tracksys":     svc.TracksysURL,
		"iiif":         svc.IIIFServerURL,
	})
}

// VersionHandler returns service version information
func (svc *ServiceContext) versionHandler(c *gin.Context) {
	build := "unknown"
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
func (svc *ServiceContext) healthCheckHandler(c *gin.Context) {
	type healthcheck struct {
		Healthy bool   `json:"healthy"`
		Message string `json:"message"`
	}

	hcMap := make(map[string]healthcheck)
	hcMap["iifman"] = healthcheck{Healthy: true}

	c.JSON(http.StatusOK, hcMap)
}

func (svc *ServiceContext) getAPIResponse(url string) ([]byte, *RequestError) {
	log.Printf("INFO: GET API Response from %s, timeout  %.0f sec", url, svc.HTTPClient.Timeout.Seconds())
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_14_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.128 Safari/537.36")

	startTime := time.Now()
	resp, rawErr := svc.HTTPClient.Do(req)
	elapsedNanoSec := time.Since(startTime)
	elapsedMS := int64(elapsedNanoSec / time.Millisecond)
	bodyBytes, err := handleAPIResponse(url, resp, rawErr)
	if err != nil {
		log.Printf("ERROR: %s : %d:%s. Elapsed Time: %d (ms)", url, err.StatusCode, err.Message, elapsedMS)
		return nil, err
	}

	log.Printf("INFO: successful response from %s. Elapsed Time: %d (ms)", url, elapsedMS)
	return bodyBytes, nil
}

func handleAPIResponse(logURL string, resp *http.Response, err error) ([]byte, *RequestError) {
	if err != nil {
		status := http.StatusBadRequest
		errMsg := err.Error()
		if strings.Contains(err.Error(), "Timeout") {
			status = http.StatusRequestTimeout
			errMsg = fmt.Sprintf("%s timed out", logURL)
		} else if strings.Contains(err.Error(), "connection refused") {
			status = http.StatusServiceUnavailable
			errMsg = fmt.Sprintf("%s refused connection", logURL)
		}
		return nil, &RequestError{StatusCode: status, Message: errMsg}
	} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		status := resp.StatusCode
		errMsg := string(bodyBytes)
		return nil, &RequestError{StatusCode: status, Message: errMsg}
	}

	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	return bodyBytes, nil
}

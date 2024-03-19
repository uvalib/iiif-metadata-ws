package main

import (
	"fmt"
	"log"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// version of the service
const version = "6.2.0"

// main entry point
func main() {
	log.Printf("===> iiif-metadata-ws staring up <===")

	cfg := loadConfig()
	svc := initializeService(cfg)

	gin.SetMode(gin.ReleaseMode)
	gin.DisableConsoleColor()
	router := gin.Default()
	router.Use(cors.Default())

	// Set routes and start server
	router.GET("/favicon.ico", svc.favIconHandler)
	router.GET("/version", svc.versionHandler)
	router.GET("/healthcheck", svc.healthCheckHandler)
	router.GET("/config", svc.configHandler)
	router.GET("/pid/:pid", svc.getManifest)
	router.GET("/pid/:pid/manifest.json", svc.getManifest)
	router.GET("/pid/:pid/exist", svc.manifestExist)

	portStr := fmt.Sprintf(":%d", cfg.port)
	log.Printf("INFO: start iiif manifest service on port %s", portStr)
	log.Fatal(router.Run(portStr))
}

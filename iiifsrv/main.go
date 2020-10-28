package main

import (
	"fmt"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"log"
)

// version of the service
const version = "4.0.0"

//
// main entry point
//
func main() {
	log.Printf("===> iiif-metadata-ws staring up <===")

	cfg := loadConfig()
	svc := InitializeService(cfg)

	gin.SetMode(gin.ReleaseMode)
	gin.DisableConsoleColor()
	router := gin.Default()
	router.Use(cors.Default())

	// Set routes and start server
	router.GET("/favicon.ico", svc.FavHandler)
	router.GET("/version", svc.VersionHandler)
	router.GET("/healthcheck", svc.HealthCheckHandler)
	router.GET("/config", svc.ConfigHandler)
	router.GET("/pid/:pid", svc.IiifHandler)
	router.GET("/pid/:pid/manifest.json", svc.IiifHandler)
	router.GET("/pid/:pid/exist", svc.ExistHandler)
	api := router.Group("/api")
	{
		api.GET("/aries", svc.AriesPingHandler)
		api.GET("/aries/:id", svc.AriesLookupHandler)
	}

	portStr := fmt.Sprintf(":%d", cfg.port)
	log.Printf("INFO: start iiif manifest service on port %s", portStr)
	log.Fatal(router.Run(portStr))
}

//
// end of file
//

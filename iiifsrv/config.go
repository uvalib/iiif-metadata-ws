package main

import (
	"flag"
	"log"
)

// configuration data
type serviceConfig struct {
	port         int
	hostName     string
	tracksysURL  string
	iiifURL      string
	cacheBucket  string
	cacheRootURL string
}

func loadConfig() *serviceConfig {
	cfg := serviceConfig{}

	flag.IntVar(&cfg.port, "port", 8080, "Port to offer service on (default 8080)")
	flag.StringVar(&cfg.tracksysURL, "tracksys", "http://tracksys.lib.virginia.edu", "Tracksys URL")
	flag.StringVar(&cfg.hostName, "host", "iiifman.lib.virginia.edu", "Hostname for this service")
	flag.StringVar(&cfg.iiifURL, "iiif", "https://iiif.lib.virginia.edu", "IIIF image server")
	flag.StringVar(&cfg.cacheBucket, "bucket", "iiif-manifest-cache-production", "cache bucket name")
	flag.StringVar(&cfg.cacheRootURL, "rooturl", "https://s3.us-east-1.amazonaws.com", "cache root URL")
	flag.Parse()

	log.Printf("[CONFIG] port          = [%d]", cfg.port)
	log.Printf("[CONFIG] tracksysURL   = [%s]", cfg.tracksysURL)
	log.Printf("[CONFIG] hostName      = [%s]", cfg.hostName)
	log.Printf("[CONFIG] iiifURL       = [%s]", cfg.iiifURL)
	log.Printf("[CONFIG] cacheBucket   = [%s]", cfg.cacheBucket)
	log.Printf("[CONFIG] cacheRootURL  = [%s]", cfg.cacheRootURL)

	return &cfg
}

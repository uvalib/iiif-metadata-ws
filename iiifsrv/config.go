package main

import (
	"flag"
	"log"
)

// configuration data
type serviceConfig struct {
	port        int
	hostName    string
	solrURL     string
	tracksysURL string
	apolloURL   string
	iiifURL     string
}

func loadConfig() *serviceConfig {
	cfg := serviceConfig{}

	flag.IntVar(&cfg.port, "port", 8080, "Port to offer service on (default 8080)")
	flag.StringVar(&cfg.tracksysURL, "tracksys", "http://tracksys.lib.virginia.edu/api", "Tracksys URL")
	flag.StringVar(&cfg.apolloURL, "apollo", "http://apollo.lib.virginia.edu/api", "Apollo URL")
	flag.StringVar(&cfg.solrURL, "solr", "http://solr.lib.virginia.edu:8082/solr/core", "Virgo Solr URL")
	flag.StringVar(&cfg.hostName, "host", "iiifman.lib.virginia.edu", "Hostname for this service")
	flag.StringVar(&cfg.iiifURL, "iiif", "https://iiif.lib.virginia.edu", "IIIF image server")
	flag.Parse()

	log.Printf("[CONFIG] port        = [%d]", cfg.port)
	log.Printf("[CONFIG] tracksysURL = [%s]", cfg.tracksysURL)
	log.Printf("[CONFIG] apolloURL   = [%s]", cfg.apolloURL)
	log.Printf("[CONFIG] solrURL     = [%s]", cfg.solrURL)
	log.Printf("[CONFIG] hostName    = [%s]", cfg.hostName)
	log.Printf("[CONFIG] iiifURL     = [%s]", cfg.iiifURL)

	return &cfg
}

//
// end of file
//

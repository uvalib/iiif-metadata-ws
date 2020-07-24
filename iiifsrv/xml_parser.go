package main

import (
	"bytes"
	"fmt"
	"log"
	"strings"

	xmlpath "gopkg.in/xmlpath.v2"
)

// parseMARC will extract physicsl description from MARC string
func parseMARC(data *IIIF, marc string) {
	xmlRoot, err := xmlpath.Parse(strings.NewReader(marc))
	if err != nil {
		log.Printf("WARNING: Unable to parse MARC: %s; skipping", err.Error())
		return
	}
	path := xmlpath.MustCompile("//datafield[@tag='300']/subfield")
	nodes := path.Iter(xmlRoot)
	val := getArrayValues(nodes, " ")
	if len(val) > 0 {
		data.Metadata["Physical Description"] = cleanString(val)
	}
}

// parseVirgoSolr parse the solr record for the target item and extract relevant metadata elements
func parseVirgoSolr(virgoURL string, data *IIIF) error {
	url := fmt.Sprintf("%s/select?q=id:%s", virgoURL, data.VirgoKey)
	log.Printf("INFO: get Solr record: %s", url)
	resp, err := getAPIResponse(url)
	if err != nil {
		return err
	}

	xmlRoot, parseErr := xmlpath.Parse( strings.NewReader(resp) )
	if parseErr != nil {
		log.Printf("ERROR: Unable to parse response: %s", parseErr.Error())
		log.Printf("BODY: %s", resp)
		return parseErr
	}

	// Query for the data; format_facet. This has a bunch of <str> children that
	// need to be combined to make the final format string. Skip 'Online'
	path := xmlpath.MustCompile("//arr[@name='format_facet']/str")
	nodes := path.Iter(xmlRoot)
	var buffer bytes.Buffer
	for nodes.Next() {
		val := nodes.Node().String()
		if strings.Compare("Online", val) != 0 {
			if buffer.Len() > 0 {
				buffer.WriteString("; ")
			}
			buffer.WriteString(val)
		}
	}
	if buffer.Len() > 0 {
		data.Metadata["Format"] = buffer.String()
	}

	// See if there is MARC data to parse for physical description
	path = xmlpath.MustCompile("//str[@name='marc_display']")
	marc, ok := path.String(xmlRoot)
	if ok {
		parseMARC(data, marc)
	}

	// Try published_date_display
	path = xmlpath.MustCompile("//arr[@name='published_date_display']/str")
	nodes = path.Iter(xmlRoot)
	date := getArrayValues(nodes, ", ")
	if len(date) > 0 {
		data.Metadata["Date"] = date
	}

	return nil
}

// parseTracksysSolr will get the solr add record from TrackSys and parse it for metdata elements
func parseTracksysSolr(tracksysURL string, data *IIIF) error {
	// For XML metadata
	url := fmt.Sprintf("%s/solr/%s?no_external=1", tracksysURL, data.MetadataPID)
	log.Printf("INFO: get Solr record: %s", url)
	resp, err := getAPIResponse(url)
	if err != nil {
		return err
	}

	xmlRoot, parseErr := xmlpath.Parse( strings.NewReader(resp) )
	if parseErr != nil {
		log.Printf("ERROR: unable to parse response: %s", parseErr.Error())
		log.Printf("BODY: %s", resp)
		return parseErr
	}

	// First, get format facets
	path := xmlpath.MustCompile("//field[@name='format_facet']")
	nodes := path.Iter(xmlRoot)
	var buffer bytes.Buffer
	for nodes.Next() {
		val := nodes.Node().String()
		if strings.Compare("Online", val) != 0 {
			if buffer.Len() > 0 {
				buffer.WriteString("; ")
			}
			buffer.WriteString(val)
		}
	}
	if buffer.Len() > 0 {
		data.Metadata["Format"] = buffer.String()
	}

	// Pull the Author from author_display
	path = xmlpath.MustCompile("//field[@name='author_display']")
	val, ok := path.String(xmlRoot)
	if ok {
		data.Metadata["Author"] = val
	}

	// Pull the Date from year_display
	path = xmlpath.MustCompile("//field[@name='year_display']")
	val, ok = path.String(xmlRoot)
	if ok {
		data.Metadata["Date"] = val
	}

	return nil
}

func getArrayValues(nodes *xmlpath.Iter, sep string) string {
	var buffer bytes.Buffer
	for nodes.Next() {
		val := nodes.Node().String()
		if buffer.Len() > 0 {
			buffer.WriteString(sep)
		}
		buffer.WriteString(val)
	}
	return buffer.String()
}

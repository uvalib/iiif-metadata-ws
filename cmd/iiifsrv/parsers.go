package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/spf13/viper"
	xmlpath "gopkg.in/xmlpath.v2"
)

/**
 * Parse physicsl description from MARC string
 */
func parseMarc(data *iiifData, marc string) {
	xmlRoot, err := xmlpath.Parse(strings.NewReader(marc))
	if err != nil {
		log.Printf("WARNING: Unable to parse MARC: %s; skipping", err.Error())
		return
	}
	path := xmlpath.MustCompile("//datafield[@tag='300']/subfield")
	nodes := path.Iter(xmlRoot)
	val := getArrayValues(nodes, " ")
	if len(val) > 0 {
		data.Metadata = append(data.Metadata, metadata{"Physical Description", val})
	}
}

/**
 * Parse title and description from MODS string
 */
func parseMods(mfData *masterFile, mods string) {
	xmlRoot, err := xmlpath.Parse(strings.NewReader(mods))
	if err != nil {
		log.Printf("WARNING: Unable to parse MODS: %s; skipping", err.Error())
		return
	}
	path := xmlpath.MustCompile("//titleInfo/title")
	val, ok := path.String(xmlRoot)
	if ok {
		mfData.Title = cleanString(val)
	}

	// first try <abstract displayLabel="Description">
	path = xmlpath.MustCompile("//abstract[@displayLabel='Description']")
	val, ok = path.String(xmlRoot)
	if ok {
		mfData.Description = cleanString(val)
		return
	}

	// .. next try for a provenance note
	path = xmlpath.MustCompile("//note[@type='provenance' and @displayLabel='staff']")
	val, ok = path.String(xmlRoot)
	if ok {
		mfData.Description = cleanString(fmt.Sprintf("Staff note: %s", val))
	}
}

/**
 * Parse XML solr index for format_facet and published_display (sirsi) or year_display (xml)
 */
func parseSolrRecord(data *iiifData, metadataType string) {
	if strings.Compare(metadataType, "SirsiMetadata") == 0 {
		parseVirgoSolr(data)
	} else {
		parseTracksysSolr(data)
	}
}

func parseVirgoSolr(data *iiifData) {
	solrURL := fmt.Sprintf("%s/select?q=id:%s", viper.GetString("virgo_solr_url"), data.VirgoKey)
	log.Printf("Get Solr record from %s...", solrURL)
	resp, err := http.Get(solrURL)
	if err != nil {
		log.Printf("Unable to get Solr index: %s", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("Bad response status code: %d", resp.StatusCode)
		return
	}

	xmlRoot, parseErr := xmlpath.Parse(resp.Body)
	if parseErr != nil {
		log.Printf("ERROR: Unable to parse response: %s", parseErr.Error())
		return
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
		data.Metadata = append(data.Metadata, metadata{"Format", buffer.String()})
	}

	// See if there is MARC data to parse for physical description
	path = xmlpath.MustCompile("//str[@name='marc_display']")
	marc, ok := path.String(xmlRoot)
	if ok {
		parseMarc(data, marc)
	}

	// Try published_date_display
	path = xmlpath.MustCompile("//arr[@name='published_date_display']/str")
	nodes = path.Iter(xmlRoot)
	date := getArrayValues(nodes, ", ")
	if len(date) > 0 {
		data.Metadata = append(data.Metadata, metadata{"Date", date})
		return
	}
}

func parseTracksysSolr(data *iiifData) {
	// For XML metadata
	solrURL := fmt.Sprintf("%s/%s?no_external=1", viper.GetString("tracksys_solr_url"), data.MetadataPID)
	log.Printf("Get Solr record from %s...", solrURL)
	resp, err := http.Get(solrURL)
	if err != nil {
		log.Printf("Unable to get Solr index: %s", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("Bad response status code: %d", resp.StatusCode)
		return
	}

	xmlRoot, parseErr := xmlpath.Parse(resp.Body)
	if parseErr != nil {
		log.Printf("ERROR: Unable to parse response: %s", parseErr.Error())
		return
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
		data.Metadata = append(data.Metadata, metadata{"Format", buffer.String()})
	}

	// Pull the Author from author_display
	path = xmlpath.MustCompile("//field[@name='author_display']")
	val, ok := path.String(xmlRoot)
	if ok {
		data.Metadata = append(data.Metadata, metadata{"Author", val})
	}

	// Pull the Date from year_display
	path = xmlpath.MustCompile("//field[@name='year_display']")
	val, ok = path.String(xmlRoot)
	if ok {
		data.Metadata = append(data.Metadata, metadata{"Date", val})
	}
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
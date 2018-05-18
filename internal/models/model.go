// Package models contains the types used to
// generate the JSON response; masterFile, metadata and iiifData
package models

import "strings"

// MasterFile defines the metadata required to describe an image file
type MasterFile struct {
	PID         string
	Title       string
	Description string
	Width       int
	Height      int
}

// Metadata is a key/value pair for basic metadat
type Metadata struct {
	Name  string
	Value string
}

// IIIF coontains all of the data necessary to render an IIIF manifest
type IIIF struct {
	VirgoURL    string
	IiifURL     string
	URL         string
	VirgoKey    string
	MetadataPID string
	Title       string
	StartPage   int
	ExemplarPID string
	License     string
	Related     string
	Metadata    []Metadata
	MasterFiles []MasterFile
}

// CleanString removes invalid characters from a string
func CleanString(str string) string {
	safe := strings.Replace(str, "\n", " ", -1)    /* escape for json */
	safe = strings.Replace(safe, "\r", " ", -1)    /* escape for json */
	safe = strings.Replace(safe, "\t", " ", -1)    /* escape for json */
	safe = strings.Replace(safe, "\\", "\\\\", -1) /* escape for json */
	safe = strings.Replace(safe, "\x0C", "", -1)   /* illegal in XML */
	return safe
}

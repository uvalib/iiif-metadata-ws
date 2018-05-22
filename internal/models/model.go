// Package models contains the types used to
// generate the JSON response; masterFile, metadata and iiifData
package models

import (
	"strings"
)

// MasterFile defines the metadata required to describe an image file
type MasterFile struct {
	PID         string
	Title       string
	Description string
	Width       int
	Height      int
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
	Metadata    map[string]string
	MasterFiles []MasterFile
}

// JoinMetadata takes the Metadata map and joins it
// into a JSON friendly string of the format:
//    {"label": "$KEY", "value": "$VAL"}, ...
//    with each ele separated by a comma
func (iiif IIIF) JoinMetadata() string {
	var out string
	for k, v := range iiif.Metadata {
		if len(out) > 0 {
			out = out + ", "
		}
		out = out + "{\"label\": \"" + k + "\", \"value\": \"" + v + "\"}"
	}
	return out
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

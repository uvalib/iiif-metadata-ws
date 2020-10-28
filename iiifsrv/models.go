package main

import (
	"strings"
)

// BriefMetadata defines the basic metadata for an item as
// returned by the TrackSys brief metadata API call
type BriefMetadata struct {
	PID        string
	Title      string
	Creator    string
	Rights     string
	Exemplar   string
	CatalogKey string
	CallNumber string
}

// MasterFile defines the metadata required to describe an image file
type MasterFile struct {
	PID         string
	Title       string
	Description string
	Width       int
	Height      int
	Rotation    string
}

// IIIF coontains all of the data necessary to render an IIIF manifest
type IIIF struct {
	IiifURL          string
	URL              string
	VirgoKey         string
	MetadataPID      string
	Title            string
	StartPage        int
	ExemplarPID      string
	ExemplarRotation string
	License          string
	Related          string
	Metadata         map[string]string
	MasterFiles      []MasterFile
}

// JSONMetadata takes the Metadata map and joins it
// into a JSON friendly string of the format:
//    [ {"label": "$KEY", "value": "$VAL"}, {...} ]
//    with each ele separated by a comma
func (iiif IIIF) JSONMetadata() string {
	out := "[ "
	for k, v := range iiif.Metadata {
		if len(out) > 2 {
			out = out + ", "
		}
		out = out + "{\"label\": \"" + k + "\", \"value\": \"" + v + "\"}"
	}
	out += " ]"
	return out
}

// cleanString removes invalid characters from a string
func cleanString(str string) string {
	safe := strings.Replace(str, "\n", " ", -1)    /* escape for json */
	safe = strings.Replace(safe, "\r", " ", -1)    /* escape for json */
	safe = strings.Replace(safe, "\t", " ", -1)    /* escape for json */
	safe = strings.Replace(safe, "\\", "\\\\", -1) /* escape for json */
	safe = strings.Replace(safe, "\"", "\\\"", -1) /* escape for json */
	safe = strings.Replace(safe, "\x0C", "", -1)   /* illegal in XML */
	return safe
}

//
// end of file
//

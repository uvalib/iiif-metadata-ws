package main

// CloneData contsins the filename info for a master file that is a clone
type CloneData struct {
	PID      string `json:"pid"`
	Filename string `json:"filename"`
}

// ManifestData is a record with details from a PID manifest resuest from the tracksys API
type ManifestData struct {
	PID         string     `json:"pid"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Orientation string     `json:"orientation"`
	Filename    string     `json:"filename"`
	Exemplar    bool       `json:"exemplar"`
	ClonedFrom  *CloneData `json:"cloned_from"`
}

// PIDInfo contains top level metadata for a given PID (either metadata or component)
type PIDInfo struct {
	Title           string `json:"title"`
	ContentAdvisory string `json:"advisory"`
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

// IIIF contains all of the data necessary to render an IIIF manifest
type IIIF struct {
	IIIFServerURL    string
	URL              string
	VirgoKey         string
	MetadataPID      string
	Title            string
	ContentAdvisory  string
	StartPage        int
	ExemplarPID      string
	ExemplarRotation string
	License          string
	Related          string
	MasterFiles      []MasterFile
}

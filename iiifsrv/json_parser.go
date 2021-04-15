package main

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
)

// getMetadataFromJSON parses basic IIIF Metadata from an Apollo JSON API response
func getMetadataFromJSON(data *IIIF, jsonStr string) error {
	// All of the data we care about is under the array of children
	// found in the collection key in the response. Unmarshall the string
	// into arbitrary map and find the stuff needed:
	//      title, catalogKey, callNumber, useRights
	var jsonMap map[string]interface{}
	json.Unmarshal([]byte(jsonStr), &jsonMap)

	// NOTE: this is a type assertion. Basically a cast from
	// the general interface{} to a specific type.
	collection, ok := jsonMap["collection"].(map[string]interface{})
	if !ok {
		return errors.New("unable to parse 'collection' from response")
	}

	children, ok := collection["children"].([]interface{})
	if !ok {
		return errors.New("unable to parse 'children' from collection response")
	}

	for _, c := range children {
		// c is just returned as a generic interface{}.
		// Cast it to a json model of key->values
		child := c.(map[string]interface{})

		// get the Name data for this node
		typeAttr, okName := child["type"].(map[string]interface{})
		if !okName {
			return errors.New("unable to parse 'type' from response")
		}

		// all names have a value; pick the ones we want ang get the
		// value attribute from the child, Store it in the IIIF data
		switch val := typeAttr["name"].(string); val {
		case "title":
			log.Printf("INFO: title:%s", child["value"].(string))
			data.Title = cleanString(child["value"].(string))
		case "catalogKey":
			catalogKey := child["value"].(string)
			log.Printf("INFO: catalogKey: %s", catalogKey)
			data.VirgoKey = catalogKey
		case "useRights":
			log.Printf("INFO: license: %s", child["valueURI"].(string))
			data.License = child["valueURI"].(string)
		}
	}

	// Add item-level title to the main title
	item, ok := jsonMap["item"].(map[string]interface{})
	if !ok {
		return errors.New("unable to parse 'item' from response")
	}
	children, oK := item["children"].([]interface{})
	if !oK {
		return errors.New("unable to parse 'children' from item response")
	}
	for _, c := range children {
		child := c.(map[string]interface{})
		typeAttr := child["type"].(map[string]interface{})
		val := typeAttr["name"].(string)
		if strings.Compare(val, "title") == 0 {
			data.Title = cleanString(data.Title + ": " + child["value"].(string))
		}
	}
	return nil
}

// getMasterFilesFromJSON parses basic IIIF Metadata from an Apollo JSON API response
func getMasterFilesFromJSON(data *IIIF, jsonStr string) error {

	type CloneData struct {
		PID      string `json:"pid"`
		Filename string `json:"filename"`
	}
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

	// log.Printf("DEBUG: jsonStr [%s]", jsonStr)

	var jsonResp []ManifestData
	err := json.Unmarshal([]byte(jsonStr), &jsonResp)
	if err != nil {
		return err
	}
	pgNum := 0
	for _, mfData := range jsonResp {
		var mf MasterFile
		mf.PID = mfData.PID
		if mfData.ClonedFrom != nil && mfData.ClonedFrom.PID != "" {
			// log.Printf("PID %s is a clone of %s", mf.PID, mfData.ClonedFrom.PID)
			mf.PID = mfData.ClonedFrom.PID
		}
		mf.Width = mfData.Width
		mf.Height = mfData.Height
		mf.Title = mfData.Title
		mf.Description = mfData.Description

		if mfData.Orientation == "flip_y_axis" {
			mf.Rotation = "!0"
		} else if mfData.Orientation == "rotate90" {
			mf.Rotation = "90"
		} else if mfData.Orientation == "rotate180" {
			mf.Rotation = "180"
		} else if mfData.Orientation == "rotate270" {
			mf.Rotation = "270"
		} else {
			mf.Rotation = "0"
		}

		data.MasterFiles = append(data.MasterFiles, mf)

		// // if exemplar is set, set the current page num as the start canvas
		if mfData.Exemplar {
			data.StartPage = pgNum
			data.ExemplarPID = mf.PID
			data.ExemplarRotation = mf.Rotation
			log.Printf("INFO: exemplar set to PID %s, page %d", mf.PID, data.StartPage)
		}
		pgNum++
	}
	return nil
}

//
// end of file
//

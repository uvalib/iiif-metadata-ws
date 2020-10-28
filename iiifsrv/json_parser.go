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
		case "callNumber":
			callNum := child["value"].(string)
			log.Printf("INFO: callNumber: %s", callNum)
			data.Metadata["Call Number"] = callNum
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
func getMasterFilesFromJSON(data *IIIF, jsonStr string) {
	var jsonArray []interface{}
	json.Unmarshal([]byte(jsonStr), &jsonArray)
	pgNum := 0
	for _, mfInterface := range jsonArray {
		// Extract: pid, filename, width, height, title, description (optional)
		mfJSON := mfInterface.(map[string]interface{})
		var mf MasterFile
		mf.PID = mfJSON["pid"].(string)
		mf.Width = int(mfJSON["width"].(float64))
		mf.Height = int(mfJSON["height"].(float64))
		if title, ok := mfJSON["title"]; ok {
			mf.Title = cleanString(title.(string))
		}
		if desc, ok := mfJSON["description"]; ok {
			mf.Description = cleanString(desc.(string))
		}
		if orientation, ok := mfJSON["orientation"]; ok {
			axis := orientation.(string)
			if axis == "flip_y_axis" {
				mf.Rotation = "!0"
			} else if axis == "rotate90" {
				mf.Rotation = "90"
			} else if axis == "rotate180" {
				mf.Rotation = "180"
			} else if axis == "rotate270" {
				mf.Rotation = "270"
			} else {
				mf.Rotation = "0"
			}
		} else {
			mf.Rotation = "0"
		}
		data.MasterFiles = append(data.MasterFiles, mf)

		// if exemplar is set, see if it matches the current master file filename
		// if it does, set the current page num as the start canvas
		filename := mfJSON["filename"].(string)
		if mfJSON["exemplar"] != nil {
			data.StartPage = pgNum
			data.ExemplarPID = mf.PID
			data.ExemplarRotation = mf.Rotation
			log.Printf("INFO: exemplar set to filename %s, page %d", filename, data.StartPage)
		}
		pgNum++
	}
}

//
// end of file
//

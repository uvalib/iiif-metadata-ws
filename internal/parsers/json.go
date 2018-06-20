package parsers

import (
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/uvalib/iiif-metadata-ws/internal/models"
)

// GetMetadataFromJSON parses basic IIIF Metadata from an Apollo JSON API response
func GetMetadataFromJSON(data *models.IIIF, jsonStr string) error {
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
		return errors.New("Unable to parse 'collection' from response")
	}

	children, ok := collection["children"].([]interface{})
	if !ok {
		return errors.New("Unable to parse 'children' from collection response")
	}

	for _, c := range children {
		// c is just returned as a generic interface{}.
		// Cast it to a json model of key->values
		child := c.(map[string]interface{})

		// get the Name data for this node
		typeAttr, okName := child["type"].(map[string]interface{})
		if !okName {
			return errors.New("Unable to parse 'type' from response")
		}

		// all names have a value; pick the ones we want ang get the
		// value attribute from the child, Store it in the IIIF data
		switch val := typeAttr["name"].(string); val {
		case "title":
			log.Printf("title:%s", child["value"].(string))
			data.Title = models.CleanString(child["value"].(string))
		case "catalogKey":
			catalogKey := child["value"].(string)
			log.Printf("catalogKey: %s", catalogKey)
			data.VirgoKey = catalogKey
		case "callNumber":
			callNum := child["value"].(string)
			log.Printf("callNumber: %s", callNum)
			data.Metadata["Call Number"] = callNum
		case "useRights":
			log.Printf("license: %s", child["valueURI"].(string))
			data.License = child["valueURI"].(string)
		}
	}

	// Add item-level title to the main title
	item, ok := jsonMap["item"].(map[string]interface{})
	if !ok {
		return errors.New("Unable to parse 'item' from response")
	}
	children, oK := item["children"].([]interface{})
	if !oK {
		return errors.New("Unable to parse 'children' from item response")
	}
	for _, c := range children {
		child := c.(map[string]interface{})
		typeAttr := child["type"].(map[string]interface{})
		val := typeAttr["name"].(string)
		if strings.Compare(val, "title") == 0 {
			data.Title = models.CleanString(data.Title + ": " + child["value"].(string))
		}
	}
	return nil
}

// GetMasterFilesFromJSON parses basic IIIF Metadata from an Apollo JSON API response
func GetMasterFilesFromJSON(data *models.IIIF, jsonStr string) {
	var jsonArray []interface{}
	json.Unmarshal([]byte(jsonStr), &jsonArray)
	pgNum := 0
	for _, mfInterface := range jsonArray {
		// Extract: pid, filename, width, height, title, description (optional)
		mfJSON := mfInterface.(map[string]interface{})
		var mf models.MasterFile
		mf.PID = mfJSON["pid"].(string)
		mf.Width = int(mfJSON["width"].(float64))
		mf.Height = int(mfJSON["height"].(float64))
		if title, ok := mfJSON["title"]; ok {
			mf.Title = models.CleanString(title.(string))
		}
		if desc, ok := mfJSON["description"]; ok {
			mf.Description = models.CleanString(desc.(string))
		}
		data.MasterFiles = append(data.MasterFiles, mf)

		// if exemplar is set, see if it matches the current master file filename
		// if it does, set the current page num as the start canvas
		filename := mfJSON["filename"].(string)
		if mfJSON["exemplar"] != nil {
			data.StartPage = pgNum
			data.ExemplarPID = mf.PID
			log.Printf("Exemplar set to filename %s, page %d", filename, data.StartPage)
		}
		pgNum++
	}
}

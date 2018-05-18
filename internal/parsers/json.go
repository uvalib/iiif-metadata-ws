package parsers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/uvalib/iiif-metadata-ws/internal/models"
)

// GetMetadataFromJSON parses basic IIIF Metadata from an Apollo JSON API response
func GetMetadataFromJSON(data *models.IIIF, jsonStr string) error {
	// All of the data we care about is under the array of children
	// found in the item key in the response. Unmarshall the string
	// into arbitrary map and find the stuff needed:
	//      title, catalogKey, callNumber, useRights
	var jsonMap map[string]interface{}
	json.Unmarshal([]byte(jsonStr), &jsonMap)

	// NOTE: this is a type assertion. Basically a cast from
	// the general interface{} to a specific type. As written below
	// this will panic of the data is not found. Need to re-do like:
	//      item, ok := jsonMap["item"].(map[string]interface{})
	// and check the OK for errors
	item, ok := jsonMap["item"].(map[string]interface{})
	if !ok {
		return errors.New("Unable to parse 'item' from response")
	}

	children, ok := item["children"].([]interface{})
	if !ok {
		return errors.New("Unable to parse 'children' from response")
	}

	for _, c := range children {
		// c is just returned as a generic interface{}.
		// Cast it to a json model of key->values
		child := c.(map[string]interface{})

		// get the Name data for this node
		nameAttr, ok := child["name"].(map[string]interface{})
		if !ok {
			return errors.New("Unable to parse 'name' from response")
		}

		// all names have a value; pick the ones we want ang get the
		// value attribute from the child, Store it in the IIIF data
		switch val := nameAttr["value"].(string); val {
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
			data.Metadata = append(data.Metadata, models.Metadata{"Call Number", callNum})
		case "useRights":
			log.Printf("license: %s", child["valueURI"].(string))
			data.License = child["valueURI"].(string)
		}
	}
	return nil
}

// GetMasterFilesFromJSON parses basic masmter file Metadata from a tracksys JSON API response
func GetMasterFilesFromJSON(data *models.IIIF, jsonStr string) error {
	fmt.Printf("%s", jsonStr)
	return nil
}

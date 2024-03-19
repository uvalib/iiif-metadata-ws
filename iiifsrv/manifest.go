package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
)

func (svc *ServiceContext) manifestExist(c *gin.Context) {
	pid := c.Param("pid")
	key := s3Key(pid)

	if svc.keyExist(key) {
		cacheURL := fmt.Sprintf("%s/%s/%s", svc.ManifestURL, svc.ManifestBucket, key)
		c.JSON(http.StatusOK, gin.H{
			"exists": true,
			"cached": true,
			"url":    cacheURL,
		})
		return
	}

	log.Printf("INFO: check if pid %s exists in tracksys", pid)
	pidURL := fmt.Sprintf("%s/api/pid/%s/type", svc.TracksysURL, pid)
	_, err := svc.getAPIResponse(pidURL)
	if err != nil {
		log.Printf("ERROR: check for %s in tracksys failed: %d:%s", pid, err.StatusCode, err.Message)
		c.JSON(http.StatusOK, gin.H{
			"exists": false,
			"cached": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"exists": true,
		"cached": false,
	})
}

func (svc *ServiceContext) keyExist(key string) bool {
	log.Printf("INFO: check cache for %s", key)
	listResp, err := svc.S3Client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket:  aws.String(svc.ManifestBucket),
		Prefix:  aws.String(key),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		log.Printf("ERROR: check for %s failed: %s", key, err.Error())
		return false
	}
	if *listResp.KeyCount == 0 {
		log.Printf("INFO: %s not found in cache", key)
		return false
	}
	log.Printf("INFO: %s found in cache", key)
	return true
}

func (svc *ServiceContext) getManifest(c *gin.Context) {
	pid := c.Param("pid")
	key := s3Key(pid)
	keyExists := svc.keyExist(key)
	unit := c.Query("unit")
	nocache, _ := strconv.ParseBool(c.Query("nocache"))
	refresh, _ := strconv.ParseBool(c.Query("refresh"))

	// only read from the cache if these conditions are all true
	cacheRead := nocache == false && unit == "" && refresh == false

	// if the manifest is in the cache and cache reading is available...
	if cacheRead && keyExists == true {
		manifest, err := svc.readManifestFromCache(key)
		if err != nil {
			log.Printf("ERROR: get manifest from cache failed: %s", err.Error())
			c.String(http.StatusInternalServerError, err.Error())
		} else {
			c.Header("content-type", "application/json; charset=utf-8")
			c.Header("Cache-Control", "no-store")
			c.String(http.StatusOK, manifest)
		}
		return
	}

	// Get data for all master files from units associated with the metadata record. Include unit if specified
	log.Printf("Generate new IIIF manifest for %s", pid)
	unitID, _ := strconv.Atoi(unit)
	tsURL := fmt.Sprintf("%s/api/manifest/%s", svc.TracksysURL, pid)
	if unitID > 0 {
		tsURL = fmt.Sprintf("%s?unit=%d", tsURL, unitID)
	}
	respBytes, respErr := svc.getAPIResponse(tsURL)
	if respErr != nil {
		log.Printf("ERROR: tracksys manifest request failed: %d:%s", respErr.StatusCode, respErr.Message)
		c.String(respErr.StatusCode, respErr.Message)
		return
	}

	iiifData := IIIF{
		IIIFServerURL: svc.IIIFServerURL,
		URL:           fmt.Sprintf("%s/%s/%s", svc.ManifestURL, svc.ManifestBucket, key),
		MetadataPID:   pid,
	}

	err := parseTrackSysManifest(&iiifData, string(respBytes))
	if err != nil {
		log.Printf("ERROR: Unable to parse masterfiles manifest data %s: %s", pid, err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	manifestStr, err := svc.renderIIIF(iiifData)
	if err != nil {
		log.Printf("ERROR: render iiif manifest for %s failed: %s", iiifData.MetadataPID, err.Error())
		c.String(http.StatusInternalServerError, fmt.Sprintf("Unable to render IIIF metadata: %s", err.Error()))
		return
	}

	// only write to the cache if these conditions are all true
	cacheWrite := nocache == false && unit == "" && (refresh == true || keyExists == false)
	if cacheWrite {
		err := svc.writeManifestToCache(key, manifestStr)
		if err != nil {
			// this is not fatal... just log the error
			log.Printf("ERROR: Unable to write pid %s manifest to cache: %s", pid, err.Error())
		}
	}

	c.Header("content-type", "application/json; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.String(http.StatusOK, manifestStr)
}

func (svc *ServiceContext) readManifestFromCache(key string) (string, error) {
	log.Printf("INFO: load iiif manifest %s from cache", key)
	resp, err := svc.S3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(svc.ManifestBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("get %s from cache failed: %s", key, err.Error())
	}

	defer resp.Body.Close()
	manifestBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read %s from cache failed: %s", key, err.Error())
	}
	return string(manifestBytes), nil
}

func (svc *ServiceContext) writeManifestToCache(key string, manifest string) error {
	log.Printf("INFO: write manifest to cache %s", key)
	manifestReader := strings.NewReader(manifest)
	_, err := svc.S3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(svc.ManifestBucket),
		Key:    aws.String(key),
		Body:   manifestReader,
	})
	return err
}

func (svc *ServiceContext) renderIIIF(iiifData IIIF) (string, error) {
	var outBuffer bytes.Buffer
	err := svc.IIIFTemplate.Execute(&outBuffer, iiifData)
	if err != nil {
		return "", err
	}
	log.Printf("INFO: IIIF Metadata generated for %s", iiifData.MetadataPID)
	return outBuffer.String(), nil
}

func parseTrackSysManifest(iiifData *IIIF, jsonStr string) error {
	var parsedManifest []ManifestData
	err := json.Unmarshal([]byte(jsonStr), &parsedManifest)
	if err != nil {
		return err
	}

	pgNum := 0
	for _, mfData := range parsedManifest {
		var mf MasterFile
		mf.PID = mfData.PID
		if mfData.ClonedFrom != nil && mfData.ClonedFrom.PID != "" {
			log.Printf("PID %s is a clone of %s", mf.PID, mfData.ClonedFrom.PID)
			mf.PID = mfData.ClonedFrom.PID
		}
		mf.Width = mfData.Width
		mf.Height = mfData.Height
		mf.Title = jsonEscape(mfData.Title)
		mf.Description = jsonEscape(mfData.Description)

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

		iiifData.MasterFiles = append(iiifData.MasterFiles, mf)

		// // if exemplar is set, set the current page num as the start canvas
		if mfData.Exemplar {
			iiifData.StartPage = pgNum
			iiifData.ExemplarPID = mf.PID
			iiifData.ExemplarRotation = mf.Rotation
			log.Printf("INFO: exemplar set to PID %s, page %d", mf.PID, iiifData.StartPage)
		}
		pgNum++
	}
	return nil
}

func jsonEscape(raw string) string {
	b, err := json.Marshal(raw)
	if err != nil {
		log.Printf("ERROR: unable to escape [%s] for json: %s", raw, err)
		return raw
	}
	return string(b[1 : len(b)-1])
}

func s3Key(pid string) string {
	// cleanup any special characters
	name := fmt.Sprintf("pid-%s", pid)
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	return name
}

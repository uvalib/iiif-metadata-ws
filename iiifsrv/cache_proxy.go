package main

import (
	"bytes"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

// CacheProxy contains methods for accessing the cache
type CacheProxy struct {
	config     *serviceConfig
	service    *s3.S3
	uploader   *s3manager.Uploader
	downloader *s3manager.Downloader
}

// NewCacheProxy sets up a new S3 session
func NewCacheProxy(cfg *serviceConfig) *CacheProxy {

	proxy := CacheProxy{}
	proxy.config = cfg
	sess, err := session.NewSession()
	if err == nil {
		proxy.service = s3.New(sess)
		proxy.uploader = s3manager.NewUploader(sess)
		proxy.downloader = s3manager.NewDownloader(sess)
	}

	return &proxy
}

// IsInCache identifies if the specified key is in the cache
func (cp *CacheProxy) IsInCache(key string) bool {

	sourcename := fmt.Sprintf("s3:/%s/%s", cp.config.cacheBucket, key)
	log.Printf("INFO: checking for %s", sourcename)

	listParams := s3.ListObjectsV2Input{
		Bucket:  &cp.config.cacheBucket,
		Prefix:  &key,
		MaxKeys: aws.Int64(1),
	}

	start := time.Now()
	results, err := cp.service.ListObjectsV2(&listParams)
	duration := time.Since(start)
	if err != nil {
		log.Printf("ERROR: checking for %s (%s)", sourcename, err.Error())
		return false
	}

	if len(results.Contents) == 0 {
		log.Printf("INFO: %s does not exist (duration %0.2f seconds)", sourcename, duration.Seconds())
		return false
	}
	log.Printf("INFO: %s exists (duration %0.2f seconds)", sourcename, duration.Seconds())
	return true
}

// ReadFromCache reads the contents of the specified cache element
func (cp *CacheProxy) ReadFromCache(key string) (string, error) {

	sourcename := fmt.Sprintf("s3:/%s/%s", cp.config.cacheBucket, key)
	log.Printf("INFO: downloading from %s", sourcename)

	downParams := s3.GetObjectInput{
		Bucket: &cp.config.cacheBucket,
		Key:    &key,
	}

	buffer := &aws.WriteAtBuffer{}
	start := time.Now()
	fileSize, err := cp.downloader.Download(buffer, &downParams)

	if err != nil {
		log.Printf("ERROR: downloading from %s (%s)", sourcename, err.Error())
		return "", err
	}

	duration := time.Since(start)
	log.Printf("Download of %s complete in %0.2f seconds (%d bytes)", sourcename, duration.Seconds(), fileSize)
	return string(buffer.Bytes()), nil
}

// WriteToCache writes the contents of the specified cache element
func (cp *CacheProxy) WriteToCache(key string, content string) error {

	contentSize := len(content)
	destname := fmt.Sprintf("s3://%s/%s", cp.config.cacheBucket, key)
	log.Printf("INFO: uploading to %s (%d bytes)", destname, contentSize)

	upParams := s3manager.UploadInput{
		Bucket: &cp.config.cacheBucket,
		Key:    &key,
		Body:   bytes.NewReader([]byte(content)),
	}

	// Perform an upload.
	start := time.Now()
	_, err := cp.uploader.Upload(&upParams)
	if err != nil {
		log.Printf("ERROR: uploading to %s (%s)", destname, err.Error())
		return err
	}

	duration := time.Since(start)
	log.Printf("INFO: upload of %s complete in %0.2f seconds", destname, duration.Seconds())

	return nil
}

//
// end of file
//

#!/bin/bash
env GOOS=linux go build -o dist/iiif-metadata-ws.linux
cp iiif.json dist/
cp config.yml.template dist/

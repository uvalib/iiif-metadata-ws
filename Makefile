GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOVET=$(GOCMD) vet

# project specific definitions
BASE_NAME=iiif-metadata-ws
SRC_TREE=cmd/iiifsrv

build: darwin linux copy-supporting

all: darwin linux copy-supporting

darwin:
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -a -o bin/$(BASE_NAME).darwin $(SRC_TREE)/*.go

copy-supporting:
	cp -R templates bin/templates/

linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -a -installsuffix cgo -o bin/$(BASE_NAME).linux $(SRC_TREE)/*.go

vet:
	$(GOVET) $(SRC_TREE)/*

clean:
	$(GOCLEAN)
	rm -rf bin/

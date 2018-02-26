GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
GOFMT=$(GOCMD) fmt
GOGET=$(GOCMD) get

# project specific definitions
BASE_NAME=iiif-metadata-ws
SRC_TREE=cmd/iiifsrv

build: build-darwin build-linux copy-supporting

all: deps build-darwin build-linux copy-supporting

build-darwin:
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -a -o bin/$(BASE_NAME).darwin $(SRC_TREE)/*.go

copy-supporting:
	cp -R templates bin/templates/

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -a -installsuffix cgo -o bin/$(BASE_NAME).linux $(SRC_TREE)/*.go

fmt:
	$(GOFMT) $(SRC_TREE)/*

vet:
	$(GOVET) $(SRC_TREE)/*

clean:
	$(GOCLEAN)
	rm -rf $(BIN)

deps:
	dep ensure
	dep status

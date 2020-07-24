GOCMD = go
GOBUILD = $(GOCMD) build
GOCLEAN = $(GOCMD) clean
GOGET = $(GOCMD) get
GOVET = $(GOCMD) vet
GOFMT = $(GOCMD) fmt
GOMOD = $(GOCMD) mod

# project specific definitions
BASE_NAME=iiif-metadata-ws
SRC_TREE=iiifsrv

build: darwin linux copy-supporting

linux-full: linux copy-supporting

all: darwin linux copy-supporting

darwin:
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -a -o bin/$(BASE_NAME).darwin $(SRC_TREE)/*.go

copy-supporting:
	cp -R  templates/ bin/templates

linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -a -installsuffix cgo -o bin/$(BASE_NAME).linux $(SRC_TREE)/*.go

clean:
	$(GOCLEAN)
	rm -rf bin/

dep:
	cd $(SRC_TREE); $(GOGET) -u
	$(GOMOD) tidy
	$(GOMOD) verify

fmt:
	cd $(SRC_TREE); $(GOFMT)

vet:
	cd $(SRC_TREE); $(GOVET)

check:
	go install honnef.co/go/tools/cmd/staticcheck
	$(HOME)/go/bin/staticcheck -checks all,-S1002,-ST1003,-S1008 $(SRC_TREE)/*.go
	go install golang.org/x/tools/go/analysis/passes/shadow/cmd/shadow
	$(GOVET) -vettool=$(HOME)/go/bin/shadow ./$(SRC_TREE)/...

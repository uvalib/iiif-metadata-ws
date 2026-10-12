# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

A Go (gin) web service that calls the TrackSys API to generate IIIF Presentation API 2.x manifests for a PID (a metadata or component record), caching the rendered manifests in S3.

## Commands

- `make build`: builds darwin and linux binaries into `bin/` and copies `templates/` to `bin/templates`
- `make linux` / `make darwin`: build for a single platform (darwin builds use `-race`)
- `make vet`, `make fmt`
- `make check`: installs and runs `staticcheck` (`-checks all,-S1002,-ST1003,-S1008`) and the `shadow` vet tool
- `make dep`: upgrades dependencies, then runs `go mod tidy` and `go mod verify`

The repo has no tests.

### Running locally

The service loads `./templates/iiif.json` relative to the working directory, so start it from a directory that contains `templates/` (for example `cd bin; ./iiif-metadata-ws.darwin ...`). It needs AWS credentials from the default credential chain at startup, because the S3 client is created during init. Flags: `-port` (8080), `-tracksys`, `-host`, `-iiif`, `-bucket`, `-rooturl`. Defaults point at production (see `iiifsrv/config.go`).

## Architecture

All code lives in `package main` under `iiifsrv/`:

- `main.go`: route setup and the `version` constant. Bump the version when you change behavior.
- `service.go`: `ServiceContext` (config, S3 client, a shared HTTP client with a 15s timeout, the parsed template), utility handlers, and `getAPIResponse`/`handleAPIResponse`. These turn HTTP and network failures into a `RequestError{StatusCode, Message}`.
- `manifest.go`: the core manifest logic.
- `models.go`: TrackSys response structs and the `IIIF` struct passed to the template.

### Manifest generation flow (`getManifest`)

1. Builds the S3 cache key with `s3Key(pid)`: `pid-<pid>`, with `/` and `:` replaced by `-`.
2. Cache rules: a `?unit=` request is never read from or written to the cache. `?nocache=true` skips the cache entirely. `?refresh=true` regenerates the manifest and overwrites the cached copy.
3. On a cache miss, it fetches `GET {tracksys}/api/manifest/{pid}[?unit=N]`, which returns an array of master files. `parseTrackSysManifest` maps each one to a `MasterFile`. It substitutes the `cloned_from` PID for cloned files, maps `orientation` to an IIIF rotation (`flip_y_axis` → `!0`, `rotate90` → `90`, and so on), and uses the `exemplar` file as the start canvas and thumbnail.
4. It fetches title and content advisory from `GET {tracksys}/api/pid/{pid}`. If this call fails, the manifest is still generated without them.
5. It renders `templates/iiif.json` with `text/template`, not `html/template`. String values must therefore be escaped for JSON with `jsonEscape` before they are placed in the `IIIF` struct.

`/pid/:pid/exist` checks the S3 cache first and then falls back to `GET {tracksys}/api/pid/{pid}/type`.

### Logging convention

Log lines use the prefixes `INFO:`, `WARNING:`, and `ERROR:`. A 404 from TrackSys is normal operation and is logged as `WARNING`, not `ERROR`. Keep this distinction; recent commits made it on purpose.

## Packaging and deployment

- `package/Dockerfile`: multi-stage Alpine build running `make linux`. The binary is installed as `bin/iiifsrv`, with the templates in `bin/templates`. `package/scripts/entry.sh` runs it from `bin/` and maps env vars (`TRACKSYS_URL`, `IIIF_MANIFEST_HOST`, `IIIF_URL`, `IIIF_CACHE_BUCKET`, `IIIF_CACHE_ROOT_URL`) to flags.
- The `/version` build number comes from a `../buildtag.build-<N>` file that the Dockerfile creates from the `BUILD_TAG` build arg.
- `pipeline/buildspec.yml` (AWS CodeBuild) builds the image and pushes it to ECR, then records the tag in SSM at `/containers/$CONTAINER_IMAGE/latest`. `pipeline/deployspec.yml` applies the staging terraform from `uvalib/terraform-infrastructure` (`iiif-manifest.lib.virginia.edu/staging`) and then waits for the new version with `wait_for_version.sh`.
- When you change the Go version, update both the `go.mod` `go` directive and the builder image tag in the Dockerfile.

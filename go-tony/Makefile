SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Go binary builds support both linux and darwin
OSES = linux darwin
ARCHES = amd64 arm64

# Docker builds only support linux (Alpine is Linux-only)
DOCKER_OSES = linux
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

SHORTCOMMIT ?= $(shell git rev-parse --short HEAD)
# Sanitize VERSION: replace / with - for Docker tag compatibility
VERSION ?= $(shell git describe --abbrev=0 --always | tr '/' '-')

IMAGE_SUFFIX ?= -test
IMAGE_TAG ?= $(SHORTCOMMIT)-$(GOOS)-$(GOARCH)

IMAGE_NAME ?= ghcr.io/signadot/tony
DIST_DIR ?= dist

GO ?= go
DOCKER ?= docker

MODULE_ROOT := $(shell git rev-parse --show-toplevel)

# Build
# -----------------------------------------------------------------------------

build-o:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -o $(DIST_DIR)/$(GOOS)/$(GOARCH)/o $(MODULE_ROOT)/go-tony/cmd/o

build-convert-image:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -o $(DIST_DIR)/$(GOOS)/$(GOARCH)/convert-image $(MODULE_ROOT)/go-tony/cmd/convert-image

build-os:
	for os in $(OSES); do \
		for arch in $(ARCHES); do \
			GOOS=$$os GOARCH=$$arch $(MAKE) build-o; \
		done; \
	done
	
build-convert-images:
	for os in $(OSES); do \
		for arch in $(ARCHES); do \
			GOOS=$$os GOARCH=$$arch $(MAKE) build-convert-image; \
		done; \
	done

# Docker
# -----------------------------------------------------------------------------

build-docker-o: build-o build-convert-image
	$(DOCKER) buildx build \
		--platform $(GOOS)/$(GOARCH) \
		--build-arg TARGETPLATFORM=$(GOOS)/$(GOARCH) \
		--build-arg GOOS=$(GOOS) \
		--build-arg GOARCH=$(GOARCH) \
		--provenance=false \
		-f Dockerfile \
		--load \
		-t $(IMAGE_NAME)$(IMAGE_SUFFIX):$(SHORTCOMMIT)-$(GOOS)-$(GOARCH) \
		$(DIST_DIR)

build-dockers-o:
	for os in $(DOCKER_OSES); do \
		for arch in $(ARCHES); do \
			GOOS=$$os GOARCH=$$arch $(MAKE) build-docker-o; \
		done; \
	done

push-dockers-o: build-dockers-o
	for os in $(DOCKER_OSES); do \
		for arch in $(ARCHES); do \
			$(DOCKER) push $(IMAGE_NAME)$(IMAGE_SUFFIX):$(SHORTCOMMIT)-$$os-$$arch; \
		done; \
	done
	$(DOCKER) manifest create --amend $(IMAGE_NAME)$(IMAGE_SUFFIX):$(VERSION) \
		$(foreach os,$(DOCKER_OSES),$(foreach arch,$(ARCHES),$(IMAGE_NAME)$(IMAGE_SUFFIX):$(SHORTCOMMIT)-$(os)-$(arch)))
	$(DOCKER) manifest create --amend $(IMAGE_NAME)$(IMAGE_SUFFIX):latest \
		$(foreach os,$(DOCKER_OSES),$(foreach arch,$(ARCHES),$(IMAGE_NAME)$(IMAGE_SUFFIX):$(SHORTCOMMIT)-$(os)-$(arch)))
	$(DOCKER) manifest push $(IMAGE_NAME)$(IMAGE_SUFFIX):$(VERSION)
	$(DOCKER) manifest push $(IMAGE_NAME)$(IMAGE_SUFFIX):latest

# Convenience targets
# -----------------------------------------------------------------------------

.PHONY: build build-docker build-dockers push-dockers clean

build: build-o
build-docker: build-docker-o
build-dockers: build-dockers-o
push-dockers: push-dockers-o

clean:
	rm -rf $(DIST_DIR)

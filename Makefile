# Set Shell to bash, otherwise some targets fail with dash/zsh etc.
SHELL := /bin/bash

# Disable built-in rules
MAKEFLAGS += --no-builtin-rules
MAKEFLAGS += --no-builtin-variables
.SUFFIXES:
.SECONDARY:
.DEFAULT_TARGET: help

include Makefile.vars.mk

go_build ?= go build -o $(BIN_FILENAME) ./...


.PHONY: help
help: ## Show this help
	@grep -E -h '\s##\s' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = "(: ).*?## "}; {gsub(/\\:/,":", $$1)}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code
	go vet ./...

.PHONY: lint
lint: fmt vet ## Invokes the fmt and vet targets
	@echo 'Check for uncommitted changes ...'
	git diff --exit-code

.PHONY: build
build: build\:docker ## All-in-one build target

.PHONY: build\:go
build\:go: fmt vet $(BIN_FILENAME) ## Build the binary

.PHONY: build\:docker
build\:docker: build\:go ## Build the docker image
	docker build -t $(QUAY_IMG) .

.PHONY: test
test: test\:go ## All-in-one test target

.PHONY: test\:go
test\:go: ## Run unit tests
	go test ./... -coverprofile cover.out

test-e2e: build\:go
	go test -tags=e2e -count=1

clean: ## Cleans up the generated resources
	rm -rf dist/ cover.out $(BIN_FILENAME)

###
### Assets
###

# Build the binary without running generators
.PHONY: $(BIN_FILENAME)
$(BIN_FILENAME): export CGO_ENABLED = 0
$(BIN_FILENAME):
	$(go_build)

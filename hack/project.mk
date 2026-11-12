SHELL := /bin/bash -eu -o pipefail
PROJECT := apilot
ROOT := github.com/alswl/apilot
CMD_DIR := ./cmd
OUTPUT_DIR := ./bin
BUILD_DIR := ./build
TARGETS ?= apilot apilot-app
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
VERSION ?= $(shell python3 -c 'import json; print(json.load(open("packaging/mygo.json"))["version"])')
export GOTOOLCHAIN := go1.27.3
export CGO_ENABLED := 1
export GOFLAGS ?= -mod=readonly -trimpath

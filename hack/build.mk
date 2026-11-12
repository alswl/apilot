include hack/project.mk
.DEFAULT_GOAL := build
include hack/makefile-go/build.mk
build: prepare
.PHONY: prepare
prepare:
	mkdir -p $(OUTPUT_DIR)

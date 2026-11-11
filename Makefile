GO ?= go
export GOTOOLCHAIN := go1.27.3
export CGO_ENABLED := 1

.PHONY: test race cli build app verify-ui verify-lifecycle verify

test:
	$(GO) test ./...
race:
	$(GO) test -race ./...
cli:
	mkdir -p bin
	$(GO) build -o bin/apilot ./cmd/apilot
build: cli
	$(GO) build -o bin/apilot-app ./cmd/apilot-app
app:
	$(GO) tool mygo build -skip-dmg -skip-notarize
verify-ui: app
	data_dir=$$(mktemp -d); trap 'rm -rf "$$data_dir"' EXIT; APILOT_DATA_DIR="$$data_dir" build/darwin-arm64/apilot.app/Contents/MacOS/apilot --verify-lifecycle

verify-lifecycle: build app
	APILOT_VERIFY_APP="$(CURDIR)/build/darwin-arm64/apilot.app/Contents/MacOS/apilot" scripts/verify-lifecycle.sh
verify: test race verify-lifecycle

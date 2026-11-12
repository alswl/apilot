include hack/project.mk
.DEFAULT_GOAL := build
include hack/makefile-go/general.mk

.PHONY: build cli download clean check check-format fmt vet check-version check-architecture check-docs lint test test-core race race-core race-ui app icons verify-ui verify-lifecycle verify verify-app-path
.NOTPARALLEL: verify race

##@ Checks
check: check-format vet check-version check-architecture check-docs ## Run read-only checks
lint: check ## Run read-only checks
check-format: ## Check first-party Go formatting
	python3 scripts/check-source.py format
fmt: ## Format first-party Go source
	python3 scripts/check-source.py fmt
vet: ## Run Go static analysis
	go vet ./...
check-version: ## Check App/CLI source versions
	python3 scripts/check-source.py version
check-architecture: ## Check logic and test dependencies contain no MyGo
	scripts/check-architecture.sh
check-docs: ## Check local documentation links and pinned standards
	python3 scripts/check-docs.py

TEST_FLAGS ?=

##@ Tests (macOS; core requires no desktop session)
test: ## Run all Go tests
	go test $(TEST_FLAGS) ./...
test-core: ## Run logic and real-resource integration tests without MyGo
	go test $(TEST_FLAGS) $$(scripts/check-architecture.sh --core-packages)
race-core: ## Run logic tests with race detection
	go test -race -vet=off $(TEST_FLAGS) $$(scripts/check-architecture.sh --core-packages)
race-ui: ## Run MyGo presentation tests with race detection
	go test -race -vet=off $(TEST_FLAGS) $$(scripts/check-architecture.sh --ui-packages)
race: ## Run the complete suite once, with Go scheduling packages concurrently
	go test -race -vet=off $(TEST_FLAGS) ./...

##@ Build (macOS + Xcode Command Line Tools)
build: check-version ## Build both binaries with the shared toolkit
	$(MAKE) -f hack/build.mk build
cli: check-version ## Build the standalone CLI with the shared toolkit
	$(MAKE) -f hack/build.mk build TARGETS=apilot
download: ## Download Go dependencies
	$(MAKE) -f hack/build.mk download
clean: ## Remove generated binaries only
	$(MAKE) -f hack/build.mk clean
MYGO_BUILD_FLAGS ?= -skip-notarize
app: cli ## Build ad-hoc App and DMG without release credentials
	mkdir -p resources/darwin/bin
	cp bin/apilot resources/darwin/bin/apilot
	go tool mygo build $(MYGO_BUILD_FLAGS)
	python3 scripts/verify-built-app.py --record
icons: ## Render committed brand masters
	go run packaging/render-icons.go

##@ Platform verification (requires a desktop session)
APILOT_VERIFY_APP ?= $(CURDIR)/build/darwin-arm64/Apilot.app/Contents/MacOS/Apilot
ifneq ($(VERIFY_SKIP_BUILD),1)
verify-app-path: app
endif
verify-app-path:
	APILOT_VERIFY_APP="$(APILOT_VERIFY_APP)" python3 scripts/verify-built-app.py
verify-ui: verify-app-path ## Run the packaged native UI probe with disposable data
	data_dir=$$(mktemp -d); trap 'rm -rf "$$data_dir"' EXIT; APILOT_DATA_DIR="$$data_dir" "$(APILOT_VERIFY_APP)" --verify-lifecycle
verify-lifecycle: verify-app-path ## Verify this bundle's cleanup and restart behavior
	APILOT_VERIFY_APP="$(APILOT_VERIFY_APP)" scripts/verify-lifecycle.sh
verify: check race app ## Check and verify one built bundle without duplicate tests/builds
	$(MAKE) VERIFY_SKIP_BUILD=1 verify-ui verify-lifecycle

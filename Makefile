include hack/project.mk
.DEFAULT_GOAL := build
include hack/makefile-go/general.mk

.PHONY: test-connectors generate-openapi build cli download clean check check-format fmt vet check-version check-architecture check-docs check-workbench check-openapi check-portable check-app-platform lint test test-core race race-core race-ui app icons verify-ui verify-lifecycle verify verify-app-path
.NOTPARALLEL: verify race

##@ Checks
check: check-format vet check-version check-architecture check-docs check-workbench check-openapi check-portable ## Run read-only checks
lint: check ## Run read-only checks
check-format: ## Check first-party Go formatting
	python3 scripts/check-source.py format
fmt: ## Format first-party Go source
	python3 scripts/check-source.py fmt
vet: ## Run Go static analysis
	go -C core vet ./...
	go -C desktop vet ./...
check-version: ## Check App/CLI source versions
	python3 scripts/check-source.py version
check-architecture: ## Check logic and test dependencies contain no MyGo
	scripts/check-architecture.sh
check-docs: ## Check local documentation links and pinned standards
	python3 scripts/check-docs.py
check-workbench: ## Check workbench data, launch docs, UI literals, keybindings and empty-state guidance
	python3 scripts/check-workbench.py
	go -C desktop tool mygo i18n check
	go -C desktop test ./internal/ui/workbench -run 'Test(DocumentedKeybindingProfiles|WorkbenchUILiterals|EmptyFrameworkNavigationAndConnectorScaffolds)$$'
generate-openapi: ## Export the Huma contract and regenerate the Workbench client
	go -C core run ./cmd/apilot-openapi > api/openapi.yaml.next
	mv api/openapi.yaml.next api/openapi.yaml
	cd core && go tool oapi-codegen -generate types,client -package integrations -o pkg/integrations/workbench_client.gen.go ../api/openapi.yaml

check-openapi: ## Verify the Workbench contract and generated client
	@set -e; temp_dir=$$(mktemp -d); trap 'rm -rf "$$temp_dir"' EXIT; \
	  go -C core run ./cmd/apilot-openapi > "$$temp_dir/openapi.yaml"; \
	  diff -u api/openapi.yaml "$$temp_dir/openapi.yaml"; \
	  (cd core && go tool oapi-codegen -generate types,client -package integrations -o "$$temp_dir/workbench_client.gen.go" "$$temp_dir/openapi.yaml"); \
	  diff -u core/pkg/integrations/workbench_client.gen.go "$$temp_dir/workbench_client.gen.go"

check-portable: ## Cross-compile shared packages for Linux and Windows
	bash scripts/check-portable.sh

TEST_FLAGS ?=

##@ Tests (macOS; core requires no desktop session)
test: ## Run all Go tests
	go -C core test $(TEST_FLAGS) ./...
	go -C desktop test $(TEST_FLAGS) ./...
test-connectors: ## Test independent Connector modules
	go -C connectors/protocol test $(TEST_FLAGS) ./...
	go -C connectors/github-issues test $(TEST_FLAGS) ./...

test-core: ## Run logic and real-resource integration tests without MyGo
	go -C core test $(TEST_FLAGS) ./...
race-core: ## Run logic tests with race detection
	go -C core test -race -vet=off $(TEST_FLAGS) ./...
race-ui: ## Run MyGo presentation tests with race detection
	go -C desktop test -race -vet=off $(TEST_FLAGS) ./...
race: ## Run the complete suite once, with Go scheduling packages concurrently
	go -C core test -race -vet=off $(TEST_FLAGS) ./...
	go -C desktop test -race -vet=off $(TEST_FLAGS) ./...

##@ Build (macOS + Xcode Command Line Tools)
build: check-version ## Build both binaries with the shared toolkit
	mkdir -p bin
	go -C core build -o ../bin/apilot ./cmd/apilot
	go -C desktop build -o ../bin/apilot-app ./cmd/apilot-app
	@for target in apilot apilot-app; do cp "bin/$$target" "bin/$$target-$(GOOS)-$(GOARCH)"; cp "bin/$$target" "bin/$$target-$(GOOS)-$(GOARCH)-$(VERSION)"; cp "bin/$$target" "bin/$$target-$(VERSION)"; done
cli: check-version ## Build the standalone CLI with the shared toolkit
	mkdir -p bin
	go -C core build -o ../bin/apilot ./cmd/apilot
	@cp bin/apilot "bin/apilot-$(GOOS)-$(GOARCH)"; cp bin/apilot "bin/apilot-$(GOOS)-$(GOARCH)-$(VERSION)"; cp bin/apilot "bin/apilot-$(VERSION)"
download: ## Download Go dependencies
	go -C core mod download
	go -C desktop mod download
clean: ## Remove generated binaries only
	$(MAKE) -f hack/build.mk clean
MYGO_BUILD_FLAGS ?= -skip-notarize
check-app-platform:
	@test "$(GOOS)" = darwin && test "$$(go env GOHOSTOS)" = darwin && test "$(GOARCH)" = "$$(go env GOHOSTARCH)" || { echo 'App packaging currently requires native macOS; other platforms need runtime adapters and acceptance.' >&2; exit 1; }
app: check-app-platform cli ## Build native macOS App and DMG
	go -C connectors/github-issues build -o ../../bin/apilot-github-issues ./cmd/apilot-github-issues
	python3 scripts/prepare-resources.py --os $(GOOS) --arch $(GOARCH)
	cd desktop && go tool mygo build -platform $(GOOS)/$(GOARCH) -skip-dmg -skip-notarize $(filter-out -skip-dmg -skip-notarize,$(MYGO_BUILD_FLAGS))
	python3 packaging/darwin/finalize-connectors.py build/$(GOOS)-$(GOARCH)/Apilot.app --arch $(GOARCH) $(addprefix -,$(filter -skip-dmg -skip-notarize,$(MYGO_BUILD_FLAGS)))
	python3 scripts/verify-built-app.py --record
icons: ## Render committed brand masters
	mkdir -p tmp
	go -C desktop build -o ../tmp/render-icons ../design/render-icons.go
	./tmp/render-icons

##@ Platform verification (requires a desktop session)
APILOT_VERIFY_APP ?= $(CURDIR)/build/$(GOOS)-$(GOARCH)/Apilot.app/Contents/MacOS/Apilot
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

.PHONY: dev-watch
dev-watch: ## Continuously run a focused development test
	@scripts/dev watch "$(CASE)"

.PHONY: dev dev-test
dev: ## Run a reloadable workbench development scenario
	@scripts/dev scene "$(SCENE)"
dev-test: ## Run a workbench development scenario without a window
	@scripts/dev test "$(SCENE)"

.PHONY: dev-live dev-reset
dev-live: ## Run a worktree-isolated workbench
	@scripts/dev live
dev-reset: ## Reset the stopped worktree development profile
	@scripts/dev reset

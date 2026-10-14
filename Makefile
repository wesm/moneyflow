.PHONY: build clean dev dev-backend dev-profile dev-reset dev-web fmt help install-hooks lint monarch-live-test ynab-live-test test test-editing-go test-editing-e2e test-export test-go-quick test-mcp test-provider test-provider-e2e test-provider-write test-race test-store tui-demo verify-go verify-web vet web-assets-check web-audit web-budgets web-build web-check web-demo web-e2e web-embed web-embed-check web-generate web-install web-test

GOFLAGS_TEST := -shuffle=on
VERSION := $(shell v=$$(git describe --tags --always --dirty 2>/dev/null || printf dev); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
COMMIT := $(shell v=$$(git rev-parse --short=7 HEAD 2>/dev/null || printf unknown); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
BUILD_DATE := $(shell v=$$(git show -s --format=%cI HEAD 2>/dev/null || printf unknown); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
LDFLAGS := -X github.com/wesm/moneyflow/internal/version.Version=$(VERSION) -X github.com/wesm/moneyflow/internal/version.Commit=$(COMMIT) -X github.com/wesm/moneyflow/internal/version.BuildDate=$(BUILD_DATE)
BINARY := bin/moneyflow
RELEASE_VERSION ?= 0.0.0-rc.0
RELEASE_DIR ?= dist/release
# make dev always binds 127.0.0.1. Override the ports to run several worktrees at once.
DEV_WEB_PORT ?= 5173
DEV_BACKEND_PORT ?= 8081
DEV_HOME := $(CURDIR)/tmp/dev-home
DEV_URL := http://127.0.0.1:$(DEV_WEB_PORT)/
ifeq ($(OS),Windows_NT)
BINARY := bin/moneyflow.exe
endif

build: web-embed ## Build the moneyflow binary in bin/
	mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/moneyflow

.PHONY: release-build
release-build: web-embed-check ## Build release archives (RELEASE_DIR, default dist/release)
	bash scripts/release-build.sh "$(RELEASE_VERSION)" "$(RELEASE_DIR)"

help: ## List the main targets
	@awk 'BEGIN { FS = ":[^#]*## " } /^[a-z][a-z0-9-]*:[^#]*## / { printf "  %-16s %s\n", $$1, $$2 }' \
		$(MAKEFILE_LIST)

.PHONY: test-ui-scenarios fuzz-ui ui-probe ui-probe-web
test-ui-scenarios: web-embed
	go test ./internal/uitest/... ./internal/tools/uiprobe -count=1
	cd web && bun run playwright test tests/scenario.spec.ts

fuzz-ui:
	go test ./internal/uitest/runtime -run '^$$' -fuzz '^FuzzFilteredEditing$$' -fuzztime 30s -parallel 2

UI_PROBE_ARGS ?=
ui-probe:
	go run ./internal/tools/uiprobe $(UI_PROBE_ARGS)

ui-probe-web: web-embed
	cd web && bun scripts/ui-probe.ts $(UI_PROBE_ARGS)

.PHONY: docs-build docs-check docs-test docs-serve docs-screenshots docs-browser-test
docs-build:
	bun docs/tools/site.ts build

docs-check: docs-build ## Lint Markdown and build the documentation website
	npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json AGENTS.md README.md SECURITY.md PUBLISHING.md scripts/README.md .github/DOCS_DEPLOYMENT.md 'docs/**/*.md'
	.github/scripts/check-arrow-lists.sh
	bun docs/tools/site.ts check

docs-test:
	bun test docs/tools/site.test.ts

docs-browser-test:
	bun test docs/tools/browser.test.ts

docs-screenshots: build
	cd web && bun ../docs/tools/capture.ts

docs-serve: ## Serve the documentation website locally
	bun docs/tools/site.ts serve

test: web-embed ## Run the Go tests
	MONEYFLOW_SKIP_PERF=1 go test $(GOFLAGS_TEST) ./...
	go test ./internal/analytics -run '^TestQuery100KCompletesWithinInteractiveBudget$$' -count=1
	go test ./internal/api -run '^Test(Projection|DuplicateProjection)Performance100K$$' -count=1

# The pre-commit hook runs this. It omits -shuffle so Go can reuse cached test results.
test-go-quick: web-embed ## Run the short Go tests, as the pre-commit hook does
	MONEYFLOW_SKIP_PERF=1 go test -short ./...

test-mcp: build
	MONEYFLOW_MCP_TEST_BINARY="$(abspath $(BINARY))" MONEYFLOW_SKIP_PERF=1 go test \
		./internal/mcp ./internal/app ./internal/home ./internal/httpsecurity \
		./internal/provider ./cmd/moneyflow -count=1
	CGO_ENABLED=0 MONEYFLOW_SKIP_PERF=1 go test ./internal/mcp ./cmd/moneyflow -run '^TestMCPCommandDefaultsToStdioAndPassesWritePolicy$$' -count=1
	MONEYFLOW_SKIP_PERF=1 go test -race ./internal/mcp ./cmd/moneyflow -run 'Test(MCP|HTTP|TokenStore|Supervisor|RunMCP)' -count=1
	go test ./internal/mcp -run '^TestMCPPerformance100K$$' -count=1

test-export:
	go test ./internal/exporter -count=1

test-store:
	go test ./internal/store/sqlite -run 'Test(FailureAtomicity|StoreFull|StoreBusy|StoreError|ColdProfilePerformance|BulkEditingPerformance|ProviderRefresh100KPerformance|AmazonImport100KPerformance|OpenInstallsOnlyCurrentSchema|OpenRejectsIncompatibleSchema)' -count=1
	go test ./internal/importer/amazon -run '^TestAmazonParse100KPerformance$$' -count=1
	go test ./internal/app -run '^Test(BulkEditingPerformance|EditTargetsEmptyTimeRowsPerformance|Amazon(Planning|Matching|Search)100KPerformance)' -count=1

test-provider: web-embed
	go test ./internal/provider/... -count=1
	MONEYFLOW_SKIP_PERF=1 go test ./internal/app ./internal/store/sqlite ./cmd/moneyflow ./internal/tui ./internal/api -run 'Test.*(Provider|Monarch)' -count=1
	go test ./internal/store/sqlite -run '^TestProviderRefresh100KPerformance$$' -count=1

test-provider-write: web-embed
	MONEYFLOW_SKIP_PERF=1 go test ./internal/app ./internal/store/sqlite ./internal/provider/... ./internal/api ./internal/replay -run 'Test(RefreshAndWrite|WriteLease|ConcurrentProvider|ProviderWrite|YNAB|ResponseAdjusted|IndexedReplay|RenderersAndWritePlanner|MonarchPorts|UpdateTransaction)' -count=1
	go test ./internal/app -run '^TestProviderWrite(Planning|Finalization)Performance100K$$' -count=1

monarch-live-test:
	@if [ "$$MONEYFLOW_MONARCH_LIVE" != "1" ]; then printf '%s\n' 'Set MONEYFLOW_MONARCH_LIVE=1 to opt in.' >&2; exit 2; fi
	@if [ -z "$$MONEYFLOW_MONARCH_LIVE_SESSION_FILE" ]; then printf '%s\n' 'Set MONEYFLOW_MONARCH_LIVE_SESSION_FILE to a current session file.' >&2; exit 2; fi
	@live_root=$$(mktemp -d); test -n "$$live_root"; trap 'rm -rf "$$live_root"' EXIT INT TERM; MONEYFLOW_HOME="$$live_root" go test -tags=monarchlive ./internal/provider/monarch -run '^TestLiveCharacterization$$' -count=1 -v

ynab-live-test:
	@if [ "$$MONEYFLOW_YNAB_LIVE" != "1" ]; then printf '%s\n' 'Set MONEYFLOW_YNAB_LIVE=1 to opt in.' >&2; exit 2; fi
	@if [ -z "$$MONEYFLOW_YNAB_TOKEN" ]; then printf '%s\n' 'Set MONEYFLOW_YNAB_TOKEN to a YNAB personal access token.' >&2; exit 2; fi
	go test -tags=ynablive ./internal/provider/ynab -run '^TestLiveYNABReadOnlySnapshot$$' -count=1 -v

test-race: web-embed ## Run the Go tests with the race detector
	MONEYFLOW_SKIP_PERF=1 go test -race $(GOFLAGS_TEST) ./...

vet: web-embed
	go vet ./...

lint: web-embed ## Run golangci-lint
	GOLANGCI_LINT_CACHE="$(CURDIR)/.cache/golangci-lint" golangci-lint run --config .golangci.yml

install-hooks: ## Install the prek Git hooks
	@if ! command -v prek >/dev/null 2>&1; then \
		echo "prek not found. Run mise install, then mise exec -- make install-hooks" >&2; \
		exit 1; \
	fi
	prek install -f

verify-go: ## Check Go formatting and run the tests, storage and provider gates, vet, and lint
	go run ./internal/tools/checkfmt cmd internal
	$(MAKE) test test-mcp test-store test-provider test-provider-write vet lint

fmt: ## Format the Go sources
	gofmt -w cmd internal

tui-demo: build ## Run the terminal application with temporary synthetic data
	$(BINARY) tui --demo

web-demo: build ## Serve the browser application with temporary synthetic data
	$(BINARY) web --demo --open=false

web-install: ## Install the locked frontend dependencies
	bun install --cwd web --frozen-lockfile

web-generate: ## Regenerate the API client from the OpenAPI contract
	bun run --cwd web generate

web-check: web-embed
	bun run --cwd web check
	$(MAKE) web-budgets

web-audit:
	bun run --cwd web audit

web-test:
	bun run --cwd web test

web-budgets:
	bun run --cwd web budgets

web-e2e: web-embed
	bun run --cwd web test:e2e -- --project=chromium --grep-invert 'chart cursor and drill p95'
	bun run --cwd web test:e2e -- --project=firefox --grep @smoke
	bun run --cwd web test:e2e -- --project=webkit --grep @smoke
	bun run --cwd web test:e2e -- performance.spec.ts --project=chromium --workers=1

test-editing-go: web-embed
	go test ./internal/app ./internal/tui ./internal/api -run 'Test(Editing|Identity|Restart|Concurrent|PendingOnly|Delete|Deletion|Duplicate|Drill|Privacy|Export)' -count=1

test-editing-e2e: test-editing-go
	bun run --cwd web test:e2e -- base-path.spec.ts editing.spec.ts managers.spec.ts origin.spec.ts restart.spec.ts review.spec.ts --project=chromium

test-provider-e2e: web-embed
	bun run --cwd web test:e2e -- provider.spec.ts refresh.spec.ts --project=chromium

web-build:
	bun run --cwd web build

web-assets-check: web-build
	bun run --cwd web scripts/validate-assets.ts dist

web-embed: web-assets-check ## Build the frontend into internal/web/dist for Go embedding
	bun run --cwd web scripts/embed-assets.ts

web-embed-check: web-embed
	@if test -n "$$(git ls-files internal/web/dist web/tests/screenshots)"; then \
		printf '%s\n' 'generated web assets must not be tracked on this branch' >&2; \
		exit 1; \
	fi

verify-web: ## Run every browser, API, and embedded-asset check
	$(MAKE) web-install
	$(MAKE) web-check web-test web-audit web-embed-check test-editing-go web-e2e
	go test ./internal/api -run 'Test(Projection|DuplicateProjection)Performance100K' -count=1

dev: web-install web-embed dev-profile ## Run Go under air behind Vite with live reload
	@printf '\n%s\n%s\n%s\n\n' \
		'make dev: open the "Moneyflow web:" URL that the backend prints when it starts.' \
		'$(DEV_URL) shows the profile chooser. Logs are in tmp/logs.' \
		'Press Ctrl-C to stop both servers.'
	@$(MAKE) -j2 dev-backend dev-web

dev-profile: ## Seed the synthetic dev profile in tmp/dev-home if it is missing
	@mkdir -p -m 700 tmp
	@if [ ! -f tmp/dev-home/moneyflow.db ]; then \
		go run ./internal/tools/seedprofile "$(DEV_HOME)" && printf '%s\n' 'Seeded tmp/dev-home'; \
	fi

dev-reset: ## Delete the dev profile; the next make dev seeds a fresh one
	rm -rf tmp/dev-home

dev-backend: dev-profile ## Run only the Go server under air; needs web-embed once
	@mkdir -p tmp/logs
	MONEYFLOW_HOME="$(DEV_HOME)" air -c .air.toml -- web --open=false --profile Moneyflow \
		--listen 127.0.0.1:$(DEV_BACKEND_PORT) --external-url $(DEV_URL) 2>&1 | tee tmp/logs/backend.log

dev-web: ## Run only the Vite dev server
	@mkdir -p -m 700 tmp
	@mkdir -p tmp/logs
	MONEYFLOW_DEV_WEB_PORT=$(DEV_WEB_PORT) MONEYFLOW_DEV_BACKEND_PORT=$(DEV_BACKEND_PORT) \
		bun run --cwd web dev 2>&1 | tee tmp/logs/web.log

clean: ## Remove build, test, and documentation outputs; keeps tmp/dev-home
	rm -rf bin coverage.out .cache/golangci-lint .cache/web-e2e .cache/ui-harness internal/web/dist \
		web/dist web/.vite web/coverage web/test-results web/playwright-report $(RELEASE_DIR) \
		docs/.site-work site tmp/air tmp/logs

.PHONY: test-amazon test-amazon-e2e docs-check
test-amazon: web-embed ## Run the Amazon import tests
	go test ./internal/importer/amazon ./internal/amazonimport -count=1
	go test ./internal/analytics ./internal/app ./internal/store/sqlite ./internal/tui ./internal/api ./cmd/moneyflow -run Amazon -count=1

test-amazon-e2e: web-embed ## Run the Amazon import browser tests
	bun run --cwd web test:e2e -- amazon-import.spec.ts --project=chromium

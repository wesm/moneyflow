.PHONY: build clean fmt help install-hooks lint monarch-live-test test test-editing-e2e test-export test-go-quick test-mcp test-provider test-provider-e2e test-provider-write test-race test-store tui-demo verify-go verify-web vet web-assets-check web-audit web-budgets web-build web-check web-demo web-dev web-e2e web-embed web-embed-check web-generate web-install web-test

GOFLAGS_TEST := -shuffle=on
VERSION := $(shell v=$$(git describe --tags --always --dirty 2>/dev/null || printf dev); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
COMMIT := $(shell v=$$(git rev-parse --short=7 HEAD 2>/dev/null || printf unknown); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
BUILD_DATE := $(shell v=$$(git show -s --format=%cI HEAD 2>/dev/null || printf unknown); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
LDFLAGS := -X github.com/wesm/moneyflow/internal/version.Version=$(VERSION) -X github.com/wesm/moneyflow/internal/version.Commit=$(COMMIT) -X github.com/wesm/moneyflow/internal/version.BuildDate=$(BUILD_DATE)
BINARY := bin/moneyflow
RELEASE_VERSION ?= 0.0.0-rc.0
RELEASE_DIR ?= dist/release
ifeq ($(OS),Windows_NT)
BINARY := bin/moneyflow.exe
endif

build: web-embed
	mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/moneyflow

.PHONY: release-build
release-build: web-embed-check
	bash scripts/release-build.sh "$(RELEASE_VERSION)" "$(RELEASE_DIR)"

help:
	@printf '%s\n' 'web-demo  Serve the synthetic web application at http://127.0.0.1:8080/'

.PHONY: docs-build docs-check docs-test docs-serve docs-screenshots docs-browser-test
docs-build:
	bun docs/tools/site.ts build

docs-check: docs-build
	npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json AGENTS.md README.md SECURITY.md PUBLISHING.md scripts/README.md .github/DOCS_DEPLOYMENT.md 'docs/**/*.md'
	.github/scripts/check-arrow-lists.sh
	bun docs/tools/site.ts check

docs-test:
	bun test docs/tools/site.test.ts

docs-browser-test:
	bun test docs/tools/browser.test.ts

docs-screenshots: build
	cd web && bun ../docs/tools/capture.ts

docs-serve:
	bun docs/tools/site.ts serve

test: web-embed
	MONEYFLOW_SKIP_PERF=1 go test $(GOFLAGS_TEST) ./...
	go test ./internal/analytics -run '^TestQuery100KCompletesWithinInteractiveBudget$$' -count=1
	go test ./internal/api -run '^Test(Projection|DuplicateProjection)Performance100K$$' -count=1

test-go-quick: web-embed
	MONEYFLOW_SKIP_PERF=1 go test -short $(GOFLAGS_TEST) ./...

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
	go test ./internal/app -run '^Test(BulkEditingPerformance|Amazon(Planning|Matching|Search)100KPerformance)' -count=1

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

test-race: web-embed
	MONEYFLOW_SKIP_PERF=1 go test -race $(GOFLAGS_TEST) ./...

vet: web-embed
	go vet ./...

lint: web-embed
	GOLANGCI_LINT_CACHE="$(CURDIR)/.cache/golangci-lint" golangci-lint run --config .golangci.yml

install-hooks:
	@if ! command -v prek >/dev/null 2>&1; then \
		echo "prek not found. Install with: brew install prek" >&2; \
		exit 1; \
	fi
	prek install -f

verify-go:
	go run ./internal/tools/checkfmt cmd internal
	$(MAKE) test
	$(MAKE) test-mcp
	$(MAKE) test-store
	$(MAKE) test-provider
	$(MAKE) test-provider-write
	$(MAKE) vet
	$(MAKE) lint

fmt:
	gofmt -w cmd internal

tui-demo: build
	$(BINARY) tui --demo

web-demo: build
	$(BINARY) web --demo --open=false

web-install:
	bun install --cwd web --frozen-lockfile

web-generate:
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
	bun run --cwd web test:e2e -- --project=chromium
	bun run --cwd web test:e2e -- --project=firefox --grep @smoke
	bun run --cwd web test:e2e -- --project=webkit --grep @smoke

test-editing-e2e: web-embed
	go test ./internal/app ./internal/tui ./internal/api -run 'Test(Editing|Identity|Restart|Concurrent|PendingOnly|Delete|Deletion|Duplicate|Drill|Privacy|Export)' -count=1
	bun run --cwd web test:e2e -- base-path.spec.ts editing.spec.ts origin.spec.ts restart.spec.ts review.spec.ts --project=chromium

test-provider-e2e: web-embed
	bun run --cwd web test:e2e -- provider.spec.ts --project=chromium

web-build:
	bun run --cwd web build

web-assets-check: web-build
	bun run --cwd web scripts/validate-assets.ts dist

web-embed: web-assets-check
	bun run --cwd web scripts/embed-assets.ts

web-embed-check: web-embed
	@if test -n "$$(git ls-files internal/web/dist web/tests/screenshots)"; then \
		printf '%s\n' 'generated web assets must not be tracked on this branch' >&2; \
		exit 1; \
	fi

verify-web:
	$(MAKE) web-install
	$(MAKE) web-check
	$(MAKE) web-test
	$(MAKE) web-audit
	$(MAKE) web-embed-check
	$(MAKE) test-editing-e2e
	$(MAKE) test-provider-e2e
	$(MAKE) web-e2e
	go test ./internal/api -run 'Test(Projection|DuplicateProjection)Performance100K' -count=1

web-dev:
	bun run --cwd web dev

clean:
	rm -rf bin coverage.out .cache/golangci-lint web/dist web/coverage web/test-results web/playwright-report

.PHONY: test-amazon test-amazon-e2e docs-check
test-amazon: web-embed
	go test ./internal/importer/amazon ./internal/amazonimport -count=1
	go test ./internal/analytics ./internal/app ./internal/store/sqlite ./internal/tui ./internal/api ./cmd/moneyflow -run Amazon -count=1

test-amazon-e2e: web-embed
	bun run --cwd web test:e2e -- amazon-import.spec.ts --project=chromium

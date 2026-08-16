.PHONY: build clean fmt help lint monarch-live-test parity parity-go parity-python parity-update-go parity-update-python test test-editing-e2e test-provider test-provider-e2e test-race test-store tui-demo verify-go verify-web vet web-assets-check web-audit web-budgets web-build web-check web-demo web-dev web-e2e web-embed web-embed-check web-generate web-install web-test

GOFLAGS_TEST := -shuffle=on
VERSION := $(shell v=$$(git describe --tags --always --dirty 2>/dev/null || printf dev); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
COMMIT := $(shell v=$$(git rev-parse --short=7 HEAD 2>/dev/null || printf unknown); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
BUILD_DATE := $(shell v=$$(git show -s --format=%cI HEAD 2>/dev/null || printf unknown); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
LDFLAGS := -X github.com/wesm/moneyflow/internal/version.Version=$(VERSION) -X github.com/wesm/moneyflow/internal/version.Commit=$(COMMIT) -X github.com/wesm/moneyflow/internal/version.BuildDate=$(BUILD_DATE)
BINARY := bin/moneyflow
ifeq ($(OS),Windows_NT)
BINARY := bin/moneyflow.exe
endif

build:
	mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/moneyflow

help:
	@printf '%s\n' 'web-demo  Serve the synthetic web application at http://127.0.0.1:8080/'

test:
	MONEYFLOW_SKIP_PERF=1 go test $(GOFLAGS_TEST) ./...
	go test ./internal/analytics -run '^TestQuery100KCompletesWithinInteractiveBudget$$' -count=1
	go test ./internal/api -run '^TestProjectionPerformance100K$$' -count=1

test-store:
	go test ./internal/store/sqlite -run 'Test(FailureAtomicity|StoreFull|StoreBusy|StoreError|ColdProfilePerformance|BulkEditingPerformance|ProviderRefresh100KPerformance|OpenInstallsOnlyCurrentSchema|OpenRejectsIncompatibleSchema)' -count=1
	go test ./internal/app -run '^TestBulkEditingPerformance' -count=1

test-provider:
	go test ./internal/provider/... -count=1
	MONEYFLOW_SKIP_PERF=1 go test ./internal/app ./internal/store/sqlite ./cmd/moneyflow ./internal/tui ./internal/api -run 'Test.*(Provider|Monarch)' -count=1
	go test ./internal/store/sqlite -run '^TestProviderRefresh100KPerformance$$' -count=1

monarch-live-test:
	@if [ "$$MONEYFLOW_MONARCH_LIVE" != "1" ]; then printf '%s\n' 'Set MONEYFLOW_MONARCH_LIVE=1 to opt in.' >&2; exit 2; fi
	@if [ -z "$$MONEYFLOW_MONARCH_LIVE_SESSION_FILE" ]; then printf '%s\n' 'Set MONEYFLOW_MONARCH_LIVE_SESSION_FILE to a current session file.' >&2; exit 2; fi
	@live_root=$$(mktemp -d); test -n "$$live_root"; trap 'rm -rf "$$live_root"' EXIT INT TERM; MONEYFLOW_HOME="$$live_root" go test -tags=monarchlive ./internal/provider/monarch -run '^TestLiveCharacterization$$' -count=1 -v

test-race:
	MONEYFLOW_SKIP_PERF=1 go test -race $(GOFLAGS_TEST) ./...

vet:
	go vet ./...

lint:
	GOLANGCI_LINT_CACHE="$(CURDIR)/.cache/golangci-lint" golangci-lint run --config .golangci.yml

parity-python:
	uv run python -m moneyflow.parity.semantic --check

parity-go:
	go test ./internal/tui -run 'Test(PythonSemanticFrameParity|VisualGoldens)' -count=1

parity: parity-python parity-go

parity-update-python:
	uv run python -m moneyflow.parity.semantic --update

parity-update-go:
	@printf '%s\n' 'WARNING: generated Go cell frames require explicit visual review.'
	MONEYFLOW_UPDATE_GO_FRAMES=1 go test ./internal/tui -run TestVisualGoldens -count=1

verify-go:
	go run ./internal/tools/checkfmt cmd internal
	$(MAKE) test
	$(MAKE) test-store
	$(MAKE) test-provider
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) parity

fmt:
	gofmt -w cmd internal

tui-demo: build
	$(BINARY) demo

web-demo: build
	$(BINARY) web --demo --open=false

web-install:
	bun install --cwd web --frozen-lockfile

web-generate:
	bun run --cwd web generate

web-check:
	bun run --cwd web check
	$(MAKE) web-build
	$(MAKE) web-budgets

web-audit:
	bun run --cwd web audit

web-test:
	bun run --cwd web test

web-budgets:
	bun run --cwd web budgets

web-e2e: web-build
	bun run --cwd web test:e2e -- --project=chromium
	bun run --cwd web test:e2e -- --project=firefox --grep @smoke
	bun run --cwd web test:e2e -- --project=webkit --grep @smoke

test-editing-e2e: web-build
	go test ./internal/app ./internal/tui ./internal/api -run 'Test(Editing|Identity|Restart|Concurrent|PendingOnly)' -count=1
	bun run --cwd web test:e2e -- base-path.spec.ts editing.spec.ts origin.spec.ts restart.spec.ts review.spec.ts --project=chromium

test-provider-e2e: web-build
	bun run --cwd web test:e2e -- provider.spec.ts --project=chromium

web-build:
	bun run --cwd web build

web-assets-check:
	bun run --cwd web scripts/validate-assets.ts dist

web-embed: web-assets-check
	bun run --cwd web scripts/embed-assets.ts

web-embed-check: web-assets-check
	bun run --cwd web scripts/embed-assets.ts --check

verify-web:
	$(MAKE) web-install
	$(MAKE) web-check
	$(MAKE) web-test
	$(MAKE) web-audit
	$(MAKE) web-embed-check
	$(MAKE) test-editing-e2e
	$(MAKE) test-provider-e2e
	$(MAKE) web-e2e
	go test ./internal/api -run TestProjectionPerformance100K -count=1

web-dev:
	bun run --cwd web dev

clean:
	rm -rf bin coverage.out .cache/golangci-lint web/dist web/coverage web/test-results web/playwright-report

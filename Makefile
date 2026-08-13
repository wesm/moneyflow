.PHONY: build clean fmt lint parity parity-go parity-python parity-update-go parity-update-python test test-race tui-demo verify-go vet web-audit web-build web-check web-dev web-generate web-install web-test

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

test:
	go test $(GOFLAGS_TEST) ./...

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
	test -z "$$(gofmt -l cmd internal)"
	$(MAKE) test
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) parity

fmt:
	gofmt -w cmd internal

tui-demo: build
	$(BINARY) demo

web-install:
	bun install --cwd web --frozen-lockfile

web-generate:
	bun run --cwd web generate

web-check:
	bun run --cwd web check

web-audit:
	bun run --cwd web audit

web-test:
	bun run --cwd web test

web-build:
	bun run --cwd web build

web-dev:
	bun run --cwd web dev

clean:
	rm -rf bin coverage.out .cache/golangci-lint web/dist web/coverage web/test-results web/playwright-report

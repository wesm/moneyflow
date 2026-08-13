.PHONY: build clean fmt lint parity parity-go parity-python parity-update-python test test-race tui-demo vet

GOFLAGS_TEST := -shuffle=on
VERSION := $(shell v=$$(git describe --tags --always --dirty 2>/dev/null || printf dev); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
COMMIT := $(shell v=$$(git rev-parse --short=7 HEAD 2>/dev/null || printf unknown); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
BUILD_DATE := $(shell v=$$(git show -s --format=%cI HEAD 2>/dev/null || printf unknown); printf '%s' "$$v" | LC_ALL=C tr -c 'A-Za-z0-9._+~:-' '-')
LDFLAGS := -X github.com/wesm/moneyflow/internal/version.Version=$(VERSION) -X github.com/wesm/moneyflow/internal/version.Commit=$(COMMIT) -X github.com/wesm/moneyflow/internal/version.BuildDate=$(BUILD_DATE)

build:
	mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o bin/moneyflow ./cmd/moneyflow

test:
	go test $(GOFLAGS_TEST) ./...

test-race:
	go test -race $(GOFLAGS_TEST) ./...

vet:
	go vet ./...

lint:
	GOLANGCI_LINT_CACHE="$(CURDIR)/.cache/golangci-lint" golangci-lint run --config .golangci.yml

parity-python:
	uv run python -m moneyflow.parity.semantic --check

parity-go:
	go test ./internal/tui -run TestPythonSemanticFrameParity -count=1

parity: parity-python parity-go

parity-update-python:
	uv run python -m moneyflow.parity.semantic --update

fmt:
	gofmt -w cmd internal

tui-demo: build
	./bin/moneyflow demo --fixture testdata/parity/transactions.json

clean:
	rm -rf bin coverage.out .cache/golangci-lint

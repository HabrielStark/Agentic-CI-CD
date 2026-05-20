# ReproForge CI Makefile.
#
# Targets here are designed to run sequentially when chained (`make all`) so
# heavy stages (race tests, vuln scan, container build) never overlap.
SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

GO ?= go
NODE ?= node

BIN_DIR := bin
BIN := $(BIN_DIR)/reproforge

.PHONY: all build test vet race coverage govulncheck staticcheck action-self-test \
	bench fmt clean fixture-cap

all: build vet test

build:
	$(GO) build -o $(BIN) ./cmd/reproforge

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race -count=1 ./...

coverage:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -25

govulncheck:
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

staticcheck:
	$(GO) install honnef.co/go/tools/cmd/staticcheck@latest
	staticcheck ./...

action-self-test:
	$(NODE) action/dist/index.js --self-test

fixture-cap: build
	mkdir -p /tmp/rfcap/logs/build /tmp/rfcap/artifacts
	cp fixtures/python-deterministic/job.log /tmp/rfcap/logs/build/job.log.redacted
	cp fixtures/python-deterministic/junit-results.xml /tmp/rfcap/artifacts/
	@echo "{ \"schema\": \"reproforge.capsule/v1\", \"createdAt\": \"2026-05-20T12:00:00Z\", \"generator\": \"make\", \"provider\": \"github_actions\", \"repo\": \"octocat/hello\", \"commit\": \"abc\", \"workflow\": \"ci.yml\", \"job\": \"build\", \"runner\": {\"os\":\"ubuntu-24.04\",\"arch\":\"x86_64\"}, \"failure\": {\"step\":\"Run tests\",\"command\":\"pytest -q\",\"exitCode\":1,\"fingerprint\":\"sha256:0000000000000000000000000000000000000000000000000000000000000000\"}, \"replay\": {\"modes\":[\"failed-step\"],\"network\":\"configurable\"}, \"redaction\": {\"status\":\"passed\",\"rules\":17,\"hits\":0} }" > /tmp/rfcap/capsule.json
	$(BIN) diagnose /tmp/rfcap

bench:
	$(GO) test -bench=. -run=^$ -benchmem ./...

fmt:
	$(GO) fmt ./...

clean:
	rm -rf $(BIN_DIR) coverage.out reproforge-out

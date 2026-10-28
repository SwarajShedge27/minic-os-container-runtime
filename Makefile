# MiniC — Lightweight Linux Container Runtime from Scratch
# Makefile for unprivileged developer automation and privileged integration tests.

SHELL := /bin/bash
BIN_DIR ?= bin
TARGET ?= $(BIN_DIR)/minic
GO ?= go

.PHONY: all test vet build check clean help integration-cow integration-net integration-security integration

# Default target runs standard unprivileged checks without requiring root
all: check

help:
	@echo "MiniC Developer Commands:"
	@echo "  make test                  Run unprivileged Go unit tests"
	@echo "  make vet                   Run static code analysis (go vet)"
	@echo "  make build                 Build MiniC binary to $(TARGET)"
	@echo "  make check                 Run tests, vet, dry build, and diff checks"
	@echo "  make clean                 Remove build artifacts"
	@echo ""
	@echo "Privileged Integration Tests (require Linux/WSL2 and sudo privileges):"
	@echo "  make integration-cow       Run OverlayFS copy-on-write integration test"
	@echo "  make integration-net       Run Network namespace isolation integration test"
	@echo "  make integration-security  Run Capability dropping & security integration test"
	@echo "  make integration           Run all privileged integration tests sequentially"

test:
	@echo "Running unprivileged unit tests..."
	$(GO) test -count=1 -v ./...

vet:
	@echo "Running static analysis..."
	$(GO) vet ./...

build:
	@echo "Building MiniC binary to $(TARGET)..."
	@mkdir -p $(BIN_DIR)
	$(GO) build -buildvcs=false -o $(TARGET) ./cmd/minic
	@echo "Build complete: $(TARGET)"

check: test vet
	@echo "Verifying clean compilation without modifying root-level binaries..."
	@$(GO) build -buildvcs=false -o /tmp/minic-verify-check ./cmd/minic && rm -f /tmp/minic-verify-check
	@echo "Verifying git diff formatting..."
	@git diff --check
	@echo "All unprivileged checks passed successfully."

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BIN_DIR)
	@rm -f /tmp/minic-verify-check /tmp/minic-integration* /tmp/minic-phase5*

# Privileged targets — explicitly separated so default commands never prompt for sudo
integration-cow:
	@echo "Executing OverlayFS copy-on-write integration test (requires sudo)..."
	sudo ./test/integration_cow_test.sh

integration-net:
	@echo "Executing network isolation integration test (requires sudo)..."
	sudo ./test/integration_net_test.sh

integration-security:
	@echo "Executing security hardening integration test (requires sudo)..."
	sudo ./test/integration_sec_test.sh

integration: integration-cow integration-net integration-security
	@echo "All privileged integration tests completed successfully."

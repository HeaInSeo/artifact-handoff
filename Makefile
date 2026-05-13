SHELL := /bin/bash
.SHELLFLAGS := -o pipefail -c

LOCALBIN := $(CURDIR)/bin
REPORT_DIR := $(CURDIR)/reports
GOCACHE_DIR := /tmp/artifact-handoff-gocache
GOTMPDIR_DIR := /tmp/artifact-handoff-gotmp

GOLANGCI_LINT := $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION := v2.11.3

GOVULNCHECK := $(LOCALBIN)/govulncheck
GOVULNCHECK_VERSION := v1.1.4

BUF := $(LOCALBIN)/buf
BUF_VERSION := v1.54.0

PKGS_ALL := ./...
PKGS_CORE := ./api/... ./cmd/... ./pkg/...
PKGS_COVER := ./cmd/... ./pkg/...
PKGS_SECURITY := ./cmd/... ./pkg/...

.PHONY: test test-regression coverage fmt vet lint lint-depguard lint-security vuln vuln-all golangci-lint govulncheck buf proto proto-check

test:
	go test $(PKGS_ALL)

test-regression:
	go test ./pkg/resolver -run 'TestHTTP|TestGRPC|TestSimulate'

coverage:
	@mkdir -p "$(REPORT_DIR)"
	@mkdir -p "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	GOCACHE="$(GOCACHE_DIR)" GOTMPDIR="$(GOTMPDIR_DIR)" go test $(PKGS_COVER) -coverprofile="$(REPORT_DIR)/cover.out" -covermode=atomic
	go tool cover -func="$(REPORT_DIR)/cover.out" | tee "$(REPORT_DIR)/coverage.txt"

fmt:
	go fmt $(PKGS_ALL)

vet:
	go vet $(PKGS_ALL)

golangci-lint:
	@mkdir -p "$(LOCALBIN)"
	@test -x "$(GOLANGCI_LINT)" || bash -c '\
		set -euo pipefail; \
		curl -fsSL "https://api.github.com/repos/golangci/golangci-lint/releases/tags/$(GOLANGCI_LINT_VERSION)" >/dev/null; \
		OS="$$(uname | tr A-Z a-z)"; \
		ARCH="$$(uname -m)"; \
		case "$$ARCH" in x86_64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) echo "unsupported arch: $$ARCH"; exit 1 ;; esac; \
		VER="$(GOLANGCI_LINT_VERSION)"; \
		VER="$${VER#v}"; \
		FILE="golangci-lint-$$VER-$$OS-$$ARCH.tar.gz"; \
		URL="https://github.com/golangci/golangci-lint/releases/download/$(GOLANGCI_LINT_VERSION)/$$FILE"; \
		SUM_URL="https://github.com/golangci/golangci-lint/releases/download/$(GOLANGCI_LINT_VERSION)/golangci-lint-$$VER-checksums.txt"; \
		TMP="$$(mktemp -d)"; \
		curl -fsSL "$$URL" -o "$$TMP/lint.tgz"; \
		curl -fsSL "$$SUM_URL" -o "$$TMP/checksums.txt"; \
		EXPECTED="$$(awk -v f="$$FILE" "\$$2==f{print \$$1}" "$$TMP/checksums.txt")"; \
		if [ -z "$$EXPECTED" ]; then echo "checksum not found for $$FILE"; exit 1; fi; \
		if command -v sha256sum >/dev/null 2>&1; then \
			ACTUAL="$$(sha256sum "$$TMP/lint.tgz" | awk "{print \$$1}")"; \
		elif command -v shasum >/dev/null 2>&1; then \
			ACTUAL="$$(shasum -a 256 "$$TMP/lint.tgz" | awk "{print \$$1}")"; \
		else \
			echo "no sha256 tool found (sha256sum/shasum)"; exit 1; \
		fi; \
		if [ "$$EXPECTED" != "$$ACTUAL" ]; then echo "checksum mismatch for $$FILE"; exit 1; fi; \
		tar -xzf "$$TMP/lint.tgz" -C "$$TMP"; \
		cp "$$TMP/golangci-lint-$$VER-$$OS-$$ARCH/golangci-lint" "$(GOLANGCI_LINT)"; \
		chmod +x "$(GOLANGCI_LINT)"; \
		rm -rf "$$TMP"'

govulncheck:
	@mkdir -p "$(LOCALBIN)"
	@test -x "$(GOVULNCHECK)" || GOBIN="$(LOCALBIN)" go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

lint: golangci-lint lint-depguard
	@mkdir -p "$(REPORT_DIR)"
	@$(GOLANGCI_LINT) run $(PKGS_CORE) | tee "$(REPORT_DIR)/lint.txt"

lint-depguard: golangci-lint
	@mkdir -p "$(REPORT_DIR)"
	@$(GOLANGCI_LINT) run --enable-only depguard $(PKGS_CORE) | tee "$(REPORT_DIR)/lint-depguard.txt"

lint-security: golangci-lint
	@mkdir -p "$(REPORT_DIR)"
	@echo "[artifact-handoff] security scan scope: $(PKGS_SECURITY)" | tee "$(REPORT_DIR)/lint-security-summary.txt"
	@set +e; \
	$(GOLANGCI_LINT) run --enable-only gosec $(PKGS_SECURITY) \
	| tee "$(REPORT_DIR)/gosec.txt"; \
	echo "gosec_exit=$$?" | tee -a "$(REPORT_DIR)/lint-security-summary.txt"

vuln: govulncheck
	@mkdir -p "$(REPORT_DIR)"
	@set +e; \
	$(GOVULNCHECK) $(PKGS_SECURITY) 2>&1 | tee "$(REPORT_DIR)/govulncheck-core.txt"; \
	echo "govulncheck_core_exit=$$?" | tee "$(REPORT_DIR)/govulncheck-core.summary"

vuln-all: govulncheck
	@mkdir -p "$(REPORT_DIR)"
	@set +e; \
	$(GOVULNCHECK) ./... 2>&1 | tee "$(REPORT_DIR)/govulncheck-all.txt"; \
	echo "govulncheck_all_exit=$$?" | tee "$(REPORT_DIR)/govulncheck-all.summary"

buf:
	@mkdir -p "$(LOCALBIN)"
	@test -x "$(BUF)" || GOBIN="$(LOCALBIN)" go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)

proto: buf
	$(BUF) generate

proto-check: buf
	$(BUF) lint
	$(BUF) breaking --against '.git#branch=main'
	$(BUF) generate
	git diff --exit-code -- api/proto/ahv1/

# Tooling Standardization Plan

## Goal

Use `artifact-handoff` as the first pilot repository for a unified `tori`-style Go guardrail baseline, then roll the same pattern out to other Go repositories in this workspace.

This document is the restart point. If work pauses, resume from the checklist and status below instead of rediscovering context.

## Standard Baseline

The baseline we are standardizing on is:

- repo-local `./bin/golangci-lint` only
- pinned `golangci-lint` v2 version
- checksum-verified linter download
- `make lint` as the primary fail gate
- `make lint-security` and `make vuln` as supplemental guardrails
- GitHub Actions lint workflow that runs `make lint`
- `depguard` rule to keep product-core packages Kubernetes-independent where intended

## Pilot Repository

- Repository: `artifact-handoff`
- Baseline source: `tori`
- Rationale:
  - `artifact-handoff` is already a product-shaped Go repository
  - it currently lacks consistent lint/CI guardrails
  - it is a good low-blast-radius pilot for the wider rollout

## Status

### Completed

- Added `Makefile` with `fmt`, `vet`, `test`, `lint`, `lint-depguard`, `lint-security`, `vuln`, `vuln-all`
- Added repo-local `golangci-lint` bootstrap pinned to `v2.11.3`
- Added `.golangci.yml` with baseline v2 configuration
- Added GitHub Actions lint workflow using `make lint`
- Added `coverage` and `test-regression` Makefile targets
- Added GitHub Actions test workflow for unit/regression/coverage
- Added HTTP regression coverage for canonical artifact identity handling
- Added `internal/ids` and migrated artifact/node-attempt key formatting to a single policy package
- Established this document as the handoff/restart ledger

### Current Known Issues

- There is existing user work in the tree:
  - modified: `pkg/resolver/service.go`
  - untracked: `deploy/devspace/`
- `golangci-lint` full local verification is still pending because this environment has restricted network access and the repo-local linter bootstrap has not been executed yet
- `coverage` requires local temporary cache directories in this environment; the `Makefile` already reflects that workaround

## Rollout Sequence

### Phase 1: Pilot hardening in `artifact-handoff`

- [x] add `Makefile`
- [x] add `.golangci.yml`
- [x] add `.github/workflows/lint.yml`
- [x] add `.github/workflows/test.yml`
- [x] add `coverage` and `test-regression` targets
- [x] add regression coverage for HTTP artifact identity compatibility
- [x] make `coverage` resilient to local cache/write restrictions
- [ ] verify `make lint` passes with repo-local binary
- [x] verify `make test` passes locally
- [x] verify `make test-regression` passes locally
- [x] verify `make coverage` passes locally
- [ ] verify `make test`, `make test-regression`, and `make coverage` pass in CI
- [ ] decide whether `lint-security` remains report-only or becomes a gate
- [ ] decide whether `govulncheck` remains local-only or gets its own GitHub Actions workflow
- [ ] update README with guardrail commands if desired

### Phase 2: Template extraction

- [ ] extract the common baseline shape from `artifact-handoff` and `tori`
- [ ] define which fields are repo-specific:
  - package scopes
  - depguard boundaries
  - extra security linters
  - test entrypoints
- [ ] publish a small copy/paste standard or internal template note

### Phase 3: Workspace rollout

Recommended order:

1. `spawner`
2. `my-operator`
3. `kube-slint`
4. `hello-operator`
5. `NodeVault`

Per repository checklist:

- [ ] replace ad hoc/system lint usage with repo-local `./bin/golangci-lint`
- [ ] align on pinned v2 version strategy
- [ ] align `Makefile` targets
- [ ] switch GitHub Actions lint job to `make lint`
- [ ] add or adapt `depguard`
- [ ] decide report-only versus fail-gate for security checks

## Resume Instructions

When resuming this work:

1. Read this document first.
2. Check `git status` for unrelated in-progress changes.
3. Run `go test ./...` to confirm whether the existing resolver failures still reproduce.
4. Validate the guardrail bootstrap with:
   - `make lint`
   - `make lint-security`
   - `make vuln`
5. Continue from the first unchecked item in Phase 1.

## Notes

- Keep product-core packages Kubernetes-independent unless there is an intentional architectural change.
- Do not weaken the baseline to fit one repository unless the repository boundary truly differs.
- Prefer converging other repositories upward to the `tori` standard instead of preserving multiple styles.

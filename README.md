# artifact-handoff

English: [README.md](README.md) | 한국어: [README.ko.md](README.ko.md)

`artifact-handoff` is a locality-aware artifact handoff decision service for Kubernetes batch workloads.

It answers one question: **given a producer artifact and a consumer binding, where should the consumer run, and how should it get the bytes?**

`artifact-handoff` returns decisions. It never creates Kubernetes Jobs or Pods.

---

## Core Contract

```
RegisterArtifact(artifact)  → AvailabilityState
ResolveHandoff(binding, targetNodeName) → PlacementIntent + MaterializationPlan
NotifyNodeTerminal(sampleRunID, nodeID, attemptID, state)
FinalizeSampleRun(sampleRunID)
EvaluateGC(sampleRunID)
GetSampleRunLifecycle(sampleRunID)
```

**`ResolveHandoff` returns two decision objects:**

| Field | Values | Spawner action |
|---|---|---|
| `PlacementIntent.mode` | `none \| preferred_node \| required_node` | nodeAffinity in PodSpec |
| `MaterializationPlan.mode` | `none \| local_reuse \| remote_fetch` | volume mount or init-container |

**Spawner translates these decisions into PodSpec. `artifact-handoff` never touches Kubernetes resources.**

---

## Resolution Status

`ResolveHandoff` returns one of these statuses. Spawner branches on this:

| Status | Spawner action |
|---|---|
| `RESOLVED` | proceed — `PlacementIntent` + `MaterializationPlan` are ready |
| `PENDING` | producer not yet terminal — wait and requeue |
| `MISSING` | producer succeeded but artifact not registered |
| `PRODUCER_FAILED` | producer failed or canceled — block child execution |
| `POLICY_BLOCKED` | `SameNodeOnly` violated — do not attempt fallback |
| `DIGEST_MISMATCH` | integrity/reproducibility error — fail immediately |
| `GC_EXPIRED` | sample run is GC-eligible — re-run or propagate failure |
| `UNAVAILABLE` | artifact exists but URI is absent or unroutable |

---

## Quick Start

```bash
# in-memory store (default)
go run ./cmd/artifact-handoff-resolver

# SQLite persistence
AH_STORE_DSN=sqlite:/data/ah.db go run ./cmd/artifact-handoff-resolver

# Docker / Podman
podman build -t artifact-handoff:latest .
podman run -p 8080:8080 -p 9090:9090 artifact-handoff:latest
```

Endpoints:
- HTTP: `:8080`
- gRPC: `:9090`
- Metrics (Prometheus): `:8080/metrics`
- Health: `:8080/healthz`

---

## Artifact Identity

```
sampleRunID / producerNodeID / producerAttemptID / outputName
```

Example: `run-001/node-A/attempt-1/result.json`

`attemptID` ownership: **JUMI/Executor generates → Spawner propagates → AH consumes**.
AH never generates or auto-selects an attemptID.

---

## Consume Policy

| Policy | Planning mode | Post-scheduling |
|---|---|---|
| `SameNodeOnly` | `required_node` + `local_reuse` | `POLICY_BLOCKED` if consumer on different node |
| `SameNodeThenRemote` | `preferred_node` + `remote_fetch` | `remote_fetch` with fallback metric |
| `RemoteOK` | `none` + `remote_fetch` | `remote_fetch` |

---

## Store Backend

| `AH_STORE_DSN` | Backend |
|---|---|
| `memory` (default) | In-memory, lost on restart |
| `sqlite:<path>` | SQLite with WAL mode — survives restarts |

---

## Development

```bash
make test          # unit + integration tests
make test-regression
make coverage      # HTML coverage report → reports/
make lint          # golangci-lint (auto-downloads bin/golangci-lint if absent)
make fmt           # gofmt + goimports
make vet
make vuln          # govulncheck
```

Requirements: Go 1.25+, no CGO needed (pure-Go SQLite via `modernc.org/sqlite`).

---

## Documentation

| Document | Purpose |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System architecture, subsystem boundaries, stable constraints |
| [docs/PHASE1_RESOLVER_STATUS.md](docs/PHASE1_RESOLVER_STATUS.md) | v0.1 implementation state, API surface, env vars, v0.2 backlog |

Older planning documents are in [`docs/deprecated/`](docs/deprecated/).

---

## Version

| Tag | Contents |
|---|---|
| `v0.1.0` | AH-only contract-ready baseline — all resolution paths, HTTP + gRPC, SQLite store |
| `main` | v0.2 sprint 1 — fine-grained `ResolutionStatus`, SQLite WAL + connection pool |

# artifact-handoff

English: [README.md](README.md) | 한국어: [README.ko.md](README.ko.md)

`artifact-handoff`는 Kubernetes 배치 워크로드를 위한 locality-aware artifact handoff 결정 서비스다.

하나의 질문에 답한다: **producer artifact와 consumer binding이 주어졌을 때, consumer는 어디서 실행되어야 하고 bytes를 어떻게 가져와야 하는가?**

`artifact-handoff`는 결정을 반환한다. Kubernetes Job 또는 Pod를 직접 생성하지 않는다.

---

## 핵심 계약

```
RegisterArtifact(artifact)  → AvailabilityState
ResolveHandoff(binding, targetNodeName) → PlacementIntent + MaterializationPlan
NotifyNodeTerminal(sampleRunID, nodeID, attemptID, state)
FinalizeSampleRun(sampleRunID)
EvaluateGC(sampleRunID)
GetSampleRunLifecycle(sampleRunID)
```

**`ResolveHandoff`는 두 개의 결정 객체를 반환한다:**

| 필드 | 값 | Spawner 행동 |
|---|---|---|
| `PlacementIntent.mode` | `none \| preferred_node \| required_node` | PodSpec의 nodeAffinity |
| `MaterializationPlan.mode` | `none \| local_reuse \| remote_fetch` | volume mount 또는 init-container |

**Spawner가 이 결정들을 PodSpec으로 번역한다. `artifact-handoff`는 Kubernetes 리소스를 직접 다루지 않는다.**

---

## Resolution Status

`ResolveHandoff`는 다음 중 하나의 status를 반환한다. Spawner는 이 값으로 분기한다:

| Status | Spawner 행동 |
|---|---|
| `RESOLVED` | 진행 — `PlacementIntent` + `MaterializationPlan` 준비 완료 |
| `PENDING` | producer 아직 terminal 아님 — 대기 후 requeue |
| `MISSING` | producer는 성공했지만 artifact가 등록되지 않음 |
| `PRODUCER_FAILED` | producer 실패 또는 취소 — child 실행 차단 |
| `POLICY_BLOCKED` | `SameNodeOnly` 위반 — fallback 시도 금지 |
| `DIGEST_MISMATCH` | 무결성/재현성 오류 — 즉시 실패 처리 |
| `GC_EXPIRED` | sample run이 GC 대상 — 재실행 또는 실패 전파 |
| `UNAVAILABLE` | artifact는 있지만 URI가 없거나 접근 불가 |

---

## 빠른 시작

```bash
# in-memory store (기본값)
go run ./cmd/artifact-handoff-resolver

# SQLite 영속성
AH_STORE_DSN=sqlite:/data/ah.db go run ./cmd/artifact-handoff-resolver

# Docker / Podman
podman build -t artifact-handoff:latest .
podman run -p 8080:8080 -p 9090:9090 artifact-handoff:latest
```

엔드포인트:
- HTTP: `:8080`
- gRPC: `:9090`
- 메트릭 (Prometheus): `:8080/metrics`
- 헬스체크: `:8080/healthz`

---

## Artifact 식별자

```
sampleRunID / producerNodeID / producerAttemptID / outputName
```

예시: `run-001/node-A/attempt-1/result.json`

`attemptID` 소유권: **JUMI/Executor 생성 → Spawner 전파 → AH 소비**.
AH는 attemptID를 생성하거나 자동 선택하지 않는다.

---

## Consume Policy

| 정책 | 스케줄링 전 (planning) | 스케줄링 후 (post-scheduling) |
|---|---|---|
| `SameNodeOnly` | `required_node` + `local_reuse` | 다른 노드에 배치되면 `POLICY_BLOCKED` |
| `SameNodeThenRemote` | `preferred_node` + `remote_fetch` | fallback 메트릭 포함 `remote_fetch` |
| `RemoteOK` | `none` + `remote_fetch` | `remote_fetch` |

---

## Store 백엔드

| `AH_STORE_DSN` | 백엔드 |
|---|---|
| `memory` (기본값) | In-memory, 재시작 시 소멸 |
| `sqlite:<path>` | WAL 모드 SQLite — 재시작 후에도 유지 |

---

## 개발

```bash
make test          # 단위 + 통합 테스트
make test-regression
make coverage      # HTML 커버리지 리포트 → reports/
make lint          # golangci-lint (없으면 bin/golangci-lint 자동 다운로드)
make fmt           # gofmt + goimports
make vet
make vuln          # govulncheck
```

요구사항: Go 1.25+, CGO 불필요 (`modernc.org/sqlite` pure-Go SQLite 사용).

---

## 문서

| 문서 | 목적 |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | 시스템 아키텍처, 서브시스템 경계, 안정적인 제약 |
| [docs/PHASE1_RESOLVER_STATUS.md](docs/PHASE1_RESOLVER_STATUS.md) | v0.1 구현 상태, API 목록, 환경변수, v0.2 백로그 |

이전 설계 문서는 [`docs/deprecated/`](docs/deprecated/)에 보존되어 있다.

---

## 버전

| 태그 | 내용 |
|---|---|
| `v0.1.0` | AH-only contract-ready baseline — 전체 resolution 경로, HTTP + gRPC, SQLite store |
| `main` | v0.2 sprint 1 — 세분화된 `ResolutionStatus`, SQLite WAL + connection pool |

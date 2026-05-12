# Phase 1 Resolver Status

기준일: `2026-05-12`

## AH v0.1 contract-ready 완료

Step 0~5가 모두 완료되어 AH v0.1이 JUMI/Spawner 통합 준비 상태다.

### 완료된 항목 (Steps 0–5)

**Step 0 — 빌드 기준선 및 도메인 정비**
- `Dockerfile` / `Containerfile`: golang:1.25, go.sum COPY 추가
- `ConsumePolicy.Validate()`: unknown policy → 명시적 에러
- digest: ExpectedDigest 있는데 artifact digest 없으면 에러
- `AvailabilityStateRemoteOnly` 추가, availability state 시맨틱 수정
- `NewService` panic → `(*Service, error)` 반환

**Step 1 — ArtifactKey에 ProducerAttemptID 추가**
- `Artifact.Key()` 형식: `sampleRunID/producerNodeID/producerAttemptID/outputName`
- `Binding`에 `ProducerAttemptID`, `ChildAttemptID` 필드 추가
- `NodeTerminalRecord`에 `AttemptID` 추가
- store 내부 key 구분자 `::` → `/` 통일

**Step 2 — ResolvedHandoff 확장**
- `PlacementIntent{Mode, NodeName}` 추가 (`none | preferred_node | required_node`)
- `MaterializationPlan{Mode, URI, ExpectedDigest}` 추가 (`none | local_reuse | remote_fetch`)
- `Reason`, `Retryable` 필드 추가
- `ResolveHandoffCore` 7개 반환 경로 모두 채움

**Step 3 — A→B→C AH-only simulation test**
- `TestSimulateLinearABC_LocalReuse`
- `TestSimulateLinearABC_RemoteFetch`
- `TestSimulateProducerPending`
- `TestSimulateProducerFailed`
- `TestSimulateDigestMismatch`
- `TestSimulateSameNodeOnlyViolation`
- `TestSimulateGCExpiredRun`

**Step 4 — HTTP / gRPC transport 갱신**
- proto: `PlacementIntent`, `MaterializationPlan` message 추가
- proto: `producer_attempt_id`, `child_attempt_id`, `attempt_id` 필드 추가
- gRPC handler: 모든 신규 필드 매핑 완료
- HTTP 통합 테스트:
  - `TestHTTPResolveHandoff` — `placementIntent`, `materializationPlan` JSON 검증
  - `TestHTTPResolveHandoff_LocalReuse` — local reuse 경로 검증
  - `TestHTTPNotifyTerminal` — `attemptId` 포함 요청 및 store 저장 검증

**Step 5 — SQLite store + restart persistence**
- `SQLiteStore` 구현 (`modernc.org/sqlite`, pure-Go, no CGO)
- `OpenStore(dsn string)` factory: `"memory"` | `"sqlite:<path>"` DSN 라우팅
- `main.go`: `AH_STORE_DSN` 환경변수로 store 선택 (기본값: `"memory"`)
- 테스트: artifact/terminal/lifecycle round-trip, close+reopen 재기동 persistence

---

## 핵심 계약

> **`artifact-handoff`는 Kubernetes Job 또는 Pod를 직접 생성하지 않는다.**

AH는 `ResolveHandoff` 응답으로 두 가지 결정 객체를 반환한다:

- `PlacementIntent` — consumer workload의 locality 방향
- `MaterializationPlan` — consumer node에서 bytes가 어떻게 제공되어야 하는지

Spawner가 이 결정들을 PodSpec으로 번역할 책임을 진다.

---

## 현재 실행 경로

binary: `cmd/artifact-handoff-resolver`

| 환경변수 | 기본값 | 설명 |
|----------|--------|------|
| `AH_ADDR` | `:8080` | HTTP listen 주소 |
| `AH_GRPC_ADDR` | `:9090` | gRPC listen 주소 |
| `AH_STORE_DSN` | `memory` | store 선택 (`memory` 또는 `sqlite:<path>`) |

HTTP endpoints:
- `POST /v1/artifacts:register`
- `GET /v1/artifacts:get?sampleRunId=...&producerNodeId=...&attemptId=...&outputName=...`
- `GET /v1/artifacts:list?sampleRunId=...`
- `POST /v1/handoffs:resolve`
- `POST /v1/nodes:notifyTerminal`
- `POST /v1/sampleRuns:finalize`
- `POST /v1/sampleRuns:evaluateGC`
- `GET /v1/sampleRuns:lifecycle?sampleRunId=...`
- `GET /healthz`
- `GET /metrics`

gRPC service: `ArtifactHandoffResolver` (see `api/proto/ah_v1.proto`)

---

## attemptID 소유권

| 주체 | 역할 |
|------|------|
| JUMI/Executor | attemptID 생성 (DAG 실행 attempt 관리 주체) |
| Spawner | Job/Pod label·annotation에 attemptID 전파 |
| AH | RegisterArtifact·ResolveHandoff에서 받아서 기록 (생성 안 함) |

AH는 "latest successful attempt 자동 선택" 하지 않는다. v0.1에서 `producerAttemptID`는 `ResolveHandoff` 필수 필드다.

---

## 남은 작업 (v0.2+)

- SQLite WAL mode, connection pool 고도화
- `ResolveLatestSuccessfulHandoff` API (latest attempt 자동 선택, 별도 엔드포인트)
- GC 판정 고도화 (artifact 크기/TTL 기반 backlog)
- terminal completeness 판정 고도화
- lifecycle/retention 정책 외부 주입
- JUMI/Spawner 실제 통합 테스트

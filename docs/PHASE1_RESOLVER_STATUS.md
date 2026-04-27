# Phase 1 Resolver Status

기준일: `2026-04-21`

## 완료된 항목

- resolver proto 초안 추가
- resolver binary 엔트리포인트 정리 (`cmd/artifact-handoff-resolver`)
- domain 타입 추가
- in-memory inventory/store 추가
- happy-path resolver service 추가
- HTTP shim 기반 실행 경로 추가
- `RegisterArtifact`, `ResolveHandoff`, `NotifyNodeTerminal` 테스트 추가
- 오래된 controller-biased 문서군을 `docs/deprecated/`로 격리
- `FinalizeSampleRun`, `EvaluateGC`, sample-run lifecycle 최소형 추가
- terminal node 집계와 GC blocked reason 최소 규칙 추가
- 최소 retention window / policy source 규칙 추가

## 현재 실행 경로

- binary: `cmd/artifact-handoff-resolver`
- 기본 주소: `:8080`

HTTP endpoints:
- `POST /v1/artifacts:register`
- `POST /v1/handoffs:resolve`
- `POST /v1/nodes:notifyTerminal`
- `POST /v1/sampleRuns:finalize`
- `POST /v1/sampleRuns:evaluateGC`
- `GET /v1/sampleRuns:lifecycle?sampleRunId=...`
- `GET /healthz`
- `GET /metrics`

## 의도된 위치

- `api/proto/ah_v1.proto`는 cross-repo RPC contract 초안이다.
- `pkg/resolver`는 transport-independent service 로직이다.
- HTTP shim은 초기 integration 검증용이다.
- 실제 gRPC generated code/server는 후속 단계에서 추가한다.
- sample-run lifecycle은 현재 아래 수준의 최소형이다.
  - finalized / finalizedAt
  - retentionPolicySource / retentionDuration / retentionUntil
  - retained artifact count
  - terminal node count
  - succeeded / failed / canceled node count
  - gcEligible / gcEligibleAt / gcBlockedReason

## 남은 작업

- generated code 체인 도입 여부 결정
- 실제 gRPC server 추가
- GC 판정 고도화
- terminal completeness 판정 고도화
- lifecycle/retention 정책 외부 주입
- artifact 크기/TTL 기반 backlog 계산 고도화

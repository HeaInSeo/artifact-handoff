# AH Guardrail Catalog

이 문서는 artifact-handoff 서비스에 적용된 모든 가드레일을 계층별로 정리합니다.
새 가드레일을 추가할 때 이 문서를 먼저 업데이트하세요.

---

## 계층 구조

```
[CI 자동화]  →  [로컬 툴체인]  →  [코드 레벨]  →  [Proto 계약]
```

---

## 1. CI 자동화 가드레일

PR/push마다 자동 실행됩니다. 실패 시 merge 불가입니다.

| 워크플로우 | 파일 | 트리거 조건 | 실행 내용 |
|-----------|------|------------|----------|
| **Lint** | `.github/workflows/lint.yml` | 모든 push/PR (md 제외) | `make lint` (golangci-lint + depguard) |
| **Test** | `.github/workflows/test.yml` | 모든 push/PR (md 제외) | `make test`, `make test-regression`, `make coverage` |
| **Proto Contract** | `.github/workflows/proto-contract.yml` | `api/proto/**`, `buf.yaml`, `buf.gen.yaml` 변경 push/PR | `buf lint`, `buf breaking`, drift 검사 |

### Proto Contract 워크플로우 상세

```
buf lint              → proto 스키마 스타일/규칙 검사 (MINIMAL)
buf breaking          → wire-format 파괴적 변경 차단 (WIRE_JSON)
  push 이벤트         → HEAD~1 기준 (직전 커밋과 비교)
  pull_request 이벤트 → main 기준 (브랜치 전체 비교)
buf generate          → 코드 재생성
git diff --exit-code  → 생성 코드 drift 검사
```

> **단일 브랜치 전략:** main에 직접 push하는 것이 기본 흐름입니다. push 트리거가 주 가드레일이고, pull_request는 예외적으로 브랜치를 사용할 때를 위한 보조입니다.

---

## 2. 로컬 툴체인 가드레일

개발 중 `make <target>`으로 실행합니다.

### 2.1 Go 정적 분석

```bash
make lint           # golangci-lint + depguard
make fmt            # gofmt
make vet            # go vet
```

**설정:** `.golangci.yml` (프로젝트 루트)
**버전:** `golangci-lint v2.11.3` (`bin/golangci-lint`에 설치)

주요 활성화 linter:

| Linter | 목적 |
|--------|------|
| `errcheck` | 에러 무시 차단 |
| `gosimple` | 불필요한 복잡도 제거 |
| `staticcheck` | 고급 정적 분석 |
| `unused` | 미사용 코드 탐지 |
| `depguard` | 금지 패키지 사용 차단 |

### 2.2 보안 스캔

```bash
make lint-security  # gosec (OWASP 기반 패턴 검사)
make vuln           # govulncheck (알려진 CVE 검사, core 패키지)
make vuln-all       # govulncheck (전체 패키지)
```

**결과 파일:** `reports/gosec.txt`, `reports/govulncheck-core.txt`

### 2.3 Proto 가드레일

```bash
make proto          # buf generate (생성 코드 재생성)
make proto-check    # buf lint + buf breaking + drift 검사
```

**설정:** `buf.yaml`, `buf.gen.yaml`
**버전:** `buf v1.54.0` (`bin/buf`에 설치)

---

## 3. 코드 레벨 가드레일

런타임 직전에 코드가 직접 검증합니다.

### 3.1 Store 레벨 — 데이터 무결성

| 검증 | 위치 | 동작 |
|------|------|------|
| **Artifact digest 불변성** | `inventory.MemoryStore.PutArtifact`, `inventory.SQLiteStore.PutArtifact` | 같은 key + 다른 digest → conflict error. 같은 digest → idempotent OK |
| **NodeTerminal state 불변성** | `inventory.MemoryStore.RecordNodeTerminal`, `inventory.SQLiteStore.RecordNodeTerminal` | 같은 attempt + 다른 state → conflict error. 같은 state → idempotent OK |
| **SQLite 원자성** | `SQLiteStore.PutArtifact`, `SQLiteStore.RecordNodeTerminal` | SELECT-then-write를 트랜잭션으로 감싸 TOCTOU 제거 |

### 3.2 Service 레벨 — 입력 검증

| 검증 | 위치 |
|------|------|
| `producerAttemptID` 필수 | `RegisterArtifactCore`, `ResolveHandoffCore`, `NotifyNodeTerminalCore` |
| `ConsumePolicy` 유효성 | `ConsumePolicy.Validate()` |
| `terminalState` 허용값 | `NotifyNodeTerminalCore` (Succeeded/Failed/Canceled만 허용) |
| `ArtifactID` canonical 검증 | `RegisterArtifactCore` (canonical ID와 불일치 시 거부) |

### 3.3 HTTP 레벨

| 검증 | 위치 | 값 |
|------|------|-----|
| Body 크기 제한 | `NewHTTPHandler` | 1 MiB (`MaxBytesReader`) |

---

## 4. Proto 계약 가드레일

**소유권:** `api/proto/ah_v1.proto` (→ `docs/CONTRACT_OWNERSHIP.md`)

| 규칙 | 적용 방법 |
|------|----------|
| Field number 재사용 금지 | `buf breaking` (WIRE_JSON), proto `reserved` 선언 |
| 삭제 필드 이름 재사용 금지 | proto `reserved "field_name"` |
| 파괴적 변경 = PR 차단 | `buf breaking --against '.git#branch=main'` |
| 생성 코드 drift = PR 차단 | `buf generate && git diff --exit-code` |

---

## 5. 가드레일 추가 절차

새 가드레일을 도입할 때:

1. 이 문서의 해당 계층에 항목 추가
2. 로컬 툴이면 `Makefile` 타겟 추가
3. CI 강제가 필요하면 `.github/workflows/` 워크플로우 추가 또는 기존 워크플로우에 스텝 추가
4. 코드 레벨이면 단위 테스트로 경계 케이스 커버

---

## 6. 미적용 가드레일 (계획)

| 항목 | 이유 | 예정 |
|------|------|------|
| buf STANDARD lint | 디렉토리 구조 재편 필요 (`ah/v1/` 레이아웃) | Proto v1 정식 릴리즈 전 |
| Integration test (DB) | SQLite store TOCTOU 시나리오 실사 검증 | Phase 2 |
| Rate limiting | Phase 2 범위 | Phase 2 |
| 인증/인가 | Phase 2 범위 | Phase 2 |

# artifact-handoff

`artifact-handoff`는 `artifact-handoff-poc`의 제품 지향 후속 저장소다.

이 저장소는 `artifact-handoff-poc`에서 검증한 아이디어를 바탕으로, locality-aware artifact handoff를 위한 product-owned resolver semantics를 갖는 실제 Go 기반 Kubernetes 프로젝트로 확장하기 위해 존재한다.

참고 저장소:

- `artifact-handoff-poc`: 나중에 공개 저장소로 게시되면 해당 GitHub 저장소, 현재는 주요 설계 근거로 사용되는 sibling validation 저장소

## 이 저장소가 존재하는 이유

`artifact-handoff-poc`는 이미 다음의 좁고 핵심적인 질문을 검증했다.

- artifact 위치를 기록할 수 있는가
- 그 위치를 바탕으로 same-node reuse를 유도할 수 있는가
- same-node reuse가 불가능할 때 cross-node peer fetch가 가능한가
- replica-aware fallback이 실제로 동작하는가

하지만 PoC는 의도적으로 작다.

- Python agent와 catalog
- script-assisted placement
- 좁은 랩 검증
- 의도적으로 제한된 durability, retry, policy, control-plane shape

이 저장소는 그 검증된 사실들 위에 실제 제품 경로를 세우기 위해 존재한다.

## 제품 방향

현재 의도하는 방향은 다음과 같다.

- Kubernetes batch integration을 위한 Go 기반 resolver service
- product-owned artifact semantics
- producer locality와 remote-capable fallback을 함께 읽는 placement resolution
- 교체 가능한 transport/cache backend
- Dragonfly는 제품 의미의 소유자가 아니라 backend 후보

## 비목표

현재 단계에서 이 저장소는 다음을 목표로 하지 않는다.

- 범용 P2P distribution platform
- 범용 storage product
- Dragonfly를 제품 코어로 직접 포크
- PoC 스크립트나 Python 구현을 그대로 유지하는 것

## 초기 범위

첫 구현 단계는 다음을 먼저 세워야 한다.

1. 제품 용어와 API 경계
2. resolver-service architecture
3. backend adapter 경계
4. 최소 Go 프로젝트 레이아웃
5. PoC 검증 결과에서 제품 구현으로 넘어가는 migration path

## 설계 문서

상위 진입 문서:

- 영문: [docs/PRODUCT_IMPLEMENTATION_DESIGN.md](docs/PRODUCT_IMPLEMENTATION_DESIGN.md)
- 한글: [docs/PRODUCT_IMPLEMENTATION_DESIGN.ko.md](docs/PRODUCT_IMPLEMENTATION_DESIGN.ko.md)

보조 설계 문서:

- Architecture
  - 영문: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
  - 한글: [docs/ARCHITECTURE.ko.md](docs/ARCHITECTURE.ko.md)
- Domain Model
  - 영문: [docs/DOMAIN_MODEL.md](docs/DOMAIN_MODEL.md)
  - 한글: [docs/DOMAIN_MODEL.ko.md](docs/DOMAIN_MODEL.ko.md)
- Placement And Fallback Policy
  - 영문: [docs/PLACEMENT_AND_FALLBACK_POLICY.md](docs/PLACEMENT_AND_FALLBACK_POLICY.md)
  - 한글: [docs/PLACEMENT_AND_FALLBACK_POLICY.ko.md](docs/PLACEMENT_AND_FALLBACK_POLICY.ko.md)
- Dragonfly Adapter Spec
  - 영문: [docs/DRAGONFLY_ADAPTER_SPEC.md](docs/DRAGONFLY_ADAPTER_SPEC.md)
  - 한글: [docs/DRAGONFLY_ADAPTER_SPEC.ko.md](docs/DRAGONFLY_ADAPTER_SPEC.ko.md)

Deprecated 문서군:

- resolver service 기준선과 충돌하는 오래된 문서는 [`docs/deprecated/`](docs/deprecated/) 아래로 이동했다

## `artifact-handoff-poc`와의 관계

이 저장소는 `artifact-handoff-poc`의 결과를 명시적으로 참고한다.

검증된 입력으로 계승하는 것:

- same-node reuse semantics
- cross-node peer fetch semantics
- node-local forensic failure recording
- producer-first current implementation truth
- replica fallback evidence
- dynamic placement boundary findings
- Dragonfly-as-backend boundary judgment

이 저장소에서 다시 설계하는 것:

- product-owned API와 object model
- resolver-service architecture
- placement-resolution ownership
- retry와 fallback 정책
- durable metadata/store choices
- backend abstraction과 lifecycle

## 저장소 상태

이 저장소는 현재 initial design-and-scaffold phase에 있다.

현재 구현 우선순위는 다음과 같다.

- Phase 1 resolver contract scaffold
- proto 기반 RPC 경계 초안
- in-memory inventory/store
- happy-path `RegisterArtifact`, `ResolveHandoff`, `NotifyNodeTerminal`
- sample-run lifecycle hook `FinalizeSampleRun`, `EvaluateGC`

현재 진입점:

- resolver binary: `cmd/artifact-handoff-resolver`

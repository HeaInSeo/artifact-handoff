# PRODUCT_IMPLEMENTATION_DESIGN

## 1. 목적

이 문서는 `artifact-handoff`의 초기 제품 구현 설계를 정의한다.

이 문서는 `artifact-handoff-poc`에서 검증된 사실을 제품 지향 Go 프로젝트 방향으로 번역한다.

이 문서는 PoC 설계를 그대로 반복하는 문서가 아니다. 이 문서는 다음을 결정하는 첫 제품 설계 컷이다.

- 제품이 소유해야 하는 것
- 제품이 소유하지 않아야 하는 것
- 어떤 PoC truth를 이제 고정 입력으로 볼 것인지
- resolver, placement, catalog, backend 경계를 어디에 둘 것인지

이 문서는 상위 개요 문서다.
세부 설계는 아래 문서들에서 이어진다.

- [ARCHITECTURE.ko.md](ARCHITECTURE.ko.md)
- [DOMAIN_MODEL.ko.md](DOMAIN_MODEL.ko.md)
- [API_OBJECT_MODEL.ko.md](API_OBJECT_MODEL.ko.md)
- [STATE_AND_STATUS_MODEL.ko.md](STATE_AND_STATUS_MODEL.ko.md)
- [PLACEMENT_AND_FALLBACK_POLICY.ko.md](PLACEMENT_AND_FALLBACK_POLICY.ko.md)
- [RETRY_AND_RECOVERY_POLICY.ko.md](RETRY_AND_RECOVERY_POLICY.ko.md)
- [OBSERVABILITY_MODEL.ko.md](OBSERVABILITY_MODEL.ko.md)
- [CRD_INTRODUCTION_STRATEGY.ko.md](CRD_INTRODUCTION_STRATEGY.ko.md)
- [DRAGONFLY_ADAPTER_SPEC.ko.md](DRAGONFLY_ADAPTER_SPEC.ko.md)

## 2. 이 설계의 source of truth

이 저장소는 `artifact-handoff-poc`를 주요 validation reference로 명시적으로 사용한다.

PoC가 이미 세운 최소 검증 사실은 다음과 같고, 이 설계는 이를 고정 입력으로 본다.

1. artifact 위치를 metadata로 기록할 수 있다
2. 기록된 producer locality를 바탕으로 same-node reuse를 검증할 수 있다
3. cross-node peer fetch를 검증할 수 있다
4. peer fetch 이후 second-hit local reuse를 검증할 수 있다
5. node-local failure metadata는 유용하며, 너무 이르게 global failure registry로 뭉개면 안 된다
6. producer failure 이후 replica fallback이 가능하다
7. 현재 producer-first ordering은 implementation truth이지만 final policy는 아니다
8. dynamic placement는 숨겨진 transport behavior가 아니라 product-owned resolution step으로 읽어야 한다
9. Dragonfly는 product semantics의 소유자가 아니라 replaceable backend로 읽어야 한다

## 3. 제품 문제 정의

`artifact-handoff`가 풀려는 제품 문제는 다음과 같다.

- producer workload가 artifact를 만든다
- 시스템은 artifact의 위치와 integrity를 기록해야 한다
- consumer workload는 가능하면 locality를 우선해야 한다
- locality가 불가능하면 artifact-aware semantics를 유지한 채 remote-capable path를 열어야 한다
- 제품은 artifact-handoff policy와 transport backend 구현을 분리해야 한다

이것은 일반적인 "download acceleration" 문제가 아니다.
이것은 product-owned artifact handoff 및 placement-resolution 문제다.

## 4. 제품 목표

첫 단계 목표는 다음과 같다.

1. product-owned metadata와 policy model 정의
2. resolver-owned placement resolution path 정의
3. simple local implementation과 Dragonfly를 모두 지원할 수 있는 backend abstraction 정의
4. PoC가 보여준 유용한 구분을 보존하는 failure model 정의
5. script-assisted validation을 넘어서 확장 가능한 Go 기반 resolver-service architecture 수립

## 5. 비목표

초기 구현은 다음을 목표로 하지 않는다.

- full workflow engine
- 즉시 custom scheduler로 Kubernetes scheduling 교체
- 가능한 모든 storage backend 구현
- retry/recovery 세부 정책을 처음부터 모두 확정
- Dragonfly 내부를 제품 코어로 흡수

## 6. Product-owned semantics

제품은 다음 semantics를 직접 소유해야 한다.

- `artifactId`
- producer identity와 producer locality
- consume policy
- locality preference와 downgrade semantics
- replica interpretation
- placement-resolution policy
- failure attribution semantics
- retention 및 cleanup policy

실제 artifact bytes가 나중에 Dragonfly 같은 backend에 놓이더라도, 이 semantics는 제품이 소유해야 한다.

## 7. 제안하는 시스템 shape

초기 제품 shape는 네 가지 큰 영역을 가져야 한다.

1. API와 object model
2. resolver service와 placement resolution
3. metadata 및 state services
4. backend adapters

논리 구조:

```text
Producer workload
  -> artifact registration
  -> metadata persisted

Consumer request / workflow intent
  -> consume policy read
  -> placement resolution
  -> local-preferred or remote-capable decision
  -> backend ensure-on-node
  -> runtime execution
```

## 8. 핵심 제품 개념

### 8.1 Artifact

제품 artifact는 handoff identity의 기본 단위다.

최소 필드:

- `ArtifactID`
- `Digest`
- `ProducerRef`
- `ProducerNode`
- `BackendRef`
- `State`

### 8.2 Consume Policy

Consume policy는 어떤 locality behavior가 허용되는지 정의한다.

초기 모드:

- `SameNodeOnly`
- `SameNodeThenRemote`
- `RemoteOK`

### 8.3 Placement Resolution

Placement resolution은 artifact-aware policy와 current observation을 concrete Kubernetes placement로 바꾸는 product-owned step이다.

이 레이어는 backend에 위임되면 안 된다.

### 8.4 Backend Reference

제품은 backend internals를 전역에 퍼뜨리지 말고 backend reference를 저장해야 한다.

예:

- local backend record
- Dragonfly task identifier
- 이후 backend-specific locator

## 9. 추천 interface split

설계는 PoC follow-up 방향과 비슷한 분리를 유지해야 한다.

### 9.1 `ArtifactBinding`

consumer가 어떤 artifact를 어떤 방식으로 소비해야 하는지를 설명한다.

최소 필드:

- `ArtifactID`
- `ConsumePolicy`
- `Required`

### 9.2 `PlacementIntent`

Kubernetes translation 이전의 product-owned locality intent다.

최소 필드:

- `Mode`
- `SourceArtifactID`
- `Reason`

### 9.3 `ResolvedPlacement`

Pod 또는 Job spec에 merge할 수 있는 concrete placement output이다.

최소 필드:

- `NodeSelector`
- `RequiredNodeAffinity`
- `PreferredNodeAffinity`
- `Reason`

## 10. Resolver 책임

제품 resolver service는 다음을 소유해야 한다.

- artifact registration 처리
- artifact status updates
- placement resolution
- same-node-required에서 remote-capable path로 가는 downgrade judgment
- backend orchestration request
- artifact availability status transition

Resolver service가 transport implementation 자체가 되면 안 된다.

## 11. Placement resolution path

Resolution path는 명시적이어야 한다.

추천하는 first-cut flow:

1. `ArtifactBinding` 읽기
2. artifact metadata 읽기
3. current workload intent 읽기
4. 필요 시 observable scheduling state 읽기
5. same-node-required / same-node-preferred / remote-capable path 해결
6. `ResolvedPlacement` 생성
7. concrete Kubernetes object에 merge

이는 placement가 숨겨진 side effect가 아니라 explicit product logic이 되어야 한다는 PoC 판단을 유지한다.

## 12. Failure 및 fallback 설계 입력

PoC가 보여준 다음 구분은 유지해야 한다.

- control-plane lookup failure
- peer transport failure
- producer-side integrity rejection
- consumer-side integrity mismatch
- local verification failure

초기 제품 설계는 이를 너무 빨리 하나의 generic failure로 합치지 말아야 한다.

Fallback는 결국 다음을 함께 읽어야 한다.

- consume policy
- producer locality
- replica metadata
- backend state
- scheduling observables

## 13. Backend abstraction

제품은 모든 concrete implementation 위에 backend interface를 정의해야 한다.

추천하는 first-cut interface:

- `Put`
- `EnsureOnNode`
- `Stat`
- `Warm`
- `Evict`

중요 원칙:

- `ArtifactID`는 product-owned
- backend task identifier는 adapter-owned

제품은 Dragonfly CLI나 Dragonfly-native identifier를 top-level product contract로 노출하면 안 된다.

## 14. Dragonfly 위치

Dragonfly는 backend candidate다.

이 제품 설계에서 Dragonfly는 다음으로 취급해야 한다.

- replaceable
- adapter-bound
- transport-oriented

Dragonfly가 정의하면 안 되는 것:

- artifact identity
- placement policy
- consume policy
- product failure semantics

## 15. Metadata와 state 방향

제품은 PoC보다 richer state model이 필요하겠지만, state model은 조심스럽게 키워야 한다.

초기 방향:

- producer location을 explicit하게 유지
- replica visibility를 explicit하게 유지
- local forensic usefulness를 보존
- premature global failure-state explosion을 피함

후보 상위 artifact states:

- `Registered`
- `AvailableOnProducer`
- `Replicated`
- `Unavailable`
- `Failed`

이들은 design candidate이며, 아직 고정 API commitment는 아니다.

## 16. 제안하는 Go 프로젝트 레이아웃

초기 레이아웃:

```text
.
├── cmd/
│   └── artifact-handoff-resolver/
├── docs/
├── internal/
│   ├── api/
│   ├── resolver/
│   ├── placement/
│   ├── backend/
│   ├── catalog/
│   └── runtime/
└── pkg/
```

가이드:

- `internal/api`: 내부 요청 및 domain model
- `internal/resolver`: request handling 및 orchestration logic
- `internal/placement`: placement resolution logic
- `internal/backend`: backend abstraction 및 adapter
- `internal/catalog`: metadata access 및 persistence boundary
- `internal/runtime`: execution-facing helper

이 레이아웃은 의도적으로 작게 시작하고, 구현 압력이 생길 때만 확장해야 한다.

## 17. 초기 구현 단계

### Phase 1. Design and skeleton

- repository scaffold
- README
- product design document
- package boundaries

### Phase 2. Domain model

- artifact identity model
- consume policy model
- placement-resolution interfaces
- backend interfaces

### Phase 3. Basic resolver path

- artifact registration flow
- initial placement-resolution path
- status update path

### Phase 4. First backend

- 개발용 simple local/backend adapter
- 아직 Dragonfly dependency 없음

### Phase 5. Dragonfly adapter spike

- adapter-only implementation
- version-pinned validation
- no product-semantic leakage

## 18. 열려 있는 설계 질문

다음은 의도적으로 열어 둔다.

- 정확한 API surface와 CRD를 즉시 도입할지 여부
- catalog/state의 persistence model
- node-local forensic data를 어디까지 중앙화할지
- downgrade와 retry가 어떻게 상호작용할지
- replica freshness와 ranking을 어떻게 읽을지
- runtime execution의 어느 정도를 이 저장소가 직접 소유할지

## 19. 현재 결정

현재 구현 방향 결정은 다음과 같다.

- repository name: `artifact-handoff`
- language: Go
- product type: Kubernetes batch integration용 long-lived resolver service
- PoC reference: `artifact-handoff-poc`
- backend strategy: replaceable adapters
- Dragonfly role: backend candidate only

## 20. 다음 단계

이 문서 다음의 직접 단계는 다음을 정의하는 것이다.

1. 첫 package-level interface
2. minimum domain type
3. `cmd/` 아래 첫 executable scaffold

현재 저장소 단계에서는, 더 직접적인 문서 작업은 다음과 같다.

1. architecture boundary hardening
2. domain-model hardening
3. placement and fallback policy hardening
4. Dragonfly adapter boundary hardening

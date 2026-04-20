# ARCHITECTURE

## 1. 목적

이 문서는 `artifact-handoff`의 제품 아키텍처를 정의한다.

이 문서는 다음 질문에 답한다.

- 주요 제품 subsystem은 무엇인가
- 각 subsystem은 어떤 책임을 소유하는가
- 구현이 자라더라도 어떤 경계는 안정적으로 유지되어야 하는가
- 제품은 Kubernetes runtime, metadata store, transport backend와 어떤 관계를 가져야 하는가

이 문서는 architecture 중심 문서다.
domain 의미, policy semantics, backend-specific contract는 별도 문서에서 정의한다.

관련 문서:

- [PRODUCT_IMPLEMENTATION_DESIGN.ko.md](PRODUCT_IMPLEMENTATION_DESIGN.ko.md)
- [DOMAIN_MODEL.ko.md](DOMAIN_MODEL.ko.md)
- [PLACEMENT_AND_FALLBACK_POLICY.ko.md](PLACEMENT_AND_FALLBACK_POLICY.ko.md)
- [DRAGONFLY_ADAPTER_SPEC.ko.md](DRAGONFLY_ADAPTER_SPEC.ko.md)

## 2. 아키텍처적 위치

`artifact-handoff`는 Kubernetes-native control-plane project로 만들어져야 한다.

이 저장소는 다음이 아니다.

- 범용 file transfer daemon
- standalone storage product
- 첫 단계부터 scheduler replacement
- Dragonfly-derived product

핵심 역할은 다음이다.

- artifact handoff semantics 소유
- locality-aware placement intent 해결
- backend abstraction을 통한 artifact availability 조정

## 3. PoC에서 온 아키텍처 driver

`artifact-handoff-poc`가 고정한 사실 중 다음이 아키텍처를 이끈다.

1. producer locality는 중요하며 기록 가능하다
2. same-node reuse는 실제로 유용한 경로다
3. remote peer fetch는 실제로 유용한 fallback path다
4. local forensic trace는 유용하며 너무 빨리 지워지면 안 된다
5. source selection은 backend-defined가 아니라 product-readable이어야 한다
6. placement는 script-only orchestration이 아니라 explicit control-plane logic이 되어야 한다
7. Dragonfly는 replaceable이어야 한다

## 4. Top-level subsystem

아키텍처는 다음 top-level subsystem으로 나뉘어야 한다.

1. API 및 domain layer
2. controller layer
3. placement-resolution layer
4. metadata layer
5. backend adapter layer
6. runtime integration layer
7. observability 및 operations layer

## 5. API 및 domain layer

이 레이어는 product vocabulary를 소유한다.

책임:

- artifact identity 정의
- consume-policy semantics 정의
- placement intent와 placement output 정의
- backend-neutral request/result shape 정의

이 레이어는 다음을 새지 않아야 한다.

- Dragonfly-native identifier
- raw Kubernetes scheduling detail만을 제품 언어로 쓰는 것
- backend-specific transfer semantics

## 6. Controller layer

Controller layer는 주요 orchestrator다.

책임:

- product state transition reconcile
- artifact 및 workload intent 해석
- placement resolution 호출
- backend operation 호출
- product status 갱신
- downgrade와 fallback entry logic 소유

Controller layer는 다음을 하면 안 된다.

- transport backend logic 직접 내장
- storage engine화
- generic workflow engine화

## 7. Placement-resolution layer

이 레이어는 artifact-aware policy를 concrete placement output으로 바꾼다.

입력:

- artifact metadata
- consume policy
- producer locality
- replica visibility
- 필요 시 observable scheduling/runtime state

출력:

- concrete placement constraint
- placement reason
- downgrade 또는 remote-capable decision context

이 레이어는 product-owned이며 backend adapter 위에 있어야 한다.

## 8. Metadata layer

Metadata layer는 product-readable artifact state를 소유한다.

책임:

- product artifact record 저장
- producer 및 replica visibility 저장
- placement/fallback에 필요한 state 노출
- 유용한 failure attribution 보존

중요 경계:

- metadata state는 product state다
- backend task state는 adapter-facing state다

두 레이어는 서로 참조할 수 있지만, 하나로 뭉개지면 안 된다.

## 9. Backend adapter layer

이 레이어는 artifact transport와 storage backend를 추상화한다.

책임:

- payload bytes 업로드 또는 등록
- target node에서 availability 보장
- backend-local status 노출
- 선택적으로 warm/evict 수행

이 레이어가 소유하면 안 되는 것:

- artifact identity
- same-node와 remote 정책
- placement policy
- product-level failure semantics

## 10. Runtime integration layer

이 레이어는 제품이 Kubernetes workload execution과 맞닿는 지점이다.

가능한 책임:

- `ResolvedPlacement`를 Job 또는 Pod spec에 merge
- runtime object에 product-readable reason annotation 추가
- artifact consumer에 필요한 runtime configuration 전달

이 레이어는 얇게 유지해야 한다.
이 레이어는 translation boundary이지, product semantics의 소유자가 아니다.

## 11. Observability 및 operations layer

이 레이어는 이후 다음을 노출해야 한다.

- controller event
- artifact status transition
- placement decision
- backend operation outcome
- failure attribution

PoC는 local forensic detail이 유용하다는 것을 보여줬다.
제품 아키텍처는 그 가치를 보존하면서 점진적으로 centralized visibility를 추가해야 한다.

## 12. End-to-end 논리 흐름

의도하는 상위 흐름은 다음과 같다.

```text
Producer completes
  -> product artifact record created or updated
  -> producer locality recorded
  -> backend reference recorded

Consumer intent appears
  -> artifact binding read
  -> placement resolution runs
  -> concrete placement emitted
  -> runtime object updated or created
  -> backend ensure-on-node path runs if needed
  -> status and forensic traces updated
```

## 13. 안정적으로 유지해야 할 경계

초기 구현 동안 다음 경계는 안정적으로 유지되어야 한다.

### 13.1 Product semantics 대 backend semantics

Product semantics는 `artifact-handoff` 안에 남아야 한다.

### 13.2 Placement resolution 대 runtime translation

Placement resolution은 결정한다.
Runtime translation은 적용한다.

### 13.3 Product metadata 대 backend status

제품은 artifact availability의 의미를 소유한다.
Backend는 implementation-local execution state를 노출한다.

### 13.4 Control plane 대 data plane

Control plane은 intent를 결정하고 state를 추적한다.
Data plane은 bytes를 전달하거나 이용 가능하게 만든다.

## 14. 첫 단계 아키텍처 제약

초기 아키텍처는 다음을 우선 최적화해야 한다.

- explicit boundary
- 낮은 개념 결합도
- backend replaceability
- policy clarity

다음은 초기 최적화 대상이 아니다.

- 최대 feature breadth
- microservice fragmentation
- premature plugin ecosystem

## 15. 아키텍처 리스크

주요 리스크는 다음과 같다.

1. backend-native identity가 product model로 새어 들어오는 것
2. placement logic을 runtime glue 안에 숨기는 것
3. metadata state machine을 너무 빨리 과도하게 확장하는 것
4. failure를 너무 중앙화해서 local forensic 의미를 잃는 것
5. 첫 implementation shortcut을 장기 아키텍처로 굳혀 버리는 것

## 16. 현재 아키텍처 결정

현재 아키텍처 결정은 다음과 같다.

- `artifact-handoff`는 control-plane-first Go project가 된다
- control plane은 artifact semantics, placement resolution, fallback entry logic을 소유한다
- Dragonfly를 포함한 backend는 adapter boundary 뒤에 둔다
- runtime object mutation은 translation layer이지 제품 코어가 아니다


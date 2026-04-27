# API_OBJECT_MODEL

## 1. 목적

이 문서는 `artifact-handoff`의 초기 API 및 object model 방향을 정의한다.

이 문서는 다음 질문에 답한다.

- 어떤 product object가 존재해야 하는가
- 그 object의 어떤 부분이 `spec`에 속하고 어떤 부분이 `status`에 속하는가
- 어떤 필드가 product-owned로 남아야 하는가
- backend-native meaning이 새지 않으면서 object model이 domain model과 어떤 관계를 가져야 하는가

이 문서는 CRD나 wire format을 최종 확정하지 않는다.
이 문서는 첫 제품 object model의 shape와 책임을 고정한다.

관련 문서:

- [PRODUCT_IMPLEMENTATION_DESIGN.ko.md](PRODUCT_IMPLEMENTATION_DESIGN.ko.md)
- [ARCHITECTURE.ko.md](ARCHITECTURE.ko.md)
- [DOMAIN_MODEL.ko.md](DOMAIN_MODEL.ko.md)
- [STATE_AND_STATUS_MODEL.ko.md](STATE_AND_STATUS_MODEL.ko.md)
- [PLACEMENT_AND_FALLBACK_POLICY.ko.md](PLACEMENT_AND_FALLBACK_POLICY.ko.md)

## 2. Object-model 원칙

API 및 object model은 다음 원칙을 따라야 한다.

1. product intent는 `spec`에 들어가야 한다
2. 관측된 product meaning은 `status`에 들어가야 한다
3. backend execution detail이 top-level product object를 지배하면 안 된다
4. placement intent는 explicit하게 남아야 한다
5. object design은 PoC의 유용한 구분을 보존해야 하지만 PoC 내부 shape를 그대로 복사하면 안 된다

## 3. 초기 object set

첫 product object set은 작게 유지해야 한다.

추천하는 초기 object:

1. `Artifact`
2. `ArtifactBindingPolicy`
3. `ArtifactPlacement`
4. `ArtifactBackendPolicy`

이 모두가 즉시 CRD가 되어야 하는 것은 아니다.
하지만 먼저 안정화해야 할 개념 object로는 이 구성이 맞다.

## 4. `Artifact`

`Artifact`는 주요 product object다.

이 object는 다음을 나타낸다.

- product-owned artifact identity
- producer locality와 integrity anchor
- product-visible availability 및 status

### 4.1 `Artifact.spec`

추천하는 개념 필드:

- `artifactID`
- `digest`
- `producerRef`
- `producePolicy`
- `backendPolicyRef`

`spec`는 intended identity와 producer-facing input을 설명해야 하며, transient backend result를 넣으면 안 된다.

### 4.2 `Artifact.status`

추천하는 개념 필드:

- `phase`
- `producerNode`
- `producerAvailability`
- `replicas`
- `backendRef`
- `placementSummary`
- `failureSummary`

이 구조는 runtime discovery가 일어날 때 spec를 다시 쓰지 않고 observed artifact meaning을 status에 남길 수 있게 한다.

## 5. `ArtifactBindingPolicy`

이 object는 consumer가 artifact를 어떻게 사용할 수 있는지를 설명한다.

이 object는 runtime scheduling syntax와 독립적으로 product consume semantics를 표현한다.

### 5.1 왜 이 object가 필요한가

제품은 consume behavior를 backend setting이나 raw workload annotation 안에 숨기면 안 된다.

이 object는 다음 결정을 explicit하게 만든다.

- same-node가 required인가
- same-node가 preferred인가
- remote access가 허용되는가
- 어떤 fallback path가 유효한가

### 5.2 `ArtifactBindingPolicy.spec`

추천하는 개념 필드:

- `consumePolicy`
- `required`
- `fallbackPolicy`
- `orderingPolicy`

### 5.3 `ArtifactBindingPolicy.status`

첫 단계에서는 이 object가 많은 status를 가질 필요는 없다.

status를 두더라도 최소한이면 된다.

- `accepted`
- `validationErrors`

## 6. `ArtifactPlacement`

이 object는 product level의 placement-resolution output 또는 intent를 나타낸다.

이 object가 필요한 이유는, 제품이 placement meaning을 raw Kubernetes object mutation과 분리해 유지해야 하기 때문이다.

### 6.1 무엇을 담아야 하는가

- source artifact reference
- placement mode
- resolved locality target
- downgrade 및 fallback reasoning
- runtime translation summary

### 6.2 `ArtifactPlacement.spec`

추천하는 개념 필드:

- `artifactRef`
- `placementIntent`
- `consumePolicyRef`
- `requestedBy`

### 6.3 `ArtifactPlacement.status`

추천하는 개념 필드:

- `resolvedPlacement`
- `downgraded`
- `downgradeReason`
- `remoteCapableOpened`
- `observedTrigger`

이 지점에서 same-node-required와 preferred behavior를 읽을 수 있어야 한다.

## 7. `ArtifactBackendPolicy`

이 object는 backend choice와 backend-specific knob를 core artifact identity 밖으로 빼기 위해 필요하다.

이 object는 다음을 나타낸다.

- 어떤 backend type이 허용되거나 선호되는가
- 어떤 backend option이 policy input인가
- backend가 어떤 lifecycle expectation을 따라야 하는가

### 7.1 `ArtifactBackendPolicy.spec`

추천하는 개념 필드:

- `backendType`
- `warmAllowed`
- `evictionPolicy`
- `replicaHintPolicy`
- `integrityPolicy`

### 7.2 `ArtifactBackendPolicy.status`

첫 단계에서는 최소 상태만 가지면 된다.

- `accepted`
- `backendCompatibility`

## 8. 왜 spec와 status를 깨끗하게 나눠야 하는가

Object model은 다음을 강제해야 한다.

- `spec`: 사용자 또는 제품이 의도하는 것
- `status`: 시스템이 현재 관측한 것

피해야 할 나쁜 패턴:

- 관측된 producer node를 `spec`에 쓰는 것
- backend task ID를 `spec`에서 main artifact identity처럼 저장하는 것
- 모든 local error를 top-level spec field에 직접 넣는 것

## 9. Object 관계

유용한 초기 관계 모델은 다음과 같다.

```text
Artifact
  <- referenced by ArtifactPlacement
  <- governed by ArtifactBindingPolicy
  <- shaped by ArtifactBackendPolicy
```

consumer 또는 workflow-facing system은 다음을 수행하게 된다.

1. `Artifact`를 생성하거나 참조
2. `ArtifactBindingPolicy`를 선택하거나 상속
3. `ArtifactPlacement`를 유발하거나 요청
4. controller logic이 참조된 `ArtifactBackendPolicy`를 사용하도록 둔다

## 10. Object granularity 가이드

첫 구현은 다음 두 극단을 모두 피해야 한다.

- identity, policy, placement, backend setting, 모든 observation을 하나의 giant object에 넣는 것
- 의미가 충분히 안정되기 전에 너무 많은 tiny object로 쪼개 orchestration overhead를 만드는 것

추천 접근:

- `Artifact`를 primary로 둔다
- 나머지 object는 중요한 policy boundary를 보호하는 용도로 둔다

## 11. 후보 minimal first cut

실제 첫 shipped object set은 전체 개념 set보다 더 작을 수 있다.

합리적인 first cut:

1. `Artifact`
2. embedded binding-policy field
3. embedded placement-status field
4. backend-policy reference

이것은 first implementation을 관리 가능한 수준으로 유지하면서도 진화 여지를 남긴다.

## 12. Object model 안의 backend identity 규칙

Object model은 다음을 강제해야 한다.

1. `artifactID`는 product-owned
2. `backendRef`는 status 또는 adapter-facing status subfield에 둔다
3. Dragonfly-native identity가 product identity를 대체하면 안 된다

이것이 backend-driven coupling을 막는 핵심 보호장치다.

## 13. Object model 안의 placement identity 규칙

Object model은 다음도 강제해야 한다.

1. placement intent는 concrete K8s placement와 다르다
2. resolved placement는 consume policy와 다르다
3. fallback trigger는 final artifact failure와 다르다

이 구분이 있어야 제품이 설명 가능하게 남는다.

## 14. Status 설계 방향

제품은 uncontrolled append-only detail보다 summary-style status를 선호해야 한다.

Top-level status는 다음을 강조해야 한다.

- current phase
- current producer 및 replica visibility
- current placement result
- current backend result summary
- current failure summary

상세 trace는 다른 곳에 둘 수 있다.
첫 product object model을 그것으로 압도하면 안 된다.

## 15. Object model이 해서는 안 되는 것

초기 object model은 다음을 피해야 한다.

1. backend-native API를 product object model로 그대로 노출하는 것
2. 모든 policy를 workload annotation으로 밀어 넣는 것
3. placement intent, execution result, forensic trace를 하나의 필드에 섞는 것
4. PoC script shape를 곧바로 product API로 착각하는 것

## 16. 현재 설계 결정

현재 결정은 다음과 같다.

- product object model은 product-first여야 한다
- `Artifact`가 anchor object가 되어야 한다
- placement, binding, backend concern은 first cut이 compact하게 시작하더라도 explicit하게 남아야 한다
- spec/status separation은 처음부터 유지해야 한다

## 17. 다음 후속 문서

다음으로 유용한 후속 문서는 다음과 같다.

1. `RETRY_AND_RECOVERY_POLICY`
2. `OBSERVABILITY_MODEL`
3. `CRD_INTRODUCTION_STRATEGY`


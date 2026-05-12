# DOMAIN_MODEL

## 1. 목적

이 문서는 `artifact-handoff`의 제품 domain model을 정의한다.

이 문서는 Go type definition 파일이 아니다.
이 문서의 목적은 구현 전에 주요 제품 개념의 의미를 먼저 고정하는 것이다.

## 2. 모델링 원칙

Domain model은 다음 원칙을 따라야 한다.

1. product-owned meaning 우선
2. backend-specific identifier는 top-level product identity가 아님
3. placement intent는 concrete Kubernetes placement와 다름
4. failure attribution은 모델의 일부지만 조심스럽게 키워야 함
5. 현재 PoC truth는 입력이지, hard-coded final policy가 아님

## 3. 핵심 domain entity

최소 제품 domain은 다음을 포함해야 한다.

1. `Artifact`
2. `ArtifactBinding`
3. `ConsumePolicy`
4. `PlacementIntent`
5. `ResolvedPlacement`
6. `Replica`
7. `BackendRef`
8. `FailureRecord`

## 4. Artifact

`Artifact`는 주요 제품 entity다.

의미:

- product-owned handoff unit
- producer가 만드는 것
- consumer가 요청하거나 의존하는 것

최소 개념 필드:

- `ArtifactID`
- `Digest`
- `ProducerRef`
- `ProducerNode`
- `State`
- `BackendRef`
- `Replicas`

중요 규칙:

- `ArtifactID`는 Dragonfly task ID가 아니다
- `ArtifactID`는 local file path가 아니다

## 5. ProducerRef

`ProducerRef`는 누가 artifact를 만들었는지를 product terms로 식별한다.

가능한 future shape:

- workload reference
- pipeline node reference
- job reference

중요한 것은 정확한 struct shape보다, 제품 모델이 producer meaning을 storage location과 분리해 보존해야 한다는 점이다.

## 6. ProducerNode

`ProducerNode`는 artifact의 primary locality origin을 담는다.

의미:

- same-node behavior의 첫 locality input
- 자동으로 유일한 remote source가 되는 것은 아님

이 필드는 제품 모델에 explicit하게 남아야 한다.

## 7. ConsumePolicy

`ConsumePolicy`는 consumer에게 어떤 locality behavior가 허용되는지 정의한다.

초기 개념 모드:

- `SameNodeOnly`
- `SameNodeThenRemote`
- `RemoteOK`

정책 의미:

- 이것은 product policy다
- backend transfer option과 같지 않다
- Kubernetes scheduling syntax와 같지 않다

## 8. ArtifactBinding

`ArtifactBinding`은 consumer intent를 artifact requirement와 연결한다.

의미:

- 어떤 artifact가 필요한가
- consume semantics가 얼마나 strict한가
- 실행에 꼭 필요한 artifact인가

최소 개념 필드:

- `ArtifactID`
- `ConsumePolicy`
- `Required`

이것은 placement resolution의 handoff-facing input이 되어야 한다.

## 9. PlacementIntent

`PlacementIntent`는 Kubernetes translation 이전의 product-owned locality direction이다.

가능한 모드:

- `None`
- `CoLocateWithProducer`
- `CoLocateWithReplica`
- `RemoteCapable`

중요한 구분:

- `PlacementIntent`는 product semantics다
- 아직 `nodeSelector`가 아니다
- 아직 concrete affinity stanza가 아니다

## 10. ResolvedPlacement

`ResolvedPlacement`는 placement resolution의 concrete runtime-facing output이다.

가능한 개념 필드:

- `NodeSelector`
- `RequiredNodeAffinity`
- `PreferredNodeAffinity`
- `Reason`

중요한 구분:

- 이것은 여전히 product output이다
- 하지만 runtime application에 가까운 형태다

이 객체는 product reasoning과 Kubernetes object translation 사이의 다리가 된다.

## 11. Replica

`Replica`는 artifact의 product-visible alternate availability point를 나타낸다.

최소 개념 필드:

- `Node`
- `Address`
- `State`
- `LocalityRole`

현재 PoC 입력:

- replica는 이미 source selection에 의미가 있다
- replica를 어떤 순서로 읽을지는 현재 implementation truth이지 final policy가 아니다

## 12. BackendRef

`BackendRef`는 backend-native meaning이 제품 전체로 새지 않도록 하는 backend-facing identity다.

가능한 개념 필드:

- `BackendType`
- `BackendObjectID`
- `BackendHints`

예:

- local development backend reference
- Dragonfly task identifier

중요 규칙:

- `BackendRef`는 execution을 지원한다
- `ArtifactID`는 product identity로 남는다

## 13. FailureRecord

`FailureRecord`는 의미 있는 failure attribution을 담는다.

PoC는 모든 failure가 같은 의미가 아니라는 점을 이미 증명했다.

최소 개념 축:

- `FailureClass`
- `DetectionPoint`
- `Message`
- `ObservedAt`

후보 class:

- control-plane lookup failure
- transport failure
- producer-side integrity rejection
- consumer-side integrity mismatch
- local verification failure

정확한 이름이 나중에 바뀌더라도, 모델은 이 구분을 유지해야 한다.

## 14. Artifact state

제품은 PoC보다 상위 수준의 artifact state model이 필요할 가능성이 높다.

후보 state:

- `Registered`
- `AvailableOnProducer`
- `Replicated`
- `Unavailable`
- `Failed`

이들은 개념적 후보이며, 아직 고정 API contract는 아니다.

## 15. 관계 요약

의도하는 관계는 다음과 같다.

```text
Artifact
  -> has ProducerRef
  -> has ProducerNode
  -> has BackendRef
  -> has Replicas

Consumer intent
  -> creates ArtifactBinding

ArtifactBinding + metadata + observations
  -> produce PlacementIntent
  -> resolve into ResolvedPlacement

Failures
  -> become FailureRecord
```

## 16. 모델링 제약

Domain model은 다음을 피해야 한다.

1. product identity와 backend identity 결합
2. runtime placement syntax를 유일한 policy representation으로 보는 것
3. 모든 remote availability를 익명 "source" 하나로 평평하게 만드는 것
4. 모든 failure meaning을 generic error 하나로 뭉개는 것

## 17. 현재 domain 결정

현재 결정은 다음과 같다.

- domain model은 product-first로 유지되어야 한다
- 첫 구현은 domain model이 이끌어야 한다
- backend와 runtime layer는 이 모델에 적응해야 하며, 이 모델을 정의하면 안 된다


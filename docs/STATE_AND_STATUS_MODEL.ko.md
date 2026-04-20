# STATE_AND_STATUS_MODEL

## 1. 목적

이 문서는 `artifact-handoff`의 초기 state 및 status 방향을 정의한다.

이 문서의 목적은 다음 질문에 답하는 것이다.

- 어떤 state가 제품 모델에 속하는가
- 어떤 state는 backend 또는 runtime execution에만 속하는가
- PoC가 보여준 유용한 구분을 잃지 않으면서 status를 어떻게 읽을 것인가
- PoC의 최소 상태 집합을 넘어서되 혼란스러운 state machine이 되지 않도록 어떻게 성장시킬 것인가

이 문서는 final API schema가 아니다.
이 문서는 구현 전에 state를 어떻게 조직할지에 대한 설계 기준 문서다.

관련 문서:

- [PRODUCT_IMPLEMENTATION_DESIGN.ko.md](PRODUCT_IMPLEMENTATION_DESIGN.ko.md)
- [DOMAIN_MODEL.ko.md](DOMAIN_MODEL.ko.md)
- [PLACEMENT_AND_FALLBACK_POLICY.ko.md](PLACEMENT_AND_FALLBACK_POLICY.ko.md)
- [DRAGONFLY_ADAPTER_SPEC.ko.md](DRAGONFLY_ADAPTER_SPEC.ko.md)

## 2. State 모델링 원칙

State model은 다음 원칙을 따라야 한다.

1. product state는 product-readable이어야 한다
2. backend state가 product state를 대체하면 안 된다
3. status는 단순 이력 설명이 아니라 의사결정에 도움을 줘야 한다
4. failure attribution은 의미 있게 남아야 한다
5. 모델은 PoC에서 점진적으로 자라야지, 처음부터 과도하게 풍부한 lifecycle로 뛰면 안 된다

## 3. PoC의 시작점

PoC는 이미 최소한으로 유용한 state split을 세웠다.

- catalog top-level state는 `produced` 중심
- local state로 `available-local`, `replicated`, `fetch-failed`
- local `lastError`는 유용한 forensic signal

이 분리는 의도적으로 좁았지만, 중요한 사실을 보여줬다.

- product-visible state와 local forensic state는 너무 일찍 뭉개면 안 된다

## 4. 세 가지 state layer

제품은 세 가지 layer를 구분해야 한다.

1. product artifact state
2. placement 및 handoff status
3. backend 또는 local execution status

이 layer들은 서로 참조할 수 있지만, 하나의 label로 평평하게 만들면 안 된다.

## 5. Product artifact state

이것은 artifact에 대한 top-level product view다.

후보 state:

- `Registered`
- `AvailableOnProducer`
- `Replicated`
- `Unavailable`
- `Failed`

### 5.1 `Registered`

의미:

- 제품이 artifact identity를 알고 있다
- 아직 소비 가능한 availability가 확인되지는 않았을 수 있다

### 5.2 `AvailableOnProducer`

의미:

- producer-local availability가 확인되었다
- digest와 producer locality가 기록되었다

### 5.3 `Replicated`

의미:

- producer 외에 적어도 하나의 추가 availability point가 product-visible하게 존재한다

이것이 producer copy가 사라졌다는 뜻은 아니다.
제품이 이제 alternate availability를 볼 수 있다는 뜻이다.

### 5.4 `Unavailable`

의미:

- artifact가 현재 기대되는 availability semantics를 만족시키지 못한다
- 제품은 여전히 artifact identity와 과거 reference를 알고 있을 수 있다

### 5.5 `Failed`

의미:

- 단일 local probe를 넘어서는 product-level failure condition에 도달했다

중요한 주의점:

- 모든 local failure가 즉시 top-level `Failed`가 되면 안 된다

## 6. Placement 및 handoff status

제품은 handoff progress를 artifact existence와 별도로 읽을 수 있어야 한다.

후보 status 축:

- locality target
- placement decision
- fallback stage
- availability outcome

이 모든 것을 하나의 enum으로 넣을 필요는 없다.
일부는 별도 status field로 나누는 것이 맞다.

## 7. 추천 status 차원

다음 status 차원을 권장한다.

### 7.1 Availability Status

답해야 하는 질문:

- artifact는 producer에서 이용 가능한가
- replica에서 이용 가능한가
- 현재 이용 불가능한가

### 7.2 Placement Status

답해야 하는 질문:

- placement가 resolve되었는가
- required-local, preferred-local, remote-capable 중 어떤 path로 resolve되었는가
- 어떤 reason이 사용되었는가

### 7.3 Backend Status

답해야 하는 질문:

- backend가 object를 알고 있는가
- ensure-on-node가 pending / succeeded / failed 중 어느 상태인가
- 어떤 backend-specific state가 존재하는가

### 7.4 Failure Status

답해야 하는 질문:

- 어떤 class의 failure가 관측되었는가
- 어디에서 감지되었는가
- 여전히 local-only인가, 아니면 product-relevant한가

## 8. Product state 대 backend state

둘은 반드시 분리되어야 한다.

예:

- Dragonfly task가 succeeded였다고 해서 제품이 곧바로 모든 policy 관점에서 fully available이라고 보면 안 된다
- backend miss가 곧 product artifact identity invalid를 뜻하지는 않는다
- backend state가 healthy해 보여도 local integrity mismatch는 드러나야 한다

Product state는 제품 의미를 요약해야 한다.
Backend state는 실행과 진단을 지원해야 한다.

## 9. Product state 대 local forensic status

PoC는 local forensic state가 가치 있다는 것을 보여줬다.

예:

- `fetch-failed`
- `lastError`
- local digest mismatch
- peer transport exception

제품 설계는 다음을 위한 여지를 유지해야 한다.

- node-local trace
- controller-readable summary
- escalation이 justified될 때만 product-level status 반영

## 10. Failure escalation 방향

유용한 설계 질문은 다음이다.

- 언제 local failure는 local에 머물러야 하는가
- 언제 product status로 반영되어야 하는가

권장 초기 방향:

- 단일 local probe failure는 기본적으로 local에 남긴다
- 그 failure가 availability나 policy decision에 의미 있게 영향을 줄 때 product status로 escalation한다

escalation 가능성이 높은 예:

- 허용된 모든 path에서 ensure availability를 반복적으로 실패
- producer와 replica가 일관되게 모두 unavailable
- consumption을 막는 integrity failure가 확인됨

## 11. Replica state 방향

Replica visibility는 status의 일부가 되어야 하지만, top-level artifact state에 과부하를 주면 안 된다.

각 replica에 대해 추천하는 차원:

- `Node`
- `Address`
- `ObservedState`
- `LastObservedAt`

후보 `ObservedState`:

- `Available`
- `Unreachable`
- `Rejected`
- `Unknown`

이들은 replica-local observation이지, top-level artifact state 자체는 아니다.

## 12. Placement status 방향

Placement 관련 status는 다음을 설명해야 한다.

- 제품이 무엇을 시도했는가
- 어떤 policy level을 사용했는가
- downgrade가 있었는가
- 어떤 observable이 그 downgrade를 유발했는가

후보 field:

- `PlacementMode`
- `PlacementReason`
- `Downgraded`
- `DowngradeReason`
- `RemoteCapableOpened`

이는 same-node와 remote decision의 explainability를 보존한다.

## 13. 추천 status shape

유용한 개념 shape는 다음과 같다.

```text
ArtifactStatus
  - Phase
  - ProducerAvailability
  - ReplicaSummary
  - PlacementStatus
  - BackendStatus
  - FailureSummary
```

이것이 최종 struct여야 한다는 뜻은 아니다.
관심사를 이렇게 분리하겠다는 뜻이다.

## 14. State transition 방향

초기 state-transition 방향은 단순하게 유지해야 한다.

예시 경로:

```text
Registered
  -> AvailableOnProducer
  -> Replicated
  -> Unavailable
  -> Failed
```

중요한 주의점:

- 이것을 strict linear machine으로 읽으면 안 된다
- 모든 artifact가 모든 state를 반드시 거칠 필요는 없다
- local failure가 생겨도 top-level artifact phase는 변하지 않을 수 있다

## 15. 해서는 안 되는 것

모델은 다음을 피해야 한다.

1. product phase, backend result, placement result, failure attribution을 모두 하나의 enum으로 표현하는 것
2. backend-native label이 product status를 덮어쓰는 것
3. 모든 local failure를 top-level artifact failure로 승격하는 것
4. downgrade와 fallback decision을 status에서 숨기는 것

## 16. 현재 설계 결정

현재 결정은 다음과 같다.

- top-level artifact phase는 작게 유지
- placement, backend, failure summary는 그 phase와 별도로 둠
- local forensic usefulness를 보존
- availability나 policy semantics에 의미 있는 영향을 줄 때만 product-level failure를 도입

## 17. 다음 후속 문서

다음으로 유용한 후속 문서는 다음과 같다.

1. `API_OBJECT_MODEL`
2. `RETRY_AND_RECOVERY_POLICY`
3. `OBSERVABILITY_MODEL`


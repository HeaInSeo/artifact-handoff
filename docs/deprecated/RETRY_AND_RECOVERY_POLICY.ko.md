# RETRY_AND_RECOVERY_POLICY

## 1. 목적

이 문서는 `artifact-handoff`의 초기 retry 및 recovery 방향을 정의한다.

이 문서는 다음 질문에 답한다.

- 어떤 failure를 retry해야 하는가
- 언제 retry가 local에 머물러야 하고 언제 product-level recovery decision이 되어야 하는가
- retry policy는 locality, fallback, artifact integrity와 어떻게 상호작용해야 하는가
- 어떤 failure는 추가 시도 대신 진행을 멈춰야 하는가

이 문서는 아직 정확한 timing value를 고정하지 않는다.
이 문서는 이후 구현이 따라야 할 정책 구조를 고정한다.

관련 문서:

- [PLACEMENT_AND_FALLBACK_POLICY.ko.md](PLACEMENT_AND_FALLBACK_POLICY.ko.md)
- [STATE_AND_STATUS_MODEL.ko.md](STATE_AND_STATUS_MODEL.ko.md)
- [API_OBJECT_MODEL.ko.md](API_OBJECT_MODEL.ko.md)
- [DRAGONFLY_ADAPTER_SPEC.ko.md](DRAGONFLY_ADAPTER_SPEC.ko.md)

## 2. 정책 원칙

Retry 및 recovery policy는 다음 원칙을 따라야 한다.

1. 모든 failure가 retry 대상은 아니다
2. integrity failure는 transport failure와 다르게 다뤄야 한다
3. recovery는 artifact-aware해야 한다
4. retry가 failure attribution을 조용히 지워 버리면 안 된다
5. required locality와 preferred locality는 같게 다뤄지면 안 된다

## 3. 왜 별도 정책이 필요한가

PoC는 이미 여러 failure class가 멀리서 보면 비슷해도 의미는 다르다는 것을 보여줬다.

- catalog lookup failure
- peer fetch transport failure
- producer-side integrity rejection
- consumer-side integrity mismatch
- local verification failure

유용한 제품은 이들 모두에 하나의 retry rule만 적용할 수 없다.

## 4. Retry 대 recovery

제품은 둘을 구분해야 한다.

### 4.1 Retry

Retry의 의미:

- 같은 종류의 action을 다시 시도한다
- 보통 같은 path 또는 같은 decision tier 안에서 시도한다

### 4.2 Recovery

Recovery의 의미:

- 다른 허용 path를 연다
- decision tier를 바꾼다
- 또는 상태를 상위 product outcome으로 escalation한다

예:

- 같은 candidate에 다시 peer fetch를 시도하는 것은 retry
- same-node-required에서 remote-capable path로 가는 것은 recovery
- producer candidate에서 replica candidate로 바꾸는 것은 policy tier에 따라 retry일 수도 recovery일 수도 있다

## 5. Failure class와 기본 방향

초기 정책은 failure를 다음처럼 읽어야 한다.

### 5.1 Control-plane lookup failure

예:

- metadata lookup unavailable
- product record temporarily unreadable

기본 방향:

- 짧은 retry budget은 합리적이다
- 반복 실패는 product-visible unavailability로 escalation될 수 있다

### 5.2 Transport failure

예:

- connection refused
- timeout
- peer temporarily unreachable

기본 방향:

- retry는 합리적이다
- alternate remote candidate selection도 합리적일 수 있다
- transport failure만으로 artifact identity를 즉시 invalid로 보면 안 된다

### 5.3 Producer-side integrity rejection

예:

- producer가 bytes를 주기 전에 digest mismatch로 reject

기본 방향:

- blind retry는 대체로 유용하지 않다
- 이 상태는 의미 있는 integrity signal로 읽어야 한다
- recovery에는 다른 source 또는 policy-level escalation이 필요할 수 있다

### 5.4 Consumer-side integrity mismatch

예:

- bytes를 읽었지만 consumer가 digest가 틀렸다고 판단

기본 방향:

- 같은 의심 source를 무한정 다시 시도하면 안 된다
- 허용된 alternate source 또는 escalation을 선호해야 한다

### 5.5 Local verification failure

예:

- local cached copy digest mismatch

기본 방향:

- local copy는 더 이상 신뢰하면 안 된다
- recovery는 허용된 source로부터 재획득을 우선해야 한다
- 같은 깨진 local copy를 반복 재사용하는 것은 허용되면 안 된다

## 6. Retry tier

초기 정책은 하나의 평평한 retry loop가 아니라 tier로 생각해야 한다.

### 6.1 Tier 1: same-attempt retry

예:

- transient metadata read를 다시 시도
- peer connection을 한 번 또는 소수 횟수 다시 시도

### 6.2 Tier 2: same-policy alternate candidate

예:

- 같은 remote-capable path 안에서 다음 허용 source candidate를 시도
- policy가 허용할 때 producer 다음 replica를 시도

### 6.3 Tier 3: policy-level recovery

예:

- required-locality downgrade
- remote-capable resolution opening
- placement decision tier 변경

### 6.4 Tier 4: product-level failure

예:

- 허용된 모든 path 소진
- integrity failure 때문에 모든 trusted acquisition path 차단
- artifact가 사실상 unavailable 상태가 됨

## 7. Locality-aware retry 방향

Retry policy는 locality policy를 존중해야 한다.

### 7.1 `SameNodeOnly`

방향:

- local retry는 허용될 수 있다
- remote recovery는 허용되지 않는다
- 소진 시 implicit remote continuation이 아니라 명확한 failure가 되어야 한다

### 7.2 `SameNodeThenRemote`

방향:

- 먼저 local retry
- trigger가 정당할 때 downgrade/recovery
- 이후 허용된 source에 대해 remote-capable retry

### 7.3 `RemoteOK`

방향:

- 제품은 alternate allowed remote path로 더 빨리 이동할 수 있다
- 그래도 integrity와 failure attribution 규칙은 유지해야 한다

## 8. 추천 recovery 순서

실용적인 초기 순서는 다음과 같다.

1. 짧은 local 또는 same-tier retry
2. 같은 허용 policy tier 안에서 alternate candidate
3. policy-level downgrade 또는 recovery opening
4. higher-level failure summary

이 순서는 다음 두 극단을 모두 막는다.

- 하나의 깨진 path에 끝없이 retry
- 유효한 alternate path를 보지 않고 즉시 escalation

## 9. Integrity-specific 규칙

제품은 integrity failure에 대해 더 엄격한 규칙을 가져야 한다.

추천 방향:

1. integrity failure를 ordinary transient network noise처럼 다루지 않는다
2. 이미 의심이 확인된 source를 반복 신뢰하지 않는다
3. failure evidence를 보존한다
4. 허용된다면 alternate trusted path를 선호한다
5. 단순 transport failure보다 더 빨리 escalation한다

## 10. Backoff 방향

정확한 값은 아직 열어 두되, 정책 방향은 다음과 같아야 한다.

- bounded retry count
- bounded time window
- 반복 transient failure에 대해서는 증가하는 delay
- 하나의 request path 안에 무한 retry loop를 숨기지 않음

이는 controller-driven reconciliation에서 특히 중요하다.

## 11. Status와의 상호작용

Retry와 recovery는 status에 읽히게 남아야 한다.

추천 status 질문:

- 아직 같은 종류의 action을 retry 중인가
- policy tier가 downgrade되었는가
- remote-capable recovery가 열렸는가
- 모든 path 소진 후 artifact가 unavailable이 되었는가

이 정보는 로그에만 있으면 안 되고 status model과 연결되어야 한다.

## 12. Backend와의 상호작용

Backend는 자체적인 internal retry를 수행할 수 있다.

그래도 product policy는 그 위에 남아 있어야 한다.

규칙:

1. backend retry가 product retry policy를 대체하지 않는다
2. backend retry detail은 요약될 수는 있지만 맹목적으로 미러링하면 안 된다
3. backend exhaustion은 product-readable failure meaning으로 번역되어야 한다

## 13. Blind retry를 하면 안 되는 것

초기 정책은 다음에 대해 blind retry를 피해야 한다.

1. 같은 source에서 반복되는 digest mismatch
2. 반복되는 corrupted local cache reuse
3. policy가 금지한 remote continuation
4. 구조적으로 invalid한 object reference

이 경우에는 alternate recovery 또는 명확한 failure가 필요하다.

## 14. 현재 정책 결정

현재 결정은 다음과 같다.

- transport 및 lookup failure에는 bounded retry를 줄 수 있다
- integrity failure는 더 빨리 escalation하며 alternate trusted path를 선호해야 한다
- locality policy는 어떤 recovery path가 합법적인지를 제한한다
- recovery는 숨겨진 fallback이 아니라 explicit tier로 이동해야 한다

## 15. 다음 후속 문서

다음으로 유용한 후속 문서는 다음과 같다.

1. `OBSERVABILITY_MODEL`
2. `CRD_INTRODUCTION_STRATEGY`
3. `CONTROLLER_RECONCILIATION_MODEL`


# PLACEMENT_AND_FALLBACK_POLICY

## 1. 목적

이 문서는 `artifact-handoff`의 placement 및 fallback 제품 정책 방향을 정의한다.

이 문서는 다음과 같은 PoC 결과를 바탕으로 한다.

- same-node behavior는 중요하다
- current required locality와 future preferred locality는 구분되어야 한다
- fallback trigger는 observable해야 한다
- remote-capable resolution은 artifact-aware policy step이다

## 2. 정책 범위

이 문서는 다음을 다룬다.

- locality policy
- downgrade semantics
- fallback trigger direction
- remote-capable resolution input

이 문서는 다음을 정의하지 않는다.

- backend transport protocol detail
- 모든 retry timing parameter
- 모든 scheduler integration detail

## 3. 주요 정책 목표

주요 목표는 다음과 같다.

- semantic하게 유용할 때 locality를 우선한다
- locality를 만족할 수 없을 때도 artifact-aware meaning을 잃지 않는다

이것은 다음 두 극단과 다르다.

- 무조건 same-node를 강제
- artifact-aware reasoning 없이 곧바로 아무 remote placement나 허용

## 4. 현재 PoC truth

PoC는 다음을 고정했다.

1. same-node는 explicit하게 강제할 수 있다
2. 그 current explicit path는 `required`이지 `preferred`가 아니다
3. producer-first ordering은 current implementation truth다
4. producer failure 이후 replica fallback은 실제로 동작한다
5. 의미 있는 fallback trigger candidate는 API-level observable에서 읽는 것이 맞다

이 제품 정책은 이러한 truth를 입력으로 사용하되, 이를 final policy로 고정하지는 않는다.

## 5. Locality policy level

제품은 명시적인 policy level로 생각해야 한다.

### 5.1 SameNodeOnly

의미:

- consumer는 producer-locality semantics를 반드시 따라야 한다
- remote-capable path는 허용되지 않는다

### 5.2 SameNodeThenRemote

의미:

- locality는 preferred first path다
- locality를 만족시킬 수 없거나 유지할 수 없을 때 remote-capable continuation이 허용된다

### 5.3 RemoteOK

의미:

- consumer는 same-node semantics에 묶이지 않는다
- placement는 더 이른 시점에 remote-capable path를 열 수 있다

## 6. Required 대 preferred

제품은 둘을 반드시 구분해야 한다.

### 6.1 Required locality

의미:

- placement failure는 실제 blocking condition이다
- 시스템은 조용히 다른 노드로 spill되면 안 된다

### 6.2 Preferred locality

의미:

- 시스템은 locality를 먼저 시도해야 한다
- 하지만 policy가 허용하면 다른 경로로 의도적으로 계속 진행할 수 있다

PoC는 이 둘이 뭉개지면 안 된다는 점을 보여줬다.

## 7. Fallback trigger 방향

첫 fallback-trigger 방향은 observable-first를 유지해야 한다.

현재 가장 강한 후보는 다음이다.

- `PodScheduled=False`
- reason: `Unschedulable`

이유:

- locality constraint는 결국 API object에 기록된다
- scheduling impossibility는 가능하면 나중의 generic terminal failure보다 먼저 읽어야 한다

이것이 모든 future fallback이 scheduling-triggered여야 한다는 뜻은 아니다.
첫 downgrade path는 explicit observable evidence에 기반해야 한다는 뜻이다.

## 8. Downgrade model

제품은 두 단계로 생각해야 한다.

1. `required -> preferred`
2. `preferred -> remote-capable`

이 구분이 중요한 이유:

- 모든 required-locality miss가 즉시 unconstrained remote path가 되어서는 안 된다
- policy transition은 설명 가능해야 한다

## 9. Remote-capable resolution

Remote-capable resolution은 단순한 relaxed scheduling이 아니다.

이 단계는 여전히 다음과 같은 artifact-aware input을 읽어야 한다.

- consume policy
- producer locality
- visible replica
- ordering semantics
- observable failure signal

즉 remote-capable resolution은 다음을 의미해야 한다.

- locality를 더 이상 이전과 같은 방식으로 강제하지는 않는다
- 하지만 artifact-aware source 및 placement reasoning은 여전히 살아 있다

## 10. Ordering policy 방향

제품은 다음을 명시적으로 구분해야 한다.

- current implementation truth
- intended future policy

PoC가 보여준 current truth:

- producer first
- replica fallback later

Future policy에서 아직 열려 있는 것:

- producer-first를 default로 유지할지
- 일부 조건에서 replica가 producer보다 앞설 수 있는지
- freshness, health, retry가 ordering에 어떤 영향을 줄지

## 11. Policy를 위한 failure input

Placement 및 fallback policy는 결국 최소 다음 범주를 읽어야 한다.

1. scheduling failure signal
2. metadata lookup failure
3. remote transport failure
4. producer-side integrity rejection
5. consumer-side integrity mismatch
6. local verification failure

이들 모두가 같은 policy reaction을 가져서는 안 된다.

## 12. 정책 제약

정책은 다음을 피해야 한다.

1. required locality를 preferred locality처럼 조용히 읽는 것
2. 모든 remote continuation을 동일하게 취급하는 것
3. scheduling failure와 artifact failure를 하나로 뭉치는 것
4. backend-native behavior가 제품 fallback policy가 되게 두는 것

## 13. 추천 초기 정책 결정

현재 추천하는 제품 방향은 다음과 같다.

- explicit consume-policy level을 지원
- required/preferred distinction을 보존
- observable scheduling signal을 첫 downgrade input으로 사용
- remote-capable resolution을 artifact-aware policy step으로 취급
- ordering policy는 현재 producer-first truth를 넘어서 계속 열어 둠

## 14. 남은 정책 질문

아직 열려 있는 주요 정책 질문은 다음과 같다.

1. exact downgrade timing과 retry behavior
2. remote-capable resolution 이전에 separate preferred-affinity phase가 필요한지
3. recorded order를 넘어서 replica ordering을 어떻게 읽을지
4. cleanup 및 retention policy가 fallback behavior와 어떻게 상호작용할지


# DRAGONFLY_ADAPTER_SPEC

## 1. 목적

이 문서는 `artifact-handoff`가 Dragonfly를 어떻게 사용해야 하는지 정의한다.

이 문서는 Dragonfly를 제품 코어로 정의하지 않는다.
이 문서는 Dragonfly를 backend adapter target으로 정의한다.

## 2. 핵심 adapter 결정

Dragonfly는 다음으로 취급해야 한다.

- replaceable backend
- adapter-bound transport/cache implementation
- product semantics를 소유하지 않고 제품 요구를 충족하는 시스템

Dragonfly가 되어서는 안 되는 것:

- 제품 identity model
- 제품 placement-policy engine
- 제품 failure-model owner

## 3. 왜 이 경계가 필요한가

PoC와 후속 설계는 이미 몇 가지 중요한 truth를 고정했다.

1. `artifactId`는 product-owned
2. producer와 replica interpretation은 product-owned
3. locality 및 fallback policy는 product-owned
4. Dragonfly surface는 변할 수 있으므로 adapter 뒤에 격리해야 한다

즉 integration question은 다음이 아니다.

- 제품을 어떻게 Dragonfly로 바꿀 것인가

올바른 질문은 다음이다.

- 제품이 소유하는 backend 요구를 Dragonfly로 어떻게 충족할 것인가

## 4. Adapter 책임

Dragonfly adapter는 다음을 담당해야 한다.

- payload bytes를 Dragonfly에 import 또는 register
- target node에서 bytes availability 보장
- Dragonfly-backed availability 또는 task state 조회
- 선택적 warm 또는 eviction 지원

Adapter가 담당하면 안 되는 것:

- same-node 또는 remote 사용 여부 결정
- product artifact identity 할당
- fallback ordering 결정
- business-level failure meaning 해석

## 5. Adapter에 들어가는 product-owned input

제품은 다음과 같은 input을 adapter에 넘겨야 한다.

- `ArtifactID`
- local source path 또는 content reference
- digest
- target node
- policy hint

Adapter는 이 값들로부터 Dragonfly-native input을 만들 수 있지만, 그 의미를 다시 정의하면 안 된다.

## 6. Adapter-owned output

Adapter는 다음을 반환할 수 있다.

- backend task identifier
- backend availability result
- backend-local state
- backend-local error detail

이 output은 product status가 되기 전에 번역되어야 한다.

## 7. 추천 interface shape

Dragonfly adapter는 제품 backend interface를 구현해야 한다.

- `Put`
- `EnsureOnNode`
- `Stat`
- `Warm`
- `Evict`

### 7.1 `Put`

의미:

- producer가 만든 bytes를 Dragonfly-backed storage/cache distribution에 등록

Product-facing input:

- `ArtifactID`
- `Digest`
- local source path
- optional policy hint

Product-facing output:

- `BackendRef`
- backend status

### 7.2 `EnsureOnNode`

의미:

- 특정 target node에서 artifact를 이용 가능하게 만든다

이것이 가장 중요한 runtime-facing adapter method다.

### 7.3 `Stat`

의미:

- backend가 현재 object를 알고 있는지, 어떤 state로 보는지 묻는다

### 7.4 `Warm`

의미:

- fan-out 또는 likely consumption 전에 availability를 미리 준비한다

### 7.5 `Evict`

의미:

- product policy가 허용할 때 backend가 쥔 state를 제거하거나 줄인다

## 8. Identity 규칙

다음 규칙은 반드시 지켜야 한다.

1. `ArtifactID`는 product-owned로 남는다
2. Dragonfly task ID는 adapter-owned로 남는다
3. 제품은 backend reference를 저장해야지, 자신의 identity를 Dragonfly identity로 대체하면 안 된다

이 원칙은 domain 레벨 lock-in을 막는다.

## 9. Failure translation 규칙

Dragonfly-native failure는 product-readable category로 번역되어야 한다.

예시 translation dimension:

- backend unreachable
- backend object missing
- backend integrity-related rejection
- backend timeout

Adapter는 backend detail을 보존할 수 있지만, top-level meaning은 여전히 제품이 소유해야 한다.

## 10. Versioning 및 upgrade 전략

Adapter는 upstream drift를 전제로 작성되어야 한다.

추천 규칙:

1. 검증된 Dragonfly version range를 pin
2. Dragonfly-specific CLI/API handling을 adapter 안에만 둔다
3. 제품 전체에 Dragonfly-native assumption을 퍼뜨리지 않는다
4. adapter-level compatibility check를 유지한다

이것이 adapter boundary가 필요한 주요 이유다.

## 11. Data 및 control 경계

Dragonfly adapter는 backend/data-plane bridge다.

제품 control plane이 계속 소유해야 하는 것:

- artifact record
- placement decision
- consume policy
- fallback entry 및 downgrade logic
- product status

## 12. 초기 integration 전략

추천 integration 순서는 다음과 같다.

1. 먼저 backend interface를 정의
2. 먼저 simple non-Dragonfly backend를 구현
3. product boundary가 안정된 뒤 Dragonfly를 adapter로 추가

이 순서는 Dragonfly-specific behavior가 product model을 너무 빨리 왜곡하는 위험을 줄인다.

## 13. 명시적 비목표

이 adapter spec은 다음을 전제하지 않는다.

- Dragonfly fork
- scheduler-internal customization
- manager-internal product logic
- Dragonfly-native identifier를 public product API로 쓰는 것

## 14. 현재 adapter 결정

현재 결정은 다음과 같다.

- Dragonfly는 backend candidate다
- adapter boundary는 얇고 explicit해야 한다
- product semantics는 Dragonfly 밖에 남는다
- upstream Dragonfly change는 주로 adapter에만 영향을 줘야 하며, 제품 전체 아키텍처를 흔들면 안 된다


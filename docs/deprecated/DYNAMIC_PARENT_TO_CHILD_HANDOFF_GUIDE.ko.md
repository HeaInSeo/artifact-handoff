# DYNAMIC_PARENT_TO_CHILD_HANDOFF_GUIDE

## 1. 목적

이 문서는 `artifact-handoff`가 PV/PVC 없이도 **부모에서 자식으로의 동적 artifact handoff**를 어떻게 지원해야 하는지를 실무적으로 설명한다.

이 문서는 특히 파이프라인 설계자가 다음 질문에 답할 수 있도록 돕기 위해 작성한다.

- 자식 노드가 부모가 만든 파일을 어떻게 동적으로 읽을 수 있는가
- same-node와 remote fetch는 언제 결정해야 하는가
- 부모가 끝난 뒤 어떤 정보를 반드시 기록해야 하는가
- 파이프라인은 어느 시점에 `artifact-handoff`를 호출해야 하는가
- 이 설계가 Kubernetes 안에서 실제로는 어떻게 동작하는가

이 문서는 low-level code spec이 아니다.
이 문서는 실제 파이프라인 연동을 위한 설계 가이드다.

관련 문서:

- [PRODUCT_IMPLEMENTATION_DESIGN.ko.md](PRODUCT_IMPLEMENTATION_DESIGN.ko.md)
- [ARCHITECTURE.ko.md](ARCHITECTURE.ko.md)
- [DOMAIN_MODEL.ko.md](DOMAIN_MODEL.ko.md)
- [PLACEMENT_AND_FALLBACK_POLICY.ko.md](PLACEMENT_AND_FALLBACK_POLICY.ko.md)
- [RETRY_AND_RECOVERY_POLICY.ko.md](RETRY_AND_RECOVERY_POLICY.ko.md)

## 2. 핵심 아이디어

핵심 아이디어는 단순하다.

- 부모가 실제 Kubernetes 노드에서 데이터를 만든다
- 시스템은 그 데이터가 지금 어디에 있는지 기록한다
- 자식은 그 위치를 알기 전에는 완전히 확정되지 않는다
- 자식을 만들기 직전에 시스템은 다음 중 하나를 결정한다
  - same-node reuse
  - 또는 remote-capable acquisition

즉 시스템이 하는 일은 다음이 아니다.

- "파일을 어딘가로 먼저 옮겨 두고 나중에 항상 거기서 읽자"

시스템이 하는 일은 다음이다.

- "먼저 파일이 어디 있는지 알고, 그다음 자식이 어떻게 접근할지 결정하자"

이것이 handoff를 동적으로 만드는 핵심이다.

## 3. 여기서 "동적"이란 무엇인가

이 문서에서 "동적"이란 다음 뜻이다.

- 자식이 **처음부터 완전히 고정되어 있지 않다**
- 자식의 placement와 artifact acquisition path가 **부모 결과를 본 뒤에 결정된다**

즉 실행마다 결정이 바뀔 수 있다.

예:

- run A: 부모가 `worker-0`에서 끝났으니 자식을 `worker-0`에 붙임
- run B: 부모가 `worker-1`에서 끝났으니 자식을 `worker-1`에 붙임
- run C: same-node placement가 불가능하거나 허용되지 않아서 자식을 다른 노드에 두고 remote fetch를 엶

이것은 다음과 같은 정적 설계와 다르다.

- 자식이 항상 미리 정해진 한 노드에서만 도는 설계
- 자식이 항상 미리 정해진 중앙 스토리지 경로만 읽는 설계

## 4. Kubernetes가 주는 것과 주지 않는 것

Kubernetes는 artifact-handoff semantics를 직접 주지 않고, 프리미티브만 준다.

Kubernetes가 주는 것:

- Pod와 Job
- `nodeSelector`, `affinity` 같은 node placement control
- Pod와 노드 간 네트워킹
- node-local path를 Pod에 붙일 수 있는 수단
- env, annotation, 기타 runtime metadata를 주입하는 수단

Kubernetes가 **직접 주지 않는 것**:

- "이 자식은 방금 저 부모가 만든 파일을 소비해야 한다"
- "먼저 same-node reuse를 시도하라"
- "안 되면 producer나 replica에서 fetch하라"

이 의미는 파이프라인 시스템과 `artifact-handoff`가 직접 구현해야 한다.

## 5. 반드시 받아들여야 하는 물리적 현실

부모 Pod가 파일을 만들면, 그 파일은 물리적으로 실제 어딘가에 생긴다.

즉:

- 어떤 노드의 로컬 디스크 위에 있거나
- 어떤 노드의 로컬 경로 아래에 있거나
- 어떤 backend를 통해 특정 availability location을 가진다

파이프라인이 DAG node로 생각하더라도, 실제 bytes는 결국 다음 중 하나로 존재한다.

- 어떤 머신 위에
- 어떤 경로 또는 backend object로
- 어떤 네트워크 reachability 조건 아래에

자식이 그 데이터를 소비할 수 있으려면 시스템은 최소한 다음 중 하나를 알아야 한다.

1. 자식이 같은 노드에 있고 로컬 재사용이 가능하다
2. 자식이 producer 또는 replica에서 fetch할 수 있다
3. backend가 자식 노드에서 데이터를 materialize할 수 있다

그래서 첫 번째 설계 규칙은 이것이다.

- **부모가 데이터를 만든 직후 artifact location과 integrity를 기록해야 한다**

## 6. 부모가 끝난 뒤 반드시 기록해야 할 최소 정보

부모가 끝난 뒤 시스템은 최소한 다음을 기록해야 한다.

- `artifactId`
- `digest`
- `producerPod`
- `producerJob` 또는 producer task reference
- `producerNode`
- `producerAddress` 또는 runtime acquisition endpoint
- backend가 있다면 `backendRef`
- 필요하면 `size`
- initial availability state

이 정보가 최소한으로 필요한 이유:

- `artifactId`는 자식이 무엇을 필요로 하는지 알려준다
- `digest`는 무엇이 올바른 데이터인지 알려준다
- `producerNode`는 same-node reuse 가능 여부를 알려준다
- `producerAddress`는 producer에서 fetch할 때 어디로 가야 하는지 알려준다
- `backendRef`는 backend-mediated path가 있는지 알려준다

이 metadata가 없으면 동적 handoff는 거의 추측에 가까워진다.

## 7. 파이프라인 안의 최소 연동 지점

실제 파이프라인이 `artifact-handoff`를 쓰려면 세 군데에 연동 지점이 필요하다.

### 7.1 부모 완료 직후

파이프라인은 생성된 artifact를 등록해야 한다.

여기서 기록하는 것:

- identity
- digest
- producer locality
- acquisition endpoint

### 7.2 자식 생성 직전

파이프라인은 다음을 물어야 한다.

- artifact가 어디에 있는가
- 어떤 consume policy가 적용되는가
- 자식을 same-node, preferred-local, remote-capable 중 어느 방식으로 둘 것인가

이 지점이 가장 중요한 동적 결정 시점이다.

### 7.3 자식 시작 시점 또는 직전

자식은 실제로 artifact를 확보할 수 있는 방법을 가져야 한다.

그 방법은 다음 중 하나일 수 있다.

- local reuse
- producer fetch
- replica fetch
- backend ensure-on-node

자식이 runtime에서 artifact를 확보할 방법이 없으면 placement decision만으로는 충분하지 않다.

## 8. 핵심 동적 흐름

가장 실용적인 동적 흐름은 다음과 같다.

```text
1. Parent runs
2. Parent produces artifact
3. Artifact metadata is registered
4. Child is not submitted yet
5. Pipeline asks artifact-handoff to resolve handoff strategy
6. artifact-handoff returns:
   - placement decision
   - acquisition decision
7. Pipeline creates child Job/Pod using that result
8. Child starts
9. Child acquires artifact locally or remotely
10. Child runs main computation
```

이 시점이 가장 좋은 이유는, 가장 신선한 부모 결과를 사용해서 자식을 만들 수 있기 때문이다.

## 9. 세 가지 주요 handoff 모드

실제 파이프라인 설계에서는 보통 세 가지 모드로 생각하는 것이 좋다.

### 9.1 Same-node local reuse

의미:

- 부모 노드를 알고 있다
- 자식을 같은 노드에 붙인다
- 자식은 artifact를 로컬에서 읽는다

이것이 가장 싸고 가장 단순한 성공 경로다.

### 9.2 Producer로부터의 remote-capable fetch

의미:

- 자식은 producer node 위에서 돌지 않는다
- 자식은 producer endpoint에서 artifact를 가져온다

이것도 artifact-aware한 경로다. source를 recorded producer로 쓰기 때문이다.

### 9.3 Replica로부터의 remote-capable fetch

의미:

- producer만이 유일한 source가 아니다
- 이미 알려진 replica가 자식에게 데이터를 줄 수 있다

이것은 특히 다음 상황에서 중요하다.

- producer가 unavailable하다
- replica만이 현재 healthy하거나 reachable한 source다

## 10. 결정은 어디서 해야 하는가

가장 중요한 구현 규칙은 이것이다.

- **너무 일찍 결정하면 안 된다**

결정은 보통 다음 시점에 해야 한다.

- 부모 artifact record가 생긴 뒤
- 자식 Job 또는 Pod를 만들기 직전

즉 이 결정은 보통 다음 계층에 속한다.

- controller
- orchestrator
- DAG submit hook
- 또는 submit 직전의 job-mutation step

대체로 다음에는 속하지 않는다.

- static YAML만으로는 어렵다
- runtime feedback이 없는 compile-time pipeline graph만으로는 어렵다

## 11. 결정을 위해 필요한 입력

자식을 만들기 전에 decision layer는 다음을 읽어야 한다.

- artifact identity
- producer node
- producer endpoint
- known replica
- consume policy
- same-node가 required인지 preferred인지
- 필요 시 현재 cluster/runtime condition

이 정보가 있으면 다음 질문에 답할 수 있다.

- 자식을 producer node에 강제로 붙일 것인가
- 자식을 producer node에 우선적으로 붙일 것인가
- remote-capable acquisition을 열 것인가
- source를 어디서부터 시도할 것인가

## 12. 결정은 무엇을 반환해야 하는가

Decision layer는 두 가지를 반환해야 한다.

### 12.1 Placement result

예:

- 자식을 producer node에 둔다
- producer node를 선호한다
- remote-capable path를 허용한다

이 결과는 나중에 보통 다음으로 번역된다.

- `nodeSelector`
- `nodeAffinity`
- annotation
- 또는 다른 runtime placement representation

### 12.2 Acquisition result

예:

- local path를 읽는다
- producer에서 fetch한다
- replica에서 fetch한다
- backend를 통해 ensure한다

이 결과는 자식이 실제로 artifact를 어떻게 접근할지를 말해 준다.

이 둘은 분리되어야 한다.
Placement와 acquisition은 같은 것이 아니다.

## 13. 실제로 자식에 무엇이 주입되는가

실제로는 파이프라인이 보통 다음 중 몇 가지를 자식에 주입하게 된다.

- `nodeSelector`
- `affinity`
- env
- annotation
- init-container 설정
- startup argument

자식이 읽으면 좋은 값의 예:

- `ARTIFACT_ID`
- `ARTIFACT_DIGEST`
- `ARTIFACT_EXPECTED_MODE=local|remote`
- `ARTIFACT_SOURCE_HINT=producer|replica|backend`
- `ARTIFACT_PRODUCER_NODE`
- `ARTIFACT_PRODUCER_ADDRESS`

자식은 제품 전체 상태를 다 알 필요는 없다.
자기 runtime에서 artifact를 올바르게 확보하는 데 필요한 정보만 있으면 된다.

## 14. 간단한 same-node 예시

아주 단순한 same-node 흐름은 다음과 같다.

1. parent Pod가 `worker-0`에서 끝난다
2. parent가 다음을 등록한다
   - `artifactId=a1`
   - `producerNode=worker-0`
   - `producerAddress=http://10.x.x.x:8080`
3. pipeline이 artifact-handoff에 묻는다
   - "child는 `a1`이 필요하고, 정책은 `SameNodeThenRemote`다"
4. artifact-handoff가 답한다
   - child를 `worker-0`에 둬라
   - acquisition mode는 local이다
5. pipeline이 child Job을 만든다
   - `nodeSelector=kubernetes.io/hostname=worker-0`
6. child가 `worker-0`에서 시작한다
7. child가 local artifact를 읽는다
8. child가 계속 실행된다

이것이 동적인 이유는, 선택된 노드가 부모의 실제 runtime result에서 왔기 때문이다.

## 15. 간단한 remote 예시

단순한 remote 흐름은 다음과 같다.

1. parent Pod가 `worker-0`에서 끝난다
2. artifact metadata가 등록된다
3. child는 `worker-0` 위에서 돌 수 없거나 돌지 않아야 한다
4. artifact-handoff가 반환한다
   - remote-capable placement
   - source=producer
5. child가 `worker-1`에 생성된다
6. child가 시작한다
7. child 또는 init-step이 producer endpoint에서 artifact를 가져온다
8. digest를 검증한다
9. child가 계속 실행된다

이것도 동적이다. source와 placement가 live runtime state에서 해결되었기 때문이다.

## 16. Replica 예시

이후 replica-aware 흐름은 다음과 같다.

1. parent가 `worker-0`에서 artifact를 만든다
2. 다른 성공한 consumer가 `worker-1`에 known replica를 남긴다
3. 새로운 child가 `worker-2`에서 시작하려고 한다
4. producer는 unavailable하거나, 정책상 alternate source가 허용된다
5. artifact-handoff가 반환한다
   - remote-capable acquisition
   - source=replica on `worker-1`
6. child는 replica에서 fetch한다

여기서 replica가 중요한 이유:

- producer가 영원히 유일한 source라고 가정하지 않고도 recovery가 가능해진다

## 17. 자식 runtime이 보통 필요로 하는 것

자식 runtime에는 두 가지 흔한 패턴이 있다.

### 17.1 Init-step acquisition

메인 컨테이너가 시작되기 전에 artifact를 먼저 확보한다.

장점:

- 메인 컨테이너는 이미 준비된 input을 본다
- 메인 연산 전에 failure가 명확하게 드러난다

### 17.2 In-process acquisition

애플리케이션 또는 runtime library가 시작 시점에 직접 artifact를 가져온다.

장점:

- moving piece가 적다
- 애플리케이션이 이미 artifact client를 갖고 있으면 단순하다

둘 다 가능하다.
중요한 것은 acquisition behavior가 resolved handoff strategy를 따른다는 점이다.

## 18. DAG 시스템에서 어떻게 보아야 하는가

DAG 시스템에서는 개념적으로 부모와 자식이 graph node다.

하지만 runtime에서 실제 handoff path는 다음과 같다.

- producer DAG node
  -> Kubernetes Job/Pod
  -> artifact registration
  -> child DAG node submission
  -> resolved artifact acquisition

즉 DAG 시스템은 다음처럼 생각하면 안 된다.

- "부모 output은 그래프 어딘가에 그냥 존재한다"

다음처럼 생각해야 한다.

- "부모 output은 구체적인 runtime location과 구체적인 acquisition path를 가진다"

이 인식 전환이 가장 중요하다.

## 19. 실전에서 자주 실패하는 지점

가장 흔한 설계 실수는 다음과 같다.

1. 부모 결과가 생기기 전에 자식 노드를 먼저 결정한다
2. artifact identity만 기록하고 producer locality는 기록하지 않는다
3. placement와 acquisition을 같은 것으로 취급한다
4. producer가 항상 reachable할 것이라고 가정한다
5. 모든 failure를 같은 방식으로 retry할 수 있다고 본다
6. 모든 동적 결정을 로그 속에만 숨겨 둔다

이 실수들을 피하면 설계가 훨씬 선명해진다.

## 20. 파이프라인을 만들 때 먼저 설계해야 할 것

다른 프로젝트에 이것을 붙이려면 먼저 다음을 설계하는 것이 좋다.

1. 부모 완료 직후 artifact registration point
2. 자식 submission hook 또는 controller decision point
3. artifact metadata schema
4. child acquisition mechanism
5. digest verification behavior
6. same-node와 remote-capable policy

이 여섯 가지가 선명하면 나머지는 훨씬 쉬워진다.

## 21. 현실적인 최소 설계

가장 작은 현실적 설계로 시작하려면 다음이면 충분하다.

1. parent가 `artifactId`, `digest`, `producerNode`, `producerAddress`를 등록한다
2. registration이 생길 때까지 child creation을 늦춘다
3. 먼저 same-node를 시도한다
4. 첫 번째 remote path는 producer fetch다
5. digest verification은 필수다
6. local과 remote failure reason을 보존한다

이 정도면 실제로 동작하는 첫 dynamic handoff path를 만들 수 있다.

## 22. "가능하다"의 실제 의미

그래서 누군가가 묻는다면:

- "PV/PVC 없이 부모가 만든 데이터를 자식에게 동적으로 넘기는 것이 가능한가?"

정확한 답은 다음과 같다.

- 가능하다. 단, 부모 완료 후 artifact location을 기록해야 한다
- 가능하다. 단, handoff strategy가 resolve될 때까지 child를 만들지 않아야 한다
- 가능하다. 단, child는 실제 acquisition path를 가져야 한다
- 가능하다. 단, placement와 acquisition을 분리하되 서로 연결된 decision으로 다뤄야 한다

이것이 이 프로젝트에서 말하는 "동적 handoff"의 실제 의미다.

## 23. 최종 요약

가장 짧고 정확한 요약은 다음과 같다.

> 동적 parent-to-child handoff는 부모 artifact의 실제 runtime locality를 기록하고, 그 locality를 알기 전에는 자식을 만들지 않으며, live metadata를 바탕으로 자식 placement와 artifact acquisition을 함께 결정할 때 가능하다. 정적인 shared-storage path를 가정하는 것이 아니다.

파이프라인 설계에서 가장 중요한 한 줄은 이것이다.

- 먼저 데이터가 어디 있는지 기록하라
- 그다음 자식이 어떻게 그 데이터를 얻을지 결정하라


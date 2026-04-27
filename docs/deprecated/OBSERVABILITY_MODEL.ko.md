# OBSERVABILITY_MODEL

## 1. 목적

이 문서는 `artifact-handoff`의 초기 observability 방향을 정의한다.

이 문서는 다음 질문에 답한다.

- 제품은 무엇을 가시화해야 하는가
- 어떤 관측 정보는 status, event, log, detailed trace 중 어디에 속해야 하는가
- placement, fallback, retry, failure attribution이 어떻게 설명 가능하게 남아야 하는가
- observability가 backend-native 정보에만 머무르지 않고 product-readable하게 유지되려면 어떻게 해야 하는가

이 문서는 전체 telemetry stack 구현을 정의하지 않는다.
이 문서는 이후 구현이 실현해야 할 observability model을 정의한다.

관련 문서:

- [ARCHITECTURE.ko.md](ARCHITECTURE.ko.md)
- [STATE_AND_STATUS_MODEL.ko.md](STATE_AND_STATUS_MODEL.ko.md)
- [PLACEMENT_AND_FALLBACK_POLICY.ko.md](PLACEMENT_AND_FALLBACK_POLICY.ko.md)
- [RETRY_AND_RECOVERY_POLICY.ko.md](RETRY_AND_RECOVERY_POLICY.ko.md)

## 2. Observability 원칙

Observability model은 다음 원칙을 따라야 한다.

1. backend internals를 먼저 읽지 않아도 product meaning이 보여야 한다
2. 중요한 policy decision은 설명 가능해야 한다
3. PoC의 local forensic value를 잃으면 안 된다
4. summary와 detail은 분리되어야 한다
5. status, event, log, trace는 각자 분명한 역할을 가져야 한다

## 3. 제품이 반드시 보여줘야 하는 것

최소한 다음은 제품이 보여줘야 한다.

1. artifact identity와 phase
2. producer locality
3. replica visibility
4. placement intent와 resolved placement
5. downgrade와 fallback decision
6. retry와 recovery progression
7. failure class와 detection point
8. backend interaction summary

이것이 보이지 않으면 제품은 신뢰하기도, 디버깅하기도 어렵다.

## 4. 네 가지 observability layer

제품은 네 가지 layer로 생각해야 한다.

1. status
2. event
3. log
4. detailed local 또는 backend trace

이 layer들은 서로를 보완해야지, 같은 내용을 모두 복제하면 안 된다.

## 5. Status layer

Status는 첫 번째 summary layer다.

Status는 다음 질문에 답해야 한다.

- 현재 product meaning이 무엇인가
- 현재 phase가 무엇인가
- 가장 중요한 placement와 availability 사실은 무엇인가
- recovery가 열렸는가 또는 소진되었는가

Status는 append-only debug history가 되면 안 된다.

## 6. Event layer

Event는 의미 있는 transition과 policy decision을 담아야 한다.

예:

- artifact registered
- producer locality recorded
- placement resolved
- downgrade triggered
- remote-capable path opened
- retry budget exhausted
- integrity failure escalated

Event는 사람이 읽기 쉬워야 하고 짧아야 한다.
Raw payload dump가 아니라 change를 설명해야 한다.

## 7. Log layer

Log는 status나 event에 넣기에는 너무 자세한 execution detail을 담아야 한다.

추천 log 내용:

- controller decision detail
- resolution 과정에서 사용한 candidate ordering
- backend request/response summary
- attempt count를 포함한 retry 시도
- detailed failure translation

Log도 product-readable vocabulary를 사용해야 한다.
backend-only terminology로 무너지면 안 된다.

## 8. Local forensic layer

PoC는 node-local forensic trace가 유용하다는 것을 보여줬다.

제품은 다음을 위한 여지를 보존해야 한다.

- local cache verification evidence
- node-local acquisition failure
- source-specific failure detail
- 필요 시 low-level backend/runtime artifact

이 trace를 모두 즉시 중앙화할 필요는 없다.
하지만 과도한 요약 때문에 지워버리면 안 된다.

## 9. Placement 관측

제품은 placement decision을 설명 가능하게 만들어야 한다.

운영자는 다음을 답할 수 있어야 한다.

- 어떤 placement가 의도되었는가
- 어떤 placement로 resolve되었는가
- locality가 required였는가 preferred였는가
- downgrade가 있었는가
- 어떤 observable이 그 downgrade를 유발했는가

이것은 placement가 핵심 product semantic이기 때문에 중요하다.

## 10. Fallback 관측

Fallback는 결코 silent side effect처럼 보여서는 안 된다.

시스템은 다음을 보여줘야 한다.

- fallback가 열렸는가
- 왜 열렸는가
- 어떤 policy tier로 이동했는가
- 현재 어떤 candidate class를 시도 중인가
- fallback가 성공했는가, 아니면 소진되었는가

이것이 없으면 required와 preferred behavior를 감사하기 어렵다.

## 11. Retry 및 recovery 관측

Retry와 recovery는 raw log만 뒤져야 보이는 상태가 되어서는 안 된다.

최소한 제품은 다음을 노출해야 한다.

- retry class
- retry count 또는 budget summary
- recovery tier
- exhaustion state

세부 per-attempt timing은 log 또는 trace에 둘 수 있다.
하지만 제품 summary에서도 현재 retry/recovery posture는 보여야 한다.

## 12. Failure attribution 관측

제품은 PoC에서 이미 유용하다고 입증된 failure distinction을 유지해야 한다.

예:

- lookup failure
- transport failure
- producer-side integrity rejection
- consumer-side integrity mismatch
- local verification failure

다음을 볼 수 있어야 한다.

- 무엇이 실패했는가
- 어디서 실패했는가
- 아직 local-only인가, 아니면 이미 product-impacting인가

## 13. Backend 관측

Backend는 관측 가능해야 하지만, backend state가 product view를 지배하면 안 된다.

추천 규칙:

- product summary first
- backend detail second

예:

- top-level status는 artifact availability가 degraded라고 말한다
- backend summary는 Dragonfly adapter의 ensure-on-node 실패를 말한다
- detailed log 또는 trace는 backend-specific execution detail을 담는다

## 14. Observability shape 추천

유용한 개념 shape는 다음과 같다.

```text
Status
  -> current product meaning

Events
  -> important transitions

Logs
  -> decision detail and execution detail

Local or backend traces
  -> deep forensic evidence
```

이 구조는 빠른 가독성과 깊은 디버깅을 동시에 가능하게 한다.

## 15. 해서는 안 되는 것

Observability model은 다음을 피해야 한다.

1. 모든 detail을 status에 넣는 것
2. backend-native dashboard나 log에만 의존하는 것
3. fallback과 downgrade를 opaque generic failure message 뒤에 숨기는 것
4. premature centralization 때문에 node-local forensic detail을 잃는 것

## 16. 현재 설계 결정

현재 결정은 다음과 같다.

- status는 product meaning을 요약한다
- event는 중요한 transition을 설명한다
- log는 controller 및 backend interaction detail을 설명한다
- local forensic trace는 보존한다
- backend observability는 product-readable summary로 번역된다

## 17. 다음 후속 문서

다음으로 유용한 후속 문서는 다음과 같다.

1. `CRD_INTRODUCTION_STRATEGY`
2. `CONTROLLER_RECONCILIATION_MODEL`
3. `OPERATIONAL_RUNBOOK_MODEL`


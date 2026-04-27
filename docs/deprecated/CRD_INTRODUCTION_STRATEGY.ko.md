# CRD_INTRODUCTION_STRATEGY

## 1. 목적

이 문서는 `artifact-handoff`가 CRD를 시간에 따라 어떻게 도입해야 하는지 정의한다.

이 문서는 다음 질문에 답한다.

- 어떤 conceptual object를 먼저 CRD로 만들어야 하는가
- 어떤 conceptual object는 한동안 internal로 남겨야 하는가
- public API를 너무 일찍 고정하지 않으려면 어떻게 해야 하는가
- CRD 도입은 implementation convenience가 아니라 product semantics를 따라가려면 어떻게 해야 하는가

이 문서는 정확한 CRD schema를 확정하지 않는다.
이 문서는 product object model을 Kubernetes API object로 바꾸는 staged strategy를 정의한다.

관련 문서:

- [API_OBJECT_MODEL.ko.md](API_OBJECT_MODEL.ko.md)
- [STATE_AND_STATUS_MODEL.ko.md](STATE_AND_STATUS_MODEL.ko.md)
- [PLACEMENT_AND_FALLBACK_POLICY.ko.md](PLACEMENT_AND_FALLBACK_POLICY.ko.md)
- [OBSERVABILITY_MODEL.ko.md](OBSERVABILITY_MODEL.ko.md)

## 2. 전략 원칙

CRD 도입은 다음 원칙을 따라야 한다.

1. semantics가 충분히 안정되기 전에는 public object로 노출하지 않는다
2. CRD는 backend detail이 아니라 product meaning에 고정한다
3. 초반에는 많은 약한 CRD보다 적은 수의 강한 CRD를 선호한다
4. CRD를 internal design uncertainty를 숨기는 도구로 쓰면 안 된다
5. 초기에는 일부 policy와 execution detail을 internal로 남길 여지를 보존한다

## 3. 왜 전략이 필요한가

현재 설계에는 여러 conceptual object가 있다.

- `Artifact`
- `ArtifactBindingPolicy`
- `ArtifactPlacement`
- `ArtifactBackendPolicy`

하지만 conceptual object가 곧바로 좋은 첫 CRD가 되는 것은 아니다.

CRD는 public contract shape다.
한 번 도입되면 compatibility pressure가 생긴다.
따라서 CRD 도입은 conceptual clarity보다 약간 늦게 가는 편이 맞다.

## 4. 추천 CRD 도입 순서

추천 순서는 다음과 같다.

1. `Artifact`
2. optional embedded 또는 referenced policy field
3. 이후 supporting policy CRD
4. 정말 필요할 때만 separate placement CRD

이 순서는 첫 external surface를 작게 유지하면서도 domain boundary를 내부적으로는 보존하게 해 준다.

## 5. Phase 1: 첫 CRD로서의 `Artifact`

첫 CRD는 `Artifact`가 되어야 한다.

이유:

- anchor product object다
- 가장 명확한 product meaning을 가진다
- identity, integrity, producer locality, top-level availability를 함께 묶는다

첫 CRD는 모든 policy concern을 처음부터 개별 top-level API object로 쪼개려 하면 안 된다.

### 5.1 첫 CRD 방향

첫 `Artifact` CRD는 대체로 다음을 가져야 한다.

- product identity
- digest
- producer reference input
- top-level availability status
- compact한 policy 또는 backend choice reference

이 구조가 public API를 이해하기 쉽게 유지한다.

## 6. Phase 2: policy CRD explosion 전에 embedded policy 우선

많은 policy CRD를 도입하기 전에는 다음을 선호해야 한다.

- embedded policy field
- compact reference
- internal controller-owned interpretation

이것이 특히 중요한 영역:

- consume-policy detail
- fallback-policy detail
- backend-policy hint

이유:

- 이 semantics는 아직 계속 다듬어지고 있다
- 너무 이른 CRD explosion은 너무 많은 API를 너무 빨리 굳힌다

## 7. Phase 3: semantics가 안정되면 separate policy CRD 도입

Separate policy CRD는 다음 조건이 맞을 때 적절해진다.

1. semantics가 안정되었다
2. 여러 artifact 또는 workload에 걸친 재사용이 실제로 필요해졌다
3. validation rule이 독립된 lifecycle과 governance를 필요로 한다

이후 분리 가능성이 높은 CRD 후보:

- `ArtifactBindingPolicy`
- `ArtifactBackendPolicy`

이 object들은 policy 성격이 강하므로 나중에는 독립 lifecycle이 필요할 수 있다.

## 8. Phase 4: `ArtifactPlacement`는 특히 조심해서 다룬다

`ArtifactPlacement`는 자동으로 early CRD가 되어서는 안 된다.

이유:

- placement는 execution에 매우 가깝다
- placement resolution semantics는 아직 더 진화할 가능성이 크다
- 그 가치의 일부는 우선 status나 internal reconciliation state에 더 잘 들어갈 수 있다

Separate placement CRD가 정당화되는 경우:

- explicit placement intent에 external lifecycle이 필요하다
- 외부 시스템이 placement를 독립적으로 작성하거나 관측해야 한다
- status 하나에 placement meaning을 넣기에는 과부하가 생긴다

그 전까지 placement는 다음 중 하나로 남을 수 있다.

- `Artifact.status` 안의 embedded field
- internal controller state
- 나중의 CRD candidate

## 9. 첫 CRD의 spec/status 함의

첫 CRD 도입은 다음을 강하게 지켜야 한다.

- `spec`는 intended product input을 담는다
- `status`는 observed meaning을 담는다

제품은 다음을 해서는 안 된다.

- transient backend execution state를 spec에 쓴다
- 진짜 intended input이 아닌데 observed producer node를 spec에 쓴다
- status를 uncontrolled event history처럼 만든다

## 10. Backend와 CRD 경계

CRD는 backend-neutral하게 유지되어야 한다.

즉 다음을 지켜야 한다.

- Dragonfly-native identity를 top-level artifact identity로 두지 않는다
- backend-specific operational model을 public API surface로 노출하지 않는다
- backend reference가 있더라도 translated product field여야 한다

Public object는 항상 product meaning을 먼저 설명해야 한다.

## 11. CRD 안의 failure와 status 경계

CRD 전략은 다음 구분도 유지해야 한다.

- top-level product phase
- placement 및 fallback status
- backend execution summary
- local forensic detail

이 모든 것이 처음부터 같은 CRD field set 안에 들어가야 하는 것은 아니다.

첫 CRD는 다음을 선호해야 한다.

- concise product status
- summarized failure meaning
- 필요 시 deeper evidence로 가는 link 또는 reference

## 12. 새 CRD를 도입하기 전 validation rule

새 CRD를 도입하기 전에 제품은 다음 질문에 답할 수 있어야 한다.

1. 이 CRD가 노출하는 stable product meaning은 무엇인가
2. 왜 이것을 지금 embedded로 남길 수 없는가
3. 이 CRD는 어떤 lifecycle을 소유하는가
4. 이것이 만드는 compatibility burden은 무엇인가

이 답이 약하면, 그 CRD는 아직 이르다.

## 13. 해서는 안 되는 것

CRD 전략은 다음을 피해야 한다.

1. conceptual noun마다 CRD 하나씩 만드는 것
2. internal controller bookkeeping을 public API로 노출하는 것
3. backend-native shape가 public schema에 새는 것
4. placement와 retry internal을 semantics가 안정되기 전에 permanent CRD field로 굳히는 것

## 14. 현재 전략 결정

현재 전략 결정은 다음과 같다.

- `Artifact`가 첫 번째이자 주요 CRD 후보가 되어야 한다
- policy object는 우선 compact 또는 embedded로 유지한다
- reuse와 semantics가 안정되면 separate policy CRD를 도입할 수 있다
- placement는 CRD 후보로서 특히 보수적으로 다룬다

## 15. 다음 후속 문서

다음으로 유용한 후속 문서는 다음과 같다.

1. `CONTROLLER_RECONCILIATION_MODEL`
2. `OPERATIONAL_RUNBOOK_MODEL`
3. `VERSIONING_AND_COMPATIBILITY_STRATEGY`


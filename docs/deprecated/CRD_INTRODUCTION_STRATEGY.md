# CRD_INTRODUCTION_STRATEGY

## 1. Purpose

This document defines how `artifact-handoff` should introduce CRDs over time.

It answers these questions:

- which conceptual objects should become CRDs first
- which conceptual objects should remain internal for a while
- how to avoid overcommitting to a public API too early
- how CRD introduction should follow product semantics rather than implementation convenience

This document does not finalize exact CRD schemas.
It defines the staged strategy for turning the product object model into Kubernetes API objects.

Related documents:

- [API_OBJECT_MODEL.md](API_OBJECT_MODEL.md)
- [STATE_AND_STATUS_MODEL.md](STATE_AND_STATUS_MODEL.md)
- [PLACEMENT_AND_FALLBACK_POLICY.md](PLACEMENT_AND_FALLBACK_POLICY.md)
- [OBSERVABILITY_MODEL.md](OBSERVABILITY_MODEL.md)

## 2. Strategy Principles

CRD introduction should follow these principles:

1. do not expose a public object before its semantics are stable enough
2. anchor CRDs in product meaning, not backend detail
3. prefer fewer strong CRDs over many weak CRDs early on
4. avoid using CRDs to hide internal design uncertainty
5. preserve room to keep some policy and execution detail internal at first

## 3. Why This Needs A Strategy

The design now has multiple conceptual objects:

- `Artifact`
- `ArtifactBindingPolicy`
- `ArtifactPlacement`
- `ArtifactBackendPolicy`

But a conceptual object is not automatically a good first CRD.

A CRD is a public contract shape.
Once introduced, it creates compatibility pressure.
So CRD introduction should lag slightly behind conceptual clarity.

## 4. Recommended CRD Introduction Order

The recommended order is:

1. `Artifact`
2. optional embedded or referenced policy fields
3. later supporting policy CRDs
4. only later, if justified, separate placement CRDs

This keeps the first external surface small while preserving domain boundaries internally.

## 5. Phase 1: `Artifact` As The First CRD

The first CRD should be `Artifact`.

Why:

- it is the anchor product object
- it carries the clearest product meaning
- it connects identity, integrity, producer locality, and top-level availability

The first CRD should not try to encode every policy concern as its own top-level API object immediately.

### 5.1 First-CRD direction

The first `Artifact` CRD should likely carry:

- product identity
- digest
- producer reference inputs
- top-level availability status
- compact references to policy or backend choices

This keeps the public API understandable.

## 6. Phase 2: Embedded Policy Before Policy CRD Explosion

Before introducing many policy CRDs, the product should prefer:

- embedded policy fields
- compact references
- internal controller-owned interpretation

This is especially important for:

- consume-policy details
- fallback-policy details
- backend-policy hints

Reason:

- these semantics are still being refined
- early CRD explosion would harden too many APIs too soon

## 7. Phase 3: Introduce Separate Policy CRDs When Semantics Stabilize

Separate policy CRDs become appropriate when:

1. the semantics have stabilized
2. reuse across multiple artifacts or workloads becomes real
3. validation rules need independent lifecycle and governance

The most likely candidates for later separate CRDs are:

- `ArtifactBindingPolicy`
- `ArtifactBackendPolicy`

These are policy-like enough that they may later deserve their own lifecycle.

## 8. Phase 4: Treat `ArtifactPlacement` Carefully

`ArtifactPlacement` should not automatically become an early CRD.

Why:

- placement is highly execution-adjacent
- placement resolution may still evolve significantly
- some of its value may fit better as status or internal reconciliation state first

A separate placement CRD becomes justified only if:

- explicit placement intent needs an external lifecycle
- external systems must author or observe placement independently
- status alone becomes too overloaded

Until then, placement may remain:

- embedded in `Artifact.status`
- internal controller state
- or a later CRD candidate

## 9. Spec/Status Implications For The First CRD

The first CRD introduction should strongly enforce:

- `spec` carries intended product inputs
- `status` carries observed meaning

The product should not:

- write transient backend execution state into spec
- write observed producer node into spec unless it is truly intended input
- use status as an uncontrolled event history

## 10. Backend And CRD Boundaries

CRDs must stay backend-neutral.

That means:

- no Dragonfly-native identity as the top-level artifact identity
- no backend-specific operational model as the public API surface
- backend references may exist, but as translated product fields

The public object should describe product meaning first.

## 11. Failure And Status Boundaries In CRDs

The CRD strategy should also preserve the separation between:

- top-level product phase
- placement and fallback status
- backend execution summary
- local forensic detail

Not all of these belong in the same CRD field set immediately.

The first CRD should prefer:

- concise product status
- summarized failure meaning
- links or references to deeper evidence where needed

## 12. Validation Rule For New CRDs

Before introducing a new CRD, the product should be able to answer:

1. what stable product meaning does this CRD expose
2. why can this not remain embedded for now
3. what lifecycle does this CRD own
4. what compatibility burden does this create

If these answers are weak, a new CRD is probably premature.

## 13. What Should Not Happen

The CRD strategy should avoid:

1. one CRD per conceptual noun too early
2. exposing internal controller bookkeeping as public API
3. leaking backend-native shapes into public schema
4. forcing placement and retry internals into permanent CRD fields before the semantics settle

## 14. Initial Strategy Decision

The current strategy decision is:

- `Artifact` should be the first and primary CRD candidate
- policy objects should remain compact or embedded first
- separate policy CRDs may come later once reuse and semantics stabilize
- placement should remain especially conservative as a CRD candidate

## 15. Next Follow-Up

The next useful follow-up documents are:

1. `CONTROLLER_RECONCILIATION_MODEL`
2. `OPERATIONAL_RUNBOOK_MODEL`
3. `VERSIONING_AND_COMPATIBILITY_STRATEGY`


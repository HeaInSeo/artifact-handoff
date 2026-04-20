# API_OBJECT_MODEL

## 1. Purpose

This document defines the initial API and object-model direction for `artifact-handoff`.

It answers these questions:

- which product objects should exist
- which parts of those objects belong in `spec` versus `status`
- which fields should remain product-owned
- how the object model should relate to the domain model without leaking backend-native meaning

This document does not finalize CRDs or wire formats.
It fixes the shape and responsibilities of the first product object model.

Related documents:

- [PRODUCT_IMPLEMENTATION_DESIGN.md](PRODUCT_IMPLEMENTATION_DESIGN.md)
- [ARCHITECTURE.md](ARCHITECTURE.md)
- [DOMAIN_MODEL.md](DOMAIN_MODEL.md)
- [STATE_AND_STATUS_MODEL.md](STATE_AND_STATUS_MODEL.md)
- [PLACEMENT_AND_FALLBACK_POLICY.md](PLACEMENT_AND_FALLBACK_POLICY.md)

## 2. Object-Model Principles

The API and object model should follow these principles:

1. product intent belongs in `spec`
2. observed product meaning belongs in `status`
3. backend execution detail should not dominate top-level product objects
4. placement intent should remain explicit
5. object design should preserve the PoC's useful distinctions without copying PoC internals verbatim

## 3. Initial Object Set

The first product object set should stay small.

Recommended initial objects:

1. `Artifact`
2. `ArtifactBindingPolicy`
3. `ArtifactPlacement`
4. `ArtifactBackendPolicy`

Not all of these must become CRDs immediately.
But these are the right conceptual objects to stabilize first.

## 4. `Artifact`

`Artifact` is the primary product object.

It represents:

- product-owned artifact identity
- producer locality and integrity anchors
- product-visible availability and status

### 4.1 `Artifact.spec`

Recommended conceptual fields:

- `artifactID`
- `digest`
- `producerRef`
- `producePolicy`
- `backendPolicyRef`

`spec` should describe intended identity and producer-facing inputs, not transient backend results.

### 4.2 `Artifact.status`

Recommended conceptual fields:

- `phase`
- `producerNode`
- `producerAvailability`
- `replicas`
- `backendRef`
- `placementSummary`
- `failureSummary`

This keeps observed artifact meaning in status rather than rewriting spec with runtime discoveries.

## 5. `ArtifactBindingPolicy`

This object describes how a consumer may use an artifact.

It represents product consume semantics independently of runtime scheduling syntax.

### 5.1 Why this object should exist

The product should not hide consume behavior inside backend settings or raw workload annotations.

The object exists to make these decisions explicit:

- is same-node required
- is same-node preferred
- is remote access allowed
- what fallback path is valid

### 5.2 `ArtifactBindingPolicy.spec`

Recommended conceptual fields:

- `consumePolicy`
- `required`
- `fallbackPolicy`
- `orderingPolicy`

### 5.3 `ArtifactBindingPolicy.status`

This object may not need much status in phase one.

If status is kept at all, it should stay minimal:

- `accepted`
- `validationErrors`

## 6. `ArtifactPlacement`

This object represents placement-resolution output or intent at the product level.

It exists because the product should keep placement meaning separate from raw Kubernetes object mutation.

### 6.1 What it should capture

- source artifact reference
- placement mode
- resolved locality target
- downgrade and fallback reasoning
- runtime translation summary

### 6.2 `ArtifactPlacement.spec`

Recommended conceptual fields:

- `artifactRef`
- `placementIntent`
- `consumePolicyRef`
- `requestedBy`

### 6.3 `ArtifactPlacement.status`

Recommended conceptual fields:

- `resolvedPlacement`
- `downgraded`
- `downgradeReason`
- `remoteCapableOpened`
- `observedTrigger`

This is where same-node-required versus preferred behavior becomes readable.

## 7. `ArtifactBackendPolicy`

This object exists to keep backend choice and backend-specific knobs out of core artifact identity.

It represents:

- which backend type is allowed or preferred
- which backend options are policy inputs
- what lifecycle expectations the backend should respect

### 7.1 `ArtifactBackendPolicy.spec`

Recommended conceptual fields:

- `backendType`
- `warmAllowed`
- `evictionPolicy`
- `replicaHintPolicy`
- `integrityPolicy`

### 7.2 `ArtifactBackendPolicy.status`

This likely stays minimal in phase one:

- `accepted`
- `backendCompatibility`

## 8. Why Spec And Status Must Stay Cleanly Split

The object model should enforce:

- `spec`: what the user or product intends
- `status`: what the system currently observes

Bad patterns to avoid:

- writing observed producer node into `spec`
- storing backend task IDs as the main artifact identity in `spec`
- encoding every local error directly into top-level spec fields

## 9. Object Relationships

A useful initial relationship model is:

```text
Artifact
  <- referenced by ArtifactPlacement
  <- governed by ArtifactBindingPolicy
  <- shaped by ArtifactBackendPolicy
```

A consumer or workflow-facing system would:

1. create or reference an `Artifact`
2. choose or inherit an `ArtifactBindingPolicy`
3. trigger or request `ArtifactPlacement`
4. allow controller logic to use the referenced `ArtifactBackendPolicy`

## 10. Object Granularity Guidance

The first implementation should avoid both extremes:

- one giant object that carries identity, policy, placement, backend settings, and all observations
- too many tiny objects that create orchestration overhead before meaning is stable

The recommended approach is:

- keep `Artifact` primary
- use the other objects to protect important policy boundaries

## 11. Candidate Minimal First Cut

The actual first shipped object set may be even smaller than the full conceptual set.

A reasonable first cut is:

1. `Artifact`
2. embedded binding-policy fields
3. embedded placement-status fields
4. backend-policy reference

This preserves evolution room while keeping the first implementation manageable.

## 12. Backend Identity Rules In The Object Model

The object model must enforce:

1. `artifactID` is product-owned
2. `backendRef` lives in status or adapter-facing status subfields
3. Dragonfly-native identity must not replace product identity

This is the main protection against backend-driven coupling.

## 13. Placement Identity Rules In The Object Model

The object model must also enforce:

1. placement intent is not the same as concrete K8s placement
2. resolved placement is not the same as consume policy
3. fallback trigger is not the same as final artifact failure

These distinctions are necessary if the product is to remain explainable.

## 14. Status Design Direction

The product should prefer summary-style status over uncontrolled append-only detail.

Top-level status should emphasize:

- current phase
- current producer and replica visibility
- current placement result
- current backend result summary
- current failure summary

Detailed traces can exist elsewhere.
They should not overwhelm the first product object model.

## 15. What The Object Model Should Not Do

The initial object model should avoid:

1. exposing backend-native API as the product object model
2. forcing all policy into workload annotations
3. mixing placement intent, execution result, and forensic trace into one field
4. pretending that the PoC script shape is already the product API

## 16. Initial Design Decision

The current decision is:

- the product object model should stay product-first
- `Artifact` should be the anchor object
- placement, binding, and backend concerns should remain explicit even if implementation starts with a compact first cut
- spec/status separation should be maintained from the beginning

## 17. Next Follow-Up

The next useful follow-up documents are:

1. `RETRY_AND_RECOVERY_POLICY`
2. `OBSERVABILITY_MODEL`
3. `CRD_INTRODUCTION_STRATEGY`


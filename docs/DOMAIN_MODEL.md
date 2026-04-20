# DOMAIN_MODEL

## 1. Purpose

This document defines the product domain model for `artifact-handoff`.

It is not a Go type definition file.
Its goal is to fix the meanings of the main product concepts before implementation begins.

## 2. Modeling Principles

The domain model should follow these principles:

1. product-owned meaning comes first
2. backend-specific identifiers are never top-level product identity
3. placement intent is not the same thing as concrete Kubernetes placement
4. failure attribution is part of the model, but should grow carefully
5. current PoC truths are inputs, not hard-coded final policy

## 3. Core Domain Entities

The minimum product domain should include:

1. `Artifact`
2. `ArtifactBinding`
3. `ConsumePolicy`
4. `PlacementIntent`
5. `ResolvedPlacement`
6. `Replica`
7. `BackendRef`
8. `FailureRecord`

## 4. Artifact

`Artifact` is the main product entity.

Meaning:

- a product-owned handoff unit
- the thing a producer creates
- the thing a consumer requests or depends on

Minimum conceptual fields:

- `ArtifactID`
- `Digest`
- `ProducerRef`
- `ProducerNode`
- `State`
- `BackendRef`
- `Replicas`

Important rule:

- `ArtifactID` is not a Dragonfly task ID
- `ArtifactID` is not a local file path

## 5. ProducerRef

`ProducerRef` identifies who produced the artifact in product terms.

Possible future shapes:

- workload reference
- pipeline node reference
- job reference

The important point is not the exact struct yet.
The important point is that the product model must preserve producer meaning separately from storage location.

## 6. ProducerNode

`ProducerNode` captures the primary locality origin for the artifact.

Meaning:

- first locality input for same-node behavior
- not automatically the only remote source forever

This field should remain explicit in the product model.

## 7. ConsumePolicy

`ConsumePolicy` defines what locality behavior is allowed for a consumer.

Initial conceptual modes:

- `SameNodeOnly`
- `SameNodeThenRemote`
- `RemoteOK`

Policy meaning:

- this is product policy
- this is not the same as a backend transfer option
- this is not the same as Kubernetes scheduling syntax

## 8. ArtifactBinding

`ArtifactBinding` connects a consumer intent to an artifact requirement.

Meaning:

- which artifact is needed
- how strict the consume semantics are
- whether the artifact is required for execution

Minimum conceptual fields:

- `ArtifactID`
- `ConsumePolicy`
- `Required`

This should be the handoff-facing input into placement resolution.

## 9. PlacementIntent

`PlacementIntent` represents product-owned locality direction before Kubernetes translation.

Possible modes:

- `None`
- `CoLocateWithProducer`
- `CoLocateWithReplica`
- `RemoteCapable`

Important distinction:

- `PlacementIntent` is product semantics
- it is not yet a `nodeSelector`
- it is not yet a concrete affinity stanza

## 10. ResolvedPlacement

`ResolvedPlacement` is the concrete runtime-facing output of placement resolution.

Possible conceptual fields:

- `NodeSelector`
- `RequiredNodeAffinity`
- `PreferredNodeAffinity`
- `Reason`

Important distinction:

- this is still product output
- but it is close to runtime application

This object forms the bridge between product reasoning and Kubernetes object translation.

## 11. Replica

`Replica` represents a product-visible alternate availability point for an artifact.

Minimum conceptual fields:

- `Node`
- `Address`
- `State`
- `LocalityRole`

Current PoC input:

- replicas are already meaningful for source selection
- the order in which replicas are considered is currently implementation truth, not final policy

## 12. BackendRef

`BackendRef` represents backend-facing identity without leaking backend-native meaning into the whole product model.

Possible conceptual fields:

- `BackendType`
- `BackendObjectID`
- `BackendHints`

Examples:

- local development backend reference
- Dragonfly task identifier

Important rule:

- `BackendRef` supports execution
- `ArtifactID` remains product identity

## 13. FailureRecord

`FailureRecord` captures meaningful failure attribution.

The PoC already proved that not all failures mean the same thing.

Minimum conceptual dimensions:

- `FailureClass`
- `DetectionPoint`
- `Message`
- `ObservedAt`

Candidate classes:

- control-plane lookup failure
- transport failure
- producer-side integrity rejection
- consumer-side integrity mismatch
- local verification failure

The model should preserve the distinction even if the exact names evolve later.

## 14. Artifact State

The product will likely need a higher-level artifact state model than the PoC.

Candidate states:

- `Registered`
- `AvailableOnProducer`
- `Replicated`
- `Unavailable`
- `Failed`

These are conceptual candidates.
They are not yet fixed API contracts.

## 15. Relationship Summary

The intended relationship is:

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

## 16. Modeling Constraints

The domain model should avoid:

1. coupling product identity to backend identity
2. treating runtime placement syntax as the only policy representation
3. flattening all remote availability into a single anonymous "source"
4. collapsing all failure meanings into one generic error

## 17. Current Domain Decision

The current decision is:

- the domain model must remain product-first
- the first implementation should be driven by the domain model
- backend and runtime layers should adapt to this model, not define it


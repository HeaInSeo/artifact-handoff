# PRODUCT_IMPLEMENTATION_DESIGN

## 1. Purpose

This document defines the initial implementation design for `artifact-handoff`.

It translates the validated findings from `artifact-handoff-poc` into a product-oriented Go project direction.

This document is not a replay of the PoC design. It is the first product design cut that decides:

- what the product should own
- what the product should not own
- which PoC truths are now fixed inputs
- where controller, placement, catalog, and backend boundaries should sit

## 2. Source Of Truth For This Design

This repository explicitly uses `artifact-handoff-poc` as its primary validation reference.

The PoC established the minimum validated facts that this design now treats as fixed inputs:

1. artifact location can be recorded as metadata
2. same-node reuse can be validated from recorded producer locality
3. cross-node peer fetch can be validated
4. second-hit local reuse can be validated after peer fetch
5. node-local failure metadata is useful and should not be collapsed too early into a global failure registry
6. replica fallback can work after producer failure
7. current producer-first ordering is implementation truth, but not yet final policy
8. dynamic placement should be treated as a product-owned resolution step rather than hidden transport behavior
9. Dragonfly should be treated as a replaceable backend, not as the owner of product semantics

## 3. Product Problem Statement

`artifact-handoff` is intended to solve this product problem:

- a producer workload creates an artifact
- the system must record the artifact's location and integrity
- a consumer workload should prefer locality when possible
- when locality is unavailable, the system should open a remote-capable path without abandoning artifact-aware semantics
- the product should keep artifact-handoff policy separate from transport backend implementation

This is not a generic "download acceleration" problem.
It is a product-owned artifact handoff and placement-resolution problem.

## 4. Product Goals

The first-phase goals are:

1. define a product-owned metadata and policy model
2. define a controller-owned placement resolution path
3. define a backend abstraction that can support both simple local implementations and Dragonfly later
4. define a failure model that preserves the PoC's useful distinctions
5. establish a Kubernetes-native Go architecture that can evolve beyond script-assisted validation

## 5. Non-Goals

The initial implementation is not trying to:

- become a full workflow engine
- replace Kubernetes scheduling with a custom scheduler immediately
- implement every possible storage backend
- finalize every retry/recovery policy detail up front
- absorb Dragonfly internals into the product core

## 6. Product-Owned Semantics

The product must own the following semantics:

- `artifactId`
- producer identity and producer locality
- consume policy
- locality preference and downgrade semantics
- replica interpretation
- placement-resolution policy
- failure attribution semantics
- retention and cleanup policy

These semantics must remain product-owned even if the actual artifact bytes later live in a backend such as Dragonfly.

## 7. Proposed System Shape

The initial product shape should have four major areas:

1. API and object model
2. controller and placement resolution
3. metadata and state services
4. backend adapters

Logical view:

```text
Producer workload
  -> artifact registration
  -> metadata persisted

Consumer request / workflow intent
  -> consume policy read
  -> placement resolution
  -> local-preferred or remote-capable decision
  -> backend ensure-on-node
  -> runtime execution
```

## 8. Core Product Concepts

### 8.1 Artifact

A product artifact is the unit of handoff identity.

Minimum fields:

- `ArtifactID`
- `Digest`
- `ProducerRef`
- `ProducerNode`
- `BackendRef`
- `State`

### 8.2 Consume Policy

Consume policy defines what locality behavior is allowed.

Initial modes:

- `SameNodeOnly`
- `SameNodeThenRemote`
- `RemoteOK`

### 8.3 Placement Resolution

Placement resolution is a product-owned step that converts artifact-aware policy plus current observations into concrete Kubernetes placement.

This layer should not be delegated to the backend.

### 8.4 Backend Reference

The product should store a backend reference, not backend internals everywhere.

Examples:

- local backend record
- Dragonfly task identifier
- later backend-specific locator

## 9. Recommended Interface Split

The design should preserve a split similar to the validated PoC follow-up direction:

### 9.1 `ArtifactBinding`

Describes which artifact a consumer needs and how it may be consumed.

Minimum fields:

- `ArtifactID`
- `ConsumePolicy`
- `Required`

### 9.2 `PlacementIntent`

Product-owned locality intent before Kubernetes translation.

Minimum fields:

- `Mode`
- `SourceArtifactID`
- `Reason`

### 9.3 `ResolvedPlacement`

Concrete placement output ready to merge into a Pod or Job spec.

Minimum fields:

- `NodeSelector`
- `RequiredNodeAffinity`
- `PreferredNodeAffinity`
- `Reason`

## 10. Controller Responsibilities

The product controller layer should own:

- artifact registration reconciliation
- artifact status updates
- placement resolution
- downgrade judgment from same-node-required toward remote-capable paths
- backend orchestration requests
- artifact availability status transitions

The controller layer should not directly become the transport implementation.

## 11. Placement Resolution Path

The resolution path should be explicit.

Recommended first-cut flow:

1. read `ArtifactBinding`
2. read artifact metadata
3. read current workload intent
4. read observable scheduling state if needed
5. resolve same-node-required / same-node-preferred / remote-capable path
6. emit `ResolvedPlacement`
7. merge into concrete Kubernetes object

This preserves the PoC judgment that placement must become explicit product logic rather than hidden side effects.

## 12. Failure And Fallback Design Inputs

The following PoC distinctions should be preserved:

- control-plane lookup failure
- peer transport failure
- producer-side integrity rejection
- consumer-side integrity mismatch
- local verification failure

The initial product design should avoid collapsing these into a single generic failure too early.

Fallback should eventually read:

- consume policy
- producer locality
- replica metadata
- backend state
- scheduling observables

## 13. Backend Abstraction

The product should define a backend interface above all concrete implementations.

Recommended first-cut interface shape:

- `Put`
- `EnsureOnNode`
- `Stat`
- `Warm`
- `Evict`

Important rule:

- `ArtifactID` is product-owned
- backend task identifiers are adapter-owned

The product should never expose Dragonfly CLI or Dragonfly-native identifiers as its top-level product contract.

## 14. Dragonfly Position

Dragonfly is a backend candidate.

In this product design, Dragonfly should be treated as:

- replaceable
- adapter-bound
- transport-oriented

Dragonfly should not define:

- artifact identity
- placement policy
- consume policy
- product failure semantics

## 15. Metadata And State Direction

The product will need a richer state model than the PoC, but the state model should grow carefully.

Initial direction:

- keep producer location explicit
- keep replica visibility explicit
- preserve local forensic usefulness
- avoid premature global failure-state explosion

Candidate higher-level artifact states:

- `Registered`
- `AvailableOnProducer`
- `Replicated`
- `Unavailable`
- `Failed`

These are design candidates, not yet final API commitments.

## 16. Proposed Go Project Layout

Initial layout:

```text
.
├── cmd/
│   └── artifact-handoff-controller/
├── docs/
├── internal/
│   ├── api/
│   ├── controller/
│   ├── placement/
│   ├── backend/
│   ├── catalog/
│   └── runtime/
└── pkg/
```

Guidance:

- `internal/api`: internal request and domain models
- `internal/controller`: reconciliation and orchestration logic
- `internal/placement`: placement resolution logic
- `internal/backend`: backend abstractions and adapters
- `internal/catalog`: metadata access and persistence boundaries
- `internal/runtime`: execution-facing helpers

This layout is intentionally small and should expand only when implementation pressure justifies it.

## 17. Initial Implementation Phases

### Phase 1. Design and skeleton

- repository scaffold
- README
- product design document
- package boundaries

### Phase 2. Domain model

- artifact identity model
- consume policy model
- placement-resolution interfaces
- backend interfaces

### Phase 3. Basic controller path

- artifact registration flow
- initial placement-resolution path
- status update loop

### Phase 4. First backend

- simple local/backend adapter for development
- no Dragonfly dependency yet

### Phase 5. Dragonfly adapter spike

- adapter-only implementation
- version-pinned validation
- no product-semantic leakage

## 18. Open Design Questions

The following remain intentionally open:

- exact API surface and whether CRDs are introduced immediately
- exact persistence model for catalog/state
- how far node-local forensic data should be centralized
- how downgrade and retry should interact
- how replica freshness and ranking should work
- how much of runtime execution should stay in this repository versus sibling systems

## 19. Initial Decision

The initial implementation decision is:

- repository name: `artifact-handoff`
- language: Go
- product type: Kubernetes-native control-plane project
- PoC reference: `artifact-handoff-poc`
- backend strategy: replaceable adapters
- Dragonfly role: backend candidate only

## 20. Next Step

The next direct step after this document is to define:

1. the first package-level interfaces
2. the minimum domain types
3. the first executable scaffold under `cmd/`


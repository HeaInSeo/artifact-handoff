# ARCHITECTURE

## 1. Purpose

This document defines the product architecture of `artifact-handoff`.

It answers these questions:

- what are the major product subsystems
- which subsystem owns which responsibility
- which boundaries must remain stable as implementation grows
- how the product should relate to Kubernetes runtimes, metadata stores, and transport backends

This document is architecture-focused.
Domain meanings, policy semantics, and backend-specific contracts are defined in separate documents.

Related documents:

- [PRODUCT_IMPLEMENTATION_DESIGN.md](PRODUCT_IMPLEMENTATION_DESIGN.md)
- [DOMAIN_MODEL.md](DOMAIN_MODEL.md)
- [PLACEMENT_AND_FALLBACK_POLICY.md](PLACEMENT_AND_FALLBACK_POLICY.md)
- [DRAGONFLY_ADAPTER_SPEC.md](DRAGONFLY_ADAPTER_SPEC.md)

## 2. Architectural Position

`artifact-handoff` should be built as a Kubernetes-native control-plane project.
`artifact-handoff` should be built as a long-lived resolver service for Kubernetes batch integrations.

It is not:

- a generic file transfer daemon
- a standalone storage product
- a scheduler replacement in its first phase
- a Dragonfly-derived product

Its core role is:

- own artifact handoff semantics
- resolve locality-aware placement intent
- coordinate artifact availability through backend abstractions

## 3. Architectural Drivers From The PoC

The following facts from `artifact-handoff-poc` drive the architecture:

1. producer locality matters and can be recorded
2. same-node reuse is a real and useful path
3. remote peer fetch is a real and useful fallback path
4. local forensic traces are useful and should not be erased too early
5. source selection must remain product-readable rather than backend-defined
6. placement must become explicit control-plane logic rather than script-only orchestration
7. Dragonfly must stay replaceable

## 4. Top-Level Subsystems

The architecture should be divided into the following top-level subsystems:

1. API and domain layer
2. resolver-service layer
3. placement-resolution layer
4. metadata layer
5. backend adapter layer
6. runtime integration layer
7. observability and operations layer

## 5. API And Domain Layer

This layer owns product vocabulary.

Responsibilities:

- define artifact identity
- define consume-policy semantics
- define placement intent and placement output
- define backend-neutral request and result shapes

This layer must not leak:

- Dragonfly-native identifiers
- raw Kubernetes scheduling details as the only product vocabulary
- backend-specific transfer semantics

## 6. Resolver-Service Layer

The resolver-service layer is the primary orchestrator.

Responsibilities:

- handle product state transitions
- interpret artifact and workload intent
- invoke placement resolution
- invoke backend operations
- update product status
- own downgrade and fallback entry logic

The resolver-service layer should not:

- embed transport backend logic directly
- become a storage engine
- become a generic workflow engine

## 7. Placement-Resolution Layer

This layer converts artifact-aware policy into concrete placement output.

Inputs:

- artifact metadata
- consume policy
- producer locality
- replica visibility
- observable scheduling or runtime state when relevant

Outputs:

- concrete placement constraints
- placement reason
- downgrade or remote-capable decision context

This layer is product-owned and must stay above backend adapters.

## 8. Metadata Layer

The metadata layer owns product-readable artifact state.

Responsibilities:

- persist product artifact records
- persist producer and replica visibility
- expose state needed by placement and fallback
- preserve useful failure attribution

Important boundary:

- metadata state is product state
- backend task state is adapter-facing state

These two layers may reference each other, but they must not collapse into one.

## 9. Backend Adapter Layer

This layer abstracts artifact transport and storage backends.

Responsibilities:

- upload or register payload bytes
- ensure availability on a target node
- expose backend-local status
- optionally warm or evict backend state

This layer must not own:

- artifact identity
- same-node versus remote policy
- placement policy
- failure semantics at the product level

## 10. Runtime Integration Layer

This layer is where the product touches Kubernetes workload execution.

Possible responsibilities:

- merge `ResolvedPlacement` into Job or Pod specs
- annotate runtime objects with product-readable reasons
- pass runtime configuration needed by artifact consumers

This layer should be kept thin.
It is a translation boundary, not the owner of product semantics.

## 11. Observability And Operations Layer

This layer should eventually expose:

- resolver events
- artifact status transitions
- placement decisions
- backend operation outcomes
- failure attribution

The PoC showed that local forensic detail is valuable.
The production architecture should preserve that value while gradually adding centralized visibility.

## 12. End-To-End Logical Flow

The intended high-level flow is:

```text
Producer completes
  -> product artifact record created or updated
  -> producer locality recorded
  -> backend reference recorded

Consumer intent appears
  -> artifact binding read
  -> placement resolution runs
  -> concrete placement emitted
  -> runtime object updated or created
  -> backend ensure-on-node path runs if needed
  -> status and forensic traces updated
```

## 13. Stable Boundaries

The following boundaries should remain stable through early implementation:

### 13.1 Product semantics versus backend semantics

Product semantics must stay in `artifact-handoff`.

### 13.2 Placement resolution versus runtime translation

Placement resolution decides.
Runtime translation applies.

### 13.3 Product metadata versus backend status

The product owns the meaning of artifact availability.
Backends expose implementation-local execution state.

### 13.4 Control plane versus data plane

The control plane decides intent and tracks state.
The data plane transfers or makes bytes available.

## 14. First-Phase Architectural Constraints

The initial architecture should optimize for:

- explicit boundaries
- low conceptual coupling
- backend replaceability
- policy clarity

The initial architecture should not optimize for:

- maximum feature breadth
- microservice fragmentation
- premature plugin ecosystems

## 15. Architecture Risks

The main risks are:

1. allowing backend-native identity to leak into the product model
2. hiding placement logic inside runtime glue
3. over-expanding the metadata state machine too early
4. centralizing failures so aggressively that useful local forensic meaning is lost
5. treating the first implementation shortcut as the final long-term architecture

## 16. Architecture Decision

The current architectural decision is:

- `artifact-handoff` will be a control-plane-first Go project
- the control plane owns artifact semantics, placement resolution, and fallback entry logic
- backends, including Dragonfly, sit behind adapter boundaries
- runtime object mutation is a translation layer, not the core product

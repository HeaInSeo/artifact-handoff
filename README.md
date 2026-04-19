# artifact-handoff

`artifact-handoff` is the product-oriented successor to `artifact-handoff-poc`.

This repository exists to turn the validated ideas from `artifact-handoff-poc` into a real Go-based Kubernetes project with product-owned control-plane semantics for locality-aware artifact handoff.

Reference repository:

- `artifact-handoff-poc`: <https://github.com/HeaInSeo/artifact-handoff-poc> if published later, or the local sibling validation repository used as the primary design reference

## Why This Repository Exists

`artifact-handoff-poc` already validated the narrow core question:

- can artifact location be recorded
- can same-node reuse be driven from that location
- can cross-node peer fetch work when same-node reuse is unavailable
- can replica-aware fallback be made real

But the PoC is intentionally small:

- Python agent and catalog
- script-assisted placement
- narrow lab validation
- intentionally limited durability, retry, policy, and control-plane shape

This repository exists to build the actual product path on top of those validated facts.

## Product Direction

The current intended direction is:

- Go-based Kubernetes-native control plane
- product-owned artifact semantics
- placement resolution that is aware of producer locality and remote-capable fallback
- replaceable transport/cache backends
- Dragonfly as a backend candidate, not as the owner of product semantics

## Non-Goals

At the current stage, this repository is not trying to:

- become a generic P2P distribution platform
- become a generic storage product
- directly fork Dragonfly into the product core
- carry over every PoC script or Python implementation path unchanged

## Initial Scope

The first implementation phase should establish:

1. product vocabulary and API boundaries
2. placement-resolution architecture
3. backend adapter boundaries
4. a minimum Go project layout
5. a migration path from PoC validation into product implementation

## Design Document

The primary starting point is:

- [docs/PRODUCT_IMPLEMENTATION_DESIGN.md](docs/PRODUCT_IMPLEMENTATION_DESIGN.md)

## Relationship To `artifact-handoff-poc`

This repository explicitly builds on findings from `artifact-handoff-poc`.

What is inherited as validated input:

- same-node reuse semantics
- cross-node peer fetch semantics
- node-local forensic failure recording
- producer-first current implementation truth
- replica fallback evidence
- dynamic placement boundary findings
- Dragonfly-as-backend boundary judgment

What is intentionally re-designed here:

- product-owned API and object model
- controller architecture
- placement-resolution ownership
- retry and fallback policy
- durable metadata/store choices
- backend abstraction and lifecycle

## Repository Status

This repository is in the initial design-and-scaffold phase.


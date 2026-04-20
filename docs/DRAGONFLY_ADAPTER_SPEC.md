# DRAGONFLY_ADAPTER_SPEC

## 1. Purpose

This document defines how Dragonfly should be used by `artifact-handoff`.

It does not define Dragonfly as the product core.
It defines Dragonfly as a backend adapter target.

## 2. Core Adapter Decision

Dragonfly must be treated as:

- a replaceable backend
- an adapter-bound transport/cache implementation
- a system that serves product needs without owning product semantics

Dragonfly must not become:

- the product identity model
- the product placement-policy engine
- the product failure-model owner

## 3. Why This Boundary Exists

The PoC and follow-up design already fixed several important truths:

1. `artifactId` is product-owned
2. producer and replica interpretation are product-owned
3. locality and fallback policy are product-owned
4. Dragonfly surfaces may evolve and should be isolated behind an adapter

That means the integration question is not:

- how do we turn the product into Dragonfly?

It is:

- how do we use Dragonfly to satisfy product-owned backend needs?

## 4. Adapter Responsibilities

The Dragonfly adapter should be responsible for:

- importing or registering payload bytes into Dragonfly
- ensuring bytes are available on a target node
- querying Dragonfly-backed availability or task state
- optional warming or eviction support

The adapter should not be responsible for:

- deciding whether same-node or remote should be used
- assigning product artifact identity
- deciding fallback ordering
- interpreting business-level failure meaning

## 5. Product-Owned Inputs To The Adapter

The product should pass inputs such as:

- `ArtifactID`
- local source path or content reference
- digest
- target node
- policy hints

The adapter may derive Dragonfly-native inputs from those values, but it must not redefine their meaning.

## 6. Adapter-Owned Outputs

The adapter may return:

- backend task identifier
- backend availability result
- backend-local state
- backend-local error details

These outputs must be translated before they become product status.

## 7. Recommended Interface Shape

The Dragonfly adapter should implement the product backend interface:

- `Put`
- `EnsureOnNode`
- `Stat`
- `Warm`
- `Evict`

### 7.1 `Put`

Meaning:

- register producer-created bytes into Dragonfly-backed storage or cache distribution

Product-facing inputs:

- `ArtifactID`
- `Digest`
- local source path
- optional policy hints

Product-facing outputs:

- `BackendRef`
- backend status

### 7.2 `EnsureOnNode`

Meaning:

- make sure the artifact is available on a specific target node

This is the most important runtime-facing adapter method.

### 7.3 `Stat`

Meaning:

- ask whether the backend currently knows the object and what it believes its state is

### 7.4 `Warm`

Meaning:

- proactively prepare availability before fan-out or likely consumption

### 7.5 `Evict`

Meaning:

- remove or reduce backend-held state when allowed by product policy

## 8. Identity Rules

The following rules are non-negotiable:

1. `ArtifactID` remains product-owned
2. Dragonfly task IDs remain adapter-owned
3. the product should store a backend reference, not replace its own identity with Dragonfly identity

This prevents backend lock-in at the domain level.

## 9. Failure Translation Rules

Dragonfly-native failures must be translated into product-readable categories.

Examples of translation dimensions:

- backend unreachable
- backend object missing
- backend integrity-related rejection
- backend timeout

The adapter may preserve backend detail, but the product still owns the top-level meaning.

## 10. Versioning And Upgrade Strategy

The adapter should be written with upstream drift in mind.

Recommended rules:

1. pin a tested Dragonfly version range
2. keep Dragonfly-specific CLI or API handling inside the adapter
3. never spread Dragonfly-native assumptions across the rest of the product
4. maintain adapter-level compatibility checks

This is the main reason the adapter boundary exists.

## 11. Data And Control Boundaries

The Dragonfly adapter is a backend/data-plane bridge.

The product control plane should continue to own:

- artifact records
- placement decisions
- consume policy
- fallback entry and downgrade logic
- product status

## 12. Initial Integration Strategy

The recommended integration order is:

1. define the backend interface first
2. implement a simple non-Dragonfly backend first
3. add Dragonfly as an adapter after product boundaries are stable

This reduces the risk that Dragonfly-specific behavior distorts the product model too early.

## 13. Explicit Non-Goals

This adapter specification does not assume:

- a Dragonfly fork
- scheduler-internal customization
- manager-internal product logic
- Dragonfly-native identifiers as public product API

## 14. Current Adapter Decision

The current decision is:

- Dragonfly is a backend candidate
- the adapter boundary must remain thin and explicit
- product semantics remain outside Dragonfly
- upstream Dragonfly changes should primarily affect the adapter, not the whole product architecture


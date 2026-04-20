# STATE_AND_STATUS_MODEL

## 1. Purpose

This document defines the initial state and status direction for `artifact-handoff`.

Its goal is to answer:

- which states belong to the product model
- which states belong only to backend or runtime execution
- how status should be read without collapsing useful distinctions from the PoC
- how the product can grow beyond the PoC's minimal state set without becoming a confused state machine

This document is not a final API schema.
It is the design reference for how state should be structured before implementation begins.

Related documents:

- [PRODUCT_IMPLEMENTATION_DESIGN.md](PRODUCT_IMPLEMENTATION_DESIGN.md)
- [DOMAIN_MODEL.md](DOMAIN_MODEL.md)
- [PLACEMENT_AND_FALLBACK_POLICY.md](PLACEMENT_AND_FALLBACK_POLICY.md)
- [DRAGONFLY_ADAPTER_SPEC.md](DRAGONFLY_ADAPTER_SPEC.md)

## 2. State Modeling Principles

The state model should follow these principles:

1. product state must stay product-readable
2. backend state must not replace product state
3. status should help decisions, not only describe history
4. failure attribution should remain meaningful
5. the model should grow gradually from the PoC rather than jump into an overly rich lifecycle

## 3. PoC Starting Point

The PoC already established a minimum useful state split:

- catalog top-level state centered on `produced`
- local states such as `available-local`, `replicated`, `fetch-failed`
- local `lastError` as a useful forensic signal

That split was intentionally narrow, but it proved something important:

- product-visible state and local forensic state should not be collapsed too early

## 4. Three Layers Of State

The product should distinguish three layers:

1. product artifact state
2. placement and handoff status
3. backend or local execution status

These layers may reference each other, but they should not be flattened into one label.

## 5. Product Artifact State

This is the top-level product view of an artifact.

Candidate states:

- `Registered`
- `AvailableOnProducer`
- `Replicated`
- `Unavailable`
- `Failed`

### 5.1 `Registered`

Meaning:

- the product knows the artifact identity
- the artifact may not yet be confirmed available for consumption

### 5.2 `AvailableOnProducer`

Meaning:

- producer-local availability is known
- digest and producer locality have been recorded

### 5.3 `Replicated`

Meaning:

- at least one additional product-visible availability point exists beyond the producer

This does not necessarily mean the producer copy disappeared.
It means the product can now observe alternate availability.

### 5.4 `Unavailable`

Meaning:

- the artifact cannot currently satisfy expected availability semantics
- the product may still know the artifact identity and past references

### 5.5 `Failed`

Meaning:

- a product-level failure condition has been reached that matters beyond one local probe

Important caution:

- not every local failure should immediately force top-level `Failed`

## 6. Placement And Handoff Status

The product should keep handoff progress readable separately from artifact existence.

Candidate status dimensions:

- locality target
- placement decision
- fallback stage
- availability outcome

These do not all need to be encoded as one enum.
Some should be separate status fields.

## 7. Recommended Status Dimensions

The following status dimensions are recommended.

### 7.1 Availability Status

Answers:

- is the artifact available on the producer
- is it available on a replica
- is it currently unavailable

### 7.2 Placement Status

Answers:

- has placement been resolved
- was placement resolved as required-local, preferred-local, or remote-capable
- what reason was used

### 7.3 Backend Status

Answers:

- does the backend know the object
- is ensure-on-node pending, succeeded, or failed
- what backend-specific state exists

### 7.4 Failure Status

Answers:

- what class of failure was observed
- where it was detected
- whether it is still local-only or product-relevant

## 8. Product State Versus Backend State

These two must remain separate.

Examples:

- Dragonfly task succeeded does not automatically mean the product should mark the artifact as fully available for all policy purposes
- a backend miss does not automatically mean the product artifact identity is invalid
- a local integrity mismatch should be visible even if backend state looks healthy

Product state should summarize product meaning.
Backend state should support execution and diagnosis.

## 9. Product State Versus Local Forensic Status

The PoC proved that local forensic state has value.

Examples:

- `fetch-failed`
- `lastError`
- local digest mismatch
- peer transport exception

The product design should preserve this idea by keeping room for:

- node-local traces
- controller-readable summaries
- product-level status only when escalation is justified

## 10. Failure Escalation Direction

A useful design question is:

- when should a local failure remain local
- when should it be reflected in product status

Recommended initial direction:

- keep a single local probe failure local by default
- escalate to product status when the failure changes availability or policy decisions in a meaningful way

Examples of likely escalation candidates:

- repeated inability to ensure availability on all allowed paths
- consistent producer and replica unavailability
- confirmed integrity failure that blocks consumption

## 11. Replica State Direction

Replica visibility should become part of status, but without overloading the top-level artifact state.

Recommended dimensions for each replica:

- `Node`
- `Address`
- `ObservedState`
- `LastObservedAt`

Candidate `ObservedState` values:

- `Available`
- `Unreachable`
- `Rejected`
- `Unknown`

These are replica-local observations, not necessarily top-level artifact states.

## 12. Placement Status Direction

Placement-related status should explain:

- what the product attempted
- what policy level it used
- whether downgrade occurred
- what observable triggered that downgrade

Candidate fields:

- `PlacementMode`
- `PlacementReason`
- `Downgraded`
- `DowngradeReason`
- `RemoteCapableOpened`

This preserves explainability for same-node versus remote decisions.

## 13. Suggested Status Shape

A useful conceptual shape is:

```text
ArtifactStatus
  - Phase
  - ProducerAvailability
  - ReplicaSummary
  - PlacementStatus
  - BackendStatus
  - FailureSummary
```

This is not a required final struct.
It is the intended separation of concerns.

## 14. State Transition Direction

The initial state-transition direction should stay simple.

Illustrative path:

```text
Registered
  -> AvailableOnProducer
  -> Replicated
  -> Unavailable
  -> Failed
```

Important caution:

- this should not be read as a strict linear machine
- not every artifact must pass through every state
- local failures can happen while the top-level artifact phase remains unchanged

## 15. What Should Not Happen

The model should avoid:

1. using one enum to represent product phase, backend result, placement result, and failure attribution all at once
2. overwriting product status with backend-native labels
3. promoting every local failure into top-level artifact failure
4. hiding downgrade and fallback decisions from status

## 16. Initial Design Decision

The current decision is:

- keep top-level artifact phase small
- separate placement, backend, and failure summaries from that phase
- preserve local forensic usefulness
- introduce product-level failure only when availability or policy semantics are meaningfully affected

## 17. Next Follow-Up

The next useful follow-up documents are:

1. `API_OBJECT_MODEL`
2. `RETRY_AND_RECOVERY_POLICY`
3. `OBSERVABILITY_MODEL`


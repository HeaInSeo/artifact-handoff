# OBSERVABILITY_MODEL

## 1. Purpose

This document defines the initial observability direction for `artifact-handoff`.

It answers these questions:

- what the product should make visible
- which observations belong in status, events, logs, or detailed traces
- how the product should preserve explainability for placement, fallback, retry, and failure attribution
- how observability should remain product-readable instead of backend-native only

This document does not define a full telemetry stack implementation.
It defines the observability model that implementation should later realize.

Related documents:

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [STATE_AND_STATUS_MODEL.md](STATE_AND_STATUS_MODEL.md)
- [PLACEMENT_AND_FALLBACK_POLICY.md](PLACEMENT_AND_FALLBACK_POLICY.md)
- [RETRY_AND_RECOVERY_POLICY.md](RETRY_AND_RECOVERY_POLICY.md)

## 2. Observability Principles

The observability model should follow these principles:

1. product meaning must be visible without reading backend internals first
2. important policy decisions must be explainable
3. local forensic value from the PoC must not be lost
4. summary and detail should remain separate
5. status, events, logs, and traces should each have a clear role

## 3. What The Product Must Make Visible

At minimum, the product should make these things visible:

1. artifact identity and phase
2. producer locality
3. replica visibility
4. placement intent and resolved placement
5. downgrade and fallback decisions
6. retry and recovery progression
7. failure class and detection point
8. backend interaction summary

If these are not visible, the product will be difficult to trust and debug.

## 4. Four Observability Layers

The product should think in four layers:

1. status
2. events
3. logs
4. detailed local or backend traces

These layers should complement each other rather than duplicate everything.

## 5. Status Layer

Status is the first summary layer.

Status should answer:

- what the current product meaning is
- what the current phase is
- what the most important placement and availability facts are
- whether recovery has opened or been exhausted

Status should not try to become an append-only debug history.

## 6. Event Layer

Events should capture meaningful transitions and policy decisions.

Examples:

- artifact registered
- producer locality recorded
- placement resolved
- downgrade triggered
- remote-capable path opened
- retry budget exhausted
- integrity failure escalated

Events should be human-readable and brief.
They should explain change, not dump raw payloads.

## 7. Log Layer

Logs should capture execution details that are too verbose for status or events.

Recommended log content:

- controller decision details
- candidate ordering used during resolution
- backend request and response summaries
- retry attempts with attempt counts
- detailed failure translation

Logs should still use product-readable vocabulary.
They should not devolve into backend-only terminology.

## 8. Local Forensic Layer

The PoC proved that node-local forensic traces are useful.

The product should preserve room for:

- local cache verification evidence
- node-local acquisition failures
- source-specific failure detail
- low-level backend or runtime artifacts when needed

These traces do not all need to be centralized immediately.
But the product should not erase them through over-summarization.

## 9. Observability For Placement

The product must make placement decisions explainable.

Operators should be able to answer:

- what placement was intended
- what placement was resolved
- whether locality was required or preferred
- whether downgrade happened
- which observable triggered that downgrade

This is especially important because placement is a core product semantic.

## 10. Observability For Fallback

Fallback should never look like a silent side effect.

The system should make visible:

- whether fallback opened
- why it opened
- which policy tier it moved into
- which candidate class is currently being tried
- whether fallback succeeded or was exhausted

Without this, required-versus-preferred behavior becomes hard to audit.

## 11. Observability For Retry And Recovery

Retry and recovery should be readable without searching raw logs only.

At minimum, the product should expose:

- retry class
- retry count or budget summary
- recovery tier
- exhaustion state

Detailed per-attempt timing can stay in logs or traces.
But the product summary must still show the current retry or recovery posture.

## 12. Observability For Failure Attribution

The product should preserve the failure distinctions already proven useful in the PoC.

Examples:

- lookup failure
- transport failure
- producer-side integrity rejection
- consumer-side integrity mismatch
- local verification failure

It should be possible to see:

- what failed
- where it failed
- whether it was still local-only or already product-impacting

## 13. Observability For Backends

Backends should be observable, but backend state should not dominate the product view.

Recommended rule:

- product summary first
- backend detail second

Examples:

- top-level status says artifact availability is degraded
- backend summary says ensure-on-node failed on Dragonfly adapter
- detailed logs or traces carry backend-specific execution detail

## 14. Observability Shape Recommendation

A useful conceptual shape is:

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

This preserves fast readability and deep debugging at the same time.

## 15. What Should Not Happen

The observability model should avoid:

1. forcing all detail into status
2. relying only on backend-native dashboards or logs
3. hiding fallback and downgrade behind opaque generic failure messages
4. losing node-local forensic detail because of premature centralization

## 16. Initial Design Decision

The current decision is:

- status should summarize product meaning
- events should explain important transitions
- logs should explain controller and backend interaction detail
- local forensic traces should remain preserved
- backend observability should be translated into product-readable summaries

## 17. Next Follow-Up

The next useful follow-up documents are:

1. `CRD_INTRODUCTION_STRATEGY`
2. `CONTROLLER_RECONCILIATION_MODEL`
3. `OPERATIONAL_RUNBOOK_MODEL`


# RETRY_AND_RECOVERY_POLICY

## 1. Purpose

This document defines the initial retry and recovery direction for `artifact-handoff`.

It answers these questions:

- which failures should be retried
- when retries should remain local versus become product-level recovery decisions
- how retry policy should interact with locality, fallback, and artifact integrity
- which failures should stop progress instead of triggering more attempts

This document does not pin exact timing values yet.
It fixes the policy structure that later implementation should follow.

Related documents:

- [PLACEMENT_AND_FALLBACK_POLICY.md](PLACEMENT_AND_FALLBACK_POLICY.md)
- [STATE_AND_STATUS_MODEL.md](STATE_AND_STATUS_MODEL.md)
- [API_OBJECT_MODEL.md](API_OBJECT_MODEL.md)
- [DRAGONFLY_ADAPTER_SPEC.md](DRAGONFLY_ADAPTER_SPEC.md)

## 2. Policy Principles

Retry and recovery policy should follow these principles:

1. not every failure deserves retry
2. integrity failures should be treated differently from transport failures
3. recovery should remain artifact-aware
4. retries should not silently erase failure attribution
5. required locality and preferred locality must not be treated the same

## 3. Why This Needs A Separate Policy

The PoC already proved that several failure classes look similar at a distance but mean different things:

- catalog lookup failure
- peer fetch transport failure
- producer-side integrity rejection
- consumer-side integrity mismatch
- local verification failure

A useful product cannot assign one retry rule to all of them.

## 4. Retry Versus Recovery

The product should keep these separate.

### 4.1 Retry

Retry means:

- attempt the same class of action again
- usually against the same path or same decision tier

### 4.2 Recovery

Recovery means:

- open a different allowed path
- change decision tier
- or escalate the condition into a higher-level product outcome

Examples:

- retrying a peer fetch to the same candidate is retry
- moving from same-node-required to a remote-capable path is recovery
- switching from producer candidate to replica candidate may be retry or recovery depending on the policy tier in which it happens

## 5. Failure Classes And Default Direction

The initial policy should classify failures like this.

### 5.1 Control-plane lookup failure

Examples:

- metadata lookup unavailable
- product record temporarily unreadable

Default direction:

- short retry budget is reasonable
- repeated failure may escalate to product-visible unavailability

### 5.2 Transport failure

Examples:

- connection refused
- timeout
- peer temporarily unreachable

Default direction:

- retry is reasonable
- alternate remote candidate selection may also be reasonable
- transport failure alone should not immediately invalidate the artifact identity

### 5.3 Producer-side integrity rejection

Examples:

- producer rejects with a digest mismatch before serving bytes

Default direction:

- blind retry is usually not useful
- the condition should be treated as a meaningful integrity signal
- recovery may require a different source or policy-level escalation

### 5.4 Consumer-side integrity mismatch

Examples:

- bytes were read but the consumer concluded the digest was wrong

Default direction:

- do not repeatedly retry the same suspicious source without bounds
- prefer alternate allowed source or escalation

### 5.5 Local verification failure

Examples:

- local cached copy digest mismatch

Default direction:

- local copy should no longer be trusted
- recovery should prefer re-acquisition from an allowed source
- repeated reuse of the same broken local copy is not acceptable

## 6. Retry Tiers

The initial policy should think in tiers rather than in one flat retry loop.

### 6.1 Tier 1: same-attempt retry

Examples:

- retry a transient metadata read
- retry a peer connection once or a small number of times

### 6.2 Tier 2: same-policy alternate candidate

Examples:

- same remote-capable path, but next allowed source candidate
- producer first, then replica, when policy allows

### 6.3 Tier 3: policy-level recovery

Examples:

- required-locality downgrade
- opening remote-capable resolution
- changing placement decision tier

### 6.4 Tier 4: product-level failure

Examples:

- all allowed paths exhausted
- integrity failure blocks all trusted acquisition paths
- artifact becomes effectively unavailable

## 7. Locality-Aware Retry Direction

Retry policy must respect locality policy.

### 7.1 `SameNodeOnly`

Direction:

- local retry may be allowed
- remote recovery is not allowed
- exhaustion should become a clear failure rather than an implicit remote continuation

### 7.2 `SameNodeThenRemote`

Direction:

- local retry first
- then downgrade/recovery if the trigger is justified
- then remote-capable retry against allowed sources

### 7.3 `RemoteOK`

Direction:

- the product may move more quickly into alternate allowed remote paths
- but still should preserve integrity and failure attribution rules

## 8. Suggested Recovery Order

A practical initial order is:

1. short local or same-tier retry
2. alternate candidate within the same allowed policy tier
3. policy-level downgrade or recovery opening
4. higher-level failure summary

This order prevents both extremes:

- endless retries on one broken path
- immediate escalation without trying valid alternate paths

## 9. Integrity-Specific Rules

The product should adopt stricter rules for integrity failures.

Recommended direction:

1. do not treat integrity failures as ordinary transient network noise
2. avoid repeatedly trusting a source already proven suspicious
3. preserve failure evidence
4. prefer alternate trusted paths when allowed
5. escalate sooner than for simple transport failures

## 10. Backoff Direction

Exact values can remain open for now, but the policy direction should be:

- bounded retry count
- bounded time window
- increased delay for repeated transient failures
- no unbounded retry loop hidden in one request path

This is especially important for controller-driven reconciliation.

## 11. Status Interaction

Retry and recovery must be reflected in status in a readable way.

Recommended status questions:

- are we still retrying the same class of action
- have we downgraded policy tier
- has remote-capable recovery opened
- did the artifact become unavailable after exhaustion

This should connect to the status model, not live only in logs.

## 12. Backend Interaction

Backends may perform their own internal retries.

The product policy must still remain above them.

Rules:

1. backend retries do not replace product retry policy
2. backend retry detail may be summarized, not blindly mirrored
3. backend exhaustion should be translated into product-readable failure meaning

## 13. What Should Not Be Retried Blindly

The initial policy should avoid blind retry for:

1. repeated digest mismatch from the same source
2. repeatedly corrupted local cache reuse
3. policy-forbidden remote continuation
4. structurally invalid object references

These cases require either alternate recovery or clear failure.

## 14. Initial Policy Decision

The current decision is:

- transport and lookup failures can receive bounded retry
- integrity failures should escalate faster and prefer alternate trusted paths
- locality policy constrains what recovery paths are legal
- recovery should move in explicit tiers rather than through hidden fallback

## 15. Next Follow-Up

The next useful follow-up documents are:

1. `OBSERVABILITY_MODEL`
2. `CRD_INTRODUCTION_STRATEGY`
3. `CONTROLLER_RECONCILIATION_MODEL`


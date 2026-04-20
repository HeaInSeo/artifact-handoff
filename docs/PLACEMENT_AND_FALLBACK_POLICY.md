# PLACEMENT_AND_FALLBACK_POLICY

## 1. Purpose

This document defines the product policy direction for placement and fallback in `artifact-handoff`.

It builds on the PoC findings that:

- same-node behavior matters
- current required locality and future preferred locality must be distinguished
- fallback triggers should be observable
- remote-capable resolution is an artifact-aware policy step

## 2. Policy Scope

This document covers:

- locality policy
- downgrade semantics
- fallback trigger direction
- remote-capable resolution inputs

It does not define:

- backend transport protocol details
- every retry timing parameter
- every scheduler integration detail

## 3. Main Policy Goal

The main goal is:

- prefer locality when that is semantically useful
- do not lose artifact-aware meaning when locality cannot be satisfied

This is different from both extremes:

- always force same-node no matter what
- immediately allow any remote placement without artifact-aware reasoning

## 4. Current PoC Truth

The PoC established:

1. same-node can be explicitly forced
2. that current explicit path is `required`, not `preferred`
3. producer-first ordering is current implementation truth
4. replica fallback is real after producer failure
5. a meaningful fallback trigger candidate should come from API-level observables

This product policy should use those truths as inputs without freezing them as the final policy.

## 5. Locality Policy Levels

The product should think in explicit policy levels.

### 5.1 SameNodeOnly

Meaning:

- the consumer must run with producer-locality semantics
- no remote-capable path is allowed

### 5.2 SameNodeThenRemote

Meaning:

- locality is the preferred first path
- remote-capable continuation is allowed when locality cannot be satisfied or maintained

### 5.3 RemoteOK

Meaning:

- the consumer is not limited to same-node semantics
- placement may open a remote-capable path earlier

## 6. Required Versus Preferred

The product must keep these distinct.

### 6.1 Required locality

Meaning:

- placement failure is a real blocking condition
- the system cannot quietly spill to another node

### 6.2 Preferred locality

Meaning:

- the system should try locality first
- but may deliberately continue through a different path when allowed by policy

The PoC showed that these two meanings must not be collapsed.

## 7. Fallback Trigger Direction

The first fallback-trigger direction should remain observable-first.

The strongest current candidate is:

- `PodScheduled=False`
- reason: `Unschedulable`

Why:

- locality constraints ultimately land in API objects
- scheduling impossibility should be detected before a later generic terminal failure when possible

This does not mean every future fallback must be scheduling-triggered.
It means the first downgrade path should remain grounded in explicit observable evidence.

## 8. Downgrade Model

The product should think in two stages:

1. `required -> preferred`
2. `preferred -> remote-capable`

This matters because:

- not every required-locality miss should immediately become an unconstrained remote path
- policy transition should remain explainable

## 9. Remote-Capable Resolution

Remote-capable resolution is not just relaxed scheduling.

It must still read artifact-aware inputs, including:

- consume policy
- producer locality
- visible replicas
- ordering semantics
- observable failure signal

So remote-capable resolution should mean:

- locality is no longer being forced in the same way
- but artifact-aware source and placement reasoning is still active

## 10. Ordering Policy Direction

The product should explicitly distinguish:

- current implementation truth
- intended future policy

Current truth from the PoC:

- producer first
- replica fallback later

Future policy remains open on:

- whether producer-first remains default
- whether replicas can outrank the producer in some conditions
- how freshness, health, and retry should affect ordering

## 11. Failure Inputs For Policy

Placement and fallback policy should eventually read at least these categories:

1. scheduling failure signals
2. metadata lookup failures
3. remote transport failures
4. producer-side integrity rejection
5. consumer-side integrity mismatch
6. local verification failure

Not all of these should trigger the same policy reaction.

## 12. Policy Constraints

The policy must avoid:

1. silently treating required locality as preferred locality
2. treating every remote continuation as equivalent
3. collapsing scheduling failure and artifact failure into one bucket
4. letting backend-native behavior become the product fallback policy

## 13. Recommended Initial Policy Decision

The current recommended product direction is:

- support explicit consume-policy levels
- preserve the required/preferred distinction
- use observable scheduling signals as the first downgrade input
- treat remote-capable resolution as an artifact-aware policy step
- keep ordering policy open beyond the currently validated producer-first truth

## 14. Follow-Up Policy Questions

The main policy questions still open are:

1. exact downgrade timing and retry behavior
2. whether there is a separate preferred-affinity phase before remote-capable resolution
3. how replica ordering should evolve beyond recorded order
4. how cleanup and retention policy interact with fallback behavior


# DYNAMIC_PARENT_TO_CHILD_HANDOFF_GUIDE

## 1. Purpose

This document explains, in a practical and implementation-friendly way, how `artifact-handoff` should support **dynamic parent-to-child artifact handoff** without relying on PV/PVC.

This guide is intentionally written to help pipeline designers answer questions such as:

- how can a child node read a parent-produced file dynamically
- when should the system decide same-node versus remote fetch
- what information must be recorded after the parent finishes
- where should the pipeline call into `artifact-handoff`
- what actually happens inside Kubernetes when this is done correctly

This is not a low-level code specification.
It is a design guide for building a real pipeline integration.

Related documents:

- [PRODUCT_IMPLEMENTATION_DESIGN.md](PRODUCT_IMPLEMENTATION_DESIGN.md)
- [ARCHITECTURE.md](ARCHITECTURE.md)
- [DOMAIN_MODEL.md](DOMAIN_MODEL.md)
- [PLACEMENT_AND_FALLBACK_POLICY.md](PLACEMENT_AND_FALLBACK_POLICY.md)
- [RETRY_AND_RECOVERY_POLICY.md](RETRY_AND_RECOVERY_POLICY.md)

## 2. The Main Idea

The core idea is simple:

- the parent produces data on a real Kubernetes node
- the system records where that data now lives
- the child is created only after that location is known
- right before creating the child, the system decides:
  - same-node reuse
  - or remote-capable acquisition

So the system is not doing:

- "copy the file somewhere first and always read it later"

It is doing:

- "learn where the file is first, then decide how the child should access it"

That is what makes the handoff dynamic.

## 3. What "Dynamic" Means Here

In this guide, "dynamic" means:

- the child is **not fully fixed in advance**
- the child's placement and artifact-acquisition path are decided **after the parent result is known**

That decision can change per run.

Examples:

- run A: parent finishes on node `worker-0`, so child is placed on `worker-0`
- run B: parent finishes on node `worker-1`, so child is placed on `worker-1`
- run C: same-node placement is not allowed or not possible, so child is placed elsewhere and fetches remotely

This is very different from a static design where:

- the child always runs on one predetermined node
- or the child always reads from one central storage path

## 4. What Kubernetes Gives You, And What It Does Not

Kubernetes gives you primitives, not artifact-handoff semantics.

Kubernetes gives you:

- Pods and Jobs
- node placement controls such as `nodeSelector` and `affinity`
- networking between Pods and nodes
- ways to mount node-local paths
- ways to inject env vars, annotations, and other runtime metadata

Kubernetes does **not** give you a built-in API that means:

- "this child should consume the file that this parent just created"
- "prefer same-node reuse first"
- "if that fails, fetch from producer or replica"

That product meaning must be implemented by the pipeline system and `artifact-handoff`.

## 5. The Physical Reality You Must Design Around

When a parent Pod creates a file, the file is physically created somewhere real.

That means:

- on a node-local disk
- in a node-local directory
- or through a backend that still has some concrete availability location

Even if the pipeline thinks in DAG nodes, the actual bytes exist on:

- some machine
- at some path or backend object
- and under some network reachability conditions

The child can only consume the data if the system knows at least one of these:

1. the child is on the same node and can reuse it locally
2. the child can fetch it from a producer or replica
3. the backend can materialize it on the child's node

So the first design rule is:

- **the system must record artifact location and integrity immediately after production**

## 6. The Minimum Information You Must Record After The Parent Finishes

After the parent finishes, the system should record at least:

- `artifactId`
- `digest`
- `producerPod`
- `producerJob` or producer task reference
- `producerNode`
- `producerAddress` or runtime acquisition endpoint
- `backendRef` if a backend is involved
- `size` if useful
- initial availability state

This is the minimum useful record because:

- `artifactId` tells you what the child needs
- `digest` tells you what correctness means
- `producerNode` tells you whether same-node reuse is possible
- `producerAddress` tells you how to fetch from the producer if needed
- `backendRef` tells you whether there is a backend-mediated path

Without this metadata, dynamic handoff is mostly guesswork.

## 7. The Minimum Integration Points In A Pipeline

To use `artifact-handoff` in a real pipeline, the pipeline needs three integration points.

### 7.1 After parent completion

The pipeline must register the produced artifact.

This is where the system records:

- identity
- digest
- producer locality
- acquisition endpoint

### 7.2 Before child creation

The pipeline must ask:

- where is the artifact
- what consume policy applies
- should the child be same-node, preferred-local, or remote-capable

This is the most important dynamic decision point.

### 7.3 During child startup or pre-start

The child must have a way to actually obtain the artifact.

That may be:

- local reuse
- producer fetch
- replica fetch
- backend ensure-on-node

If the child has no acquisition path at runtime, the placement decision alone is not enough.

## 8. The Core Dynamic Flow

The most practical dynamic flow looks like this:

```text
1. Parent runs
2. Parent produces artifact
3. Artifact metadata is registered
4. Child is not submitted yet
5. Pipeline asks artifact-handoff to resolve handoff strategy
6. artifact-handoff returns:
   - placement decision
   - acquisition decision
7. Pipeline creates child Job/Pod using that result
8. Child starts
9. Child acquires artifact locally or remotely
10. Child runs main computation
```

This is the cleanest place to make the decision because it uses the freshest parent result.

## 9. The Three Main Handoff Modes

For practical pipeline design, you should think in three modes.

### 9.1 Same-node local reuse

Meaning:

- the parent's node is known
- the child is placed on that same node
- the child reads the artifact locally

This is the cheapest and simplest successful path.

### 9.2 Remote-capable fetch from producer

Meaning:

- the child does not run on the producer node
- the child acquires the artifact from the producer endpoint

This is still artifact-aware because it uses the recorded producer as the source.

### 9.3 Remote-capable fetch from replica

Meaning:

- the producer is not the only useful source anymore
- an already known replica may serve the child

This becomes important later when:

- the producer is unavailable
- or a replica is the only healthy or reachable source

## 10. Where The Decision Should Be Made

The most important implementation rule is:

- **do not decide too early**

The decision should usually be made:

- after the parent artifact record exists
- before the child Job or Pod is created

That means the decision belongs in:

- a controller
- an orchestrator
- a DAG submit hook
- or a job-mutation step right before submission

It usually does **not** belong in:

- static YAML alone
- a compile-time pipeline graph with no runtime feedback

## 11. What The Decision Needs As Input

Before creating the child, the decision layer should read:

- artifact identity
- producer node
- producer endpoint
- known replicas
- consume policy
- whether same-node is required or only preferred
- current cluster or runtime conditions if relevant

This is enough to answer:

- should the child be forced onto the producer node
- should the child prefer the producer node
- should remote-capable acquisition be opened
- which source should be tried first

## 12. What The Decision Should Return

The decision layer should return two things.

### 12.1 Placement result

Examples:

- put child on producer node
- prefer producer node
- allow remote-capable path

This may later become:

- `nodeSelector`
- `nodeAffinity`
- annotations
- or another runtime placement representation

### 12.2 Acquisition result

Examples:

- read local path
- fetch from producer
- fetch from replica
- ensure via backend

This tells the child how it will actually access the artifact.

These two results should remain separate.
Placement is not the same thing as acquisition.

## 13. What Actually Gets Injected Into The Child

In practice, the pipeline will often inject some combination of:

- `nodeSelector`
- `affinity`
- env vars
- annotations
- init-container configuration
- startup arguments

Examples of useful child-facing values:

- `ARTIFACT_ID`
- `ARTIFACT_DIGEST`
- `ARTIFACT_EXPECTED_MODE=local|remote`
- `ARTIFACT_SOURCE_HINT=producer|replica|backend`
- `ARTIFACT_PRODUCER_NODE`
- `ARTIFACT_PRODUCER_ADDRESS`

The child does not need the whole product state.
It only needs the runtime information required to acquire the artifact correctly.

## 14. A Simple Same-Node Example

A very simple same-node flow is:

1. parent Pod finishes on `worker-0`
2. parent registers:
   - `artifactId=a1`
   - `producerNode=worker-0`
   - `producerAddress=http://10.x.x.x:8080`
3. pipeline asks artifact-handoff:
   - "child needs `a1`, policy is `SameNodeThenRemote`"
4. artifact-handoff says:
   - place child on `worker-0`
   - acquisition mode is local
5. pipeline creates child Job with:
   - `nodeSelector=kubernetes.io/hostname=worker-0`
6. child starts on `worker-0`
7. child reads local artifact
8. child continues

This is dynamic because the chosen node came from the parent's actual runtime result.

## 15. A Simple Remote Example

A simple remote flow is:

1. parent Pod finishes on `worker-0`
2. artifact metadata is registered
3. child cannot or should not run on `worker-0`
4. artifact-handoff returns:
   - remote-capable placement
   - source=producer
5. child is created on `worker-1`
6. child starts
7. child or an init-step fetches artifact from producer endpoint
8. digest is verified
9. child continues

This is still dynamic because the source and placement were resolved from live runtime state.

## 16. A Replica Example

Later, a replica-aware flow looks like this:

1. parent produces artifact on `worker-0`
2. another successful consumer on `worker-1` leaves a known replica
3. a new child now starts on `worker-2`
4. producer is unavailable or policy allows alternate source
5. artifact-handoff returns:
   - remote-capable acquisition
   - source=replica on `worker-1`
6. child fetches from the replica

This is why replicas are important:

- they allow remote-capable recovery without assuming the producer remains the only valid source forever

## 17. What The Child Runtime Usually Needs

There are two common child-runtime patterns.

### 17.1 Init-step acquisition

The artifact is obtained before the main container starts.

Pros:

- the main container sees a ready input
- failure is explicit before main compute begins

### 17.2 In-process acquisition

The application or runtime library fetches the artifact at startup.

Pros:

- fewer moving pieces
- easier if the application already has an artifact client

Both are valid.
The important part is that the acquisition behavior follows the resolved handoff strategy.

## 18. How This Looks In A DAG System

In a DAG system, the conceptual parent and child are graph nodes.

But at runtime, the real handoff path is:

- producer DAG node
  -> Kubernetes Job/Pod
  -> artifact registration
  -> child DAG node submission
  -> resolved artifact acquisition

So the DAG system should not think:

- "parent output just exists somewhere in the graph"

It should think:

- "parent output has a concrete runtime location and a concrete acquisition path"

That is the key mental shift.

## 19. Where This Usually Fails In Practice

The most common design mistakes are:

1. deciding the child node before the parent result exists
2. recording only artifact identity but not producer locality
3. treating placement and acquisition as the same thing
4. assuming the producer will always be reachable
5. assuming any failure can be retried the same way
6. hiding all dynamic decisions in logs only

If you avoid these, the design becomes much clearer.

## 20. What To Design First If You Are Building A Pipeline

If you are integrating this into another project, design these first:

1. artifact registration point after parent completion
2. child submission hook or controller decision point
3. artifact metadata schema
4. child acquisition mechanism
5. digest verification behavior
6. same-node versus remote-capable policy

If these six are clear, the rest becomes much easier.

## 21. A Practical Minimum Design

If you want the smallest realistic design, start with this:

1. parent registers `artifactId`, `digest`, `producerNode`, `producerAddress`
2. child creation is delayed until registration exists
3. same-node is attempted first
4. remote fetch from producer is the first remote path
5. digest verification is mandatory
6. local and remote failure reasons are preserved

This is enough to build a real first dynamic handoff path.

## 22. What "Possible" Really Means

So, when someone asks:

- "can parent-produced data be handed to the child dynamically without PV/PVC?"

The correct answer is:

- yes, if the pipeline records artifact location after parent completion
- yes, if the child is submitted only after the handoff strategy is resolved
- yes, if the child has a real acquisition path
- yes, if placement and acquisition are treated as separate but coordinated decisions

That is the practical meaning of "dynamic handoff" in this project.

## 23. Final Summary

The shortest correct summary is:

> A dynamic parent-to-child handoff is possible when the parent artifact is registered with real runtime locality, the child is created only after that locality is known, and the system resolves both child placement and artifact acquisition from that live metadata instead of assuming a static shared-storage path.

If you are designing a pipeline, the most important thing to remember is:

- first record where the data is
- then decide how the child will get it


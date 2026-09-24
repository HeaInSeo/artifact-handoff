# F4 Mode B — RunID identity (FD-DATA-01 R07/R08/R10)

Status: implemented in artifact-handoff. The JUMI consumer change (always send `run_id`,
remove the `FirstNonEmpty(SampleRunID, RunID)` fallback) is a separate successor.

## Rule

`run_id` is the canonical execution identity. `sample_run_id` is grouping metadata only.

| Concern | Keyed by |
|---|---|
| Artifact key / canonical artifact ID | `run/<runID>/<producerNode>/<producerAttempt>/<output>` |
| Node terminal partition | `run/<runID>/<node>/<attempt>` |
| Lifecycle, finalize, GC scope | `run_lifecycles.run_id` |
| Sample grouping (list only) | `sample_run_id` metadata — every listed item keeps its own `run_id` |

- A missing `run_id` is rejected (`INVALID_ARGUMENT` / HTTP 400) on every write and
  lookup, including the store layer. It is never derived from `sample_run_id`.
- The leading `run` segment keeps the key space disjoint from pre-F4 keys (a key
  component can never contain `/`), even when a RunID string equals an old SampleRunID.

## Wire (additive)

`run_id` was added with new field numbers only: `ArtifactRef=12`, `ArtifactBinding=13`,
`NotifyNodeTerminalRequest=5`, `FinalizeSampleRunRequest=2`, `EvaluateGCRequest=2`,
`GetSampleRunLifecycleRequest=2`, `GetSampleRunLifecycleResponse=16`. No number is reused
or retyped (`buf breaking` WIRE_JSON passes). RPC names are unchanged; `FinalizeSampleRun`,
`EvaluateGC` and `GetSampleRunLifecycle` act on the Run named by `run_id`.

An old client that sends only `sample_run_id` is refused, not reinterpreted.

HTTP: JSON `runId`; query `runId` for `artifacts:get`, `artifacts:list` (or
`sampleRunId` for grouping) and `sampleRuns:lifecycle`; new `GET /v1/sampleRuns:runs?sampleRunId=`
lists a Sample's Run lifecycles.

## SQLite migration (schema version 2)

One transaction, idempotent on reopen:

1. refuse a store stamped with a newer `schema_version` (downgrade protection);
2. add `run_id` to `artifacts` and `node_terminals`, create `run_lifecycles`, index `run_id`;
3. record the legacy disposition once in `ah_schema_meta`
   (`legacy_unresolved_artifacts`, `legacy_unresolved_node_terminals`,
   `legacy_unresolved_sample_run_lifecycles`) and stamp `schema_version=2`.

Pre-F4 rows are **legacy-unresolved**: kept as-is (no delete, no backfill from
`sample_run_id`), never returned by any lookup or grouping, and never GC-evaluated. The
pre-F4 `sample_run_lifecycles` table is never read. `SQLiteStore.LegacyDisposition`
reports the recorded counts. Binaries older than this change do not read the schema
version, so the downgrade refusal protects only from this version onward.

## Acceptance evidence (tests)

| R10 | Test |
|---|---|
| 1 same Sample R1/R2 distinct identity | `TestR10_SameSampleRunsHaveDistinctIdentity` |
| 2 R1 GC does not touch R2 | `TestR10_R1GCDoesNotTouchR2` |
| 3 sample metadata does not change the key | `TestR10_SampleMetadataDoesNotChangeKey` |
| 4 missing RunID fails closed (service + gRPC) | `TestR10_MissingRunIDFailsClosed`, `TestSQLite_WritesRequireRunID`, `TestKeysRequireRunID` |
| 5 ambiguous legacy row not attributed | `TestSQLite_LegacyRowsAreQuarantinedNotAttributed` |
| 6 multiple Runs per Sample listable, not merged | `TestR10_SampleGroupingListsRunsWithoutMergingIdentity` |
| 7 no cross-Run key collision under concurrency | `TestR10_ConcurrentRunsDoNotCollide`, `TestRunKeysDisjointFromLegacySampleKeys` |
| A2 schema downgrade refusal | `TestSQLite_RefusesNewerSchemaVersion` |
| A3 migration crash-safety / idempotent reopen | `TestSQLite_RunIdentityMigrationIsIdempotent` (single-transaction migration) |
| A4 no reused field numbers | `buf breaking --against '.git#branch=main'` (WIRE_JSON) |

A1 (new JUMI vs old AH capability) and A5 (RunID charset at JUMI) are JUMI-side and
belong to the consumer successor.

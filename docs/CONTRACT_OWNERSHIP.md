# AH Contract Ownership

## Ownership

AH owns the artifact handoff API contract.  
`api/proto/ah/v1/ah_v1.proto` is the single source of truth.

JUMI, Spawner, and all other consumers must not copy or vendor stale generated protobuf files.  
Consumers must depend on a tagged AH version.

## Field Number Policy

- v1 protobuf field numbers must **never** be reused, even if the original field is removed.
- Removing a field requires a `reserve` statement:
  ```protobuf
  reserved 3;
  reserved "old_field_name";
  ```
- Breaking changes (incompatible wire format) require one of:
  - A new package: `ah.v2`
  - An explicit pre-v1 reset documented in git history with a clear commit message

## Versioning

- While the package is `ah.v1` but pre-release (no tagged v1.0.0), breaking changes are allowed
  with a documented git commit explaining the reset.
- Once a tagged v1.0.0 is cut and any consumer has integrated, the field number policy above
  becomes strictly enforced.

## Consumer Responsibilities

- Consumers must treat `resolution_status`, `decision`, `placement_intent`, and
  `materialization_plan` as the authoritative handoff contract.
- Consumers must not re-interpret `placement_intent` directly as Kubernetes scheduling constraints.
  That translation belongs in the consumer (Spawner).
- Consumers must not derive node placement from `materialization_plan.uri` alone.

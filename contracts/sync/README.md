# Sync contracts

Task 013 freezes provider-neutral sync policy and propagation metadata/result shapes.
Provider request/response DTOs do not belong here. Connector adapters translate these
canonical boundaries to their own APIs while preserving stable idempotency,
correlation and causation metadata when the remote platform supports it.

`source_of_truth` is a conflict policy, not an unconditional write bypass. Normal
inbound/outbound flow follows `direction`; source-of-truth is consulted only when
both local version and remote revision advanced from the last synchronized state.

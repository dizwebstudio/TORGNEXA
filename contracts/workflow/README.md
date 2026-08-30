# Workflow contracts

`definition-v1.schema.json` is the strict Draft 2020-12 input contract for a
bounded provider-neutral workflow. Graph reachability, cycle detection,
allowlisted action semantics and deterministic plan hashing are enforced by the
Go compiler because those invariants span multiple nodes.

`run-v1.schema.json` and `step-evidence-v1.schema.json` are the public,
payload-free state/evidence contracts. They expose only opaque identifiers,
digests, bounded machine error codes and timestamps; credentials and raw event
payloads are not valid workflow data.

Runtime limits are intentionally stricter than a generic JSON schema: 64 nodes,
128 edges, 16 KiB definitions, 8 attempts per step, 100 active workflows, 120
new runs per minute and 8 concurrently claimed runs per workspace. The Go
compiler/repository enforce the cross-record graph, quota and lease invariants.

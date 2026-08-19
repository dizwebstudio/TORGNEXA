# Sandbox / Test Mode

Task 029 turns the Task-025 signed `AdmissionPlan` into an executable dry-run/test boundary without opening third-party provider admission.

## Modes

### `dry_run`

Dry-run is side-effect free by construction:

- production **and sandbox** secret providers are never called;
- `UseSecret` receives only a synthetic non-secret placeholder inside callback scope;
- network calls are recorded as bounded `ExternalAction` intents and no transport is invoked;
- output contains resource ids, operation kind and before/after SHA-256 digests, never full provider payloads or credentials;
- result status is `planned`.

### `test`

Test mode may execute mediated network calls and resolve credentials, but only with a `CredentialBinding{tier:"sandbox"}`. `tier:"production"` is rejected before the secret broker is touched. There is deliberately no production override in Task 029.

## Host-mediated egress

A connector cannot import direct network packages. It asks the host runtime to perform a network action against an exact destination already present in the signed/granted Task-025 plan.

Before transport, the host:

1. matches exact DNS host + port against the grant;
2. resolves the hostname on every request;
3. rejects loopback, private, link-local, carrier-grade NAT, benchmark, documentation, multicast and other special-use addresses;
4. returns a pinned IP dial target while retaining the original hostname as TLS `ServerName`.

This prevents a provider from switching an allowlisted DNS name to `127.0.0.1`, RFC1918, metadata-service or similar internal destinations. The transport must dial the pinned address rather than perform a second DNS lookup.

## Linux reference sandbox

`scripts/check-connector-sandbox-linux.sh` builds the deterministic emulator with `CGO_ENABLED=0` and executes it through `internal/platform/connectorsandbox.LinuxSandbox`.

The reference sandbox uses:

- Linux user namespace;
- mount namespace;
- network namespace with no external route;
- IPC and UTS namespaces;
- minimal chroot containing only the emulator binary;
- fixed minimal environment (`LANG`, `TZ`, `PATH`, sandbox mode only);
- host-side wall/CPU/RSS/output/concurrency enforcement from the signed `IsolationLimits`.

The isolation probe fails if the child can observe a host production-secret environment variable, `/run/secrets/torgnexa-production`, `/etc/passwd`, or direct Internet TCP connectivity.

Production may use a stronger container/microVM backend, but it must preserve or exceed these semantics.

## Deterministic emulator

`internal/platform/connectorsandbox/emulator` is provider-neutral test support. It exercises capability authorization, scoped secret use, mediated egress and deterministic change-digest output. It is not located under `connectors/` or `plugins/`, is not a marketplace provider and cannot open provider admission.

## Result contract

`contracts/sandbox/dry-run-result-v1.schema.json` defines the operation result. It contains only:

- connector/request/capability identity;
- planned resource changes as hashes;
- bounded network intents without query strings;
- normalized reason code;
- measured resource usage;
- explicit isolation evidence;
- canonical UTC completion time.

Replayable fixtures therefore remain sanitized by construction.

## Provider admission

Tasks 010, 025, 029 and 064 are repository-complete. `provider_admission.enabled` remains `false` in the Task-064 completion change because Task 080 requires Task 064 to be completed in the merge base before a later protected admission-control change. A successful sandbox or conformance probe alone never admits or executes an arbitrary provider artifact.

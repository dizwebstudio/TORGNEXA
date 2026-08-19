# ADR-0038: Connector sandbox and dry-run runtime

Status: Accepted

## Context

Task 025 stabilized a signed, least-privilege `AdmissionPlan` but deliberately left its limits and grants inert. Connector SDK v1 therefore still needed an executable host boundary that could prove dry-run side-effect suppression, production-credential isolation, mediated egress, and resource ceilings before provider conformance could begin.

## Decision

Add the host-owned `internal/platform/connectorsandbox` runtime inside the existing `connector-plugin-runtime-capabilities` pillar. Dry-run never invokes a secret source or network transport; it returns only bounded change digests and external-action intents. Test mode accepts only explicitly sandbox-tier credential bindings. Production-tier credentials are rejected before any secret broker callback.

All connector egress remains host mediated. Exact granted DNS host/port pairs are resolved by the host on each use, special/private/link-local/documentation addresses are rejected, and the returned dial plan pins the resolved IP while preserving the original TLS server name. Provider code still cannot import direct network packages under the Task-025 architecture policy.

Task 029 also provides a Linux reference isolation runner for the deterministic emulator. It executes a statically built host-approved emulator through user/mount/network/IPC/UTS namespaces, an empty/minimal environment, and a minimal chroot. The host monitors wall time, CPU runtime, RSS, output bytes and concurrent calls against signed Task-025 ceilings. The reference probe must demonstrate that host production environment values, host secret files and direct network access are absent.

The deterministic emulator lives under `internal/platform/connectorsandbox/emulator`; it is test support, not an admitted provider. No third-party provider is registered by this task and `provider_admission.enabled` remains false until Task 064 conformance completes.

## Consequences

Connector SDK operations can now be previewed without production side effects, and test execution has a concrete isolation boundary rather than a documentation flag. DNS rebinding/private-address escape is fail-closed at the host egress planner. Signed resource ceilings now have executable enforcement in the Linux reference sandbox.

The reference runner intentionally does not make arbitrary third-party artifacts executable merely because they are signed. Provider activation still requires Task 064 evidence and the architecture provider-admission gate. Production deployments must qualify equivalent or stronger Linux/container isolation and may replace the reference runner behind the same security semantics.

## Alternatives considered

Running dry-run through production credentials and suppressing only writes was rejected because reads can leak data and token use itself violates isolation. Allowing direct `net/http` from a sandboxed connector was rejected because DNS rebinding and private-address routing would bypass the host grant. Treating a container name or boolean `sandbox=true` as proof was rejected because it provides no deterministic credential/network/filesystem/resource evidence.

## Compatibility impact

Connector SDK v1 root interfaces and Task-025 signed descriptor/grant/admission-plan contracts are unchanged. Task 029 adds separate sandbox result/probe contracts and host runtime packages. No existing API/event payload is rewritten and provider admission remains disabled.

## Migration and data impact

Task 029 adds no database migration and stores no new production data. Dry-run results are bounded transient artifacts containing hashes/normalized metadata rather than provider payloads or credentials. The Linux reference sandbox uses a temporary chroot removed after execution.

## Security and privacy impact

Dry-run touches neither production nor sandbox secret providers. Test mode refuses production-tier credential bindings before broker invocation. Host egress checks exact grants, re-resolves DNS, rejects special/private addresses and returns pinned-IP/TLS-name dial plans. The external Linux probe verifies environment, host secret filesystem and direct network isolation. Result contracts exclude raw secrets, query strings and full provider bodies.

## Operational impact

Linux CI runs `scripts/check-connector-sandbox-linux.sh` as part of `make check`. A Linux environment without `unshare` fails sandbox qualification rather than silently degrading. Deployment runtimes may use stronger container/microVM isolation but must preserve the same credential, egress and resource-limit semantics. Task 064 remains the final provider-conformance prerequisite before provider admission can be enabled.

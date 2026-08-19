# Plugin Security

Official in-tree connectors share the same SDK as future third-party plugins.

Tasks 025 and 029 establish the executable isolation contract before provider admission:
- signed artifact/provenance + immutable digest;
- least-privilege capability/secret/network grants;
- host-owned dry-run with no secret or network provider calls;
- sandbox-tier-only credentials in test mode;
- exact outbound DNS host/port allowlist with private/special-address rejection and IP pinning;
- Linux reference user/mount/network/IPC/UTS namespace + minimal chroot execution for the deterministic emulator;
- wall/CPU/RSS/output/concurrency ceilings;
- architecture-level denial of provider direct process/filesystem/network imports.

Plugins never receive direct SQL credentials or global tenant access. Task 029 deliberately has no production-credential override. Task 064 now proves conformance against these boundaries; provider admission remained disabled in the Task-064 completion change. Task 011 is the later reviewed admission change and registers only the read-only Wildberries reference after the prerequisite set is complete. Deployment isolation may be stronger (container/microVM) but may not be weaker.

## Task 078 governance composition

Marketplace review and installation do not weaken this boundary. Public/private listings expose trust and the complete requested authority before activation, but cryptographic authenticity is never treated as tenant authorization. Exact-artifact consent and append-only artifact/publisher-key/installation revocations are evaluated before `Prepare`; a new digest cannot inherit a prior grant. Official/verified marketplace publication additionally requires Task-065 SBOM/provenance evidence.

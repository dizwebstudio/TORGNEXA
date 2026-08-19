# ADR 0068: Provider-neutral EDO SDK and adapters

## Status
Accepted — Task 68.

## Context
Task 070 needs Diadoc and Saby EDO connectivity while preserving one provider-neutral document state model. Remote EDO status must remain authoritative and signed artifacts must use the isolated Task 069 signing boundary.

## Decision
1. Extend Connector SDK v1 additively with EDO document read/send/sign-request interfaces.
2. Admit separate `diadoc` and `saby-edo` providers behind typed injected transports.
3. Keep host document state provider-neutral and refresh it from remote authoritative status.
4. Require signed-artifact/signature references for send workflows and preserve idempotency/external IDs.

## Alternatives considered
- Embed vendor SDK models in Core: rejected because it leaks provider semantics.
- Browser automation/private endpoints: rejected.
- Treat local `sent` as final: rejected because provider-side acceptance/signing transitions are authoritative.

## Compatibility impact
The SDK extension is capability-specific and additive; the root connector/runtime interfaces are unchanged. Existing document flows remain compatible.

## Migration and data impact
Migration `000043` adds tenant-scoped EDO document projection and append-only remote-status evidence. No destructive migration is introduced.

## Operational impact
Each provider requires approved credentials/egress and remote-status polling/webhook reconciliation. Operators must monitor document aging and retry only idempotent operations.

## Security and privacy impact
Credentials stay in host secret management. Signed artifacts are referenced, not copied into generic state. Provider code has no direct database/Core authority.

## Consequences
TORGNEXA can support multiple EDO providers without changing the host document model; additional providers require normal conformance/admission.

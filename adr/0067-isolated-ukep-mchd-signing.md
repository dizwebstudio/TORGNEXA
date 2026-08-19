# ADR 0067: Isolated UKEP and MChD signing foundation

## Status
Accepted — Task 67.

## Context
Tasks 069, 070 and later regulated document flows require qualified electronic signing and machine-readable powers of attorney without allowing private key material to cross generic API, event, plugin or persistence boundaries.

## Decision
1. Define a host-owned signing service around opaque key references, certificate metadata, signing requests and MChD authority references.
2. Require an explicit approval reference before every signing operation.
3. Persist only certificate/request/evidence metadata; private keys and PINs never enter domain records.
4. Make signing evidence append-only and idempotency-keyed.

## Alternatives considered
- Put PKCS#12/private keys in connector secrets: rejected because generic connector code must not handle raw signing keys.
- Sign inside EDO providers: rejected because signing is a reusable regulated capability and would create provider lock-in.
- Allow unsigned approval bypass: rejected fail-open behavior.

## Compatibility impact
All interfaces are additive and host-side. Existing Connector SDK and public HTTP contracts remain unchanged.

## Migration and data impact
Migration `000042` adds certificate metadata, MChD authorities, signing requests and append-only signing evidence under tenant RLS. Existing data is untouched.

## Operational impact
Production must bind the signer to an approved cryptographic/HSM/CSP implementation, enforce certificate expiry/revocation checks and monitor signing failures and approval latency.

## Security and privacy impact
Private keys never leave the signer boundary. Stored material is metadata/evidence only; approval and audit references are mandatory for regulated writes.

## Consequences
EDO, fiscal and other regulated workflows can request signatures safely while cryptographic implementation remains replaceable and isolated.

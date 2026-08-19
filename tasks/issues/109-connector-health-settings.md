# Task 109: Connector Health Settings

## Status
`done`

## Objective
Show connection health, rate limits, recent failures and reauthorization actions for configured connector accounts.

## Dependencies
105, 106, 108, 060, 066

## Acceptance
- health distinguishes configuration, authentication, rate-limit and remote-service states;
- errors are structured and redact credentials/PII;
- reauthorization does not silently broaden capabilities;
- history is bounded and tenant-scoped;
- operational status does not replace authoritative audit evidence.

## Implementation
- Every production health check appends a bounded tenant-scoped normalized snapshot.
- API exposes recent history with machine-readable health categories and reason codes.
- Existing OAuth reauthorization remains capability-preserving and audited.
- Health history is operational evidence only; authoritative audit evidence is unchanged.

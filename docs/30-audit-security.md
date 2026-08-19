# Audit & Security Controls

Audit is distinct from application logs. It records business/security actions and is append-oriented.

Actors: user, service account, connector, n8n, OpenClaw/MCP, scheduler/system.

Record: tenant/workspace, action, target, risk class, correlation, approval/signing reference, safe before/after summary, timestamp and request/source identity.

Security baseline: MFA at IdP, scoped API keys/service accounts, session policy, least privilege, dependency/image scanning, CSP/CSRF protections for UI, rate limits, secure headers and tenant-isolation tests.

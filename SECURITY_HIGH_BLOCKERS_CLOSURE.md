# TORGNEXA HIGH security blocker closure

Date: 2026-08-12

## Closed: production API security composition

- one production composition root;
- health is the only public API route;
- all configured application routes require authn -> tenant resolution -> authz;
- private route registration without any required security dependency fails at startup;
- security edge wraps the route table before application handlers;
- feature-specific handler factories are package-private;
- AST regression test rejects future exported handler factories that could bypass the root;
- production trusted-proxy configuration is explicit and validated.

## Closed: unsafe JavaScript supply-chain publication

- floating `@n8n/node-cli: *` and `n8n-workflow: *` removed;
- n8n release build graph reduced to exact `typescript@5.9.3`;
- `n8n-workflow` exact peer is `2.16.0`;
- committed npm lock records SHA-512 integrity;
- release/CI policy rejects floating/ranged dependencies and lock drift;
- CI uses `npm ci --ignore-scripts`, pinned Node 22.16.0 and a full-SHA pinned setup-node action;
- frontend is explicitly source-only until its own reviewed lockfile is committed, so an
  unlocked transitive frontend graph cannot enter production release bytes.

This closes the original HIGH condition: neither an uncomposed private HTTP endpoint nor an
unlocked JavaScript dependency graph can be silently introduced into the current production
release path.

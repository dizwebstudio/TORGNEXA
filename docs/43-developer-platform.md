# Developer Platform

TORGNEXA is API-first and must be pleasant to integrate without reading internal source code.

## Deliverables

- versioned OpenAPI and webhook docs;
- generated/supported Go, TypeScript and Python SDKs (Task 062 repository-complete; deterministic OpenAPI-only generation under `sdk/`);
- API key/service-account management with scopes;
- webhook registration, signing, retries and delivery inspection;
- connector/plugin SDK documentation and examples;
- sandbox/demo tenant and fixtures;
- compatibility/deprecation policy;
- developer portal content generated from repository contracts.

No SDK may expose unstable internal database models as public API.

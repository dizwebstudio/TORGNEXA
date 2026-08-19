# Task 032: Frontend Shell

## Status
Repository-complete. Production Vite bundling and Node dependency lock/scanner qualification remain dependency-enabled CI/Task-065 release evidence; they are not claimed by this sandbox.

## Objective
Create the React/TypeScript/Vite frontend shell with a host-owned OIDC authentication boundary, capability-aware navigation/route guards, and API access exclusively through the generated TypeScript SDK from Task 062.

## Dependencies
002, 017, 022, 026, 062

## Deliverables
React 19 + TypeScript + Vite shell; TanStack Query server-state boundary; memory-only OIDC auth adapter contract; public non-secret UI-session schema; centralized capability-aware navigation and direct-route guards; same-origin generated SDK composition; API-backed Catalog, Orders and Notifications screens; safe placeholders for later atomic domains; deterministic frontend logic/type/static-security checks; ADR-0047 and architecture/governance evidence; documentation.

## Acceptance
Anonymous, malformed or expired sessions fail closed. Browser access tokens remain runtime-only memory material and never enter the public session projection, URL, DOM/logging contract, localStorage, sessionStorage or cookies; refresh/downstream provider credentials are outside the shell. Navigation and direct routes require exact capability claims but remain presentation-only controls: server OIDC/RBAC/tenant resolution is authoritative. The frontend must call same-origin `/api/v1` through `@torgnexa/sdk`, must not synthesize organization/workspace selectors, copy generated clients, or branch on provider identities. Catalog, Orders and Notifications decode bounded public responses and handle loading/empty/error states. `make frontend-check` must pass deterministic logic tests, repository TypeScript validation and static policy checks; when dependencies are installed it additionally runs the real Vite production build.

# ADR-0186 — Frontend session and cache lifetime

Status: Accepted

## Context

Audit A01 found that the application-wide QueryClient survived logout/login.
Fresh orders and financial results from A could be displayed to B without a
request; an API error could leave the previous data visible. Server-side RLS
cannot prevent rendering an already cached browser result. Late authentication
and API responses also need an explicit owner after account changes.

## Decision

Keep ADR-0047's React, host-owned OIDC adapter and TanStack Query boundaries.
Publish normalized identity and an internal session lifetime atomically through
a small controller consumed with `useSyncExternalStore`. Each lifetime owns its
own QueryClient and authenticated component tree. Query keys remain resource
keys inside that isolated client; no bearer token or raw subject becomes a key.

Compare subject, optional host-owned cache scope, normalized roles and
capabilities. The optional `cacheScope` covers issuer/organization/workspace/login
context. Ordinary token renewal retains the lifetime only when this context is
unchanged. Adapters without a scope conservatively retire it on token changes.
Adding or removing scope metadata also retires it.

Retirement synchronously aborts API work, cancels queries and clears query and
mutation caches. A keyed provider remounts the entire authenticated subtree,
including local component state. Logout retires before awaiting remote work.
Host change notifications retire immediately before looking up the new session.
Failed/missing/malformed sessions and hard expiry also retire. An old expiry
callback rechecks the current expiry so it cannot discard an already renewed
session that retained the same lifetime.

Generated SDK fetches combine caller cancellation with the lifetime signal and
check it after responses. Even transports that complete despite abort cannot
return stale results or trigger 401 refresh/logout in the next session. A 401
replay is allowed only within the same active context. Request/interaction
revisions in the controller and Community adapter fence late authentication
results, including an old authorization-code exchange after a new login.

## Compatibility and migration

No API/event/SQL changes, new dependencies or data migration. The optional
memory-only adapter field preserves v1 shape compatibility. Hosts without it
may observe extra refetches and an interrupted request when their token changes;
they can provide a complete stable scope to preserve normal renewal behavior.
Hosts must notify subscribers when their context changes. Unreported changes in
an external host cannot be inferred by the shell.

Rebuild and deploy the frontend, then reload existing open tabs to replace the
old application-wide cache. A service already running old assets is not patched
by changing source files. Server-side auth, tenant resolution, RLS and idempotency
continue to apply independently of the UI.

## Security and privacy

Cache scope is bounded identity-mapping metadata. It is excluded from public
session projection, DOM, logs, query keys, URLs and browser persistence. The
Community adapter reads scope claims only for partitioning; it does not treat
decoded claims as authorization evidence or synthesize API tenant selectors.
Frontend static policy permits those claim names only in that adapter.
No new persisted personal fields or retention paths are introduced. All
regression fixtures are synthetic. Cancellation cannot roll back writes already
accepted by the server.

## Validation

Logic tests cover context changes, scoped renewal, stale adapter/API responses,
expiry and 401 write replay. A standalone headless Chrome regression runs real
React StrictMode, QueryClient, generated SDK and Orders UI with synthetic auth
and API responses. It verifies A → logout → B, B loading/failure/recovery, late A
200/401, workspace and permission changes, and hard expiry. This is a browser
integration test, not a live Keycloak/provider qualification.

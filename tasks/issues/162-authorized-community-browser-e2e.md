# Task 162: Authorized Community browser E2E

## Status

`repository-complete` — 2026-08-29.

## Objective

Provide a repeatable authenticated browser check for the local Community
deployment. The check must exercise the real Keycloak authorization-code flow
with the synthetic `demo` workspace member before reading commerce data.

## Deliverables

- dependency-free Chrome DevTools Protocol runner and shell entrypoint;
- idempotent demo-user/workspace bootstrap before the browser starts;
- catalog verification with a loaded product thumbnail and main product image;
- product-card image tab verification;
- orders verification with a loaded product thumbnail in the list and detail;
- failure screenshot written outside the repository for diagnosis;
- documented `make community-e2e` workflow.

## Scope limits

The test does not persist browser profiles, tokens, cookies or tenant
selectors. Before the assertions it idempotently runs the synthetic demo
dataset action; user-owned catalog and order records are not changed. The
demo password is accepted only as a local test default and can be overridden
for a local Keycloak realm through `TORGNEXA_DEMO_PASSWORD`.

## Verification

Run `make community-e2e` after the Community stack is available. The target
reconciles the Keycloak demo account and workspace membership, opens a clean
Chrome profile, signs in through the visible Keycloak form, then verifies the
catalog, product images, orders and order actions through the rendered UI.
Repository-only frontend validation remains covered by
`./scripts/check-frontend-shell.sh`.

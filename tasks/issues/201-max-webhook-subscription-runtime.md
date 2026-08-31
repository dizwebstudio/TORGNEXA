# Task 201: MAX — жизненный цикл подписки webhook

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить существующие MAX subscribe/unsubscribe операции к reviewed
builtin runtime и authenticated Social host route без ослабления tenant,
secret, idempotency или audit boundaries.

## Deliverables

- admit MAX `SocialWebhookController` in the registry;
- verify exact update types, verification secret and HTTPS endpoint boundary;
- keep only explicit `success=true` as a successful provider result;
- update support tests, matrix, connector docs, ADR, architecture review and
  execution backlog.

## Scope limits

Provider-native polling, callback actions, comments, analytics and arbitrary
update types remain fail-closed. No new persistence or public endpoint is
introduced.

## Verification

Run `gofmt`, targeted and full `go test ./...`, `go vet ./...`, contract and
migration checks, frontend catalog/shell/build checks, architecture review and
`git diff --check`.

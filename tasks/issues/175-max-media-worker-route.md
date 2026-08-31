# Task 175: MAX media publication worker route

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the runtime gap between the qualified MAX media adapter and the
application worker for released images, image galleries and supported videos.

## Deliverables

- exact MAX `/uploads` admission for image/video initialization;
- official upload-host allowlist and bounded multipart upload transport;
- callback-scoped bot token propagation for the upload request;
- runtime-support and generated catalog admission for media and video;
- deterministic host, multipart and adapter tests;
- synchronized matrix, MAX docs, task record, ADR and architecture review.

## Scope limits

Only the existing provider-neutral image/gallery/video request shapes are
enabled. Image formats and video containers follow the adapter's documented
limits; upload URLs are restricted to `iu.oneme.ru` and `omub.okcdn.ru` with
HTTPS and port 443. Buttons, webhooks, edit/delete, status reads and arbitrary
files remain outside the application runtime subset.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, the frontend generated-catalog checks and
`git diff --check`. Live credentialed MAX qualification remains a separate
release gate.

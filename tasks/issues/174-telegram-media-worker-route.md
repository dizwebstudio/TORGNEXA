# Task 174: Telegram media publication worker route

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the runtime gap between the qualified Telegram social adapter and the
application worker for image, photo-album and MP4-video publications.

## Deliverables

- worker mapping for Core image, gallery and video variants;
- tenant-scoped `MediaAccessor` backed by the Task-088 released-upload gate;
- provider transport support for bounded Telegram multipart uploads;
- runtime-support and generated catalog admission for media and video;
- deterministic multipart, worker mapping and existing adapter tests;
- synchronized matrix, Telegram docs, task record, ADR and architecture review.

## Scope limits

Only one photo, 2–10 JPEG/PNG/WebP photos, or one MP4 video is admitted. The
worker revalidates the released upload before opening content and never accepts
client object keys. URL buttons, edit/delete, inbound webhooks and arbitrary
Telegram file types remain fail-closed.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, the frontend generated-catalog checks and
`git diff --check`. Live credentialed Telegram qualification remains a
separate release gate.

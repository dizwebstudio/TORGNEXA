# Task 182: MAX HTTPS publication buttons

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the application-runtime gap for the already qualified MAX
`social.post.buttons` adapter capability.

## Deliverables

- admit `social.post.buttons` in the built-in MAX runtime-support contract;
- regenerate the frontend catalog and Go runtime projection;
- expose the capability to the existing Social API, worker and `/social` UI;
- update the fail-closed matrix, MAX documentation and architecture evidence;
- cover the admission with runtime-support and existing adapter tests.

## Scope limits

Only bounded HTTPS URL buttons are admitted. The existing limits remain: up to
eight buttons, labels up to 64 characters and URLs up to 2048 characters, with
at most three buttons per MAX row. Edit/delete, status reads and webhooks stay
outside the connected application workflow.

## Security and compatibility

No API, event or database shape changes are required because the provider-
neutral button contract was admitted by Task 181. Account and channel
capability settings, tenant scope, worker lease, released-upload checks and
ambiguous-write handling remain mandatory.

## Verification

Run the runtime-support tests, generated catalog check, frontend shell
validation, contract/migration/SDK checks, `go test ./...`, `go vet ./...` and
`git diff --check`. Credentialed live MAX qualification remains a separate
release gate.

# Task 135 — VK production runtime composition

Status: Planned

## Problem

The VK Connector SDK adapter and manifest exist, while the production runtime
still classifies VK as planned. Task 134 supplies the missing host-owned OAuth
access-token projection and refresh boundary, but no Social/API/worker route is
yet admitted for VK.

## Scope

- Compose the existing VK adapter through provider-neutral application ports.
- Admit only capabilities with a complete API, persistence, worker and operator
  workflow; all remaining manifest capabilities stay unavailable.
- Use Task 134 for current access-token delivery and automatic refresh without
  exposing OAuth bundle material to the adapter.
- Add deterministic transport, conformance, failure/idempotency and runtime
  readiness evidence for the admitted capability subset.
- Update the generated runtime catalog and user documentation truthfully.

## Acceptance criteria

- VK moves from `planned` only after every advertised production operation has
  an executable end-to-end route and fail-closed negative coverage.
- Browser login is required initially and after explicit reauthorization only;
  ordinary access-token expiry is handled by the host refresh runtime.
- Core and application modules do not branch on the VK identifier; provider
  dispatch remains inside the built-in composition boundary.
- Access, refresh and client credential material never appears in logs, events,
  audit records, API responses or normal database columns.
- Remote writes are idempotent or fail as outcome-unknown without unsafe replay.
- Go test/vet, contracts, architecture, conformance, frontend and rebuilt
  runtime health gates pass.

## Explicit exclusions

- No capability is admitted from manifest/SDK presence alone.
- No scraping, browser-cookie automation or undocumented private endpoint.
- No weakening of Task 134 refresh locking, secret rotation or reauthorization
  semantics.
- Live-provider readiness is not claimed without a dedicated non-production VK
  account and retained remote evidence.

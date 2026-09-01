# Task 135 — VK production runtime composition

Status: Repository-complete

## Problem

The VK Connector SDK adapter and manifest exist, while the production runtime
still classifies VK as planned. Task 134 supplies the missing host-owned OAuth
access-token projection and refresh boundary, but no Social/API/worker route is
yet admitted for VK.

Repository status: the text-publication slice is now admitted. VK is available
in «Интеграции» and «Публикации» through the shared Social Core workflow; media,
comments and analytics remain explicitly SDK-ready but runtime-deferred until
their complete host and frontend workflows are implemented.

## Scope

- Compose the existing VK adapter through provider-neutral application ports.
- Admit only capabilities with a complete API, persistence, worker and operator
  workflow; all remaining manifest capabilities stay unavailable.
- Use Task 134 for current access-token delivery and automatic refresh without
  exposing OAuth bundle material to the adapter.
- Add deterministic transport, conformance, failure/idempotency and runtime
  readiness evidence for the admitted capability subset.
- Update the generated runtime catalog and user documentation truthfully.

## Repository completion evidence

- `builtin-runtime-support-v1` admits only `social.post.text` with an exact
  16,384-rune limit and a strict non-secret `{ "group_id": 12345 }` config
  template;
- the built-in registry composes VK health and text-publisher routes through
  the common pinned HTTPS transport and Task-134 secret callback;
- the transport validates VK API method names, parameters, upload authorities,
  multipart bounds and never places the OAuth token in query parameters;
- the `/integrations` and `/social` frontend flows expose VK setup, health and
  text publication, while the catalog marks non-admitted SDK capabilities as
  unavailable;
- generated connector catalogs and VK/runtime documentation were regenerated.

## Acceptance criteria

- VK moves from `planned` only for the explicitly admitted `social.post.text`
  slice; every other manifest capability remains fail-closed and is not
  presented as an available application operation.
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

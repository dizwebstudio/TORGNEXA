# Task 147 — Ozon Pay and Ozon Доставка runtime surfaces

Status: Repository implementation complete

## Problem

Ozon Pay is the prerequisite payment/delivery service for many internet shops,
but the catalog exposed only the Ozon marketplace product reader. Operators
could not create a distinct Ozon Pay or Ozon Доставка account, check its Seller
API access, or see the qualification boundary in the UI.

## Scope

- add `ozon-pay` on the separate finance surface and `ozon-delivery` on the
  separate Delivery surface;
- keep `client_id`/`api_key` encrypted in SecretProvider and verify them via
  bounded host-mediated Seller API probes;
- render both cards and truthful setup guidance in Settings → Integrations;
- register manifests, runtime support, SDK adapters, conformance evidence and
  architecture review.

## Explicit exclusions

Ozon Pay payment creation/status/refund/webhooks and Ozon Delivery rates,
shipment creation/cancellation, labels, tracking and pickup points are not
admitted by this task. They require current merchant/delivery API contracts,
idempotency and non-production qualification. A healthy Seller API probe must
not be interpreted as activation of those services.

## Acceptance criteria

- Ozon Pay and Ozon Доставка are visible as separate integration cards;
- account creation, encrypted credential enrollment and health checks use the
  existing tenant-scoped connector-account control plane;
- no payment or shipment operation can be enabled by the runtime support
  contract;
- manifests, docs, architecture policy/reviews and generated catalogs agree;
- Go, frontend, contract, generator and connector transport tests pass.

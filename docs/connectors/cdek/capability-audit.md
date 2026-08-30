# Capability audit

Only capabilities demonstrated by the current official interface and Connector SDK v1 are admitted. CDEK OAuth client credentials are checked against the token endpoint and a bounded city-directory read; the access token is discarded after the probe. The host transport and application runtime expose bounded read-only delivery-point, rate-preview and tracking routes when `pickup.points.read`, `logistics.rates.read` or `logistics.track.read` is explicitly enabled. Rate requests accept up to 50 parcels and normalize at most 100 tariff results using fixed-decimal money parsing. Tracking accepts one remote reference and normalizes at most 100 status records, selecting the latest valid timestamp. The application returns neutral option identifiers and tracking fields; remote tariff/PVZ identifiers remain provider-local. Shipment cancellation and creation are admitted only through tenant-scoped durable host routes: a matching approved request, account capability, operation receipt, optimistic version, audit and transactional outbox are required before the worker calls the adapter. Cancellation resolves a shipment number to a UUID and performs one DELETE by UUID; creation maps the canonical `cdek_tariff_<code>` service code to the official order payload and sends one `POST /v2/orders`. Creation requests are retained only as encrypted SecretProvider material, never in the event payload. A timeout or ambiguous remote outcome is not blindly retried: the local shipment becomes `unknown` and must be reconciled from tracking. Labels and returns remain closed until current fixtures, canonical mapping and non-production qualification are retained. No browser-cookie automation, private editor endpoints, raw card credentials, or provider-specific Core branches are permitted.

## Shipment-creation qualification evidence

The repository qualification covers the complete host boundary. The CDEK
transport fixture verifies OAuth bearer authorization, the official order
payload (`type`, order number, tariff code, locations, contacts and packages),
absence of the client secret from the request body, normalized remote UUID and
tracking number, and rejection of an unqualified tariff or malformed response.
The API fixture verifies that an approved request and idempotency key are
required before an encrypted payload is queued. Worker fixtures verify the
approved execution path and the no-blind-retry rule: a transport error after
submission persists `unknown`, completes approval as failed and returns a
permanent outcome for reconciliation. These checks are covered by
`TestCDEKShipmentCreationUsesOfficialOrderPayload`,
`TestCDEKShipmentCreationRejectsUnqualifiedServiceCodeAndMalformedResponse`,
`TestLogisticsShipmentCreationRouteRequiresApprovalAndQueuesEncryptedPayload`,
`TestLogisticsCreateRouteExecutesApprovedCDEKShipmentOnce` and
`TestLogisticsCreateRouteMarksRemoteOutcomeUnknownWithoutRetry`.

Official documentation: https://apidoc.cdek.ru/

# Capability audit

Only capabilities demonstrated by the current official interface and Connector SDK v1 are admitted. CDEK OAuth client credentials are checked against the token endpoint and a bounded city-directory read; the access token is discarded after the probe. The host transport and application runtime expose bounded read-only delivery-point, rate-preview and tracking routes when `pickup.points.read`, `logistics.rates.read` or `logistics.track.read` is explicitly enabled. Rate requests accept up to 50 tariff results using fixed-decimal money parsing. Tracking accepts one remote reference and normalizes at most 100 status records, selecting the latest valid timestamp. The application returns neutral option identifiers and tracking fields; remote tariff/PVZ identifiers remain provider-local. Shipment cancellation and creation are admitted only through tenant-scoped durable host routes: a matching approved request, account capability, operation receipt, optimistic version, audit and transactional outbox are required before the worker calls the adapter. Cancellation resolves a shipment number to a UUID and performs one DELETE by UUID; creation maps the canonical `cdek_tariff_<code>` service code to the official order payload and sends one `POST /v2/orders`. Creation requests are retained only as encrypted SecretProvider material, never in the event payload. A timeout or ambiguous remote outcome is not blindly retried: the local shipment becomes `unknown` and must be reconciled from tracking. Label reads use the official barcode-print request after resolving the CDEK order UUID when needed; the host returns only a neutral PDF artifact reference. `logistics.webhooks.verify` accepts only CDEK `ORDER_STATUS` events and re-fetches the order through OAuth before returning normalized evidence; the public route stores only a body digest and append-only replay receipt.

The bounded return operation admits `mail_type=refusal` and
`mail_type=client_return`: refusal calls the official
`POST /v2/orders/{uuid}/refusal` endpoint without a body, while client return
calls `POST /v2/orders/{uuid}/clientReturn` with the explicitly supplied
`tariff_code`; both require a matching provider entity in the response. No
browser-cookie automation, private editor endpoints, raw card credentials, or
provider-specific Core branches are permitted.

## Shipment-creation qualification evidence

The repository qualification covers the complete host boundary. The CDEK
transport fixture verifies OAuth bearer authorization, the official order
payload (`type`, order number, tariff code, locations, contacts and packages),
the refusal and client-return endpoints, client-return tariff serialization,
absence of the client secret from request bodies, normalized remote UUID and
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

## Label qualification evidence

The label route is bounded to one shipment reference and the CDEK formats A4,
A5 and A6 (the neutral API accepts `pdf` as A4). The transport resolves a
numeric CDEK shipment number through the official order lookup, verifies the
returned UUID and submits exactly one `POST /v2/print/barcodes` request with
`order_uuid`, one copy and the selected format. The response must contain a
valid print-request UUID; the host exposes it as an opaque
`cdek:print:barcode:` artifact reference and never returns provider URLs or
credentials. The API rejects disabled capabilities and malformed provider
responses. These checks are covered by
`TestCDEKLabelCreatesBarcodePrintRequest` and
`TestCDEKLabelRejectsMismatchedLookupOrMalformedResponse`.

Official documentation: https://apidoc.cdek.ru/

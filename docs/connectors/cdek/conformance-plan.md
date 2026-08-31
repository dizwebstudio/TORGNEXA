# Conformance plan

Run the mandatory Task-064 13-check Connector SDK suite with synthetic credentials, tenant-isolation probes, retry/error normalization, idempotency/replay and Linux sandbox isolation. Production credentials are forbidden in the harness.

The logistics return fixture uses `mail_type=refusal`, resolves a numeric
shipment reference to one CDEK order UUID when required, calls exactly
`POST /v2/orders/{uuid}/refusal`, rejects a mismatched response entity and
never sends a request body or credentials. It also covers
`POST /v2/orders/{uuid}/clientReturn`, serializes the explicit `tariff_code`,
and rejects a client-return request without a positive tariff.

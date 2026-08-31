# 074 Logistics contract v1
- rate quote = service code + exact cost + min/max delivery timestamps;
- shipment create/cancel, label, tracking and return are additive capability ports;
- routing consumes normalized SLA/cost only; provider tariff codes stay mappings;
- remote tracking is authoritative and replay-safe.
- formed-batch order restore is a separate approval-bound operation: it returns
  an exact provider-order set to the provider backlog and rejects partial
  acknowledgements; it is not shipment cancellation or a parcel return.
- batch-order listing is a bounded read-only projection: it exposes only
  provider order/batch references, barcode, normalized status and observation
  time, and rejects mismatched or duplicate provider rows.
- single-order lookup is a bounded read-only projection over the provider
  order ID; the response must contain exactly that ID and one batch identity.
- batch-name lookup is a bounded read-only projection over one provider batch
  reference and cannot include its order rows.
- merchant-order-number search is a bounded read-only projection over the
  provider backlog; it requires exact external-ID matching, rejects duplicate
  or oversized results, and cannot include PII or raw provider payload.
- changing a formed batch hand-off date is a separate approval-bound write;
  the provider acknowledgement is normalized to the exact batch ID, ISO date,
  `UPDATED` status and `updated=true`, with tenant-scoped idempotency receipt.
- dissolving a provider Pre-Alert batch is a separate approval-bound write;
  the Dellin acknowledgement is normalized to the exact batch ID,
  `CANCELLED` status and `cancelled=true`, with a tenant-scoped idempotency
  receipt. It is not cancellation of an individual shipment or a manual
  parcel return.

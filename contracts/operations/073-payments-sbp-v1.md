# 073 Payment/SBP contract v1
- create/status/refund/reconcile/webhooks;
- exact integer minor units and ISO-shaped currency;
- no raw card data;
- idempotency required for create/refund;
- webhook verification precedes replay-dedup evidence;
- remote payment/commission status is authoritative.

# Capability audit

Task `180` admits `payments.webhooks` through the already implemented shared payment webhook receiver. The callback is verified by an mTLS status re-fetch; the request body and optional signature header are not authoritative. SBP payment create/status/refund/reconcile and verified webhook baseline do not handle card data.

Repository runtime support is complete. Live acquiring-bank qualification, including the bank's callback delivery and current production contract, remains an external release gate.

No undocumented browser/cookie/private endpoint path is permitted. Official NSPK SBP API: https://sbp.nspk.ru/api/

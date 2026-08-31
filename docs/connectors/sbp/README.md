# sbp

Task 073 reference connector. SBP payment create/status/refund/reconcile and verified webhook baseline without card data. The webhook is admitted through the shared public receiver and independently re-checks the payment status over the account's mTLS channel.

Runtime support is code-complete; a live acquiring-bank qualification is still required before production claims are made.

Official NSPK SBP API: https://sbp.nspk.ru/api/

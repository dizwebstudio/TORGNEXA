# sbp Connector Spec

Family: `payment`. SBP payment create/status/refund/reconcile and verified webhook baseline without card data.

Production networking is host-injected; provider code has no direct SQL/Core authority. The webhook body is not trusted: the host transport re-fetches the order through the mTLS-authenticated status API and the shared public receiver applies only the verified status. Official NSPK SBP API: https://sbp.nspk.ru/api/

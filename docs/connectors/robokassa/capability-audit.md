# Capability audit

`payments.refund` is admitted against Robokassa's official merchant refund
contract. The runtime never treats the local invoice number as the refund
identifier: it reads `Info.OpKey` from the authenticated OpStateExt response,
requires a successfully paid operation, signs the refund JWT with the
callback-scoped Password3 and accepts only a non-empty provider `requestId`.
The full/partial distinction is exact and uses integer minor units. A missing
Password3, missing OpKey, non-successful payment, malformed response or
non-2xx response fails closed.

The account secret format is `login\npassword1\npassword2\npassword3` for the
full payment/refund surface. Three-line secrets remain compatible with the
non-refund operations. The ResultURL webhook remains admitted: its MD5
signature is checked, state is re-fetched through OpStateExt, deliveries are
deduplicated, and the response carries the provider-required `OK` plus invoice
ID. No browser-cookie automation, private editor endpoints, raw card
credentials or provider-specific Core branches are permitted.

Official documentation: https://docs.robokassa.ru/

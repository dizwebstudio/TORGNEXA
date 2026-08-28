# Capability audit

Only capabilities demonstrated by the current official interface and Connector SDK v1 are admitted. `payments.refund` is declared in the manifest for interface completeness but is not in `operational_capabilities`: Robokassa has no merchant-level refund API, and the transport returns a normalized `unsupported` remote error without any network call, never a faked success. POST operations carry a caller-supplied Idempotence-Key equivalent (Robokassa's deterministic InvId). No browser-cookie automation, private editor endpoints, raw card credentials, or provider-specific Core branches are permitted.

Official documentation: https://docs.robokassa.ru/

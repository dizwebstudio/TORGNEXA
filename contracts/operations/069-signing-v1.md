# 069 Signing contract v1
- private keys never cross generic API/event/plugin boundaries;
- request = artifact ref + digest + certificate ref + MЧД ref + approval + idempotency;
- result = opaque signature ref + algorithm/certificate/authority metadata;
- evidence is append-only and tenant-scoped.

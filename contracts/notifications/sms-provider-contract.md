# SMS Provider Contract

Required port methods: `Send`, `GetStatus`, `Health`. Optional webhook/delivery-report registration and sender metadata. Phone numbers are PII; providers receive only tenant-authorized destination/body. Provider secrets use SecretProvider. Delivery callbacks are idempotent and map to normalized notification status. Marketing and transactional policy are separate.

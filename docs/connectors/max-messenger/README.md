# MAX Connector

Task 042 adds the MAX Bot API adapter on top of Task-020 Social Core.

The admitted baseline publishes text, images/galleries, one video and HTTPS URL buttons to one configured MAX channel, provides read-after-publish status, receives production Webhook updates through a verified and host-deduplicated boundary, and supports approval-bound edit/delete of one already published message. Provider scheduling, comments, analytics, callback actions and Long Polling are outside this admission.

Task 020 remains owner of Content, immutable ContentVariant, Publication state, scheduling, audit and outbox evidence. Task 009 remains the intended durable Inbox/dedup owner for webhook delivery claims. The provider owns only MAX protocol adaptation and bounded remote identifiers.

Task 175 composes the production application subset for text, released
image/gallery, video and HTTPS URL button publication through the existing
Social API, leased worker and append-only remote receipts. Task 183 also
composes inbound MAX Webhook reception through the public social webhook
route, tenant-scoped Inbox and transactional outbox. Approval-bound edits and
deletes use the exact configured channel and provider message ID; text, released
media and HTTPS buttons are revalidated before an edit, while deletion accepts
only an explicit provider success. Task 201 also connects subscription
lifecycle management to the authenticated host route with tenant-scoped
idempotency and audit evidence.

# MAX Connector

Task 042 adds the MAX Bot API adapter on top of Task-020 Social Core.

The admitted baseline publishes text, images/galleries, one video and HTTPS URL buttons to one configured MAX channel, provides read-after-publish status, and receives production Webhook updates through a verified and host-deduplicated boundary. Provider scheduling, edit/delete, comments, analytics, callback actions and Long Polling are outside this admission.

Task 020 remains owner of Content, immutable ContentVariant, Publication state, scheduling, audit and outbox evidence. Task 009 remains the intended durable Inbox/dedup owner for webhook delivery claims. The provider owns only MAX protocol adaptation and bounded remote identifiers.

Task 133 composes the deliberately smaller production application subset:
`social.post.text` only, up to 4000 Unicode code points, through the existing
Social API, leased worker and append-only remote receipts. Media, buttons,
status reads and webhook methods remain implemented SDK capabilities but are
not presented as end-to-end application functionality yet.

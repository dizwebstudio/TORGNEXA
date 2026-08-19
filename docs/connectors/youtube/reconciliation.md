# YouTube Reconciliation

Task 048 has two asynchronous reconciliation boundaries.

## Upload session reconciliation

A resumable session is transport-owned and provider-visible only through an opaque `SessionID`.

After an interrupted/ambiguous chunk upload, TORGNEXA asks the transport to perform the official empty status `PUT` (`Content-Range: bytes */TOTAL`). The returned `Range` is converted to `NextOffset` and must be:

- within `0..TOTAL`;
- monotonic relative to the current chunk;
- aligned to 256 KiB when more bytes remain;
- never overlapping or skipping bytes.

Only the confirmed suffix is resent. If the status probe itself cannot establish a safe offset, the operation fails with `upload_outcome_unknown` rather than creating a second video upload.

## Video processing reconciliation

After the final upload response returns a video ID, Core stores the provider result `youtube:<channel>:<video>` and polls `ReadSocialPublicationStatus` as required by Task-020.

The provider reads owner-visible `status` and `processingDetails` using `videos.list`. Failed/rejected processing remains terminal evidence; it is not retried as a new publication automatically.

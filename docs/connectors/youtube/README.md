# YouTube Connector

Task `048` implements the YouTube Data API v3 Social Core adapter.

Repository admission is deliberately narrow:

- `social.post.video` through the documented resumable `videos.insert` upload protocol;
- `social.comments.read` through bounded `commentThreads.list` reads.

Task-020 remains authoritative for scheduling. Native `status.publishAt`, comment writes and analytics are not admitted by this change because the frozen provider-neutral SDK does not currently carry the additional semantics needed to do those operations without ambiguity or metric invention.

See `spec.md`, `capability-audit.md`, `reconciliation.md`, and `conformance-plan.md`.

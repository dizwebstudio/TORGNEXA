# Odnoklassniki (OK) Connector

Task `045` registers provider `odnoklassniki` as a Task-020 Social Core adapter.

The provider publishes group media topics through the official OK REST API, uploads released Task-088 photos/videos through provider-issued upload URLs, reads publication existence, and projects bounded group-topic statistics. It does not add OK-specific branches to Core.

## Admitted capabilities

- `social.post.text`
- `social.post.media`
- `social.post.video`
- `social.analytics.read`

Not admitted in Task 045: user-wall/note publication, comments, edit/delete, scheduled provider-native posting, polls/music, webhooks, group administration, or advertising surfaces.

See `spec.md`, `capability-audit.md`, `reconciliation.md`, and the Task-064 conformance evidence in `conformance-report.json`.

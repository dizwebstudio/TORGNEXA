# OK Conformance Plan

Task `045` must pass the unchanged Task-064 Connector SDK v1 admission suite.

Provider-specific deterministic evidence additionally covers:

1. manifest JSON parity and exact `GroupID` health binding;
2. OAuth access token + application-secret isolation and request-signature presence;
3. text `GROUP_THEME` publication and exact remote-ID status reconciliation;
4. released image upload-ticket -> multipart -> token -> topic flow;
5. released MP4 upload-ticket -> multipart -> `video.update` -> topic flow;
6. bounded `group.getStatTopic` analytics projection;
7. non-retryable `write_outcome_unknown` for ambiguous publication writes;
8. upload URL authority validation and provider-error normalization.

Canonical machine evidence is `conformance-report.json` and must validate through `conformance.Require`.

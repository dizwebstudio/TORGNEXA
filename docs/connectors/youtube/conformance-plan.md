# YouTube Conformance Plan

Task 048 must satisfy the Task-064 thirteen-check Connector SDK admission suite and provider-specific deterministic tests for:

1. manifest JSON/runtime parity;
2. exact `channels.list?mine=true` channel binding;
3. Task-088 released video validation;
4. deterministic YouTube title/description/category/privacy mapping;
5. 256 KiB-aligned chunking with a bounded shorter final chunk;
6. successful multi-chunk resumable completion;
7. ambiguous chunk -> status probe -> exact-offset resume;
8. status probe confirming an already accepted full chunk;
9. upload-limit/quota normalization;
10. ambiguous session-start fail-closed behavior;
11. processing/published/rejected status projection and foreign-channel rejection;
12. bounded `commentThreads.list` mapping with opaque cursor preservation;
13. YouTube title/description contract rejection (`<`, `>`, 5000-byte description bound).

Production qualification additionally requires a non-production Google Cloud project with YouTube Data API enabled, verified OAuth client/scopes, an owned test channel and evidence of the API project's YouTube compliance/audit state. Synthetic fixtures do not claim live quota or compliance qualification.

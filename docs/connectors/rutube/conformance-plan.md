# RUTUBE Conformance Plan

Task 046 must satisfy the Task-064 thirteen-check Connector SDK admission suite and provider-specific tests for:

1. manifest JSON/runtime parity;
2. exact channel health binding;
3. released MP4 validation;
4. create -> upload -> commit ordering;
5. canonical Publication ID propagation as external ID;
6. processing -> published status projection;
7. foreign-channel rejection;
8. quota normalization with bounded Retry-After;
9. rate-limit normalization;
10. ambiguous upload fail-closed behavior;
11. ambiguous commit fail-closed behavior;
12. failed moderation/status reason safety;
13. invalid upload-session capacity/expiry rejection.

Production qualification additionally requires a non-production RUTUBE partner/channel account and the official current partner contract used by the host `PartnerTransport`. Synthetic fixtures do not claim live API qualification.

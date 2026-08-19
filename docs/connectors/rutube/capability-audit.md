# RUTUBE Capability Audit — Task 046

Audit date: **2026-08-12**.

## Current evidence

Official RUTUBE documentation available publicly on the audit date describes player/embed integration and the public play-options endpoint:

- https://rutube.ru/info/embed/

RUTUBE's historical official PHP API client documents video upload, callback/errback conversion status and links to the former developer portal, but the repository now explicitly states that it is no longer maintained:

- https://github.com/rutube/php-api-client
- https://rutube.github.io/php-api-client/class-Rutube.Video.html

The public developer portal referenced by that client is no longer a reliable current contract. Contemporary third-party automation products state that official RUTUBE API integrations exist, indicating an account/partner integration path, but those claims are not used as endpoint-level authority.

## Admission decision

Task 046 admits only the provider-neutral `social.post.video` capability and only through a typed `PartnerTransport` whose production implementation must be configured from a current official RUTUBE partner/account contract. The connector itself contains no guessed path, cookie, CSRF token, browser automation, DOM selector, private Studio endpoint or reverse-engineered RPC.

The typed boundary requires these operations:

1. exact channel resolution;
2. create upload session with canonical publication external ID and bounded metadata;
3. stream one current Task-088 released MP4 into that session;
4. commit/finalize the session;
5. read the resulting video processing/publication status.

The transport reports only bounded typed failures (`unauthorized`, `forbidden`, `rate_limited`, `quota_exceeded`, `rejected`, `unavailable`, `unknown_write`, etc.). Raw partner responses and credentials do not cross the provider boundary.

## Explicitly not admitted

- Studio browser/session-cookie automation;
- undocumented HTTP endpoints;
- comments read/reply;
- analytics;
- edit/delete;
- provider-native scheduling;
- live-stream creation;
- monetization/advertising administration.

TORGNEXA's Task-020 scheduler remains authoritative for scheduled publication. Additional RUTUBE capabilities require a new audit against a current official contract.

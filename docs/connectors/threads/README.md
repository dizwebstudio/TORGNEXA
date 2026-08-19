# Threads Connector

Task 044 adds Threads publishing and token lifecycle support on top of Task-020 Social Core without changing Core or the Connector/Runtime SDK-v1 roots.

The admitted baseline publishes text, images/image carousels and one MP4 video through the official Threads API. Media is revalidated through Task-088 and exposed to Meta only via a host-owned short-lived HTTPS `MediaStager`.

The provider also implements explicit short-lived-to-long-lived token exchange and long-lived token refresh through a host-owned `TokenSink`; plaintext replacement tokens are never returned as application results.

# VK Connector

Task 040 adds the first Social Core provider adapter. The admitted baseline publishes canonical TORGNEXA text/image publications to a configured VK community wall, reads/replies to comments, and reads post-reach analytics.

The connector deliberately does **not** declare video, edit, or delete capabilities. Provider-specific scheduling is also forbidden: Task 020 remains the only scheduler and calls this adapter only when a canonical Publication is READY.

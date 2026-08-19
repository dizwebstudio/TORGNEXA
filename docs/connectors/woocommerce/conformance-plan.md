# WooCommerce conformance plan

Run Task-064's 13 mandatory checks using only synthetic Consumer Key/Secret/webhook secret material and the Linux connector sandbox emulator. The candidate proves SDK/manifest validity, callback-scoped auth, normalized health/errors, bounded retry, idempotent-write semantics, webhook replay suppression, tenant isolation, dry-run/no-side-effect admission, production-credential rejection, egress/resource enforcement and sandbox isolation.

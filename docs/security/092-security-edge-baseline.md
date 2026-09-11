# Security Edge Baseline

Vendor-neutral edge policy validates trusted proxies, HSTS/security headers, request/upload limits, CORS/CSRF, admin allowlists and security signal export without replacing application authentication. Production API rate limits are atomic in a shared Valkey namespace and split into pre-auth IP, authenticated tenant/principal and public-webhook budgets; local memory is limited to explicit development/single-process use. Valkey failure is fail-closed (`503`, `Retry-After: 1`) and confirmed exhaustion returns `429` with the counter TTL.

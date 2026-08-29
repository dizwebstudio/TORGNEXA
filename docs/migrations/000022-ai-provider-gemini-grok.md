# Migration 000022 — Google Gemini and Grok

This additive expand migration widens the tenant-scoped AI provider allow-list
for Google Gemini and xAI Grok. Existing accounts, credentials and provider
identity remain unchanged. Older binaries can continue reading the schema;
the new providers become usable after the binary and migration are deployed.

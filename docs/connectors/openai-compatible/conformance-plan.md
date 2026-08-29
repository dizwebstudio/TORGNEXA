# OpenAI-compatible conformance plan

The canonical Task-064 suite passes all thirteen admission checks using a synthetic fixture transport (`connectors/ai/openai-compatible/conformance.go`) that never performs real network I/O. `docs/connectors/openai-compatible/conformance-report.json` is the retained machine-readable evidence from an actual `internal/platform/connectors/conformance.Run()` execution, including the Linux-namespace `sandbox_isolation` probe (requires a privileged Linux runner; verified locally with `docker run --privileged`, matching `scripts/check-connector-sandbox-linux.sh`'s own `unshare` requirement).

Package-level tests (`connector_test.go`) additionally cover: request/response marshaling against the real Chat Completions shape, the default-host fallback, and manifest/account mismatch rejection.

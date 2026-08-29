# GigaChat (Sber) conformance plan

The canonical Task-064 suite passes all thirteen admission checks using a synthetic fixture transport (`connectors/ai/gigachat/conformance.go`) that answers both the `/api/v2/oauth` and `/api/v1/chat/completions` paths without real network I/O. `docs/connectors/gigachat/conformance-report.json` is the retained machine-readable evidence from an actual `internal/platform/connectors/conformance.Run()` execution, including the Linux-namespace `sandbox_isolation` probe (requires a privileged Linux runner; verified locally with `docker run --privileged`).

Package-level tests (`connector_test.go`) additionally cover the two-request OAuth-then-completion sequence and header shapes for both stages.

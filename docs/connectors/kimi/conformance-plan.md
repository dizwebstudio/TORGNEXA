# Kimi (Moonshot AI) conformance plan

The canonical Task-064 suite passes all thirteen admission checks using a synthetic fixture transport (`connectors/ai/kimi/conformance.go`) that never performs real network I/O. `docs/connectors/kimi/conformance-report.json` is the retained machine-readable evidence from an actual `internal/platform/connectors/conformance.Run()` execution, including the Linux-namespace `sandbox_isolation` probe (requires a privileged Linux runner; verified locally with `docker run --privileged`).

Package-level tests (`connector_test.go`) additionally cover the default-Moonshot-host fallback and header shape.

# YandexGPT conformance plan

The canonical Task-064 suite passes all thirteen admission checks using a synthetic fixture transport (`connectors/ai/yandexgpt/conformance.go`) that never performs real network I/O; the sandbox fixture's generic `Health` probe exercises the boundary-only `Health` method (no FolderID), consistent with the frozen `sdk.Connector` interface. `docs/connectors/yandexgpt/conformance-report.json` is the retained machine-readable evidence from an actual `internal/platform/connectors/conformance.Run()` execution, including the Linux-namespace `sandbox_isolation` probe (requires a privileged Linux runner; verified locally with `docker run --privileged`).

Package-level tests (`connector_test.go`) additionally cover the missing-FolderID rejection and the `gpt://<folder_id>/<model>` URI construction.

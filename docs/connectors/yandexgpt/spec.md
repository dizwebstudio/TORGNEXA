# YandexGPT connector specification

## Capability

- provider id: `yandexgpt`
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: `api_key` (`Api-Key` header), required
- host: `llm.api.cloud.yandex.net`

## Transport contract

Unlike the other three AI connectors, model selection is a folder-scoped URI (`gpt://<folder_id>/<model>`) rather than a bare model name, so a tenant-configured `FolderID` is mandatory for a live completion. The frozen `sdk.Connector.Health(ctx, account, runtime)` boundary carries no such tenant configuration, so `Health` validates the account/manifest binding only and reports `HealthHealthy` without an outbound call; the live probe (`HealthCheckWithFolder`) is reachable only through `internal/platform/builtinruntime`, which already resolves the account's configured FolderID. `Complete` performs one host-mediated `POST /foundationModels/v1/completion` through `Transport.Do(ctx, Request)`; this package never imports `net/*`.

## Security and privacy

The credential is read only inside `runtime.Secrets().UseSecret(...)` and is never logged. FolderID is non-secret tenant configuration, not credential material.

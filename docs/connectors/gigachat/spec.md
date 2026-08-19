# GigaChat (Sber) connector specification

## Capability

- provider id: `gigachat`
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: `basic` (a base64 "client id:client secret" Authorization key), required
- OAuth host: `ngw.devices.sberbank.ru`
- completion host: `gigachat.devices.sberbank.ru`

## Transport contract

`Complete` performs two host-mediated calls through the same `Transport.Do(ctx, Request)`: first `POST /api/v2/oauth` (form-encoded, `scope=GIGACHAT_API_PERS`) exchanging the stored Authorization key for a short-lived bearer token, then `POST /api/v1/chat/completions` with that token. Both calls go through the host-owned DNS-pinned transport; this package never imports `net/*`.

GigaChat's public endpoints are signed by the Russian national root CA ("Russian Trusted Root CA" / Minsvyaz), which most default trust stores do not carry. Deployments that need GigaChat reachability must add that CA to the outbound TLS trust store used by the host transport; this connector never disables certificate verification to work around a missing CA.

## Security and privacy

The credential is read only inside `runtime.Secrets().UseSecret(...)` and is never logged. The exchanged OAuth access token lives only for the duration of one `Complete` call and is never persisted.

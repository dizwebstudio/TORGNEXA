# CBR FX connector specification

## Capability

- provider id: `cbr-fx`
- family: `fx`
- SDK: v1
- capability: `fx.rates.read`
- authentication: none
- source id emitted to host: `cbr`
- rate type: `official`

## Transport contract

The provider receives a host-injected typed `Transport.Daily(ctx, asOf)`. Production binding must use the official Bank of Russia daily XML endpoint documented as `/scripts/XML_daily.asp?date_req=dd/mm/yyyy`. The date is supplied explicitly; there is no provider-side concept of an implicit current rate.

The XML `ValCurs/@Date` becomes the fact `effective_at`. `Value / Nominal` is computed as exact decimal text. Only decimal-power nominals are accepted by this reference adapter; an unexpected remote shape fails closed instead of approximating.

## Pair semantics

The initial reference adapter admits exact foreign currency -> RUB pairs such as `USD/RUB`. `RUB/USD` is not implicitly inverted. Cross rates that need a second source/fact remain unavailable until an explicit fact route exists.

## Security and privacy

There is no credential. Provider code imports only `internal/platform/connectors`, receives no SQL/filesystem/process authority, and emits bounded source references such as `daily/2026-08-12/R01235` without query strings.

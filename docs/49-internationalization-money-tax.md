# Internationalization, Money & Tax Abstractions

The initial market is Russia, but core primitives must not assume one locale, currency, timezone, address grammar or tax regime. Task `076a` established the shared representation; Task `076b` audited the implemented Commerce Core and closed the parent Task `076`.

## Money and currency

- `Money` is **integer minor units + a three-letter uppercase currency code**. Binary floating-point money is forbidden in domain models, API/event contracts, persistence, calculations and tests.
- Currency syntax is validated centrally. The core deliberately does not embed a static "currently active currencies" registry because that data changes; `CurrencyMetadataProvider` is the authority boundary for policy/registry validation and minor-unit scale.
- Arithmetic is currency-safe and overflow-checked. Cross-currency arithmetic fails; conversion belongs to the FX capability and must use an explicit immutable FX fact.
- Wire representation is `{ "minor_units": <int64>, "currency": "RUB" }`. A major-unit JSON number such as `123.45` is not a Money representation.

## Decimal quantities

- `Decimal` is signed fixed point backed by an integer coefficient and an explicit scale, currently capped at nine fractional digits.
- JSON representation is a decimal **string**, never a JSON number. This avoids parser/runtime float conversion and preserves exact values across Go, JavaScript, databases and connectors.
- `Quantity` couples a Decimal with an explicit provider-neutral `UnitCode`; provider-specific unit aliases/mappings stay in adapters.
- Shared arithmetic aligns scales exactly and checks overflow. Rounding is never implicit; any future operation that can lose precision must require an explicit rounding policy.

## UTC and timezone edges

- Persisted and event timestamps are `UTCInstant` and serialize in canonical RFC3339/RFC3339Nano UTC form ending in `Z`.
- A non-zero offset timestamp is not accepted as a persisted `UTCInstant`; callers normalize explicitly before persistence.
- `TimeZone` is `UTC` or a named IANA area/location. Machine-local zones and naked fixed offsets are not timezone identities.
- The Go runtime embeds `time/tzdata`, so scheduling behavior is tied to the released binary/toolchain rather than an arbitrary host zoneinfo package.
- Local wall-clock values exist only at scheduling/presentation boundaries. `ResolveLocalTime` detects DST gaps and folds. A nonexistent wall time fails; an ambiguous wall time fails unless the caller explicitly selects the earlier or later instant.

## Locale and presentation

- Domain values remain language-neutral. `Locale` is the canonical `language[-Script][-REGION]` subset used at presentation boundaries (`ru-RU`, `en-US`, `zh-Hans-CN`, `es-419`).
- Localized money, quantity and instant formatting is behind `LocalizationPort`.
- Business logic must never parse localized display strings back into domain values.

## Address

- `Address` stores provider-neutral country/region/city/postal-code/line components and makes no Russian-address ordering assumptions.
- Country syntax is an uppercase two-letter code. Provider/country-specific validation and presentation live in adapters.
- `AddressFormatter` is the formatting port. The canonical Address contract is data, not a printable mailing label.

## Tax

- Tax/VAT treatment is explicit metadata: jurisdiction, category, optional decimal `rate_fraction`, tax-included flag and optional reason code.
- `rate_fraction` is an exact decimal string where `0.2` means 20%; it is never inferred from strings such as product labels or provider descriptions.
- Standard/reduced/zero categories carry an explicit rate. Exempt/out-of-scope/reverse-charge treatments do not carry a rate; zero requires exactly `0`.
- Country-specific tax decisions belong behind `TaxProvider`. The generic core contains no Russian VAT engine, EU OSS rules, US sales-tax rules or marketplace-specific tax inference.

## Canonical contracts

Task `076a` publishes Draft 2020-12 contracts under `contracts/domain/` for Money, Quantity, UTC instant, locale/timezone edge context, Address and TaxTreatment. Because the frozen architecture forbids Core→Platform imports, Pricing/Inventory/Orders retain self-contained value objects that mirror these exact representations; PostgreSQL/EventBus adapters convert them to the shared wire primitives. Task `076b` adds executable parity audits that prevent float storage/wire drift and local-time persistence. Contract fixtures include negative float/non-UTC/non-canonical cases.

## Commerce-core audit result

Task `076b` audited Tasks `004`, `005` and `006` and closes parent Task `076` at repository level:

- Catalog keeps presentation text Unicode/locale-neutral and rejects persisted non-UTC timestamps; Product/Offer Draft 2020-12 contracts now require canonical `Z` timestamps.
- Pricing keeps its frozen-Core Money mirror (`int64` minor units + currency), maps it to the shared Task-076 wire Money at the adapter boundary, and has regression coverage for RUB/EUR/JPY/BHD plus UTC-only persistence without embedding currency-specific decimal assumptions.
- Inventory keeps its frozen-Core exact Decimal/Quantity/UnitCode mirror; persistence remains coefficient+scale+unit with no binary float and regression covers EA/KG/L exact quantities.
- Orders keeps exact Money/Decimal/Quantity mirrors compatible with Task-076 wire primitives, keeps explicit jurisdiction/rate/tax-included facts, and is tested with non-Russian currency/jurisdiction plus IANA-local-time normalization to UTC.
- A repository audit test scans Commerce Core source, migrations and public/event schemas for float types, naive timestamps, primitive-shape drift and non-canonical wire values; provider-specific Core leakage remains enforced by the frozen architecture checker.
- No provider implementation exists yet; provider-specific unit/currency/address/tax translation remains mandatory at future Connector SDK adapters and is additionally protected by the architecture provider gate.

Russian compliance remains a dedicated module, not embedded into generic money/order types. Exchange rates remain provider facts with source/time and are never silently applied. Task `089a` implements the immutable FX fact/provider/policy boundary described in `docs/63-fx-exchange-rate-provider.md`; cross-currency conversion remains disabled until `089b`.

# Task 076: Internationalization/money/tax hardening

## Status
Repository implementation complete — stages `076a` and `076b` are complete. Canonical Go 1.26.5 / PostgreSQL runtime qualification remains a CI/staging obligation.

## Objective
Audit core for locale/currency/timezone/tax assumptions and introduce provider-neutral abstractions.

## Dependencies
004, 005, 006 for final Task `076` closure. Stage `076a` is intentionally executed before those tasks; stage `076b` closes the parent after commerce-core implementation.

## Stage 076a — primitive foundation

- [x] `Money` uses signed `int64` minor units plus canonical three-letter Currency; binary floating-point money is absent from the primitive API/wire contract.
- [x] Money add/subtract is currency-safe and overflow-checked; cross-currency arithmetic fails.
- [x] `CurrencyMetadataProvider` separates authoritative currency/minor-unit metadata from the shared syntax primitive.
- [x] `Decimal` is exact fixed point with maximum scale 9, normalized canonical string encoding, overflow-checked add/subtract/compare, and no JSON-number input.
- [x] `Quantity` combines Decimal with an explicit canonical provider-neutral unit code.
- [x] `UTCInstant` permits persistence only at offset `00:00` and emits canonical `Z`; localized time is not a persisted domain primitive.
- [x] `TimeZone` uses `UTC` or named IANA zones with embedded Go tzdata; machine-local/fixed-offset timezone identities are rejected.
- [x] `LocalDateTime` plus `ResolveLocalTime` rejects DST gaps and requires an explicit earlier/later policy for DST folds.
- [x] Tests cover Europe/Amsterdam 2026 DST gap/fold behavior and ordinary UTC conversion.
- [x] `Locale` validates a canonical provider-neutral `language[-Script][-REGION]` subset and remains presentation-only.
- [x] `Address` is country-neutral data; formatting is behind `AddressFormatter`.
- [x] Locale-sensitive money/quantity/instant rendering is behind `LocalizationPort`.
- [x] `TaxTreatment` carries explicit jurisdiction/category/rate-fraction/included/reason metadata; tax is never inferred from arbitrary strings.
- [x] Country/provider-specific tax decisions are behind `TaxProvider` and are not embedded in generic core types.
- [x] Draft 2020-12 contracts and positive/negative fixtures exist for Money, Quantity, UTCInstant, Locale/Timezone, Address and TaxTreatment.
- [x] ADR `0022` remains the governing decision and the architecture review records the implementation inside the existing shared-types pillar.
- [ ] Canonical Go 1.26.5 CI must repeat root test/vet/build and semantic contract checks; this sandbox cannot download the required toolchain/dependencies.

## Stage 076b — commerce-core audit

- [x] Audit Tasks `004–006` source for float-based money/quantity and replace violations with the stage `076a` primitives.
- [x] Audit catalog/price/inventory/order migrations for exact numeric/minor-unit storage and UTC-only timestamps.
- [x] Audit public/event schemas for Money/Quantity canonical wire shapes and UTC `Z` timestamps.
- [x] Add cross-locale/currency/timezone/tax regression tests to actual Catalog, Price/Inventory and Orders behavior.
- [x] Verify provider adapters map provider-local currency/unit/address/tax representations at the boundary rather than leaking them into the core.
- [x] Close parent Task `076` only after these checks pass.

## Acceptance
No float money; UTC persistence; locale/tax tests.

## Repository status
Parent Task `076` repository implementation complete. Stage `076b` audits the frozen-Core mirrors in Pricing/Inventory/Orders against the shared Task-076 primitives, hardens Catalog UTC wire contracts, and adds executable source/migration/schema plus cross-locale/currency/timezone/tax regression coverage. No provider implementation exists yet; the architecture/provider gate continues to enforce that future provider-local representations are translated at connector boundaries. Exact Go 1.26.5 and live PostgreSQL qualification must repeat in CI/staging.

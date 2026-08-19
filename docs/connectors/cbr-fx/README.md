# Bank of Russia FX connector

`cbr-fx` is the Task-089b reference FX source adapter. It implements the additive Connector SDK v1 `FXRateReader` capability and has no direct dependency on TORGNEXA finance, Core, SQL or network packages.

The host transport binds `Daily(ctx, asOf)` to the Bank of Russia official daily XML service. The documented request form is `XML_daily.asp?date_req=dd/mm/yyyy`; the provider parses the returned `ValCurs/@Date`, `CharCode`, `Nominal` and `Value`, converts nominal quotations exactly, and returns a provider-neutral observation. The host-side `fx.ConnectorProvider` then creates the immutable `RateFact`.

Only explicitly published foreign-currency/RUB official quotations are admitted. The adapter does not invent an inverse `RUB/XXX` fact, triangulate, or fall back to a stale observation.

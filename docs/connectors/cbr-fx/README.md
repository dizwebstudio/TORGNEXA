# Bank of Russia FX connector

`cbr-fx` is the Task-089b reference FX source adapter. It implements the additive Connector SDK v1 `FXRateReader` capability and has no direct dependency on TORGNEXA finance, Core, SQL or network packages.

The host transport binds `Daily(ctx, asOf)` to the Bank of Russia official daily XML service. The documented request form is `XML_daily.asp?date_req=dd/mm/yyyy`; the provider parses the returned `ValCurs/@Date`, `CharCode`, `Nominal` and `Value`, converts nominal quotations exactly, and returns a provider-neutral observation. The host-side `fx.ConnectorProvider` then creates the immutable `RateFact`.

Only explicitly published foreign-currency/RUB official quotations are admitted. The adapter does not invent an inverse `RUB/XXX` fact, triangulate, or fall back to a stale observation.

Task 131 supplies the production binding. The worker resolves the reviewed
daily currency set on startup and every six hours, with a 15-minute whole-table
transport cache and a 14-day host freshness ceiling. The existing authenticated
`GET /api/v1/fx/rates` endpoint and Finance page read the persisted immutable
facts. No tenant connector account or secret enrollment is involved.

The production batch admits 53 pairs exactly. IRR/RUB remains unsupported:
the current official nominal produces a scale-10 unit rate, exceeding the
canonical scale-9 boundary. Silent source rounding is forbidden.

# CBR FX capability audit

| Capability | Decision | Evidence boundary |
|---|---|---|
| `fx.rates.read` | admitted | Official Bank of Russia daily XML quotation, explicit requested date, immutable host-side fact |
| implicit inverse | rejected | An inverse pair is a different fact and is never synthesized silently |
| arbitrary cross-rate graph | rejected | Task 089 permits direct or one explicit pivot only; this adapter returns only the official pair it observes |
| write/mutation | rejected | Central-bank reference source is read-only |
| credentials | none | Official daily XML read requires no connector secret |

Remote response drift, missing currency, future effective dates and unsupported pair/rate type are normalized to explicit provider errors.

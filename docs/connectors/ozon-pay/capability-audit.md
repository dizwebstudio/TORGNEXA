# Ozon Pay capability audit — 2026-08-28

| SDK capability | Ozon surface | Runtime decision |
|---|---|---|
| `payments.create` | Ozon Pay merchant API | deferred; no guessed charge endpoint |
| `payments.status.read` | Ozon Pay merchant API | deferred |
| `payments.refund` | Ozon Pay merchant API | deferred |
| `payments.webhooks` | Ozon Pay webhook contract | deferred until signature rules are qualified |
| Seller API credential health | `POST /v3/product/list` | enabled as a bounded prerequisite probe |

The connector is visible and configurable in the finance surface, but the
runtime support contract has no operational payment capabilities. This keeps
the catalog truthful: a healthy Seller API key is not the same as an activated
Ozon Pay merchant account.

# Ozon Delivery capability audit — 2026-08-28

| SDK capability | Ozon surface | Runtime decision |
|---|---|---|
| `logistics.rates.read` | Delivery checkout/rates API | deferred until request schema is qualified |
| `logistics.shipment.create` | Delivery order API | deferred; no guessed shipment write |
| `logistics.shipment.cancel` | Delivery cancellation API | deferred |
| `logistics.track.read` | Delivery tracking API | deferred |
| `logistics.label.read` | Delivery label API | deferred |
| `pickup.points.read` | Delivery pickup-point API | deferred |
| Seller API warehouse health | `POST /v2/warehouse/list` | enabled as a bounded prerequisite probe |

The separate card is therefore usable for encrypted credential setup and
connection verification, while the runtime support contract exposes zero
shipment operations. A successful health check must not be interpreted as an
available delivery quote or a created parcel.

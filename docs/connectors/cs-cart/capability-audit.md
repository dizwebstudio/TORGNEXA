# CS-Cart capability audit

| Capability | Manifest | Runtime | Решение |
|---|---:|---:|---|
| `inventory.read` | да | да | admitted; one storefront location |
| `orders.read` | да | да | admitted; list + detail projection |
| `prices.read` | да | да | admitted; base product projection |
| `prices.write` | да | да | admitted; product PUT + read-after-write |
| `products.read` | да | да | admitted |
| `products.write` | да | да | admitted |
| `inventory.write` | да | да | admitted; one storefront location + read-after-write |
| order status writes | нет | нет | не заявляются |

Синхронизируются сущности `products` (inbound + outbound), `prices`,
`inventory` и `orders` (inbound). Поля SKU, название, статус и описание ограничены SDK-проекцией;
числовые идентификаторы CS-Cart сохраняются как remote IDs. Цена читается из
`price`, а `list_price` — как необязательный `compare_at`; option combinations
остаются fail-closed. Запись цены обновляет эти два поля через product PUT и
подтверждается повторным чтением. Остатки читаются и записываются через
`amount` в единой локации `cs-cart-store`; multi-warehouse semantics не
заявляются. Заказные
строки с option combinations и неизвестные статусы остаются fail-closed.

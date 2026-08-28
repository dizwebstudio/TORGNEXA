# CS-Cart capability audit

| Capability | Manifest | Runtime | Решение |
|---|---:|---:|---|
| `products.read` | да | да | admitted |
| `products.write` | да | да | admitted |
| inventory / prices / orders | нет | нет | не заявляются |

Синхронизируется только сущность `products`. Поля SKU, название, статус и
описание ограничены SDK-проекцией; числовые идентификаторы CS-Cart сохраняются
как remote IDs.

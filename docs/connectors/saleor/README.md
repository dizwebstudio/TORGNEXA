# Saleor

Коннектор для self-hosted Saleor v3 GraphQL API. App access token передаётся
как `Authorization: Bearer <token>`; API при ошибках может вернуть HTTP 200 с
верхнеуровневым GraphQL `errors`, поэтому транспорт анализирует оба уровня.

Runtime поддерживает `products.read`/`write`, `prices.read`/`write`,
`inventory.read`/`write`, `orders.read`, отмену заказа, `returns.read` и
входящие webhooks Saleor с detached RS256/JWKS-подписью. Создание продукта и
устаревший HMAC-вариант webhook с `secretKey` остаются fail-closed (см.
[capability-audit.md](capability-audit.md)).

Docker-стенд и credentialed проверки описаны в
[docker-live-qualification.md](docker-live-qualification.md), а исполняемый
smoke находится в [scripts/saleor-smoke.sh](../../../scripts/saleor-smoke.sh).
Docker qualification фиксируется в
[live-qualification-status.json](live-qualification-status.json); внешний
staging endpoint требует отдельный App token и собственные channel/warehouse.

Последняя Docker-проверка (2026-08-29) прошла полностью: SKU `111223580`,
`default-channel`/USD, склад `default`; чтение, product/price/stock-записи,
read-after-write и cleanup прошли без ошибок. Это не заменяет внешний
merchant-staging gate.

Официальные материалы: [Saleor Platform](https://github.com/saleor/saleor-platform),
[Saleor API reference](https://docs.saleor.io/api-reference/).

# Task 153 — CS-Cart storefront connector

Добавить CS-Cart как отдельный storefront-коннектор с официальным REST API,
реальными product read/write, base price, inventory и order read маршрутами, карточкой в каталоге интеграций и
документированными credential/runtime config.

Acceptance criteria:

- CS-Cart виден в «Интернет-магазины»;
- Basic Auth и API 2.0 вызовы проходят через host transport;
- каталог товаров поддерживает cursor pagination и read-after-write;
- базовые цены читаются через bounded product projection и попадают в
  inbound price reconciliation;
- остаток читается из `amount` в одной локации `cs-cart-store` и попадает в
  inbound inventory reconciliation;
- заказы читаются списком и деталями с bounded строками и попадают в inbound
  order reconciliation;
- базовые цены и остатки записываются через product PUT с проверкой валюты,
  единой storefront-локации и read-after-write в outbound reconciliation;
- стандартный статус заказа записывается через order PUT с фиксированной
  таблицей кодов и read-after-write;
- runtime support заявляет стандартную запись статуса заказа и не заявляет
  неподдержанные сущности;
- тесты, контракты, архитектурный review и документация обновлены;
- `scripts/cscart-smoke.sh` предоставляет credentialed API 2.0 Basic Auth
  smoke для настоящего non-production store, отдельно от SDK-конформанса;
- live-квалификация считается выполненной только после запуска smoke на
  лицензированном тестовом магазине с включённым administrator API access.

Текущий статус: repository-qualified (SDK 13/13); live/Docker-квалификация
заблокирована до появления такого магазина и scoped API key. См.
`docs/connectors/cs-cart/docker-live-qualification.md`.

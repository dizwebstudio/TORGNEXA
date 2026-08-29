# Task 153 — CS-Cart storefront connector

Добавить CS-Cart как отдельный storefront-коннектор с официальным REST API,
реальным product read/write маршрутом, карточкой в каталоге интеграций и
документированными credential/runtime config.

Acceptance criteria:

- CS-Cart виден в «Интернет-магазины»;
- Basic Auth и API 2.0 вызовы проходят через host transport;
- каталог товаров поддерживает cursor pagination и read-after-write;
- runtime support не заявляет неподдержанные сущности;
- тесты, контракты, архитектурный review и документация обновлены;
- `scripts/cscart-smoke.sh` предоставляет credentialed API 2.0 Basic Auth
  smoke для настоящего non-production store, отдельно от SDK-конформанса;
- live-квалификация считается выполненной только после запуска smoke на
  лицензированном тестовом магазине с включённым administrator API access.

Текущий статус: repository-qualified (SDK 13/13); live/Docker-квалификация
заблокирована до появления такого магазина и scoped API key. См.
`docs/connectors/cs-cart/docker-live-qualification.md`.

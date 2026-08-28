# Спецификация Ozon Доставка

- идентификатор: `ozon-delivery`
- семейство: `logistics`
- поверхность: отдельный Delivery-контур
- авторизация: JSON `{"client_id":"…","api_key":"…"}` из кабинета продавца Ozon
- endpoint проверки: `https://api-seller.ozon.ru/v2/warehouse/list`

Условия сервиса для интернет-магазинов описаны на [странице Ozon
Pay/Доставка](https://finance.ozon.ru/business/acquiring/internet/dostavka), а
технические Seller API — в [официальной документации Ozon](https://docs.ozon.ru/api/seller/).
TORGNEXA проверяет Seller API и доступ к складам ограниченным запросом
`POST /v2/warehouse/list`. Это не означает, что у продавца включены все
тарифы, ПВЗ или доставка сторонних заказов.

Заявленные манифестом `logistics.rates.read`, `logistics.shipment.create`,
`logistics.shipment.cancel`, `logistics.track.read`, `logistics.label.read` и
`pickup.points.read` остаются закрытыми в production runtime до фиксации
актуальных схем checkout/shipment, идемпотентности и прав доступа.

Ключи доступны только host transport внутри callback `SecretProvider` и не
попадают в нормализованные события или Core.

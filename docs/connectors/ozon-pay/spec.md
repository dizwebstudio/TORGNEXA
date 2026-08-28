# Спецификация Ozon Pay

- идентификатор: `ozon-pay`
- семейство: `payment`
- поверхность: отдельный finance-контур
- авторизация: JSON `{"client_id":"…","api_key":"…"}` из кабинета продавца Ozon
- endpoint проверки: `https://api-seller.ozon.ru/v3/product/list`

Ozon описывает оплату и доставку для интернет-магазинов в [разделе Ozon
Pay](https://finance.ozon.ru/business/acquiring/internet/dostavka), а Seller
API публикуется в [документации Ozon](https://docs.ozon.ru/api/seller/).
Проверка TORGNEXA подтверждает только доступ пары `Client-Id`/`Api-Key` к
Seller API. Она не подтверждает, что для мерчанта включён эквайринг Ozon Pay.

Заявленные манифестом `payments.create`, `payments.status.read`,
`payments.refund` и `payments.webhooks` остаются закрытыми в production runtime:
нет проверенного в этом репозитории merchant endpoint, схемы идемпотентности и
webhook-подписи. Вызов платёжных операций возвращает fail-closed
`ErrUnavailable`, пока не появится отдельное подтверждённое API-исследование.

Секрет читается только внутри callback `SecretProvider`; сырые ключи, ответы
Ozon и платёжные данные не попадают в Core, события или журнал аудита.

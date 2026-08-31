# Robokassa

Платёжный коннектор: JWT-signed CreateInvoice, XML OpStateExt для статуса,
MD5-signed ResultURL webhook и официальный merchant Refund API. Возврат
получает `OpKey` через OpStateExt, подписывает компактный JWT алгоритмом HS256
с `Password3` и возвращает асинхронный `requestId` как `accepted`. Полный
возврат не передаёт `RefundSum`; частичный передаёт точную сумму в рублях.

Секрет аккаунта хранится одной callback-scoped строкой:
`login\npassword1\npassword2\npassword3`. Четвёртая строка нужна для возвратов;
старый трёхстрочный секрет продолжает работать для оплаты, статуса, сверки и
webhook.

Официальная документация: https://docs.robokassa.ru/

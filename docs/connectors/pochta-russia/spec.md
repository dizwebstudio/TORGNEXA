# Спецификация коннектора «Почта России»

- идентификатор: `pochta-russia`
- семейство: `logistics`
- версия SDK: `1`
- авторизация для проверки: JSON `{ "token": "…", "key": "…" }`
- endpoint проверки: `https://otpravka-api.pochta.ru/1.0/settings`

Проверка передаёт токен приложения в заголовке
`Authorization: AccessToken <token>` и сгенерированный ключ пользователя в
заголовке `X-User-Authorization: Basic <key>`. Успешным считается ответ
`2xx` с корректным JSON; тело ответа не выходит из host-side transport.

Манифест перечисляет нормализуемые операции расчёта, отправлений, отмены,
возврата, этикетки, трекинга и пунктов выдачи. В runtime включены bounded
read-only `pickup.points.read` и `logistics.rates.read`: поиск ОПС выполняется
через официальный `GET /postoffice/1.0/by-address`, данные отделения — через
`GET /postoffice/1.0/{postal-code}`, а тариф — через
`GET https://tariff.pochta.ru/v2/calculate/tariff/delivery` для объекта 23030.
Тарифный запрос принимает индексы и суммарный вес, возвращает нейтральный
предпросмотр в RUB и не передаёт секреты кабинета. Отправления, возвраты,
этикетки и трекинг остаются fail-closed до qualification. Для защиты от
fan-out общий запрос принимает SDK-лимит до 500, но один вызов к Почте России
ограничен 50 ОПС.

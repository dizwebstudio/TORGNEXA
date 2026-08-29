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
возврата, этикетки, трекинга и пунктов выдачи. Они пока не являются
исполняемыми runtime-маршрутами: в runtime support коннектор зарегистрирован
как `separate_surface/logistics` с нулём operational capabilities.

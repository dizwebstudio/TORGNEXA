# Почта России

«Почта России» подключена к каталогу TORGNEXA на отдельной поверхности
«Доставка». Сейчас доступны создание кабинета, шифрованное сохранение
учётных данных, проверка доступа к официальному API «Отправка» и bounded
чтение пунктов выдачи/ОПС по городу.

Для проверки используйте JSON:

```json
{
  "token": "токен авторизации приложения",
  "key": "ключ авторизации пользователя в base64",
  "tracking_login": "логин API отслеживания",
  "tracking_password": "пароль API отслеживания"
}
```

Транспорт передаёт токен только в `Authorization: AccessToken …`, а ключ — в
`X-User-Authorization: Basic …`. Секреты остаются callback-scoped в
`SecretProvider`; ответ API и персональные данные получателя не сохраняются.

Проверка выполняет безопасный GET `/1.0/settings` на
`otpravka-api.pochta.ru`. `pickup.points.read` сначала вызывает официальный
`GET /postoffice/1.0/by-address`, затем получает полную карточку каждого
индекса через `GET /postoffice/1.0/{postal-code}`. Результат ограничен
параметром запроса и 50 пунктами за один вызов, а закрытые отделения
помечаются неактивными. `logistics.rates.read` выполняет read-only GET
`https://tariff.pochta.ru/v2/calculate/tariff/delivery` для объекта 23030,
передаёт индексы и суммарный вес и возвращает проверенную сумму с НДС в
копейках. Тарифный калькулятор не получает секреты кабинета. Оформление
отправлений, печатные формы и возвраты остаются закрытыми до квалификации
текущего write-контракта API на тестовом кабинете Почты России. Для
`logistics.track.read` выполняется один SOAP 1.2 вызов
`POST https://tracking.russianpost.ru/rtm34` с методом
`getOperationHistory`, `MessageType=0` и `Language=RUS`; ответ ограничен 100
историями, а наружу возвращается только последний нормализованный статус.
Логин и пароль tracking API не смешиваются с заголовками «Отправки» и
остаются callback-scoped.

Источники: [официальная спецификация API «Отправка»](https://otpravka.pochta.ru/specification),
[спецификация тарифного калькулятора](https://tariff.pochta.ru/post-calculator-api.pdf),
[сервис отслеживания](https://tracking.pochta.ru/specification) и
[инструкция по подключению](https://tracking.pochta.ru/support/faq/how_to_get_access).

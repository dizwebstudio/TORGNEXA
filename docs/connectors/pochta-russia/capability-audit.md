# Аудит возможностей «Почты России»

Официальная спецификация API «Отправка» описывает два значения для доступа:
токен авторизации приложения и ключ авторизации пользователя. REST-запросы
используют `Authorization: AccessToken …` и
`X-User-Authorization: Basic …`. В качестве bounded health probe выбран
метод текущих настроек пользователя `GET /1.0/settings`.

Подтверждённые источники:

- [Спецификация API «Отправка»](https://otpravka.pochta.ru/specification) — авторизация, заказы, партии, документы и настройки;
- [Расчёт стоимости пересылки](https://tariff.pochta.ru/post-calculator-api.pdf) — отдельный тарифный API;
- [Спецификация сервиса отслеживания](https://tracking.pochta.ru/specification) — SOAP-трекинг и отдельный доступ;
- [Как получить доступ к API](https://tracking.pochta.ru/support/faq/how_to_get_access) — регистрация и параметры доступа.

В текущем runtime включены credential-проверка, bounded
`logistics.batches.read`, `pickup.points.read`, read-only
`logistics.rates.read`, `logistics.shipment.cancel`, одиночное
`logistics.shipment.create`, `logistics.return.create`, `logistics.label.read`
и единичное `logistics.track.read`. Создание выполняется
через официальный `PUT /1.0/user/backlog`: адаптер принимает только два
известных типа посылки, требует индексы РФ, адрес с номером дома, двух- или
трёхчастное ФИО и целые сантиметры габаритов, а из ответа принимает ровно
один `result-ids`. Удаление одного нового заказа использует официальный
`DELETE /1.0/backlog` и принимает только ровно один совпавший `result-id`.
Возврат для ранее созданного отправления выполняется официальным
`PUT /1.0/returns` с одним исходным RPO (`direct-barcode`) и ограниченным
типом посылки; подтверждается один `return-barcode`, без сохранения сырого
ответа. Этикетка запрашивается официальным
`GET /1.0/forms/backlog/{order-id}/forms` с бумажным форматом и текущей датой;
host проверяет PDF и возвращает только content-addressed ссылку. Для тарифа используется официальный
`GET https://tariff.pochta.ru/v2/calculate/tariff/delivery` с объектом
«Посылка онлайн обыкновенная» (`23030`); запрос передаёт индексы и суммарный
вес, а ответ проверяется по `paynds` и контрольному сроку доставки. Тарифный
калькулятор не получает секреты кабинета.

Для tracking используется отдельный SOAP 1.2 endpoint
`https://tracking.russianpost.ru/rtm34`, метод `getOperationHistory`, русский
язык и отдельные `tracking_login`/`tracking_password`. Принимается один
14-значный российский или 13-значный S10 barcode; история ограничена 100
записями, а в runtime выходит только последний нормализованный статус.

`logistics.batches.read` подтверждён официальным `GET /1.0/batch`. Runtime
ограничивает страницу 100 записями, передаёт только фильтры типа/категории
почты, проверяет уникальный `batch-name`, безопасный статус и диапазон
`shipment-count`, после чего возвращает нейтральную проекцию без состава
заказов. `logistics.batches.create` подтверждён официальным `POST
/1.0/user/shipment`: адаптер принимает только 1–100 числовых order IDs,
опциональную дату и флаг онлайн-баланса, возвращает один проверенный
идентификатор партии и не раскрывает состав заказов. `logistics.batches.submit`
теперь передаёт сформированную партию в работу через approval-bound
`POST /api/v1/logistics/batches/{batch_id}/submit`, который вызывает официальный
`POST /1.0/batch/{batch-name}/checkin` и принимает только ответ с `f103-sent`.

`logistics.return.separate.create` теперь допускает standalone-возврат через
approval-bound `POST /api/v1/logistics/returns/separate`. Runtime вызывает
официальный `PUT /1.0/returns/return-without-direct` с одной записью и
принимает только ответ с `position=0` и валидным `return-barcode`; сырые
адреса, имена и ответ провайдера не сохраняются.

Прочие документы и возвраты, не покрытые
существующим RPO,
требуют актуальных
обезличенных fixtures, маппинга почтовых сервисов и доказанной идемпотентности
на тестовом кабинете. Возвратная этикетка для существующего RPO использует
отдельный easy-return PDF маршрут: принимает только domestic/S10 barcode и
валидный PDF, наружу возвращает только SHA-256-based opaque reference. До
отдельной квалификации остальные перечисленные операции остаются
fail-closed.

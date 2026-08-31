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

`logistics.batches.archive` переводит сформированную партию в архив через
approval-bound `POST /api/v1/logistics/batches/archive/{batch_id}`. Runtime
вызывает официальный `PUT /1.0/archive` с одним числовым именем партии и
принимает только точное совпадение `batch-name`; результат фиксируется как
`ARCHIVED`, без сохранения сырого ответа.

`logistics.batches.unarchive` возвращает партию из архива через
approval-bound `POST /api/v1/logistics/batches/archive/revert/{batch_id}`.
Runtime вызывает официальный `POST /1.0/archive/revert` с одним числовым
именем партии и принимает только точное совпадение `batch-name`; результат
фиксируется как `RESTORED` с `archived=false`, без сохранения сырого ответа.

`logistics.batches.archive.read` читает архив партий через
`GET /api/v1/logistics/batches/archive`, который вызывает официальный
`GET /1.0/archive`. Host ограничивает ответ 100 записями, проверяет уникальные
имена, статусы и неотрицательное количество отправлений и не пропускает
состав заказов в нейтральную проекцию.

`logistics.return.separate.create` теперь допускает standalone-возврат через
approval-bound `POST /api/v1/logistics/returns/separate`. Runtime вызывает
официальный `PUT /1.0/returns/return-without-direct` с одной записью и
принимает только ответ с `position=0` и валидным `return-barcode`; сырые
адреса, имена и ответ провайдера не сохраняются.

`logistics.return.separate.delete` удаляет отдельную возвратную отправку через
approval-bound `DELETE /api/v1/logistics/returns/separate/{return_id}`. Runtime
вызывает официальный `DELETE /1.0/returns/delete-separate-return?barcode=...`,
передаёт только проверенный ШПИ и принимает `2xx` с пустым ответом или пустым
`code`; ответ с любым кодом ошибки отклоняется. В host projection попадает
только `DELETED`/`deleted=true`, а повтор контролируется tenant-scoped
operation receipt. Операция необратима и требует тестового возврата,
approval, capability и live qualification.

`logistics.return.separate.edit` редактирует отдельную возвратную отправку
через approval-bound `POST /api/v1/logistics/returns/separate/{return_id}`.
Runtime вызывает официальный `POST /1.0/returns/{barcode}`, передаёт только
разрешённые адреса, ценность, вид отправления и имена и принимает только
ответ с тем же ШПИ. Нормализованный результат — `UPDATED`/`updated=true`;
сырой ответ, адреса и имена не попадают в receipt.

Форма Ф103 партии доступна через `logistics.label.read` с явным форматом
`batch_f103_pdf` и официальный `GET /1.0/forms/{batch-name}/f103pdf`.
Host принимает только числовой номер партии, проверяет `application/pdf` и
сигнатуру `%PDF-`, после чего возвращает content-addressed opaque reference.
Форма уже сформированного заказа доступна через явный формат
`formed_order_pdf` и официальный `GET /1.0/forms/{order-id}/forms`; host
ограничивает order ID одним числовым значением, передаёт бумажный формат и
текущую дату, проверяет PDF и возвращает только content-addressed opaque
reference.

Прочие документы и возвраты, не покрытые
существующим RPO,
требуют актуальных
обезличенных fixtures, маппинга почтовых сервисов и доказанной идемпотентности
на тестовом кабинете. Возвратная этикетка для существующего RPO использует
отдельный easy-return PDF маршрут: принимает только domestic/S10 barcode и
валидный PDF, наружу возвращает только SHA-256-based opaque reference. До
отдельной квалификации остальные перечисленные операции остаются
fail-closed.

`logistics.orders.restore` возвращает выбранные заказы из сформированной
партии в список «Новые» через approval-bound `POST
/api/v1/logistics/orders/restore` и официальный `POST /1.0/user/backlog`.
Host требует 1–100 числовых ID, отвергает provider errors, частичное или
несовпадающее подтверждение и сохраняет только нормализованный статус
`restored`. Операция отдельна от отмены заказа.

`logistics.batches.orders.read` допускается как bounded read-only проекция
заказов через `GET /1.0/batch/{batch-name}/shipment`. Qualification evidence
должно подтвердить числовой batch ID, query `size`/`page`/`sort=ask`, оба
заголовка авторизации, exact batch match, duplicate rejection и отсутствие
PII в проекции. В SDK разрешены только provider order ID, batch ID, barcode,
нормализованный lowercase status и UTC observation time; сырой ответ и поля
получателя не сохраняются.

`logistics.orders.read` квалифицируется как bounded read-only lookup одного
заказа через `GET /1.0/shipment/{id}`. Evidence должно подтвердить числовой
order ID, отсутствие query/body, оба заголовка авторизации, exact ID match,
одну нормализованную строку результата и отсутствие PII. Object и single-item
array допускаются только при ровно одной записи; другой ID, отсутствующий
batch ID и malformed fields должны оставаться provider failure.

Проверка `logistics.batches.read` теперь также включает точечный lookup партии
через `GET /1.0/batch/{batch-name}`. Evidence должна подтвердить числовой
batch ID, отсутствие query/body, exact name match, ровно одну проекцию и
отсутствие строк заказов/raw payload. Ответ с другим именем, несколькими
объектами, невалидным status/count или provider error остаётся fail-closed.

Для `logistics.orders.search` fixture должен проверить официальный
`GET /1.0/backlog/search?query=...`, оба заголовка авторизации и bounded
лимит не более 100 строк. Harness должен принять только результаты с точным
номером магазина, нормализовать статус в lowercase и отклонить дубликаты,
невалидные ID/status, номер, отличный от query, и ответ сверх лимита. Поля
получателя, адреса и raw provider payload из fixture не должны попасть в
безопасную проекцию. Live qualification выполняется read-only на тестовом
заказе и не требует approval или operation receipt.

Для `logistics.batches.sending_date.write` fixture должен проверить
`POST /1.0/batch/{batch-name}/sending/YYYY/MM/DD` с числовым batch ID, датой в
трёх path-сегментах, отсутствием query и тела и обоими заголовками
авторизации. Успешным считается `2xx` с пустым телом или JSON без
`error-code`; любой error code, malformed response, нечисловая партия или
другая дата должны оставаться provider failure. Host projection ограничена
точным batch ID, ISO-даты, `UPDATED`/`updated=true` и UTC observation time.
Live qualification выполняется на disposable сформированной партии с
проверкой approval, tenant-scoped idempotency receipt и поведения после
timeout/неоднозначного ответа. Контракт подтверждён [официальной
спецификацией API «Отправка»](https://otpravka.pochta.ru/specification).

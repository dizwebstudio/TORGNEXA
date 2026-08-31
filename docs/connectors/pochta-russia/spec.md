# Спецификация коннектора «Почта России»

- идентификатор: `pochta-russia`
- семейство: `logistics`
- версия SDK: `1`
- авторизация для проверки: JSON `{ "token": "…", "key": "…", "tracking_login": "…", "tracking_password": "…" }`
- endpoint проверки: `https://otpravka-api.pochta.ru/1.0/settings`

Проверка передаёт токен приложения в заголовке
`Authorization: AccessToken <token>` и сгенерированный ключ пользователя в
заголовке `X-User-Authorization: Basic <key>`. Успешным считается ответ
`2xx` с корректным JSON; тело ответа не выходит из host-side transport.

Манифест перечисляет нормализуемые операции расчёта, партий, отправлений,
отмены, возврата, этикетки, трекинга и пунктов выдачи. В runtime включены
bounded `logistics.batches.create`, `logistics.batches.read`,
`logistics.batches.submit`, `logistics.batches.archive`, `logistics.batches.unarchive`, `pickup.points.read`, `logistics.rates.read`,
`logistics.shipment.cancel`, `logistics.shipment.create`, `logistics.return.create`,
`logistics.return.separate.create`, `logistics.return.separate.delete`, `logistics.label.read`
и `logistics.track.read`. Для сформированных партий также включена отдельная
операция `logistics.orders.restore`.
Создание одного заказа
выполняется через официальный `PUT /1.0/user/backlog`; адаптер принимает
только известные коды посылки, требует российские индексы и адрес с номером
дома, преобразует размеры из миллиметров в целые сантиметры и проверяет ровно
один `result-ids` в ответе. Отмена одного нового заказа выполняется через
официальный `DELETE /1.0/backlog`, отправляет один числовой ID и принимает
только точное совпадение единственного `result-ids`; это не отменяет уже
сформированную партию. Возврат для существующего RPO выполняется отдельным
`PUT /1.0/returns` с `direct-barcode` и ограниченным allow-list `mail-type`;
адаптер принимает только один подтверждённый `return-barcode`. Формирование
партии выполняется через approval-bound `POST /api/v1/logistics/batches`,
который вызывает официальный `POST /1.0/user/shipment` с 1–100 числовыми ID
заказов и передаёт только опциональные `sending-date` и
`use-online-balance`. Tenant-scoped receipt защищает от повторной отправки;
неоднозначная ошибка остаётся pending до сверки, поэтому автоматический retry
запрещён. Передача партии в работу выполняется через
`POST /api/v1/logistics/batches/{batch_id}/submit` и официальный
`POST /1.0/batch/{batch-name}/checkin`; при необходимости передаётся
`useOnlineBalance=true`, а ответ принимается только при `f103-sent`. Операция
требует approval и idempotency receipt. Перевод сформированной партии в архив
доступен через approval-bound `POST /api/v1/logistics/batches/archive/{batch_id}`;
адаптер вызывает официальный `PUT /1.0/archive` с массивом из одного
числового имени партии и принимает только точное подтверждение `batch-name`.
Результат нормализуется в `ARCHIVED`. Чтение архивных партий доступно через
`GET /api/v1/logistics/batches/archive` и официальный `GET /1.0/archive`;
host ограничивает ответ 100 записями и возвращает только идентификатор,
статус и количество отправлений. Восстановление доступно отдельной
approval-bound операцией `POST /api/v1/logistics/batches/archive/revert/{batch_id}`:
адаптер вызывает официальный `POST /1.0/archive/revert` с массивом из одного
числового имени партии и принимает только точное подтверждение `batch-name`;
результат нормализуется в `RESTORED` с `archived=false`. Отдельное возвратное отправление
создаётся через approval-bound `POST /api/v1/logistics/returns/separate`, который
вызывает официальный `PUT /1.0/returns/return-without-direct` ровно для одного
отправления. Адаптер передаёт адреса, вид отправления, объявленную ценность,
имена и необязательные номер заказа/индекс ОПС; принимает только ответ с
`position=0` и проверенным `return-barcode`. Сумма хранится в minor units и
передаётся Почте в целых рублях. Адреса и имена не сохраняются в operation
receipt, в receipt попадает только нормализованный ШПИ и статус. Отдельная
отмена отдельной возвратной отправки выполняется через approval-bound
`DELETE /api/v1/logistics/returns/separate/{return_id}` и официальный
`DELETE /1.0/returns/delete-separate-return?barcode={barcode}`. Запрос не
содержит тела; адаптер принимает `2xx` с пустым телом или пустым `code`,
отклоняет любой provider error code и возвращает только `DELETED` с
`deleted=true`. Операция необратима, требует approval и защищена
tenant-scoped idempotency receipt.
Редактирование отдельной возвратной отправки выполняется через
approval-bound `POST /api/v1/logistics/returns/separate/{return_id}` и
официальный `POST /1.0/returns/{barcode}`. Запрос повторяет только
разрешённые поля standalone-отправки; runtime принимает ответ лишь при
точном совпадении `return-barcode` и нормализует его в `UPDATED`.
Отдельная
возвратная этикетка запрашивается форматом `return_pdf`
через `GET /1.0/forms/{rpo}/easy-return-pdf` с фиксированным
`print-type=PAPER`; допускается только domestic/S10 RPO-barcode, а ответ
принимается после проверки `application/pdf` и сигнатуры `%PDF-`.
Форма Ф103 одной партии запрашивается форматом `batch_f103_pdf` через
`GET /1.0/forms/{batch-name}/f103pdf`; номер партии должен быть числовым,
а ответ также проверяется как PDF и возвращается только как opaque digest-
ссылка.
Форма уже сформированного заказа запрашивается форматом
`formed_order_pdf` через официальный `GET /1.0/forms/{order-id}/forms` с
бумажным форматом и текущей датой; order ID ограничен одним числовым
значением, а ответ проходит ту же проверку PDF и digest-нормализацию.
PDF-печатная форма заказа
запрашивается через официальный `GET /1.0/forms/backlog/{order-id}/forms` с
бумажным форматом и текущей датой, а наружу возвращается только
content-addressed ссылка после проверки PDF. Поиск ОПС выполняется
через официальный `GET /postoffice/1.0/by-address`, данные отделения — через
`GET /postoffice/1.0/{postal-code}`, а тариф — через
`GET https://tariff.pochta.ru/v2/calculate/tariff/delivery` для объекта 23030.
Тарифный запрос принимает индексы и суммарный вес, возвращает нейтральный
предпросмотр в RUB и не передаёт секреты кабинета. Tracking выполняет один
SOAP 1.2 `getOperationHistory` по domestic/S10 barcode, ограничивает историю
100 записями и возвращает последний нормализованный статус. Для защиты от
fan-out общий запрос принимает SDK-лимит до 500, но один вызов к Почте России
ограничен 50 ОПС.
Справочник партий читается через официальный `GET /1.0/batch`; адаптер
передаёт только bounded фильтры `mailType`/`mailCategory`, `size` и `page`,
проверяет уникальные имена партий, статусы и неотрицательное число
отправлений. Это read-only проекция: строки заказов в неё не включаются;
передача партии в работу выполняется отдельным check-in маршрутом.

Возврат заказов из сформированной партии в список «Новые» выполняется через
approval-bound `POST /api/v1/logistics/orders/restore`. Адаптер вызывает
официальный `POST /1.0/user/backlog` с массивом из 1–100 числовых внутренних
идентификаторов заказов. Ответ содержит `result-ids` и поэлементные `errors`;
частичный результат, ошибка или несовпадение набора идентификаторов отклоняются
как provider failure. Полный набор нормализуется в статус `restored`, а
tenant-scoped operation receipt не допускает повторного внешнего вызова при
неопределённом исходе. Это возврат в backlog, а не отмена заказа и не возврат
посылки отправителю.

Состав одной сформированной партии читается отдельной bounded read-only
операцией `logistics.batches.orders.read` через
`GET /api/v1/logistics/batches/orders`. Host вызывает официальный
`GET /1.0/batch/{batch-name}/shipment` с числовым именем партии и параметрами
`size`, `page`, `sort=ask`. В нейтральную проекцию попадают только ID заказа,
имя партии, barcode, нормализованный статус и время наблюдения; получатель,
адреса и прочие поля ответа провайдера не пересекают границу коннектора.
Несовпадение партии, дубликаты, некорректные ID/barcode/status и ответ больше
запрошенного лимита отклоняются как provider failure.

Поиск партии по имени доступен через существующую capability
`logistics.batches.read` и `GET /api/v1/logistics/batches/{batch_id}`. Host
вызывает официальный `GET /1.0/batch/{batch-name}` без query/body, принимает
числовое имя партии и возвращает только ID, статус, число отправлений и UTC
время наблюдения. Ответ с другим именем, несколькими партиями, невалидным
статусом или количеством отклоняется как provider failure.

Поиск одного заказа в партии доступен через read-only capability
`logistics.orders.read` и `GET /api/v1/logistics/orders/{order_id}`. Host
вызывает официальный `GET /1.0/shipment/{id}`, принимает только числовой ID,
сверяет его с ID в ответе и возвращает ту же безопасную проекцию, что и список
партии. Пустой/массивный ответ более чем с одной строкой, другой ID,
отсутствующий batch ID, невалидный barcode или статус закрываются как
provider failure.

Поиск заказов по назначенному магазином номеру доступен через bounded
read-only capability `logistics.orders.search` и
`GET /api/v1/logistics/orders/search`. Host вызывает официальный
`GET /1.0/backlog/search?query={external_id}`, передаёт только проверенный
номер заказа и ограничивает выдачу 100 строками. В нейтральную проекцию
попадают ID заказа, исходный номер магазина, необязательные номер партии и
ШПИ, нормализованный статус и UTC-время наблюдения. Точные совпадения номера
магазина обязательны; адреса, получатели и raw provider payload за границу
коннектора не выходят. Невалидный ответ, дубликаты и превышение лимита
остаются provider failure. Маршрут подтверждён [официальной спецификацией API
«Отправка»](https://otpravka.pochta.ru/specification).

Изменение дня передачи сформированной партии доступно через отдельную
approval-bound capability `logistics.batches.sending_date.write` и маршрут
`POST /api/v1/logistics/batches/sending-date/{batch_id}`. Host принимает
числовое имя партии и дату `YYYY-MM-DD`, вызывает официальный
`POST /1.0/batch/{batch-name}/sending/YYYY/MM/DD` без тела и query-параметров.
Почта России может подтвердить операцию пустым ответом; также допускается
JSON-объект без `error-code`, но любой код ошибки, malformed response или
несовпадение партии остаются provider failure. Наружу выходит только точное
имя партии, новая дата, `UPDATED`/`updated=true` и время наблюдения. Approval,
tenant-scoped idempotency receipt и проверка нормализованного результата
обязательны.

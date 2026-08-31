# Почта России

«Почта России» подключена к каталогу TORGNEXA на отдельной поверхности
«Доставка». Доступны создание кабинета, шифрованное сохранение учётных
данных, проверка доступа к официальному API «Отправка», bounded чтение
пунктов выдачи/ОПС по городу, чтение справочника партий и создание одиночного
заказа в backlog, а также формирование, передача в работу, архивирование и
возврат из архива партии из уже созданных заказов, а также чтение архива
партий без выгрузки строк заказов.

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
одного заказа использует официальный `PUT /1.0/user/backlog`: runtime принимает
только `pochta_parcel_online`/`pochta_parcel`, требует индексы РФ, адрес с
номером дома, двух- или трёхчастное ФИО и целые сантиметры габаритов, а
возвращает только проверенный `result-ids` как remote reference. Удаление
одного нового заказа выполняется через официальный `DELETE /1.0/backlog` с
одним числовым ID и допускается только при точном совпадении единственного
`result-id` в ответе. Возврат для ранее созданного отправления выполняется
через официальный `PUT /1.0/returns` с одним `direct-barcode` исходного RPO
и провайдерным `mail-type`; принимается только один подтверждённый
`return-barcode`. Формирование партии доступно отдельной approval-bound
операцией `POST /api/v1/logistics/batches`: она вызывает официальный
`POST /1.0/user/shipment` с массивом числовых order IDs и опциональными
`sending-date`/`use-online-balance`. Дубликаты защищены tenant-scoped
idempotency receipt; при неоднозначной сетевой ошибке повторная отправка
запрещена до сверки у провайдера. Передача сформированной партии в работу
доступна отдельной approval-bound операцией
`POST /api/v1/logistics/batches/{batch_id}/submit`: она вызывает официальный
`POST /1.0/batch/{batch-name}/checkin`, по флагу добавляет
`useOnlineBalance=true` и принимает только подтверждение `f103-sent`. Результат
нормализуется в статус партии, а повтор защищён тем же tenant-scoped
idempotency receipt. Перевод сформированной партии в архив выполняется через
approval-bound `POST /api/v1/logistics/batches/archive/{batch_id}` и официальный
`PUT /1.0/archive`; адаптер отправляет один числовой номер партии и принимает
только точное `batch-name`. Возврат партии из архива выполняется через
approval-bound `POST /api/v1/logistics/batches/archive/revert/{batch_id}` и
официальный `POST /1.0/archive/revert`; адаптер принимает только точное
`batch-name` и нормализует результат в `RESTORED`. Архив партий читается
через `GET /api/v1/logistics/batches/archive` и официальный `GET /1.0/archive`;
в ответ выходят только идентификатор, статус и количество отправлений.
Отдельная возвратная отправка без исходного RPO
выполняется через approval-bound `POST /api/v1/logistics/returns/separate` и
официальный `PUT /1.0/returns/return-without-direct`. Запрос ограничен одним
отправлением; наружу возвращаются только ШПИ, статус и время наблюдения после
проверки `position=0`/`return-barcode`. Адреса и имена не попадают в durable
receipt.
Удаление такой отправки доступно через approval-bound
`DELETE /api/v1/logistics/returns/separate/{return_id}` и официальный
`DELETE /1.0/returns/delete-separate-return?barcode={barcode}`. Host передаёт
только ШПИ, принимает успешный `2xx` с пустым телом или пустым `code` и
отклоняет любой код ошибки; наружу выходит только нормализованный статус
`DELETED`. Повтор не вызывает второй внешний запрос благодаря
tenant-scoped operation receipt. Это необратимое действие и допускается
только для тестового возврата после approval.
Редактирование отдельной возвратной отправки доступно через approval-bound
`POST /api/v1/logistics/returns/separate/{return_id}` и официальный
`POST /1.0/returns/{barcode}`. Host отправляет полный ограниченный набор
полей, принимает только подтверждение того же ШПИ и возвращает
`UPDATED`/`updated=true`; пустой, ошибочный или не совпадающий ответ
провайдера отклоняется.
Возвратная этикетка для существующего RPO
доступна отдельным форматом `return_pdf` через
`GET /1.0/forms/{rpo}/easy-return-pdf`. Принимается domestic/S10 RPO-barcode;
runtime явно передаёт `print-type=PAPER`. PDF проверяется по `Content-Type` и
сигнатуре `%PDF-`, а наружу передаётся только content-addressed opaque
reference. Для `logistics.label.read` обычный
заказ запрашивается через
`GET /1.0/forms/backlog/{order-id}/forms` с бумажным форматом и текущей датой.
PDF проверяется по `Content-Type` и сигнатуре `%PDF-`, а наружу передаётся
только content-addressed opaque reference; бинарное тело не выходит из
host-side transport. Для
`logistics.track.read` выполняется один SOAP 1.2 вызов
`POST https://tracking.russianpost.ru/rtm34` с методом
`getOperationHistory`, `MessageType=0` и `Language=RUS`; ответ ограничен 100
историями, а наружу возвращается только последний нормализованный статус.
Логин и пароль tracking API не смешиваются с заголовками «Отправки» и
остаются callback-scoped.

`logistics.batches.read` выполняет bounded `GET /1.0/batch` на официальном
API «Отправка». В запрос можно передать `mailType`, `mailCategory`, страницу и
размер страницы (до 100); наружу выходят только проверенные идентификатор,
статус, число отправлений и время наблюдения. Содержимое заказов и сырые
ответы провайдера за границу host transport не выходят. Формирование партии
возвращает только тот же нейтральный batch projection; передача её в работу
выполняется через отдельный approval-bound check-in маршрут.

Источники: [официальная спецификация API «Отправка»](https://otpravka.pochta.ru/specification),
[спецификация тарифного калькулятора](https://tariff.pochta.ru/post-calculator-api.pdf),
[сервис отслеживания](https://tracking.pochta.ru/specification) и
[инструкция по подключению](https://tracking.pochta.ru/support/faq/how_to_get_access).

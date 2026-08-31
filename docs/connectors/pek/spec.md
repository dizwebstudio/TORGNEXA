# Спецификация коннектора ПЭК

- идентификатор: `pek`
- семейство: `logistics`
- версия SDK: `1`
- авторизация: Basic (`username` личного кабинета + активный ключ доступа)
- endpoint проверки: `https://kabinet.pecom.ru/api/v1/branches/all/`

Заявленные SDK-возможности:

- `logistics.rates.read` — расчёт стоимости и сроков;
- `logistics.label.read` — PDF-этикетка одного груза или печатная форма заявки;
- `logistics.return.create` — возврат одного принятого груза отправителю;
- `logistics.shipment.cancel` — аннулирование одного ранее созданного
  предварительного оформления;
- `logistics.shipment.create` — предварительное оформление/заявка на забор;
- `logistics.track.read` — текущий статус груза;
- `pickup.points.read` — справочник филиалов и пунктов выдачи.

Входные и выходные модели используют нейтральные `RateRequest`,
`ShipmentCreateRequest`, `ShipmentStatusRequest` и `PickupPointQuery`. Коды
тарифов, филиалов и грузов остаются внутри адаптера и не становятся полями
Core.

Runtime включает bounded read-only `pickup.points.read`,
`logistics.rates.read` и `logistics.track.read`, а также ограниченные
`logistics.shipment.create`, `logistics.shipment.cancel`,
`logistics.return.create` и `logistics.label.read`.
Create допускается только для одного российского заказа типа `orderType=0`
с сервисом `pek_type_3`, настроенным sender warehouse и не более 50 мест.
Справочник выполняется через
`POST https://kabinet.pecom.ru/api/v1/branches/all/`, расчёт — через
`POST https://kabinet.pecom.ru/api/v1/calculator/calculateprice/`, а текущий
статус — через `POST https://kabinet.pecom.ru/api/v1/cargos/basicstatus/`.
Предварительное оформление одной заявки выполняется через
`POST https://kabinet.pecom.ru/api/v1/preregistration/submit/`; sender warehouse
берётся из tenant-конфигурации, receiver передаётся как склад выдачи или
нормализованный адрес. Ответ принимается только при наличии `documentId` и
ровно одного числового `cargoCode`. Аннулирование одного предварительного
оформления выполняется через
`POST https://kabinet.pecom.ru/api/v1/order/cancellation/` с массивом из одного
кода груза; ответ принимается только при точном совпадении кода и
`success=true`.
Запрос статуса содержит один код груза, ответ ограничен 50 элементами и
нормализуется в нейтральный статус. Возврат с `mail_type=pek_cargo_return`
вызывает `POST /api/v1/cargos/cancelandreturncargo/` с одним JSON-полем `code`;
только ответ с `success=true` нормализуется как созданный возврат с тем же
кодом груза. `logistics.label.read` с форматом `pdf`
вызывает `/order/print/` с `type=simple`, а с форматом `request_pdf` — тот же
метод с `type=big`; оба ответа декодируются из base64, проверяются как PDF и
выдаются только как непрозрачные digest-ссылки. Расчёт принимает до 50 мест и возвращает
не более 100 вариантов с точной денежной нормализацией. Отмена сформированного
груза и пакетная печать (`type=multiple`) остаются закрыты.

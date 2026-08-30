# Спецификация коннектора ПЭК

- идентификатор: `pek`
- семейство: `logistics`
- версия SDK: `1`
- авторизация: Basic (`username` личного кабинета + активный ключ доступа)
- endpoint проверки: `https://kabinet.pecom.ru/api/v1/branches/all/`

Заявленные SDK-возможности:

- `logistics.rates.read` — расчёт стоимости и сроков;
- `logistics.shipment.create` — предварительное оформление/заявка на забор;
- `logistics.track.read` — текущий статус груза;
- `pickup.points.read` — справочник филиалов и пунктов выдачи.

Входные и выходные модели используют нейтральные `RateRequest`,
`ShipmentCreateRequest`, `ShipmentStatusRequest` и `PickupPointQuery`. Коды
тарифов, филиалов и грузов остаются внутри адаптера и не становятся полями
Core.

Runtime сейчас включает только `pickup.points.read`. Запрос выполняется через
`POST https://kabinet.pecom.ru/api/v1/branches/all/`, ограничивается по размеру
ответа и городу, а в результат попадают только склады с явной операцией
«Выдача грузов». Остальные capability из манифеста остаются закрытыми.

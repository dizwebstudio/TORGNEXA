# Спецификация коннектора «Деловые Линии»

- идентификатор: `dellin`
- семейство: `logistics`
- версия SDK: `1`
- авторизация для проверки: JSON `{ "appkey": "…", "pat": "…" }`
- endpoint проверки: `https://api.dellin.ru/v4/auth/login.json`

Проверка выполняет официальный token-based login и убеждается, что API вернул
непустой `sessionID`. Значение сессии является временным и не покидает
callback проверки.

В runtime включено только bounded read-only чтение справочника терминалов/ПВЗ
(`pickup.points.read`): сначала выполняется `POST
https://api.dellin.ru/v3/public/terminals.json` с appkey, затем загружается
возвращённый официальный URL каталога на том же host и из выбранного города
нормализуется не более запрошенного лимита пунктов. Внешний URL принимается
только при точном host `api.dellin.ru` и пути
`/catalog/terminals_v3.json`; remote ID не становится ID склада в Core.

Заявленные в SDK capability (`logistics.rates.read`, `logistics.shipment.create`,
`logistics.shipment.cancel`, `logistics.track.read` и `logistics.label.read`)
пока не являются runtime-маршрутами. Это явно отражено в runtime support как
`separate_surface/logistics` с единственной operational capability
`pickup.points.read`.

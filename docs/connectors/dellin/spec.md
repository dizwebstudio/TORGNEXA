# Спецификация коннектора «Деловые Линии»

- идентификатор: `dellin`
- семейство: `logistics`
- версия SDK: `1`
- авторизация для проверки: JSON `{ "appkey": "…", "pat": "…" }`
- endpoint проверки: `https://api.dellin.ru/v4/auth/login.json`

Проверка выполняет официальный token-based login и убеждается, что API вернул
непустой `sessionID`. Значение сессии является временным и не покидает
callback проверки.

Заявленные в SDK capability (`logistics.rates.read`, `logistics.shipment.create`,
`logistics.shipment.cancel`, `logistics.track.read`, `logistics.label.read` и
`pickup.points.read`) пока не являются runtime-маршрутами. Это явно отражено в
runtime support как `separate_surface/logistics` с нулём operational
capabilities.

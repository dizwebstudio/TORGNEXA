# CDEK

СДЭК подключён к каталогу TORGNEXA на отдельной поверхности «Доставка».
Доступны создание кабинета, шифрованное сохранение JSON с `client_id` и
`client_secret`, OAuth-проверка токена и короткий запрос справочника городов.

Host transport также умеет bounded read-only запрос списка ПВЗ по стране и
городу: OAuth-токен живёт только внутри callback, а ответ нормализуется в
канонический `PickupPoint`. Операция доступна через защищённый маршрут
`GET /api/v1/logistics/pickup-points` только при явно включённом
`pickup.points.read`; provider-specific идентификаторы не становятся
идентификаторами складов Core.

SDK-кандидат по-прежнему покрывает rates, shipment lifecycle, tracking,
cancellation, labels, pickup points и return flow, но тарифы и операции с
отправлениями остаются закрытыми до квалификации актуальных контрактов и
идемпотентного маппинга.

Official documentation: https://apidoc.cdek.ru/

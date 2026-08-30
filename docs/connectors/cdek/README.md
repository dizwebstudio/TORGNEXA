# CDEK

СДЭК подключён к каталогу TORGNEXA на отдельной поверхности «Доставка».
Доступны создание кабинета, шифрованное сохранение JSON с `client_id` и
`client_secret`, OAuth-проверка токена и короткий запрос справочника городов.

Host transport также умеет bounded read-only запрос списка ПВЗ по стране и
городу: OAuth-токен живёт только внутри callback, а ответ нормализуется в
канонический `PickupPoint`. Операция пока не включена в application runtime:
для этого нужны актуальные provider fixtures, host-side маршрут и отдельная
квалификация окружения.

SDK-кандидат по-прежнему покрывает rates, shipment lifecycle, tracking,
cancellation, labels, pickup points и return flow, но тарифы и операции с
отправлениями остаются закрытыми до квалификации актуальных контрактов и
идемпотентного маппинга.

Official documentation: https://apidoc.cdek.ru/

# Деловые Линии

«Деловые Линии» подключены к каталогу TORGNEXA на отдельной поверхности
«Доставка». Доступны создание кабинета, шифрованное сохранение appkey/PAT и
проверка авторизации через официальный API. Сессионный идентификатор из ответа
проверки не сохраняется.

Через защищённый маршрут доступно bounded read-only чтение справочника
терминалов/ПВЗ (`pickup.points.read`). Маршрут включается только явной записью
capability в runtime-support и возвращает нормализованные remote IDs, название,
город и адрес; он не создаёт и не изменяет склады TORGNEXA.

`logistics.track.read` выполняет один read-only запрос истории статусов по
номеру документа через `POST https://api.dellin.ru/v3/orders/statuses_history.json`.
История ограничена 100 записями, выбирается последняя дата, а состояние
нормализуется в нейтральный статус.

`logistics.rates.read` выполняет bounded предпросмотр тарифа через официальный
калькулятор `POST https://api.dellin.ru/v2/calculator.json`. В запрос передаются
текстовые адреса и агрегированные габариты/вес не более 50 мест; денежная сумма
нормализуется в целые копейки RUB, а наружу возвращается один нейтральный
вариант. `logistics.shipment.create` оформляет address-to-address заявку или
bounded маршрут terminal-to-terminal через официальный
`POST https://api.dellin.ru/v2/request.json`. Для
авторизованного запроса в runtime-конфигурации должны быть явно заданы UID
заказчика, `sender_counteragent_id`, UID характера груза, дата передачи, окно
времени и тип оплаты. Для терминального маршрута `sender_terminal_id` задаёт
терминал отправителя, а `pickup_point_ref` — числовой терминал получателя;
адресные поля общей формы в этом режиме не передаются провайдеру. Получатель
передаётся как официальный анонимный физический получатель; запрос ограничен
50 местами. Неоднозначный сетевой
результат не повторяется автоматически. Отмена доставки до адреса и отмена
забора от адреса доступны через официальные `POST
https://api.dellin.ru/v3/orders/cancel_delivery.json` и `POST
https://api.dellin.ru/v3/orders/cancel_pickup.json`; нейтральный режим отмены
выбирается как `delivery` или `pickup`. Ответ `data.status=success` означает
приём заявки, поэтому локально показывается `cancellation_pending` до
подтверждения истории статусов. Отмена терминального заказа и возвраты
остаются закрытыми.

Для сформированной Pre-Alert пакетной заявки доступна отдельная
approval-bound операция `logistics.batches.cancel`. Она вызывает официальный
`POST https://api.dellin.ru/v2/batch_request/cancel.json` с числовым
`batchRequestID` и принимает только `metadata.status=200` вместе с
`data.state=success`; результат нормализуется в `CANCELLED`. Эта операция
расформировывает пакетную заявку и не отменяет отдельную перевозку.

`logistics.label.read` получает PDF-форму накладной через официальный `POST
https://api.dellin.ru/v1/printable.json`. В `remote_id` передаётся UID
накладной из журнала заказов (`docUID`), а режим формы фиксирован как `order`.
Ответ принимается только при совпадении UID и валидном PDF; наружу выходит
контентно-адресуемая ссылка без тела документа и URL Деловых Линий.

Источники: [авторизация пользователя](https://dev.dellin.ru/api/auth/login/),
[оформление сборного груза](https://dev.dellin.ru/api/ordering/ltl-request/),
[калькулятор](https://dev.dellin.ru/api/calculation/calculator/),
[журнал заказов](https://dev.dellin.ru/api/orders/search/),
[история статусов заказа](https://dev.dellin.ru/api/orders/statuses-history/),
[печатные формы документов](https://dev.dellin.ru/api/orders/print/) и
[справочник терминалов](https://dev.dellin.ru/api/terminals/directory/).

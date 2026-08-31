# CS-Cart reconciliation

`product_code` используется как стабильный seller SKU. При записи без
`remote_id` коннектор выполняет точный поиск по `pcode`; совпадающее состояние
возвращает duplicate receipt. После POST/PUT состояние перечитывается по
`product_id`. Сетевые ошибки записи не повторяются вслепую: сначала выполняется
тот же поиск и сравнение, иначе возвращается `write_outcome_unknown`.

Цены читаются из `price` и `list_price` product projection, а остаток — из
`amount` по `product_id`. Для price/inventory reconciliation используются
соответствующие offer mappings и единая remote-локация `cs-cart-store`; если
mapping отсутствует, worker сохраняет drift для явного решения оператора.

Смена статуса заказа выполняется через `PUT /orders/{id}` только для
стандартной таблицы статусов и подтверждается повторным чтением заказа.

# План conformance для «Деловых Линий»

`connectors/logistics/dellin` содержит sandbox-кандидат и детерминированную проверку
здоровья без сети и боевых credentials. Общий harness проверяет SDK v1,
границу секретов, нормализацию ошибок, tenant isolation, egress и sandbox.

Для rates уже добавлены обезличенные fixtures калькулятора и bounded read
маршрут: используется временный sessionID, текстовые адреса, не более 50 мест
и точное преобразование суммы в копейки RUB. Для address-to-address заявки
добавлены обезличенные fixtures официального `v2/request.json`, проверка
обязательной runtime-конфигурации и нормализация `requestID` без повторной
отправки при неопределённом результате. Для bounded terminal-to-terminal
варианта fixture проверяет `derival.variant=terminal`, числовые
`derival.terminalID`/`arrival.terminalID` из явных ссылок и отсутствие адресных
объектов; sender terminal берётся только из runtime-конфигурации. Для этикетки
добавлены fixtures
`v1/printable.json`: проверяются `docUID`, `mode=order`, совпадение UID,
base64-декодирование и PDF-сигнатура; URL провайдера игнорируется. Для отмены
address-delivery добавлены fixtures `cancel_delivery` и `cancel_pickup`, проверка
числового `orderID`, `requester=sender`, `data.status=success` и нормализация в
`cancellation_pending`; финальное решение перевозчика подтверждается отдельным
tracking/read маршрутом. Отмена терминального заказа и возвраты по-прежнему
требуют тестовый
appkey/PAT, зафиксированная версия Public API, fixtures `request/orders/terminals`
и доказательство идемпотентного восстановления после неоднозначного ответа.

Для Pre-Alert добавлен отдельный fixture `batch_request/cancel`: проверяются
числовой `batchRequestID`, отсутствие PAT в запросе, фиксированный endpoint и
строгое принятие только `metadata.status=200`/`data.state=success`. Результат
нормализуется в `CANCELLED`; операция не расширяет границу до отмены отдельной
терминальной перевозки или ручного возврата.

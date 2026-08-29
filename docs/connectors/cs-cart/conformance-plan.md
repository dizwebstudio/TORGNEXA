# CS-Cart conformance plan

Перед включением нового runtime-маршрута необходимо повторно прогнать
conformance suite с тестовым CS-Cart store или детерминированным sandbox:

1. валидная/невалидная Basic Auth;
2. постраничное чтение и tenant isolation;
3. создание и обновление с повтором idempotency key;
4. read-after-write и reconciliation после timeout;
5. rate-limit и безопасное преобразование ошибок.

Текущий встроенный кандидат выполняет детерминированные SDK-пробы; сохранённый
отчёт проходит все 13 обязательных проверок, включая `sandbox_isolation`.
Реальная live-квалификация по-прежнему требует доступного CS-Cart API endpoint
с тестовыми Basic Auth-учётными данными.

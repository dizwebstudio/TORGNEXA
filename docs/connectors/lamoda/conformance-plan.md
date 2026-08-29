# План conformance для Lamoda

Проверяется Connector SDK v1 и безопасная граница health-only адаптера:

1. строгая валидация манифеста и capability vocabulary;
2. callback-scoped credential access без утечки API key;
3. нормализация provider-unavailable и auth failure;
4. bounded timeout/body, egress policy и отсутствие произвольных private hosts;
5. tenant isolation, idempotency/replay harness, dry-run и sandbox checks.

До включения доменных операций необходимо получить партнёрский тестовый
кабинет Lamoda, зафиксировать Seller API v2 contract, добавить synthetic
fixtures и пройти отдельный architecture review.

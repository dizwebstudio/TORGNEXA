# План conformance для «Деловых Линий»

`connectors/dellin` содержит sandbox-кандидат и детерминированную проверку
здоровья без сети и боевых credentials. Общий harness проверяет SDK v1,
границу секретов, нормализацию ошибок, tenant isolation, egress и sandbox.

Перед включением тарифов и заявок необходимо получить тестовый appkey/PAT,
зафиксировать текущую версию Public API, добавить обезличенные fixtures для
calculator/request/orders/terminals и доказать идемпотентное восстановление
после неоднозначного ответа.

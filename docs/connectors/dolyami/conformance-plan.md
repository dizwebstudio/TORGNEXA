# План conformance для «Долями»

1. Проверить манифест и payment capability vocabulary.
2. Проверить callback-scoped доступ к логину/паролю и mTLS-ключу без утечки.
3. Проверить строгую валидацию HTTPS probe и публичную egress-политику.
4. Проверить нормализацию auth/network ошибок и лимиты ресурсов.
5. До domain-admission пройти детерминированные тесты идемпотентности,
   replay вебхуков, tenant isolation и dry-run на официальных fixtures.

# Материалы аудита 2026-09-08

Связанный [отчёт](../2026-09-08-code-audit.md). Все данные синтетические; production credentials и внешние аккаунты не использовались.

`api-regression.go.txt` и `session-regression.go.txt` содержат временные Go-тесты. Расширение `.txt` исключает их из обычного `go test ./...`: они намеренно проверяют желаемое защитное поведение и падают на найденных дефектах. Использовались существующие test doubles API; session-тест обращался к реальному repository и PostgreSQL.

API-тесты и кеш можно воспроизвести из корня репозитория с установленными Go 1.26.7 и frontend dependencies:

```bash
python3 - <<'PY'
import json
from pathlib import Path
root = Path.cwd()
evidence = root / 'docs/audits/2026-09-08-evidence'
overlay = {'Replace': {
    str(root / 'internal/app/api/audit_regression_test.go'):
        str(evidence / 'api-regression.go.txt'),
}}
Path('/tmp/torgnexa-audit-overlay.json').write_text(json.dumps(overlay))
PY
go test -overlay=/tmp/torgnexa-audit-overlay.json ./internal/app/api \
  -run '^TestAudit(PaymentWebhook|OIDCUnverified|RealtimeSurvives)' -count=1 -v
node docs/audits/2026-09-08-evidence/cache-repro.mjs
```

Ожидается падение четырёх новых API-тестов на исходном коде. В сохранённом API-журнале также есть существующий `TestAuditRouteUsesAuthorizedScope`, который попал под более широкий фильтр исходного запуска и прошёл.

Session-тест требует **пустую отдельную** базу `audit`, доступную через Unix-сокет `/tmp/torgnexa-audit-pg-socket` пользователем `postgres`. Он создаёт synthetic workspace, применяет DDL `migrations_legacy_pre_v1/000061_settings_security.sql` без migration_history insert, создаёт роль `auditapp` и задерживающий тестовый trigger. Подключение к существующей базе приложения для этого теста не подходит. В проведённом аудите использовался одноразовый Docker-контейнер PostgreSQL 18.6 с `--network none`, tmpfs для PGDATA и отдельным сокетом; контейнер удалён после проверки.

Для session-теста overlay дополнительно отображает путь `internal/platform/postgres/securitysettingsrepo/audit_session_test.go` на полный путь к `session-regression.go.txt`. Команда:

```bash
go test -overlay=/tmp/torgnexa-audit-overlay.json \
  ./internal/platform/postgres/securitysettingsrepo \
  -run '^TestAuditConcurrentFirstSessionObservation$' -count=1 -v
```

Журналы `api-regression.log.txt` и `session-regression.log.txt` сохраняют результаты воспроизведения. `go-test`, `go-vet`, `contracts`, `migrations`, `architecture`, `race`, `govulncheck` и `npm-audit` сохраняют результаты штатных проверок. Пустой `go-vet.log.txt` соответствует успешному запуску без диагностик. Проверка race ограничена API, securityedge, outbox и webhooks.

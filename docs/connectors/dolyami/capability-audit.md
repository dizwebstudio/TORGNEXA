# Аудит возможностей «Долями»

## Admitted

- tenant-scoped сохранение логина/пароля и mTLS-сертификата через
  SecretProvider;
- bounded HTTPS health-check к endpoint, заданному оператором;
- нормализация доступности и ошибок без сохранения ответа или секрета.

## Не admitted

`payments.create`, `payments.refund`, `payments.status.read` и
`payments.webhooks` присутствуют в манифесте как SDK-контракт, но не имеют
операционного маршрута в текущем runtime. Сверка, commit/cancel, проверка
подписи вебхуков и финансовые записи остаются закрытыми.

Для promotion потребуются демо- и боевой кабинет, зафиксированная версия
API, синтетические fixtures, проверка IP/подписи вебхука, идемпотентные
неопределённые записи и отдельный architecture review.

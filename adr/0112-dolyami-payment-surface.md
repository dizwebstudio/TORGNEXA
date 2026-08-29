# ADR 0112: «Долями» как health-only поверхность платежей

Status: Accepted

## Context

Официальная интеграция «Долями» требует партнёрские логин/пароль и mTLS-
сертификат, а боевой endpoint выдаётся после подключения магазина. В
репозитории нет подтверждённого демо-кабинета и payment bridge, который можно
безопасно объявить production-ready.

## Decision

Добавить `dolyami` в `connectors/payments` как `separate_surface` с
`health_only=true`. Проверка использует host-mediated mTLS/basic transport и
операторский HTTPS `probe_url`; платёжные операции и вебхуки остаются
закрытыми.

## Consequences

Карточка позволяет безопасно проверить конкретные merchant credentials, но не
создаёт ложного обещания принимать платежи. Promotion потребует fixtures,
идемпотентности, проверки webhook signature и отдельного review.

## Security and privacy impact

Сертификат и приватный ключ доступны только внутри SecretProvider callback;
сертификатный HTTP-клиент одноразовый и не переиспользует TLS-соединения между
tenant accounts. Публичный API, БД и события не меняются.

## Compatibility impact

Изменение аддитивно: публичные OpenAPI/event-контракты и Connector SDK v1 не
изменяются. Новая карточка использует существующую модель connector account и
отдельную finance surface.

## Migration and data impact

Миграция не требуется. Используются существующие account, SecretProvider и
история health-check; в базе сохраняются только ссылка на секрет и
нормализованный результат проверки.

## Operational impact

Оператор должен получить у персонального менеджера «Долями» логин/пароль,
mTLS-сертификат и endpoint демо- или боевого домена. Истечение сертификата
останавливает health-check и требует ротации секрета.

## Alternatives considered

Оставить сервис невидимым означало бы скрыть полезный контур проверки. Объявить
платежи рабочими без демо-кабинета и fixtures означало бы нарушить
runtime-truthful каталог. Поэтому выбран health-only admission с явным
qualification gate.

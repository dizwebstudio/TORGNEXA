# Task 158 — «Долями» в каталоге платежей

Status: Repository implementation complete

## Scope

- добавить карточку сервиса «Долями» в категорию «Платежи»;
- сохранить реальные требования API: логин/пароль и mTLS-сертификат;
- выполнить bounded health-check настроенного HTTPS endpoint через
  host-mediated transport;
- не объявлять создание, commit/cancel, refund, status или webhooks рабочими
  до отдельной квалификации.

## Acceptance criteria

- карточка «Долями» видна в каталоге и поддерживает зашифрованное подключение;
- runtime support — `separate_surface/finance/health_only`;
- манифест, policy, generated catalogs, docs и conformance evidence согласованы;
- сертификат и логин/пароль не попадают в логи, события или конфигурацию;
- Go/frontend/contract проверки проходят.

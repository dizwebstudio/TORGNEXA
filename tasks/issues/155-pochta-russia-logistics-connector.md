# Task 155 — «Почта России» logistics connector

Status: Repository implementation complete

## Problem

Каталог доставки не включал «Почту России», хотя её официальный сервис
«Отправка» предоставляет REST API для бизнес-отправителей и отдельный API
отслеживания. Пользователю нужна отдельная карточка без обещания операций,
которые ещё не прошли квалификацию.

## Scope

- новый `connectors/logistics/pochta-russia` пакет с SDK-манифестом и
  детерминированным conformance-кандидатом;
- проверка двух credential-значений официального API «Отправка»:
  application access token и user authorization key;
- host-side HTTPS probe `GET /1.0/settings` с фиксированным публичным
  доменом `otpravka-api.pochta.ru`;
- карточка «Почта России» на поверхности «Доставка», фирменная презентация и
  понятная подсказка формата credentials;
- runtime support `separate_surface/logistics`: кабинет и health-check
  доступны, тарифы, отправления, документы, возвраты и трекинг остаются
  fail-closed.

## Acceptance criteria

- «Почта России» отображается в разделе «Интеграции → Доставка»;
- токен и ключ остаются callback-scoped в SecretProvider и не попадают в
  логи, события или ответы приложения;
- health probe использует официальные заголовки `Authorization: AccessToken`
  и `X-User-Authorization: Basic` и проверяет корректный JSON-ответ;
- SDK-тесты и conformance-отчёт выполняются без сети и production secrets;
- generated catalogs, runtime-support contract, policy/review и документация
  синхронизированы.

## Qualification

Для включения расчёта тарифа, создания/отмены отправлений, печатных форм,
возвратов, пунктов выдачи и tracking нужны тестовый кабинет бизнес-отправителя,
актуальные обезличенные fixtures и проверка идемпотентного восстановления.

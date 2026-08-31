# Task 177: Почта России — возвратная этикетка

## Status

`repository-complete` — 2026-08-31.

## Objective

Закрыть fail-closed операцию возвратной этикетки Почты России через уже
допущенную границу `logistics.label.read`, не смешивая её с обычной формой
заказа и не передавая PDF за пределы host-side transport.

## Deliverables

- добавить отдельный формат `return_pdf` для RPO-barcode;
- вызвать официальный маршрут easy-return PDF с фиксированным `print-type=PAPER`
  и проверить HTTP, media type и сигнатуру PDF;
- вернуть только content-addressed opaque reference;
- добавить детерминированные тесты корректного маршрута и отказа на неверном RPO;
- синхронизировать matrix, спецификацию, capability audit, conformance plan,
  runtime catalog, ADR и architecture review.

## Scope limits

Обычная форма заказа (`pdf`) продолжает работать через backlog-форму. Отдельные
возвратные отправления, формирование партий, hand-off и прочие документы не
включаются этой задачей. Сетевой timeout и неоднозначный ответ остаются
ошибкой/unknown по существующей политике и не создают фиктивную этикетку.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, frontend generated-catalog checks and
`git diff --check`.

# Epic 171 — Маркировка и УПД

## Что является источником истины

PostgreSQL хранит локальное состояние batch/code/package/operation/document.
Remote observations и drifts неизменяемы. Kafka получает только envelope и
безопасную metadata; raw Data Matrix не попадает в события. Артефакты с
исходными кодами живут в отдельном защищённом хранилище с TTL и удаляются
после завершения print/remote шага.

## Рабочий процесс

```text
получение → резервирование → печать → сканирование → агрегация
    → УПД 5.03 → УКЭП/МЧД → ЭДО → ввод/вывод → reconciliation
```

Каждый remote write получает idempotency key. `timeout` после отправки — это
`unknown`, а не сигнал повторить запрос. Worker сначала выполняет status
read/reconciliation и только затем предлагает оператору retry. Любая запись
с capability `marking.*.write` проходит approval и audit.

## Операторские сценарии

- неправильный GTIN/SKU — скан отклоняется с `gtin_mismatch`;
- повторный скан — `duplicate`, количество не увеличивается;
- скан сверх задания — `overflow`, WMS task не закрывается;
- повторная печать — новая попытка print job, но код помечается использованным;
- отказ титула покупателя — документ `rejected`, формируется correction/manual
  attention, автоматическая повторная отправка запрещена;
- истёкший сертификат или отсутствующая МЧД — signing gate блокирует отправку;
- падение worker — lease/retry восстанавливает queued работу, unknown требует
  remote observation.

## Qualification

`171.15` использует только синтетические коды, GTIN/SKU и документы. Live
qualification для Chestny ZNAK, Diadoc, Saby EDO, KKT/OFD и marketplace
заказов/поставок не считается выполненной до получения официального
non-production контура и evidence, которое можно воспроизвести.

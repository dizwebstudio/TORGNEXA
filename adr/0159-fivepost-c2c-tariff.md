# ADR-0159 — тариф C2C 5Post

Status: Accepted

## Context

Официальный API Партнеров 5Post v7.32 описывает `POST
/api/v1/tariff/c2c`. Метод считает доставку между двумя точками 5BOX и
требует `pointFrom`, `pointTo` и массив грузомест с весом в миллиграммах.
Стоимость и сроки возвращаются в виде десятичной суммы и диапазона дней.

## Decision

Допустить для 5Post только bounded C2C rate preview через существующий
`logistics.rates.read` и нейтральный `POST /api/v1/logistics/rates`. Запрос
получает optional `from_point_ref`, `to_point_ref`, `calculate_date`,
`declared_value` и `payment_value`; транспорт требует от первых двух валидные
UUID и не выводит их из адресов.

Host вызывает официальный endpoint на фиксированном host, переводит вес из
граммов SDK в миллиграммы 5Post, суммирует `paymentWithVat` по транзакциям и
преобразует десятичные значения в RUB minor units без бинарной арифметики.
Диапазон доставки ограничен 3660 днями. Поля `maxDeliveryDays` и встречающийся
в документации вариант с кириллической `х` принимаются только как два имени
одного ответа.

## Security and privacy impact

API key и JWT остаются callback-scoped. В запрос уходят только адреса, размеры,
явные point UUID и необязательные суммы; raw tariff response и credentials не
выходят из host transport. Фиксированный HTTPS host, лимиты HTTP и точная
валидация ответа сохраняются.

## Compatibility impact

Изменение аддитивное: optional поля rate request не меняют существующие
перевозчики, а capability `logistics.rates.read` добавляется только 5Post.
Существующие callers без point refs продолжают работать для других carriers;
5Post без обязательных UUID отклоняется fail-closed.

## Migration and data impact

Миграция не требуется. Расчёт использует существующий account/SecretProvider и
операционный rate-preview маршрут; provider response не сохраняется как
долговечная бизнес-сущность.

## Operational impact

Оператор сначала получает UUID точек из bounded справочника ПВЗ и передаёт их
в форму тарифа. Коммерческие тарифы вне документированного C2C метода, курьерская
доставка и live qualification остаются отдельными границами.

## Alternatives considered

Подставлять адрес или город вместо UUID отвергнуто: официальный endpoint
адресует точки идентификаторами. Использовать `float64` отвергнуто: это может
изменить сумму в minor units. Оставить capability закрытой отвергнуто после
появления официального endpoint и deterministic fixture.

## Consequences

5Post теперь даёт проверяемый предпросмотр C2C тарифа в общем разделе
интеграции. Операция остаётся чтением и не создаёт отправление; прочие
provider-specific тарифы по-прежнему fail-closed.

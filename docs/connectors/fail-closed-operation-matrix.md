# Матрица fail-closed операций коннекторов

Матрица показывает разницу между capability, заявленной в `manifest.json`, и
операцией, допущенной в built-in runtime. Capability в колонке «закрыто» не
выдаётся UI и не попадает в worker-маршрут. Изменять её можно только вместе с
официальным контрактом провайдера, адаптером, детерминированными тестами и
qualification evidence.

## Доставка

| Коннектор | В runtime | Пока fail-closed |
|---|---|---|
| СДЭК | тарифы, отправление create/cancel, отказ с возвратом в интернет-магазин, клиентский возврат с тарифом, tracking, label, ПВЗ, проверка ORDER_STATUS webhook | — |
| Деловые Линии | тарифы, отправление create (address-to-address), заявка на отмену address-delivery, tracking, label, ПВЗ | терминальная отмена/ручной возврат |
| ПЭК | тарифы, bounded self-delivery shipment create, tracking, ПВЗ, отмена предварительного оформления, одиночная PDF-этикетка, печатная форма заявки | отмена сформированного груза/возврат, пакетная печать заявок |
| Почта России | партии (bounded read/archive-read/create/submit/archive/unarchive), тарифы, tracking, ПВЗ, label, возвратная этикетка для существующего RPO, создание одиночного заказа в backlog, cancel нового заказа, возврат для существующего RPO, отдельное возвратное отправление и его удаление | прочие операции партий, документов и возвратов |
| 5Post | проверка кабинета | тарифы, create/cancel, tracking, label, ПВЗ |
| Ozon Доставка | проверка кабинета | тарифы, create/cancel, tracking, label, ПВЗ |

## Магазины и маркетплейсы

| Группа | В runtime | Пока fail-closed |
|---|---|---|
| 1С-Битрикс, CS-Cart | товары, цены, остатки, заказы; для CS-Cart и статусы заказов | только операции, которых нет в манифесте/runtime-проекции; Bitrix webhooks требуют отдельного контракта |
| Magento | товары (обновление), цены, остатки, заказы, статусы, возвраты по заявленной проекции | создание товара без `attribute_set_id`/полей цены, `notifications.receive` |
| Medusa | товары (обновление), цены, остатки, заказы, статусы, возвраты по заявленной проекции | создание товара без обязательных цен варианта, `notifications.receive` |
| Saleor | товары (обновление), цены, остатки, заказы, статусы, возвраты и detached RS256/JWKS-webhooks по заявленной проекции | создание товара без product type/варианта/цены; legacy HMAC-webhooks с `secretKey` |
| Shopify | товары с exact-SKU create/update, цены, остатки, заказы, статусы, возвраты по заявленной проекции | переименование SKU, `notifications.receive` |
| Shopware | товары (обновление), цены, остатки, заказы, статусы, возвраты по заявленной проекции | создание товара без `taxId`/цены, `notifications.receive` |
| WooCommerce | товары, цены, остатки, заказы, статусы, возвраты, HMAC-webhooks | операции вне runtime-проекции |
| Yandex Market | товары, цены, остатки (чтение/передача), заказы, HMAC/токен-уведомления по runtime-проекции | операции вне runtime-проекции |
| PrestaShop, OpenCart | товары/цены/остатки/заказы и статусы по runtime-проекции | операции вне их manifest/runtime-проекций |
| Wildberries, Ozon, Megamarket, Magnit Market, AliExpress RU | только операции, отражённые в runtime support и worker bridge | остальные заявленные операции из manifest |
| Lamoda, М.Видео | health-check отдельной поверхности | товары, цены, остатки, заказы |

## Платежи, социальные и регулируемые контуры

| Группа | В runtime | Пока fail-closed |
|---|---|---|
| Robokassa | create, status, reconcile, refund, webhooks | — |
| YooKassa | create, status, refund, reconcile, webhooks | — |
| СБП | create, status, refund, reconcile, webhooks | live-квалификация эквайера |
| Telegram | текстовая, фото/альбомная и MP4-видеопубликация через released-upload bridge, HTTPS-кнопки для одиночного сообщения | edit/delete, webhooks |
| MAX | текстовая, фото/альбомная и видео-публикация через released-upload bridge, HTTPS-кнопки для публикаций, приём входящих webhooks через Inbox/outbox | edit/delete, управление подпиской webhook |
| AI, объявления, ЭДО, маркировка, гос-системы | health-check/отдельная поверхность согласно runtime support | domain-операции из manifest до отдельной qualification |

Источник истины для машинной проверки —
`contracts/connectors/builtin-runtime-support-v1.json` плюс манифесты
коннекторов. Для перевозчиков детали write-boundary находятся в
[qualification-документе](logistics-write-qualification.md), а для Dellin — в
[спецификации](dellin/spec.md).

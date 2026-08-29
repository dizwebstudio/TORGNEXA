# Аудит возможностей М.Видео

М.Видео добавлена в каталог как отдельная marketplace health-check surface.
SDK-пакет и карточка не означают, что у TORGNEXA уже есть согласованный
production seller/partner API или доступ к конкретному кабинету.

Admitted в текущем runtime:

- tenant-scoped сохранение API key через SecretProvider;
- bounded HTTPS health probe к явно настроенному оператором endpoint;
- безопасная нормализация доступности без хранения тела ответа.

Не admitted: товары, остатки, цены, заказы, публикация, webhooks и финансовые
операции. Их нельзя включить настройкой capability; требуется новая
квалификация провайдера и архитектурное review.

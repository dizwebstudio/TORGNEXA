# Аудит возможностей ПЭК

Официальная документация ПЭК подтверждает Basic-аутентификацию логином
личного кабинета и активным ключом доступа, JSON POST по HTTPS и методы
калькулятора, подачи заявок, статусов грузов и справочников филиалов.

Подтверждённые источники:

- [Аутентификация и протокол API ПЭК](https://test-kabinet.pecom.ru/preweb/api/v1) — Basic, HTTPS, JSON и POST;
- [Публичный API ПЭК](https://pecom.ru/business/developers/api_public/) — расчёт стоимости и список городов;
- [Операции с грузами](https://test-kabinet.pecom.ru/preweb/api/v1/help/cargos) — статусы грузов;
- [Заявки на забор](https://test-kabinet.pecom.ru/preweb/api/v1/help/cargopickup) — оформление заявок.

В production-каталоге доступны проверка credentials и bounded read-only
`pickup.points.read`. Чтение использует официальный `/branches/all/`, связывает
город с его division IDs и фильтрует склады по разрешённой операции выдачи.
Запись заявки, расчёт, автоматическая отмена, вебхуки и печатные формы не
включены в runtime support до получения тестового кабинета, актуальных fixtures
и проверки идемпотентности.

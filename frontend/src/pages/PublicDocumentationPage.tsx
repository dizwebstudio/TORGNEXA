import type {ReactNode} from "react";
import {connectorCatalog} from "../generated/connector-catalog";

export const documentationSections = [
  ["start", "Быстрый старт"],
  ["interface", "Интерфейс и навигация"],
  ["overview", "Обзор"],
  ["catalog-orders", "Каталог и заказы"],
  ["inventory-incidents", "Остатки и инциденты"],
  ["integrations", "Интеграции"],
  ["social", "Публикации"],
  ["sync", "Синхронизация"],
  ["master-data", "Контрагенты и финансы"],
  ["control", "Согласования и документы"],
  ["monitoring", "Уведомления, отчёты и аудит"],
  ["settings", "Настройки"],
  ["automation", "AI, MCP и плагины"],
  ["developer", "API и расширения"],
  ["security", "Доступ и безопасность"],
  ["environment", "Переменные .env"],
  ["operations", "Эксплуатация"],
  ["troubleshooting", "Решение проблем"],
] as const;

type RuntimeEntry = (typeof connectorCatalog)[number];
type RuntimeFilter = (entry: RuntimeEntry) => boolean;

const runtimeGroups: readonly {title: string; copy: string; filter: RuntimeFilter}[] = [
  {title: "Готово в «Интеграциях»", copy: "Карточки можно подключить, проверить, активировать и использовать в общем контуре каталога товаров.", filter: entry => entry.runtime.stage === "ready"},
  {title: "Провайдеры ИИ", copy: "Рабочая генерация текста находится в настройках провайдеров ИИ и отчётах, а не в обычной синхронизации кабинетов.", filter: entry => entry.runtime.surface === "ai_providers"},
  {title: "Финансы и платежи", copy: "Курсы валют, платёжные шлюзы и подготовленные отдельные поверхности используют свои операционные маршруты.", filter: entry => entry.runtime.surface === "finance"},
  {title: "Доставка", copy: "Для перевозчиков текущая среда разрешает безопасное подключение и проверку доступности; отправления включаются после квалификации API.", filter: entry => entry.runtime.surface === "logistics"},
  {title: "Публикации и CRM", copy: "Social Core принимает текстовые публикации, а Bitrix24 работает в отдельном CRM-контуре без подмены продуктовой синхронизации.", filter: entry => (entry.runtime.surface === "social" && !entry.runtime.healthOnly) || entry.runtime.surface === "crm"},
  {title: "Проверка доступности по категориям", copy: "Маркетплейсы, объявления, социальные сети, ЭДО и госсистемы можно подключить и проверить по официальному API. Доменные операции остаются закрыты до отдельной квалификации.", filter: entry => entry.runtime.healthOnly === true},
];

function connectorNames(filter: RuntimeFilter): string {
  return connectorCatalog.filter(filter).map(entry => entry.name).join(", ");
}

function RuntimeMatrix() {
  const counts = {
    ready: connectorCatalog.filter(entry => entry.runtime.stage === "ready").length,
    separate: connectorCatalog.filter(entry => entry.runtime.stage === "separate_surface").length,
    planned: connectorCatalog.filter(entry => entry.runtime.stage === "planned").length,
  };
  return <div className="docs-runtime-panel">
    <div className="docs-runtime-summary" aria-label="Сводка состояния среды">
      <article><strong>{counts.ready}</strong><span>готовых коннекторов</span></article>
      <article><strong>{counts.separate}</strong><span>отдельных поверхностей</span></article>
      <article><strong>{counts.planned}</strong><span>в плане</span></article>
    </div>
    <div className="docs-runtime-groups">
      {runtimeGroups.map(group => <article key={group.title}><h3>{group.title}</h3><p>{group.copy}</p><small>{connectorNames(group.filter)}</small></article>)}
    </div>
    <p className="docs-runtime-note">Счётчики и состав берутся из сгенерированного каталога среды. «Готово» означает исполняемый маршрут текущей сборки, а не только наличие SDK или манифеста.</p>
  </div>;
}

function IntegrationConnectionGuide() {
  return <>
    <h3>Пошаговое подключение</h3>
    <p>Все действия начинаются в рабочем контуре после входа: откройте «Настройки → Интеграции», выберите семейство и карточку провайдера. У каждой карточки есть собственная панель; в ней не смешиваются секреты, несекретные параметры и разрешённые возможности.</p>
    <ol className="docs-steps">
      <li><strong>Подготовьте доступ</strong><span>Нужны права на чтение и создание connector account. Для внешней системы заранее создайте технического пользователя с минимальными scope и разрешите исходящий HTTPS только к официальному API.</span></li>
      <li><strong>Создайте логический кабинет</strong><span>Нажмите «Добавить кабинет» и задайте стабильный идентификатор, например <code>shop-ru-main</code>. Это внутренний ID для связей и аудита, а не ID кабинета у провайдера.</span></li>
      <li><strong>Сохраните учётные данные</strong><span>Вставьте только секретный материал из подсказки карточки и нажмите «Сохранить зашифрованно». После сохранения секрет повторно не показывается и не попадает в URL, события или журналы.</span></li>
      <li><strong>Заполните параметры среды</strong><span>Если карточка показывает «Параметры среды · без секретов», отредактируйте JSON: адрес, портал, канал, склад или сопоставление. Токены, секрет клиента и закрытый ключ в этот блок не помещаются.</span></li>
      <li><strong>Пройдите «Проверить»</strong><span>Проверка выполняет реальный запрос к официальному адресу доступности и сохраняет нормализованный результат. До состояния <code>healthy</code> активация и пробный запуск недоступны.</span></li>
      <li><strong>Включите работающие возможности</strong><span>Раскройте «Разрешённые возможности», оставьте только нужные операции и сохраните. Запись во внешнюю систему требует разрешения, политики и при необходимости согласования; возможности только из манифеста не включаются.</span></li>
      <li><strong>Активируйте кабинет</strong><span>Кнопка «Включить» появляется после состояния <code>healthy</code> и хотя бы одной реально поддерживаемой возможности. Для карточек только с проверкой доступности доменные операции не выдаются.</span></li>
      <li><strong>Выполните предварительную проверку и импорт</strong><span>Нажмите «Пробный запуск», проверьте число политик, чтений и записей, затем запустите «Первоначальный импорт». После проверки включите расписание.</span></li>
    </ol>

    <h3>Как передавать учётные данные</h3>
    <div className="docs-tab-guide">
      <article><strong>OAuth 2.0</strong><span>Сохраните OAuth client JSON, нажмите «Войти» и завершите вход у провайдера. Access/refresh token хранит и обновляет сервер. «Войти снова» означает, что доступ отозван или refresh token больше не принят.</span></article>
      <article><strong>Ключ API / токен Bearer</strong><span>Вставьте ключ или токен из кабинета провайдера. Не добавляйте вручную <code>Authorization</code> и не используйте один рабочий ключ между рабочими пространствами.</span></article>
      <article><strong>Логин и пароль / сертификат</strong><span>Передайте логин-пароль или материал сертификата строго в формате подсказки карточки. Секреты шифруются через хранилище секретов; закрытый ключ не передаётся в интерфейс или фоновый процесс открытым текстом.</span></article>
      <article><strong>Без авторизации</strong><span>Банк России не создаёт кабинет рабочего пространства: фоновый процесс получает официальный ежедневный документ и сохраняет неизменяемый факт курса в разделе «Финансы».</span></article>
    </div>

    <div className="docs-split">
      <div><h3>Параметры среды</h3><ul><li><code>store_host</code>, <code>base_path</code>, <code>store_currency</code> — интернет-магазин и связка с ERP.</li><li><code>portal_host</code> — Bitrix24; <code>shop_domain</code> — Shopify.</li><li><code>channel</code> и <code>warehouse</code> — Saleor.</li><li><code>business_id</code>, <code>campaign_id</code>, <code>inventory_mode</code>, <code>price_mode</code> — Яндекс Маркет.</li><li><code>chat_id</code> — Telegram/MAX; токен бота при этом остаётся учётными данными.</li></ul></div>
      <div><h3>Версионность и повтор</h3><ul><li>Создание, учётные данные, разрешение, проверка, включение и расписание используют ожидаемую версию кабинета.</li><li>Конфликт версии означает, что другой оператор уже изменил кабинет: обновите панель и повторите действие.</li><li>Повторная отправка с тем же ключом идемпотентности безопасна; не создавайте новую операцию, пока первая имеет неизвестный результат.</li></ul></div>
    </div>

    <h3>Рабочие шаблоны текущих карточек</h3>
    <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Карточка</th><th>Учётные данные</th><th>Параметры среды / результат проверки</th></tr></thead><tbody>
      <tr><td><strong>1С-Битрикс</strong></td><td><code>{`{ user_id, webhook_code }`}</code></td><td><code>store_host</code>, <code>base_path</code>, <code>catalog_iblock_id</code>, <code>store_currency</code>; товары read/write.</td></tr>
      <tr><td><strong>CS-Cart</strong></td><td><code>{`{ email, api_key }`}</code></td><td><code>store_host</code>, <code>base_path</code>, <code>store_currency</code>; официальный REST API 2.0, товары read/write.</td></tr>
      <tr><td><strong>OpenCart</strong></td><td>Bearer token модуля TORGNEXA</td><td><code>store_host</code>, <code>base_path</code>, <code>store_currency</code>; сначала установите <code>torgnexa.ocmod.zip</code> — он добавляет <code>extension/torgnexa/api/*</code>, затем доступны товары read/write.</td></tr>
      <tr><td><strong>Shopify</strong></td><td>OAuth client JSON</td><td><code>shop_domain</code>, <code>store_currency</code>; OAuth с host-owned refresh, товары read/write.</td></tr>
      <tr><td><strong>Bitrix24 CRM</strong></td><td>OAuth client JSON</td><td><code>portal_host</code>; лиды, сделки, контакты, компании и товарные строки в отдельном CRM-контуре.</td></tr>
      <tr><td><strong>Telegram / MAX</strong></td><td>Токен бота</td><td>Числовой <code>chat_id</code> в параметрах среды; рабочий сценарий — текстовые публикации, лимиты 4096 / 4000.</td></tr>
      <tr><td><strong>СДЭК / Деловые Линии / ПЭК / 5Post / Ozon Доставка / Почта России</strong></td><td>JSON учётных данных провайдера</td><td>Создание кабинета и официальная проверка доступности; отправления, тарифы и этикетки не включаются автоматически.</td></tr>
      <tr><td><strong>СБП / YooKassa / Robokassa</strong></td><td>JSON учётных данных провайдера</td><td>Отдельный раздел «Финансы»; разрешения различаются, возврат доступен только при <code>payments.refund</code>.</td></tr>
    </tbody></table></div>
    <Callout title="Не угадывайте формат JSON" tone="warning">Подсказка placeholder в drawer является частью контракта карточки. Если провайдер требует поля, которых нет в подсказке, сначала обновите manifest/connector spec и квалификацию, а не обходите форму произвольным payload.</Callout>

    <h3>Что означает «только проверка доступности»</h3>
    <p>Lamoda, М.Видео, Auto.ru, Avito, CIAN, Chestny ZNAK, Diadoc, EGAIS, Instagram, Odnoklassniki, RUTUBE, Saby EDO, Threads, VetIS/Mercury, VK и YouTube сейчас можно подключить в своей категории, сохранить учётные данные и проверить официальный адрес API. Это подтверждает доступ к API, но не включает синхронизацию товаров, цены, остатки, заказы, публикацию, сообщения, ЭДО или регулируемую запись; рабочая связка появится только после отдельной квалификации.</p>
  </>;
}

function OpenCartDockerGuide() {
  return <div className="docs-opencart-guide" id="opencart-smoke">
    <h3>OpenCart: проверка bridge в Docker Compose</h3>
    <p>Для проверки интеграции без внешнего магазина используйте изолированный стенд OpenCart 4.1.0.4 + MariaDB. Он собирается из <code>docker-compose.opencart-test.yml</code>, устанавливает модуль <code>torgnexa.ocmod.zip</code> из текущих исходников и загружает синтетические товары и заказ. Повторный запуск <code>up</code> с сохранённым томом восстанавливает конфигурацию и не запускает установку из командной строки заново. Рабочие ключи в стенд не попадают.</p>
    <ol className="docs-steps compact">
      <li><strong>Соберите и запустите</strong><span>Выполните команды из корня репозитория. Первый запуск скачает проверенный SHA-256 архив OpenCart и создаст схему.</span><pre><code>{`docker compose -f docker-compose.opencart-test.yml up -d --build\ndocker compose -f docker-compose.opencart-test.yml ps`}</code></pre></li>
      <li><strong>Запустите smoke</strong><span>Скрипт проверит health, каталог, SKU, цену, остаток, создание товара, заказы и идемпотентность.</span><pre><code>scripts/opencart-smoke.sh</code></pre></li>
      <li><strong>Разберите ошибку</strong><span>Логи и ручной health-запрос помогают отделить проблему Compose, модуля и маршрута.</span><pre><code>{`docker compose -f docker-compose.opencart-test.yml logs --tail=200 opencart\ncurl -i -H 'Authorization: Bearer torgnexa-demo-bridge-token-2026' \\\n  'http://127.0.0.1:8095/index.php?route=extension/torgnexa/api/health'`}</code></pre></li>
      <li><strong>Удалите стенд</strong><span>После проверки удалите контейнеры и synthetic volume, чтобы маленький VPS не удерживал лишние данные.</span><pre><code>docker compose -f docker-compose.opencart-test.yml down -v</code></pre></li>
    </ol>
    <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Демо-объект</th><th>Ожидаемое значение</th><th>Что проверяет</th></tr></thead><tbody>
      <tr><td><code>DEMO-COFFEE-001</code></td><td>quantity 24, price 1499.90 USD</td><td>Чтение каталога и поиск по SKU</td></tr>
      <tr><td><code>DEMO-TEA-002</code></td><td>quantity 8, price 799.00 USD</td><td>Пагинация и проекция товара</td></tr>
      <tr><td>Order <code>9001</code></td><td>одна строка кофе</td><td>Список, статус и повторное чтение заказа</td></tr>
    </tbody></table></div>
    <Callout title="Только локальная проверка" tone="warning">Адрес <code>127.0.0.1:8095</code>, токен Bearer <code>torgnexa-demo-bridge-token-2026</code> и пароли MariaDB — синтетические значения для теста. Не публикуйте порт и не переносите эти учётные данные в рабочую среду.</Callout>
    <figure><img src="/docs/opencart-smoke.png" alt="Страница документации TORGNEXA с инструкцией OpenCart Docker smoke-test"/><figcaption>Инструкция доступна в публичной документации: команды, ожидаемые проверки и очистка тестового стека.</figcaption></figure>
    <figure><img src="/docs/opencart-store.png" alt="Демо-магазин OpenCart с синтетическими товарами TORGNEXA"/><figcaption>Демо-магазин после seed: три синтетических товара видны через обычный поиск OpenCart.</figcaption></figure>
    <p>Полный текст, список endpoint-проверок и совместимость со схемой OpenCart 4.1 находятся в <code>docs/connectors/opencart/docker-smoke.md</code>.</p>
  </div>;
}

function WooCommerceDockerGuide() {
  return <div className="docs-opencart-guide" id="woocommerce-smoke">
    <h3>WooCommerce: проверка REST API в Docker Compose</h3>
    <p>Для проверки WooCommerce без внешнего магазина используйте изолированный стенд WordPress 6.8.2 + WooCommerce 9.8.5 + MariaDB. Он создаёт синтетический каталог, заказ и отдельную тестовую пару Consumer Key/Consumer Secret. REST-запросы идут по TLS с самоподписанным сертификатом только внутри локального стенда.</p>
    <ol className="docs-steps compact">
      <li><strong>Соберите и запустите</strong><span>Команды выполняются из корня репозитория. Первый запуск устанавливает WordPress и активирует WooCommerce.</span><pre><code>{`docker compose -f docker-compose.woocommerce-test.yml up -d --build\ndocker compose -f docker-compose.woocommerce-test.yml ps`}</code></pre></li>
      <li><strong>Запустите smoke</strong><span>Скрипт проверит Basic Auth, каталог, SKU, цену, управляемый остаток, заказ, статус и endpoint возвратов.</span><pre><code>scripts/woocommerce-smoke.sh</code></pre></li>
      <li><strong>Откройте демо-витрину</strong><span>Товары доступны в обычной HTTP-витрине, а REST API проверяется отдельно по HTTPS.</span><pre><code>{`http://127.0.0.1:8096/shop/\nhttps://127.0.0.1:8446/wp-json/wc/v3`}</code></pre></li>
      <li><strong>Удалите стенд</strong><span>После проверки удалите контейнеры и synthetic volume, чтобы маленький VPS не удерживал лишнюю базу.</span><pre><code>docker compose -f docker-compose.woocommerce-test.yml down -v</code></pre></li>
    </ol>
    <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Демо-объект</th><th>Ожидаемое значение</th><th>Что проверяет</th></tr></thead><tbody>
      <tr><td><code>TORGNEXA-WOO-COFFEE</code></td><td>quantity 24, price 1499.90 USD</td><td>Каталог и поиск по SKU</td></tr>
      <tr><td><code>TORGNEXA-WOO-TEA</code></td><td>quantity 8, price 799.00 USD</td><td>Пагинация и проекция товара</td></tr>
      <tr><td>Синтетический заказ</td><td>две единицы кофе</td><td>Список и изменение статуса заказа</td></tr>
    </tbody></table></div>
    <Callout title="Только локальная проверка" tone="warning">Порты <code>8096</code>/<code>8446</code>, самоподписанный TLS-сертификат, ключ и секрет потребителя, а также пароли MariaDB — синтетические значения. Не публикуйте их и не переносите в рабочую среду.</Callout>
    <figure><img src="/docs/woocommerce-guide.png" alt="Страница документации TORGNEXA с инструкцией WooCommerce Docker smoke-test"/><figcaption>Инструкция запуска, проверки и очистки WooCommerce-стенда.</figcaption></figure>
    <figure><img src="/docs/woocommerce-store.png" alt="Демо-магазин WooCommerce с синтетическими товарами TORGNEXA"/><figcaption>Демо-витрина WooCommerce после загрузки синтетических товаров.</figcaption></figure>
    <p>Полный текст и ограничения квалификации находятся в <code>docs/connectors/woocommerce/docker-smoke.md</code>. Рабочий процесс TORGNEXA сейчас маршрутизирует только сущность <code>products</code>; дополнительные REST-возможности не расширяют среду автоматически.</p>
  </div>;
}

function PrestaShopDockerGuide() {
  return <div className="docs-opencart-guide" id="prestashop-smoke">
    <h3>PrestaShop: проверка Webservice API в Docker Compose</h3>
    <p>Для проверки PrestaShop без внешнего магазина используйте изолированный стенд на официальном образе PrestaShop 8.1 + MariaDB. Скрипт инициализации включает штатный Webservice API, создаёт синтетические товары и ограниченный ключ API. Чтения идут через официальный вывод JSON, записи — через XML PATCH; рабочие учётные данные в стенд не попадают.</p>
    <ol className="docs-steps compact">
      <li><strong>Соберите и запустите</strong><span>Команды выполняются из корня репозитория. Первый запуск установит магазин и подготовит контейнер Webservice Symfony до проверки доступности.</span><pre><code>{`docker compose -f docker-compose.prestashop-test.yml up -d --build; docker compose -f docker-compose.prestashop-test.yml ps`}</code></pre></li>
      <li><strong>Запустите smoke</strong><span>Скрипт проверит Basic Auth, каталог, reference, чтение товара, XML-изменение цены и StockAvailable, а также ресурс заказов.</span><pre><code>scripts/prestashop-smoke.sh</code></pre></li>
      <li><strong>Откройте API вручную</strong><span>Webservice API использует ключ как имя пользователя Basic Auth; пароль оставьте пустым.</span><pre><code>{`curl --globoff -u '0123456789abcdef0123456789abcdef:' http://127.0.0.1:8097/api/products?output_format=JSON`}</code></pre></li>
      <li><strong>Удалите стенд</strong><span>После проверки удалите контейнеры и synthetic volume, чтобы небольшой VPS не удерживал тестовую базу.</span><pre><code>docker compose -f docker-compose.prestashop-test.yml down -v</code></pre></li>
    </ol>
    <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Демо-объект</th><th>Ожидаемое значение</th><th>Что проверяет</th></tr></thead><tbody>
      <tr><td><code>TORGNEXA-PS-COFFEE</code></td><td>quantity 24, price 1499.90 EUR</td><td>Каталог, reference и price PATCH</td></tr>
      <tr><td><code>TORGNEXA-PS-TEA</code></td><td>quantity 8, price 799.00 EUR</td><td>Пагинация и локализация названия</td></tr>
      <tr><td><code>StockAvailable</code></td><td>quantity 37 после smoke</td><td>Авторитетный остаток PrestaShop и XML PATCH</td></tr>
    </tbody></table></div>
    <Callout title="Только локальная проверка" tone="warning">Порт <code>8097</code>, ключ API <code>0123456789abcdef0123456789abcdef</code> и пароли MariaDB — синтетические значения. Не публикуйте адрес и не переносите эти учётные данные в рабочую среду.</Callout>
    <figure><img src="/docs/prestashop-guide.png" alt="Страница документации TORGNEXA с инструкцией PrestaShop Webservice smoke-test"/><figcaption>Инструкция запуска, проверки официального Webservice API и очистки стенда.</figcaption></figure>
    <figure><img src="/docs/prestashop-store.png" alt="Демо-магазин PrestaShop с синтетическими товарами TORGNEXA"/><figcaption>Демо-витрина PrestaShop после seed: синтетические товары доступны через обычный storefront.</figcaption></figure>
    <p>Полный список запросов, ограничения ключа и формат официальных JSON/XML ответов находятся в <code>docs/connectors/prestashop/docker-smoke.md</code>. Рабочий процесс TORGNEXA по-прежнему маршрутизирует только сущность <code>products</code>; возможности манифеста не превращаются в автоматическую синхронизацию.</p>
  </div>;
}

function SaleorDockerGuide() {
  return <div className="docs-opencart-guide" id="saleor-smoke">
    <h3>Saleor: проверка GraphQL API в Docker Compose</h3>
    <p>Для проверки Saleor без внешнего магазина используйте изолированный официальный образ Saleor Platform 3.23, PostgreSQL и Valkey. Bootstrap выполняет миграции и загружает демо-каталог с локальным администратором; Dashboard, worker и storefront выключены, чтобы не перегружать небольшую VPS.</p>
    <ol className="docs-steps compact">
      <li><strong>Соберите и запустите</strong><span>Команды выполняются из корня репозитория. API будет доступен только локально на порту 18000.</span><pre><code>docker compose -f docker-compose.saleor-test.yml up -d; docker compose -f docker-compose.saleor-test.yml ps</code></pre></li>
      <li><strong>Получите временный токен</strong><span>Disposable seed создаёт <code>admin@example.com</code> / <code>admin</code>. Токен нужен только для локального smoke и не должен попадать в логи.</span><pre><code>tokenCreate(email: &quot;admin@example.com&quot;, password: &quot;admin&quot;)</code></pre></li>
      <li><strong>Запустите credentialed smoke</strong><span>Проверяются GraphQL auth errors (HTTP 200), каталог и SKU, channel/warehouse, записи товара, цены и остатков, read-after-write и автоматический cleanup.</span><pre><code>SALEOR_GRAPHQL_URL=http://127.0.0.1:18000/graphql/ SALEOR_TEST_SKU=111223580 SALEOR_CHANNEL=default-channel SALEOR_WAREHOUSE=default SALEOR_ALLOW_HTTP=1 SALEOR_ALLOW_WRITES=1 scripts/saleor-smoke.sh</code></pre></li>
      <li><strong>Удалите стенд</strong><span>После проверки удалите только Saleor-контейнеры и synthetic volumes.</span><pre><code>docker compose -f docker-compose.saleor-test.yml down -v</code></pre></li>
    </ol>
    <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Демо-объект</th><th>Значение проверки</th><th>Что подтверждает</th></tr></thead><tbody>
      <tr><td><code>111223580</code></td><td>SKU, Darko Polo</td><td>productVariants read и mapping variant/product</td></tr>
      <tr><td><code>default-channel</code></td><td>USD, price read/write</td><td>channel resolution и публикация</td></tr>
      <tr><td><code>default</code></td><td>stock read/write</td><td>warehouse resolution и reconciliation</td></tr>
    </tbody></table></div>
    <Callout title="Только локальная проверка" tone="warning">Порт <code>18000</code>, секретный ключ Saleor и учётные данные администратора — синтетические. Для внешнего тестового стенда используйте HTTPS, ограниченный токен приложения и отдельные тестовые SKU, канал и склад. Создание товара и входящие вебхуки остаются запрещены при ошибке по текущему Connector SDK.</Callout>
    <p>Полная процедура и машиночитаемый результат находятся в <code>docs/connectors/saleor/docker-live-qualification.md</code> и <code>docs/connectors/saleor/live-qualification-status.json</code>. Docker smoke прошёл 2026-08-29; merchant staging требует отдельной проверки.</p>
  </div>;
}

function ShopifyDockerGuide() {
  return <div className="docs-opencart-guide" id="shopify-smoke">
    <h3>Shopify: protocol smoke и Dev Store qualification</h3>
    <p>Shopify — SaaS и не поставляет self-hosted Docker-магазин. Поэтому локальный Compose запускает только stateful protocol double для проверки Admin REST request/response shapes, авторизации, записей и reconciliation. Это не merchant store.</p>
    <ol className="docs-steps compact">
      <li><strong>Запустите protocol double</strong><span>Выполните команды из корня репозитория; endpoint доступен только на loopback.</span><pre><code>docker compose -f docker-compose.shopify-test.yml up -d; docker compose -f docker-compose.shopify-test.yml ps</code></pre></li>
      <li><strong>Запустите smoke</strong><span>Проверяются access rejection, API version 2026-07, каталог, locations, orders/refunds, product/price/inventory writes, read-after-write и cleanup.</span><pre><code>SHOPIFY_BASE_URL=http://127.0.0.1:18001 SHOPIFY_API_TOKEN=shopify-local-token SHOPIFY_TEST_SKU=TORGNEXA-SHOPIFY-001 SHOPIFY_ALLOW_HTTP=1 SHOPIFY_ALLOW_WRITES=1 scripts/shopify-smoke.sh</code></pre></li>
      <li><strong>Проверьте реальный Dev Store</strong><span>Для merchant qualification создайте Shopify Dev Store и app token в Dev Dashboard, затем используйте HTTPS и синтетический SKU.</span><pre><code>SHOPIFY_BASE_URL=https://your-shop.myshopify.com SHOPIFY_API_TOKEN=token SHOPIFY_TEST_SKU=TORGNEXA-SHOPIFY-SMOKE scripts/shopify-smoke.sh</code></pre></li>
      <li><strong>Удалите double</strong><span>После локальной проверки удалите только тестовый контейнер и сеть.</span><pre><code>docker compose -f docker-compose.shopify-test.yml down -v</code></pre></li>
    </ol>
    <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Режим</th><th>Что подтверждает</th><th>Статус</th></tr></thead><tbody>
      <tr><td>Docker protocol double</td><td>REST contract, auth, product/price/inventory reconciliation</td><td>PASS 2026-08-29</td></tr>
      <tr><td>Shopify Dev Store</td><td>Реальные scopes, OAuth token, merchant data/API limits</td><td>Требует credentials</td></tr>
    </tbody></table></div>
    <Callout title="Важно" tone="warning">Admin REST API Shopify считается legacy для новых приложений. Коннектор закреплён на API <code>2026-07</code> и не принимает молчаливый fall-forward. Product create, webhooks и необратимые order status writes остаются закрытыми.</Callout>
    <p>Полная инструкция и статус qualification находятся в <code>docs/connectors/shopify/docker-live-qualification.md</code> и <code>docs/connectors/shopify/live-qualification-status.json</code>.</p>
  </div>;
}

const routes = [
  ["Обзор", "/", "Показатели, онбординг и состояние операционного контура"],
  ["Каталог", "/catalog", "Товары, предложения, категории и изображения"],
  ["Заказы", "/orders", "Поиск, статусы и подробности заказов"],
  ["Остатки", "/inventory", "Позиции, склады, инциденты, fulfillment и импорт"],
  ["Инциденты", "/incidents", "Отклонения, сбои складов и действия оператора"],
  ["Интеграции", "/integrations", "Подключения маркетплейсов и внешних систем"],
  ["Публикации", "/social", "Текстовые публикации в подключённые социальные каналы"],
  ["Синхронизация", "/sync", "Политики, запуски и расхождения"],
  ["Контрагенты", "/counterparties", "Единый справочник юридических лиц и ролей"],
  ["Финансы", "/finance", "Расчёты площадок, курсы валют и платежи"],
  ["Согласования", "/approvals", "Политики и выполнение чувствительных операций"],
  ["Сертификаты и документы", "/compliance", "Разрешительные документы и запросы приватности"],
  ["Уведомления", "/notifications", "Ошибки, предупреждения и системные события"],
  ["Отчёты", "/reports", "Аналитика, фильтры и экспорт"],
  ["Аудит", "/audit", "История привилегированных действий"],
  ["Настройки", "/settings", "Рабочее пространство, доступ, автоматизация и безопасность"],
] as const;

const environmentGroups = [
  {
    title: "Версия и журналирование",
    rows: [
      ["TORGNEXA_VERSION", "0.1.0-dev", "Версия сборки и метка миграций. Для релиза укажите фактическую SemVer-версию."],
      ["TORGNEXA_LOG_LEVEL", "info", "Уровень журнала: debug, info, warn или error."],
    ],
  },
  {
    title: "Обязательные секреты",
    rows: [
      ["TORGNEXA_SECRETS_MASTER_KEY", "генерируется", "Base64 от 32 случайных байт. Ключ шифрования секретов интеграций."],
      ["POSTGRES_PASSWORD", "генерируется", "Пароль владельца PostgreSQL для миграций и bootstrap-операций."],
      ["TORGNEXA_APP_DB_PASSWORD", "генерируется", "Пароль ограниченной прикладной роли PostgreSQL."],
      ["KEYCLOAK_DB_PASSWORD", "генерируется", "Пароль отдельной базы и роли Keycloak."],
      ["KEYCLOAK_ADMIN_USERNAME", "admin", "Имя локального bootstrap-администратора Keycloak."],
      ["KEYCLOAK_ADMIN_PASSWORD", "генерируется", "Пароль bootstrap-администратора Keycloak."],
      ["GARAGE_RPC_SECRET", "генерируется", "64 шестнадцатеричных символа для внутреннего RPC Garage."],
      ["GARAGE_ADMIN_TOKEN", "генерируется", "Административный токен Garage."],
      ["GARAGE_METRICS_TOKEN", "генерируется", "Токен доступа к метрикам Garage."],
      ["S3_ACCESS_KEY", "генерируется", "Идентификатор доступа к S3-совместимому хранилищу."],
      ["S3_SECRET_KEY", "генерируется", "Секрет соответствующего S3-ключа."],
      ["CLICKHOUSE_PASSWORD", "генерируется", "Пароль пользователя аналитической базы ClickHouse."],
    ],
  },
  {
    title: "Хранилище и аналитика",
    rows: [
      ["S3_BUCKET", "torgnexa", "DNS-безопасное имя bucket: строчные буквы, цифры, точки и дефисы."],
      ["TORGNEXA_S3_REQUEST_TIMEOUT", "30s", "Таймаут S3-запроса: от 1s до 2m."],
      ["CLICKHOUSE_USERNAME", "torgnexa", "Пользователь ClickHouse. Не меняйте отдельно от сохранённого volume."],
      ["TORGNEXA_CLICKHOUSE_QUERY_TIMEOUT", "5s", "Таймаут аналитического запроса: от 100ms до 30s."],
    ],
  },
  {
    title: "Порты хоста",
    rows: [
      ["POSTGRES_PORT", "5432", "PostgreSQL на 127.0.0.1."],
      ["KAFKA_PORT", "9092", "Kafka на 127.0.0.1."],
      ["VALKEY_PORT", "6379", "Valkey на 127.0.0.1."],
      ["CLICKHOUSE_HTTP_PORT", "8123", "ClickHouse HTTP на 127.0.0.1."],
      ["CLICKHOUSE_NATIVE_PORT", "9000", "Нативный протокол ClickHouse."],
      ["S3_PORT", "9002", "S3 API Garage."],
      ["KEYCLOAK_PORT", "8081", "Публичный локальный порт Keycloak."],
      ["TORGNEXA_API_PORT", "8080", "REST API TORGNEXA."],
      ["TORGNEXA_MCP_PORT", "8090", "MCP transport."],
      ["TORGNEXA_FRONTEND_PORT", "5173", "Web-интерфейс TORGNEXA."],
      ["TORGNEXA_DOCKER_NETWORK_MTU", "1376", "MTU внутренней Docker-сети. Увеличивайте только если путь до внешних API гарантированно поддерживает большее значение."],
    ],
  },
  {
    title: "Пул PostgreSQL",
    rows: [
      ["TORGNEXA_DB_MAX_OPEN_CONNS", "20", "Максимум открытых соединений: 1–1000."],
      ["TORGNEXA_DB_MAX_IDLE_CONNS", "10", "Неактивные соединения: от 0 до MAX_OPEN_CONNS."],
      ["TORGNEXA_DB_CONN_MAX_LIFETIME", "30m", "Время жизни соединения: от 1m до 24h."],
      ["TORGNEXA_DB_CONN_MAX_IDLE_TIME", "5m", "Допустимый простой: от 1s до 1h."],
      ["TORGNEXA_DB_CONNECT_TIMEOUT", "5s", "Таймаут подключения: от 100ms до 1m."],
    ],
  },
  {
    title: "OIDC и worker",
    rows: [
      ["TORGNEXA_OIDC_MANAGED_ISSUER_HOSTS", "пусто", "CSV allowlist внешних issuer host без схемы и пути. Пусто означает default deny."],
      ["TORGNEXA_KAFKA_TOPIC_PARTITIONS", "1", "Количество partitions для создаваемых Kafka topics. Для одновузлового Community оставьте 1."],
      ["TORGNEXA_KAFKA_TOPIC_REPLICATION_FACTOR", "1", "Replication factor Kafka topics. Для одновузлового Community оставьте 1."],
      ["TORGNEXA_KAFKA_CONSUMER_GROUP", "torgnexa.webhooks.v1", "Общее имя consumer group для одного логического worker-кластера."],
      ["TORGNEXA_WORKER_POLL_INTERVAL", "500ms", "Интервал опроса: от 50ms до 30s."],
      ["TORGNEXA_WORKER_DISPATCH_BATCH", "32", "Размер пачки: от 1 до 1000."],
      ["TORGNEXA_WORKER_LEASE", "90s", "Lease обработки: от 10s до 10m."],
      ["TORGNEXA_WORKER_RECONCILIATION_ENABLED", "true", "Включает периодическую сверку и обработку расхождений."],
      ["TORGNEXA_WORKER_UPLOADS_ENABLED", "true", "В Community Compose включает проверяемую обработку изображений через ClamAV и S3."],
    ],
  },
  {
    title: "ClamAV",
    rows: [
      ["TORGNEXA_CLAMAV_NETWORK", "tcp", "Сетевой тип подключения к антивирусному демону."],
      ["TORGNEXA_CLAMAV_ADDRESS", "clamav:3310", "Адрес ClamAV в Community Compose; для отдельного worker укажите доступное имя или host:port."],
      ["TORGNEXA_CLAMAV_ENGINE_VERSION", "runtime", "Метка версии антивирусного движка для аудита."],
      ["TORGNEXA_CLAMAV_SIGNATURE_VERSION", "runtime", "Метка версии базы сигнатур."],
      ["TORGNEXA_CLAMAV_TIMEOUT", "30s", "Таймаут проверки: от 1s до 2m."],
    ],
  },
  {
    title: "Внешние уведомления",
    rows: [
      ["NOTIFICATION_SMTP_ADDRESS", "пусто", "SMTP-сервер в формате host:port."],
      ["NOTIFICATION_SMTP_FROM", "пусто", "Адрес отправителя. Для email нужны одновременно address и from."],
      ["NOTIFICATION_SMTP_USERNAME", "пусто", "Необязательное имя SMTP-пользователя."],
      ["NOTIFICATION_SMTP_PASSWORD", "пусто", "Секретный пароль SMTP."],
      ["NOTIFICATION_SMTP_SERVER_NAME", "пусто", "Имя сервера для TLS-проверки сертификата."],
      ["NOTIFICATION_SMTP_IMPLICIT_TLS", "false", "true только для implicit TLS; для STARTTLS оставьте false."],
      ["NOTIFICATION_CHAT_ENDPOINT", "пусто", "HTTPS endpoint корпоративного bot gateway."],
      ["NOTIFICATION_DELIVERY_TIMEOUT", "10s", "Таймаут доставки: от 1s до 1m."],
    ],
  },
] as const;

function DocSection({id, title, intro, children}: {id: string; title: string; intro?: string; children: ReactNode}) {
  return <section id={id}>
    <header className="docs-section-heading"><div><p className="eyebrow">РУКОВОДСТВО</p><h2>{title}</h2>{intro ? <p>{intro}</p> : null}</div><a href="#start" aria-label="К началу документации">↑</a></header>
    {children}
  </section>;
}

function Callout({title, tone = "info", children}: {title: string; tone?: "info" | "warning" | "success"; children: ReactNode}) {
  return <div className={`docs-callout ${tone}`}><strong>{title}</strong><span>{children}</span></div>;
}

function FeatureGrid({items}: {items: readonly (readonly [string, string])[]}) {
  return <div className="docs-feature-grid">{items.map(([title, copy]) => <article key={title}><h3>{title}</h3><p>{copy}</p></article>)}</div>;
}

function EnvironmentTables() {
  return <div className="docs-env-groups">{environmentGroups.map((group) => <div className="docs-env-group" key={group.title}>
    <h3>{group.title}</h3>
    <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Переменная</th><th>По умолчанию</th><th>Назначение и ограничения</th></tr></thead><tbody>
      {group.rows.map(([name, fallback, description]) => <tr key={name}><td><code>{name}</code></td><td><code>{fallback}</code></td><td>{description}</td></tr>)}
    </tbody></table></div>
  </div>)}</div>;
}

export function PublicDocumentationPage() {
  return <div className="docs-shell">
    <header className="docs-header">
      <a className="docs-brand" href="/"><span className="brand-mark small">TN</span><span><strong>TORGNEXA</strong><small>Документация</small></span></a>
      <nav aria-label="Навигация документации"><a href="#start">Руководство</a><a className="docs-login-link" href="/">Войти</a></nav>
    </header>
    <div className="docs-layout">
      <aside className="docs-toc" aria-label="Содержание"><strong>Содержание</strong>{documentationSections.map(([id, label]) => <a key={id} href={`#${id}`}>{label}</a>)}</aside>
      <main className="docs-content">
        <section className="docs-hero" id="start">
          <div className="docs-version"><span>Руководство пользователя</span><span>Текущий интерфейс</span></div>
          <h1>Как работать в TORGNEXA</h1>
          <p className="docs-lead">Актуальное руководство по ежедневной работе: от первого входа и подключения маркетплейса до сверки данных, согласований и контроля безопасности.</p>
          <div className="docs-hero-actions"><a className="button primary" href="/">Войти в TORGNEXA</a><a className="button secondary" href="#interface">Изучить интерфейс</a></div>
          <div className="docs-install-address"><span>Адрес локальной установки</span><code>http://127.0.0.1:5173</code></div>
        </section>

        <DocSection id="interface" title="Первый вход и интерфейс" intro="Публичная документация открывается без авторизации. Рабочие разделы становятся доступны после входа.">
          <ol className="docs-steps">
            <li><strong>Откройте приложение</strong><span>На стартовом экране нажмите «Войти». Технический термин OIDC пользователю в кнопке не показывается.</span></li>
            <li><strong>Пройдите проверку личности</strong><span>Используйте учётную запись, выданную администратором. Пароль обрабатывает провайдер входа, а не TORGNEXA.</span></li>
            <li><strong>Проверьте рабочий контекст</strong><span>В верхней панели отображаются организация и рабочее пространство, полученные из проверенной сессии.</span></li>
            <li><strong>Начните с «Обзора»</strong><span>Интерфейс покажет доступные шаги и проблемы, требующие внимания.</span></li>
          </ol>
          <figure><img src="/docs/login.png" alt="Экран входа TORGNEXA с кнопкой «Войти»"/><figcaption>Публичный экран входа: одна понятная кнопка «Войти» и ссылка на это руководство.</figcaption></figure>
          <Callout title="Сессия обновляется автоматически" tone="success">Короткий access token продлевается в фоне. Повторный вход нужен после завершения SSO-сессии, явного выхода или отзыва сессии администратором.</Callout>
          <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Раздел</th><th>Адрес</th><th>Назначение</th></tr></thead><tbody>{routes.map(([label, path, description]) => <tr key={path}><td><strong>{label}</strong></td><td><code>{path}</code></td><td>{description}</td></tr>)}</tbody></table></div>
          <p>Левая навигация показывает только разделы, разрешённые вашей роли. Отсутствующий пункт обычно означает, что разрешение не выдано; это не удаляет данные.</p>
        </DocSection>

        <DocSection id="overview" title="Обзор" intro="Главная страница отвечает на два вопроса: всё ли работает и что делать дальше.">
          <FeatureGrid items={[
            ["Ключевые показатели", "Сводные значения по товарам, заказам, остаткам и подключённым каналам."],
            ["Онбординг", "Пошаговая готовность рабочего пространства: подключение площадки, импорт и запуск синхронизации."],
            ["Операционный поток", "Краткий путь от внешней площадки через нормализацию к контролю и аналитике."],
            ["Состояние сервисов", "Сигналы о проблемах коннекторов и действиях, которые требуют оператора."],
            ["Центр активности", "Непрочитанные события и ожидающие согласования собраны в верхней панели."],
            ["Быстрый поиск", "Cmd/Ctrl+K открывает разрешённые разделы и ищет товары и заказы через серверные API."],
          ]}/>
          <p>Тема, плотность таблиц и мобильное меню сохраняются в интерфейсе без превращения браузера в источник бизнес-состояния. Плашка состояния подключения показывает только состояние SSE-канала инвалидаций: данные всё равно перечитываются обычными API с проверкой прав.</p>
          <Callout title="Данные не появились сразу?">После подключения кабинета сначала запустите первоначальный импорт в «Синхронизации». Пустой обзор до импорта не означает потерю данных.</Callout>
        </DocSection>

        <DocSection id="catalog-orders" title="Каталог и заказы" intro="Карточки и заказы хранятся в канонической модели, независимо от форматов конкретной площадки.">
          <div className="docs-split">
            <div><h3>Каталог</h3><ul><li>Используйте серверный поиск и курсорную пагинацию по 25 строк.</li><li>При создании товара код, название, описание и изображение заполняются во вкладке «Основные данные»; после сохранения доступны «Предложения», «Категории» и отдельное управление изображениями.</li><li>Загруженное изображение проходит карантин и проверку; HTTPS-ссылка проверяется до публикации.</li><li>Публикация учитывает обязательные поля, возможности коннектора и требования compliance.</li></ul></div>
            <div><h3>Заказы</h3><ul><li>Фильтруйте по поиску и нормализованному статусу через серверный API.</li><li>В боковой панели проверяйте источник, состав, суммы и историю заказа.</li><li>Не создавайте повторную операцию с новым idempotency key, пока результат первой неизвестен.</li><li>Ошибки доставки данных разбирайте через синхронизацию и инциденты.</li></ul></div>
          </div>
          <Callout title="Деньги и количества">Суммы передаются в минимальных единицах вместе с валютой. Дробные количества используют точное decimal-представление.</Callout>
        </DocSection>

        <DocSection id="inventory-incidents" title="Остатки и инциденты" intro="Остатки отражают доступное количество, резервы, состояние складов и исключения fulfillment.">
          <div className="docs-tab-guide">
            <article><strong>Позиции</strong><span>Физический остаток, резерв и доступный ATP по товару и складу.</span></article>
            <article><strong>Склады</strong><span>Операционное состояние и приоритетный резервный склад.</span></article>
            <article><strong>Инциденты</strong><span>Нарушения маршрута и задачи, которые нельзя завершить автоматически.</span></article>
            <article><strong>Fulfillment</strong><span>Ручной резерв позиции заказа — только для исключительных случаев.</span></article>
            <article><strong>Импорт</strong><span>Безопасное массовое обновление из подготовленного источника.</span></article>
          </div>
          <p>Перевод склада в недоступное или утраченное состояние открывает инцидент и пытается перенести обязательства на резервный склад. Физический товар при этом не «перемещается» автоматически. Заказы без маршрута остаются видимыми оператору. В «Инцидентах» объединяются warehouse incidents, открытые расхождения, проблемные кабинеты и ожидающие согласования; строки ведут в соответствующий рабочий раздел.</p>
          <Callout title="Корректировки требуют причины" tone="warning">История остатков ведётся как журнал. Исправление создаёт новую запись, а не переписывает прошлое.</Callout>
        </DocSection>

        <DocSection id="integrations" title="Интеграции" intro="Раздел оформлен как каталог площадок: карточка показывает назначение, статус, доступные возможности и подключённые кабинеты.">
          <ol className="docs-steps compact">
            <li><strong>Выберите площадку</strong><span>Откройте «Интеграции» в левом меню и выберите карточку маркетплейса или внешней системы.</span></li>
            <li><strong>Создайте кабинет</strong><span>Для одной площадки можно завести несколько кабинетов с разными правами и назначением.</span></li>
            <li><strong>Передайте учётные данные</strong><span>Форма строится по manifest коннектора. Секрет вводится только в предусмотренный момент.</span></li>
            <li><strong>Проверьте подключение</strong><span>Успешная проверка доступности обязательна перед активацией и импортом.</span></li>
            <li><strong>Выберите возможности</strong><span>Разрешите нужные чтения и записи, затем настройте импорт и расписание.</span></li>
          </ol>
          <FeatureGrid items={[
            ["Карточки площадок", "Логотип, тип системы, состояние, возможности и понятное действие в одном месте."],
            ["Несколько кабинетов", "Независимые подключения одного семейства без смешивания внешних идентификаторов."],
            ["Безопасные секреты", "API-ключи, OAuth-токены и сертификаты хранятся как зашифрованные ссылки и не возвращаются в UI."],
            ["Контроль записи", "Запись во внешнюю систему проходит проверку разрешения, политики и при необходимости согласование."],
            ["Параметры среды", "Адрес магазина, portal host и другие несекретные параметры версионируются отдельно от учётных данных."],
            ["Пробный запуск", "Перед импортом доступна предварительная проверка: она считает политики, чтение и запись, но не меняет внешний сервис."],
          ]}/>
          <RuntimeMatrix/>
          <figure><img src="/docs/integrations.png" alt="Раздел «Интеграции» TORGNEXA с карточками подключения"/><figcaption>Экран каталога: карточка провайдера ведёт к подключению кабинета и проверке доступа.</figcaption></figure>
          <IntegrationConnectionGuide/>
          <OpenCartDockerGuide/>
          <WooCommerceDockerGuide/>
          <PrestaShopDockerGuide/>
          <SaleorDockerGuide/>
          <ShopifyDockerGuide/>
          <figure><img src="/docs/integration-connection.png" alt="Пошаговое подключение кабинета интеграции TORGNEXA"/><figcaption>Визуальная шпаргалка к панели подключения: кабинет, учётные данные, проверка, возможности и запуск импорта.</figcaption></figure>
          <p>Для OAuth-подключения нажмите «Войти». Токен доступа обновляется сервером автоматически до истечения срока. Повторный вход требуется только если площадка отозвала доступ, отклонила токен обновления или не выдала его; карточка кабинета покажет «Войти снова».</p>
          <p>К текущим готовым storefront-маршрутам относятся 1С‑Битрикс, CS-Cart, Magento, Medusa, OpenCart, Shopify и Shopware; для них рабочий контур ограничен товарами и явно указанными направлениями синхронизации. Bitrix24 — отдельный CRM-контур: лиды, сделки, контакты, компании и товарные строки не превращаются в product sync.</p>
          <Callout title="Боевые учётные данные" tone="warning">Не используйте боевые ключи в тестовом контуре. Выдавайте минимальные права, а при раскрытии немедленно отзывайте ключ у провайдера и выполняйте его ротацию.</Callout>
        </DocSection>

        <DocSection id="social" title="Публикации" intro="Social Core хранит контент, канал, расписание и историю статусов независимо от конкретной социальной сети.">
          <p>В текущей версии рабочей среды подключены Telegram и MAX для текстовых сообщений. Лимит Telegram — 4096 символов, MAX — 4000. В «Интеграциях» создайте кабинет нужного провайдера, сохраните токен бота, заполните <code>chat_id</code> по шаблону, выполните проверку, включите <code>social.post.text</code> и активируйте кабинет. Затем создайте активный канал и публикацию в разделе «Публикации».</p>
          <div className="docs-callout warning"><strong>Защита от дублей</strong><span>Если после отправки невозможно подтвердить результат провайдера, worker переводит публикацию в ошибку <code>write_outcome_unknown</code> и не повторяет удалённую запись автоматически.</span></div>
        </DocSection>

        <DocSection id="sync" title="Синхронизация" intro="Политика задаёт, какие данные движутся, в каком направлении и где находится источник истины.">
          <div className="docs-definition-grid"><div><dt>Политики</dt><dd>Кабинет, сущность, направление и источник истины.</dd></div><div><dt>Запуски</dt><dd>Пробный запуск, первоначальный импорт, расписание и результат.</dd></div><div><dt>Расхождения</dt><dd>Сравнение TORGNEXA с площадкой и безопасное reconciliation.</dd></div></div>
          <ol><li>Подключите кабинет в разделе «Интеграции» и дождитесь состояния <code>healthy</code>.</li><li>Выполните пробный запуск: предварительная проверка действует 30 минут и ничего не меняет во внешней системе.</li><li>Запустите первоначальный импорт из действующей предварительной проверки.</li><li>После проверки задайте режим «Инкрементальный» или «Полная сверка по расписанию» и период от 15 минут до 7 дней.</li><li>Разбирайте конфликты как расхождения — не редактируйте историю вручную.</li></ol>
          <p>Импорт и расписание выполняются на сервере, поэтому закрытие вкладки браузера не прерывает задачу. В карточке кабинета и на странице синхронизации видны последняя задача, число запусков и код последней ошибки.</p>
          <Callout title="Повторная доставка безопасна">События публикуются через transactional outbox, а потребители подавляют дубли с помощью inbox/idempotency.</Callout>
        </DocSection>

        <DocSection id="master-data" title="Контрагенты и финансы" intro="Единые справочники не позволяют разным модулям создавать противоречивые версии одной сущности.">
          <div className="docs-split"><div><h3>Контрагенты</h3><p>Ищите юридическое лицо по каноническому справочнику и проверяйте его роли: покупатель, поставщик или партнёр. ERP, ЭДО, платежи и закупки ссылаются на эту запись.</p></div><div><h3>Финансы</h3><p>Вкладки «Расчёты», «Курсы валют» и «Платежи» показывают журнал продаж, комиссий, возвратов, выплат, официальные FX-факты и операции платёжных шлюзов. Конвертация использует сохранённый источник и точный курс; исправления оформляются корректирующими записями.</p></div></div>
          <p>В текущей рабочей среде платёжные операции доступны для СБП, YooKassa и Robokassa: создание, статус и сверка. Возврат разрешается только шлюзам с правом <code>payments.refund</code>; у Robokassa возврат на уровне продавца намеренно не заявлен. Ozon Pay пока ограничен проверкой Seller API.</p>
          <p>Для входящих уведомлений используйте <code>POST /api/v1/webhooks/payments/&#123;connector_id&#125;/&#123;organization_id&#125;/&#123;workspace_id&#125;/&#123;account_id&#125;</code>. Это публичный callback без пользовательской сессии: сервер проверяет активный платёжный кабинет, повторно подтверждает состояние у провайдера, записывает свидетельство и применяет переход ровно один раз. Провайдер получает унифицированный <code>200</code> при ошибке до проверки, а тело callback не считается источником статуса.</p>
        </DocSection>

        <DocSection id="control" title="Согласования, сертификаты и документы" intro="Чувствительные и юридически значимые действия отделены от обычного редактирования данных.">
          <FeatureGrid items={[
            ["Политики", "Определяют риск, лимиты, роли согласующих и условия исполнения."],
            ["Запросы", "Фиксируют инициатора, действие, объект и причину без выполнения операции."],
            ["Решение", "Уполномоченный пользователь одобряет или отклоняет запрос с полной аудируемостью."],
            ["Исполнение", "Разрешённое действие выполняется отдельно и идемпотентно."],
          ]}/>
          <p>В «Сертификатах и документах» контролируются декларации, разрешения, сроки действия и результаты проверок. Там же создаются запросы субъекта персональных данных: доступ, экспорт, исправление, удаление или ограничение обработки.</p>
        </DocSection>

        <DocSection id="monitoring" title="Уведомления, отчёты и аудит" intro="Три раздела отвечают за оперативную реакцию, анализ и доказуемость действий.">
          <div className="docs-tab-guide"><article><strong>Уведомления</strong><span>Ошибки, предупреждения и системные события с признаком прочтения.</span></article><article><strong>Отчёты</strong><span>Периоды 7, 30 и 90 дней, поиск, графики, CSV/PDF и доступная AI-аналитика.</span></article><article><strong>Аудит</strong><span>Append-only история привилегированных изменений с субъектом, временем и результатом.</span></article><article><strong>Realtime</strong><span>SSE сообщает об изменениях и обновляет уже разрешённые запросы без копирования бизнес-данных.</span></article></div>
          <p>Экспорт отчёта сохраняет выбранные фильтры. PDF создаётся сервером и скачивается готовым файлом. Аналитические проекции могут обновляться с небольшой задержкой и не являются транзакционной истиной.</p>
        </DocSection>

        <DocSection id="settings" title="Настройки" intro="Настройки разделены на семь вкладок; состав доступных действий зависит от роли администратора.">
          <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Вкладка</th><th>Что находится внутри</th></tr></thead><tbody>
            <tr><td><strong>Основные</strong></td><td>Профиль, способы входа, роли, рабочее пространство, подписка, участники, сессии, интеграции и провайдеры ИИ.</td></tr>
            <tr><td><strong>Провайдеры входа</strong></td><td>Корпоративный OIDC, проверка обнаружения конфигурации, правила сопоставления и активация.</td></tr>
            <tr><td><strong>Каналы и важность</strong></td><td>In-app, email и SMS, важность, категории, тихие часы и часовой пояс.</td></tr>
            <tr><td><strong>MCP-агенты</strong></td><td>Сервисные аккаунты, одноразовые токены, инструменты, политики и аварийная остановка.</td></tr>
            <tr><td><strong>Контроль и сценарии</strong></td><td>Trust controls и ограничения автоматизированных действий.</td></tr>
            <tr><td><strong>Вебхуки</strong></td><td>HTTPS-получатели, типы событий, подпись, ротация и повтор доставки.</td></tr>
            <tr><td><strong>Плагины</strong></td><td>Проверенные расширения, разрешения и сетевые зависимости до активации.</td></tr>
          </tbody></table></div>
          <Callout title="Конфликт версии" tone="warning">Если другой администратор сохранил запись раньше, обновите данные, проверьте изменения и повторите операцию.</Callout>
        </DocSection>

        <DocSection id="automation" title="AI, MCP, webhooks и плагины" intro="Автоматизация получает только явно выданные возможности и никогда не обходит серверные проверки.">
          <ul><li><strong>Провайдеры ИИ</strong> включают Claude, DeepSeek, GigaChat, Kimi, OpenAI-совместимый, Qwen, YandexGPT и локальные Ollama, LM Studio, Open WebUI. Локальные серверы уже должны быть запущены; TORGNEXA не скачивает модели, не включает потоковую выдачу и вызовы инструментов.</li><li><strong>Политика передачи данных ИИ</strong> ограничивает классы данных, провайдеров, моделей, размер запроса и месячный лимит. Предварительная проверка редактирует чувствительные фрагменты и не отправляет тестовый запрос наружу.</li><li><strong>MCP-аккаунт</strong> получает одноразовый токен Bearer и ограниченный набор инструментов. В базовой сборке без настроенной политики управления <code>tools/list</code> пуст, а <code>tools/call</code> отклоняется.</li><li><strong>Аварийная остановка</strong> блокирует всех MCP-агентов рабочего пространства до явного возобновления.</li><li><strong>Вебхук</strong> доставляет выбранные события на HTTPS-адрес с подписью, повторными попытками и очередью ошибок, историей попыток и ручным повтором по идентификатору доставки.</li><li><strong>Плагин</strong> показывает запрошенные права, классы секретов и сетевые адреса; просмотр каталога ничего не устанавливает.</li></ul>
          <Callout title="ИИ не является привилегированным обходом">Даже действительный токен не отменяет границу рабочего пространства, разрешения, лимит запросов, класс риска, политику и согласование.</Callout>
        </DocSection>

        <DocSection id="developer" title="API и расширения" intro="Публичная поверхность для интеграторов строится вокруг версионированных контрактов, а не внутренних таблиц.">
          <FeatureGrid items={[
            ["REST API", "Основной HTTP-контур находится под /api/v1; организация и рабочее пространство выводятся из проверенной авторизации, а не из произвольных данных запроса."],
            ["SDK", "OpenAPI генерирует поддерживаемые клиенты Go, TypeScript и Python с политикой совместимости."],
            ["Вебхуки", "Исходящие события подписываются, доставляются с повторными попытками и сохраняют неизменяемую историю попыток."],
            ["n8n и MCP", "n8n остаётся внешней интеграцией; MCP/OpenClaw получают только scoped-инструменты и governed доступ."],
          ]}/>
          <pre><code>GET  /api/v1/health{`\n`}GET  /api/v1/products?limit=25&amp;q=SKU-42{`\n`}POST /api/v1/webhook-subscriptions{`\n`}POST /mcp</code></pre>
          <p>Для мутаций используйте idempotency key и обрабатывайте 401/403/409/429 явно. В событиях и webhook envelope не передавайте access token, приватные ключи, полные платёжные данные или лишние PII.</p>
        </DocSection>

        <DocSection id="security" title="Доступ и безопасность" intro="TORGNEXA использует модель default deny: разрешено только то, что выдано явно.">
          <ul className="docs-checklist"><li>Контекст организации и рабочего пространства берётся из проверенной сессии.</li><li>Пароли остаются у провайдера идентификации; TORGNEXA их не видит и не хранит.</li><li>Ключи, токены, приватные ключи и платёжные данные нельзя помещать в комментарии, логи и экспорт.</li><li>Не отключайте последнего активного администратора рабочего пространства.</li><li>Завершайте неизвестные активные сессии во вкладке «Основные».</li><li>Опасные действия проходят проверку политики и согласования и фиксируются в аудите.</li></ul>
          <p>Внешний OIDC-провайдер сначала создаётся как черновик: адрес издателя должен входить в список разрешённых адресов развёртывания, обнаружение конфигурации проходит проверку, и только затем конфигурацию можно активировать.</p>
        </DocSection>

        <DocSection id="environment" title="Переменные окружения .env" intro="Полный справочник Community-контура: как безопасно создать файл, что можно менять и какие значения принимает каждая переменная.">
          <h3>Как заполнить файл</h3>
          <ol className="docs-steps compact">
            <li><strong>Сгенерируйте секреты</strong><span>В корне репозитория выполните <code>make community-init</code>. Команда создаст случайные значения и установит права <code>0600</code>.</span></li>
            <li><strong>Измените только нужное</strong><span>Обычно это занятые порты, уровень журнала, OIDC allowlist, SMTP/chat и адрес ClamAV.</span></li>
            <li><strong>Проверьте конфигурацию</strong><span>Выполните <code>docker compose --env-file .env config --quiet</code> и <code>make community-check</code>.</span></li>
            <li><strong>Запустите контур</strong><span>Используйте <code>make community-up</code>, затем проверьте состояние командой <code>make community-status</code>.</span></li>
          </ol>
          <Callout title="Не копируйте .env.example как готовый файл" tone="warning">Пустые секреты в шаблоне оставлены намеренно. В работающей установке не заменяйте сгенерированные учётные данные отдельно от постоянных томов.</Callout>
          <h3>Форматы значений</h3>
          <ul><li>Строка записывается как <code>ИМЯ=значение</code>, без пробелов вокруг знака равенства.</li><li>Логические значения: <code>true</code> или <code>false</code>.</li><li>Длительности: <code>500ms</code>, <code>30s</code>, <code>5m</code> или <code>1h</code>.</li><li>CSV-списки разделяются запятыми и не содержат пустых элементов.</li><li>Порты — целые числа от 1 до 65535.</li></ul>
          <EnvironmentTables/>
          <h3>Как работает ClamAV</h3>
          <p>Загруженный файл сначала попадает в закрытый карантин Garage/S3. После проверки размера, MIME-типа и SHA-256 worker передаёт содержимое в ClamAV. Чистый файл можно выпустить для использования, заражённый отклоняется, а при недоступности сканера файл остаётся в карантине. Fail-open режима нет.</p>
          <Callout title="Загрузка изображений в Community">Community Compose запускает официальный ClamAV и worker по адресу <code>clamav:3310</code>. Файл сначала проходит карантин, проверку и только после выпуска становится доступен карточке товара.</Callout>
          <h3>Пример SMTP</h3>
          <pre><code>NOTIFICATION_SMTP_ADDRESS=smtp.company.ru:587{`\n`}NOTIFICATION_SMTP_FROM=torgnexa@company.ru{`\n`}NOTIFICATION_SMTP_USERNAME=torgnexa{`\n`}NOTIFICATION_SMTP_PASSWORD=replace-with-secret{`\n`}NOTIFICATION_SMTP_SERVER_NAME=smtp.company.ru{`\n`}NOTIFICATION_SMTP_IMPLICIT_TLS=false{`\n`}NOTIFICATION_DELIVERY_TIMEOUT=10s</code></pre>
          <h3>Существующий .env</h3>
          <p>Если тома уже содержат данные, не запускайте генератор с <code>--force</code> и не ротируйте секреты по одному. Потерянный <code>.env</code> нельзя заменить новым без согласованной процедуры восстановления учётных данных в PostgreSQL, Keycloak, ClickHouse и Garage.</p>
          <Callout title="Рабочая среда" tone="warning">Корневой <code>.env</code> предназначен для локального Community-контура на одном узле. В рабочей среде используйте внешнее хранилище секретов или секреты Docker/Kubernetes, TLS-шлюз и отдельные процедуры ротации.</Callout>
        </DocSection>

        <DocSection id="operations" title="Эксплуатация Community-контура" intro="Перед обновлением проверьте состояние сервисов, резервные копии и возможность восстановления.">
          <p>Создание и заполнение конфигурации подробно разобрано в разделе <a href="#environment">«Переменные окружения .env»</a>.</p>
          <pre><code>docker compose --env-file .env ps{`\n`}curl http://127.0.0.1:8080/api/v1/health{`\n`}docker compose --env-file .env logs --tail=100 api</code></pre>
          <p>PostgreSQL — операционная истина, Kafka — событийная платформа, ClickHouse — аналитика, Valkey — кеши и координация. Нормальное состояние: необходимые сервисы и frontend имеют статус healthy.</p>
          <h3>Рабочая среда на отдельном VPS</h3>
          <p>Для рабочей среды используйте защищённый ручной процесс SSH по точному тегу: GitHub Environment с проверяющим, закреплённый <code>known_hosts</code>, отдельный рабочий слой, TLS-шлюз, внешнее хранилище секретов и проверку доступности после переключения релиза. Community Compose остаётся локальным эталоном для одного узла и не превращается в HA- или CDN-топологию.</p>
          <ol><li>Подготовьте рабочий <code>.env</code> с правами <code>0600</code> и без идентификаторов разработки OIDC.</li><li>Проверьте резервное копирование, восстановление и откат до первого развёртывания.</li><li>Запустите только вручную одобренный точный тег и дождитесь проверки API.</li><li>При неуспешной проверке доступности вернитесь на предыдущий выпуск и сохраните свидетельство.</li></ol>
          <Callout title="Перед обновлением" tone="warning">Сделайте резервную копию, проверьте миграции и отрепетируйте восстановление. Один только факт создания backup не подтверждает его пригодность.</Callout>
        </DocSection>

        <DocSection id="troubleshooting" title="Решение проблем" intro="Начните с симптома, затем проверяйте ближайшую границу: сессию, права, API или внешний коннектор.">
          <dl className="docs-faq">
            <div><dt>Слишком часто появляется экран входа</dt><dd>Access token должен обновляться автоматически. Проверьте доступность <code>/oidc/silent-callback.html</code>, Web Origins и политику встраивания провайдера. Повторный вход ожидаем после окончания SSO-сессии, выхода или отзыва.</dd></div>
            <div><dt>Раздел отсутствует в меню</dt><dd>Проверьте разрешения пользователя и текущее рабочее пространство. Скрытие пункта — следствие правил доступа, а не удаление данных.</dd></div>
            <div><dt>Не удалось загрузить данные</dt><dd>Проверьте API health и сессию. Ответ 403 означает отсутствие разрешения, 409 — конфликт версии, 429 — превышение лимита.</dd></div>
            <div><dt>Интеграция не активируется</dt><dd>Откройте карточку площадки и проверьте учётные данные, параметры OAuth, разрешения и проверку доступности.</dd></div>
            <div><dt>Синхронизация не запускается</dt><dd>Убедитесь, что кабинет активен, политика создана, направление поддерживается, а предыдущий запуск не требует решения оператора.</dd></div>
            <div><dt>Секрет случайно раскрыт</dt><dd>Немедленно отзовите его у провайдера, выполните ротацию и проверьте аудит. Удаления текста из UI недостаточно.</dd></div>
          </dl>
          <figure><img src="/docs/documentation.png" alt="Публичная документация TORGNEXA"/><figcaption>Руководство доступно до входа и адаптируется под настольный и мобильный экран.</figcaption></figure>
        </DocSection>
      </main>
    </div>
  </div>;
}

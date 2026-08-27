import type {ReactNode} from "react";

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
  ["security", "Доступ и безопасность"],
  ["environment", "Переменные .env"],
  ["operations", "Эксплуатация"],
  ["troubleshooting", "Решение проблем"],
] as const;

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
  ["Финансы", "/finance", "Расчёты площадок и курсы валют"],
  ["Согласования", "/approvals", "Политики и выполнение чувствительных операций"],
  ["Сертификаты и документы", "/compliance", "Разрешительные документы и запросы приватности"],
  ["Уведомления", "/notifications", "Ошибки, предупреждения и системные события"],
  ["Отчёты", "/reports", "Аналитика, фильтры и экспорт"],
  ["Аудит", "/audit", "История привилегированных действий"],
  ["Настройки", "/settings", "Workspace, доступ, автоматизация и безопасность"],
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
      ["TORGNEXA_KAFKA_CONSUMER_GROUP", "torgnexa.webhooks.v1", "Общее имя consumer group для одного логического worker-кластера."],
      ["TORGNEXA_WORKER_POLL_INTERVAL", "500ms", "Интервал опроса: от 50ms до 30s."],
      ["TORGNEXA_WORKER_DISPATCH_BATCH", "32", "Размер пачки: от 1 до 1000."],
      ["TORGNEXA_WORKER_LEASE", "90s", "Lease обработки: от 10s до 10m."],
      ["TORGNEXA_WORKER_RECONCILIATION_ENABLED", "true", "Включает периодическую сверку и обработку расхождений."],
      ["TORGNEXA_WORKER_UPLOADS_ENABLED", "false", "Включает обработку файлов только после настройки доступного ClamAV."],
    ],
  },
  {
    title: "ClamAV",
    rows: [
      ["TORGNEXA_CLAMAV_NETWORK", "tcp", "Сетевой тип подключения к антивирусному демону."],
      ["TORGNEXA_CLAMAV_ADDRESS", "127.0.0.1:3310", "Адрес clamd в формате host:port. В контейнере используйте доступное Docker DNS-имя."],
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
            <li><strong>Проверьте рабочий контекст</strong><span>В верхней панели отображаются организация и workspace, полученные из проверенной сессии.</span></li>
            <li><strong>Начните с «Обзора»</strong><span>Интерфейс покажет доступные шаги и проблемы, требующие внимания.</span></li>
          </ol>
          <figure><img src="/docs/login.png" alt="Экран входа TORGNEXA с кнопкой «Войти»"/><figcaption>Публичный экран входа: одна понятная кнопка «Войти» и ссылка на это руководство.</figcaption></figure>
          <Callout title="Сессия обновляется автоматически" tone="success">Короткий access token продлевается в фоне. Повторный вход нужен после завершения SSO-сессии, явного выхода или отзыва сессии администратором.</Callout>
          <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Раздел</th><th>Адрес</th><th>Назначение</th></tr></thead><tbody>{routes.map(([label, path, description]) => <tr key={path}><td><strong>{label}</strong></td><td><code>{path}</code></td><td>{description}</td></tr>)}</tbody></table></div>
          <p>Левая навигация показывает только разделы, разрешённые вашей роли. Отсутствующий пункт обычно означает, что capability не выдана; это не удаляет данные.</p>
        </DocSection>

        <DocSection id="overview" title="Обзор" intro="Главная страница отвечает на два вопроса: всё ли работает и что делать дальше.">
          <FeatureGrid items={[
            ["Ключевые показатели", "Сводные значения по товарам, заказам, остаткам и подключённым каналам."],
            ["Онбординг", "Пошаговая готовность workspace: подключение площадки, импорт и запуск синхронизации."],
            ["Операционный поток", "Краткий путь от внешней площадки через нормализацию к контролю и аналитике."],
            ["Состояние сервисов", "Сигналы о проблемах коннекторов и действиях, которые требуют оператора."],
          ]}/>
          <Callout title="Данные не появились сразу?">После подключения кабинета сначала запустите первоначальный импорт в «Синхронизации». Пустой обзор до импорта не означает потерю данных.</Callout>
        </DocSection>

        <DocSection id="catalog-orders" title="Каталог и заказы" intro="Карточки и заказы хранятся в канонической модели, независимо от форматов конкретной площадки.">
          <div className="docs-split">
            <div><h3>Каталог</h3><ul><li>Используйте поиск и серверную пагинацию по 25 строк.</li><li>Откройте товар и переходите между вкладками «Карточка», «Предложения», «Категории» и «Изображения».</li><li>Загруженное изображение проходит карантин и проверку; HTTPS-ссылка проверяется до публикации.</li><li>Публикация учитывает обязательные поля, возможности коннектора и требования compliance.</li></ul></div>
            <div><h3>Заказы</h3><ul><li>Фильтруйте по поиску и нормализованному статусу.</li><li>В боковой панели проверяйте источник, состав, суммы и историю заказа.</li><li>Не создавайте повторную операцию с новым idempotency key, пока результат первой неизвестен.</li><li>Ошибки доставки данных разбирайте через синхронизацию и инциденты.</li></ul></div>
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
          <p>Перевод склада в недоступное или утраченное состояние открывает инцидент и пытается перенести обязательства на резервный склад. Физический товар при этом не «перемещается» автоматически. Заказы без маршрута остаются видимыми оператору.</p>
          <Callout title="Корректировки требуют причины" tone="warning">История остатков ведётся как журнал. Исправление создаёт новую запись, а не переписывает прошлое.</Callout>
        </DocSection>

        <DocSection id="integrations" title="Интеграции" intro="Раздел оформлен как каталог площадок: карточка показывает назначение, статус, доступные возможности и подключённые кабинеты.">
          <ol className="docs-steps compact">
            <li><strong>Выберите площадку</strong><span>Откройте «Интеграции» в левом меню и выберите карточку маркетплейса или внешней системы.</span></li>
            <li><strong>Создайте кабинет</strong><span>Для одной площадки можно завести несколько кабинетов с разными правами и назначением.</span></li>
            <li><strong>Передайте учётные данные</strong><span>Форма строится по manifest коннектора. Секрет вводится только в предусмотренный момент.</span></li>
            <li><strong>Проверьте подключение</strong><span>Успешный health check обязателен перед активацией и импортом.</span></li>
            <li><strong>Выберите возможности</strong><span>Разрешите нужные чтения и записи, затем настройте импорт и расписание.</span></li>
          </ol>
          <FeatureGrid items={[
            ["Карточки площадок", "Логотип, тип системы, состояние, возможности и понятное действие в одном месте."],
            ["Несколько кабинетов", "Независимые подключения одного семейства без смешивания внешних идентификаторов."],
            ["Безопасные секреты", "API-ключи, OAuth-токены и сертификаты хранятся как зашифрованные ссылки и не возвращаются в UI."],
            ["Контроль записи", "Запись во внешнюю систему проходит capability, policy и при необходимости approval."],
          ]}/>
          <p>Для OAuth-подключения нажмите «Войти». Access token обновляется сервером автоматически до истечения срока. Повторный вход требуется только если площадка отозвала доступ, отклонила refresh token или не выдала его; карточка кабинета покажет «Войти снова».</p>
          <Callout title="Production credentials" tone="warning">Не используйте боевые ключи в тестовом контуре. Выдавайте минимальные права, а при раскрытии немедленно отзывайте ключ у провайдера и ротируйте его.</Callout>
        </DocSection>

        <DocSection id="social" title="Публикации" intro="Social Core хранит контент, канал, расписание и историю статусов независимо от конкретной социальной сети.">
          <p>В текущей версии production runtime подключены Telegram и MAX для текстовых сообщений. Лимит Telegram — 4096 символов, MAX — 4000. В «Интеграциях» создайте кабинет нужного провайдера, сохраните bot token, заполните <code>chat_id</code> по шаблону, выполните проверку, включите <code>social.post.text</code> и активируйте кабинет. Затем создайте активный канал и публикацию в разделе «Публикации».</p>
          <div className="docs-callout warning"><strong>Защита от дублей</strong><span>Если после отправки невозможно подтвердить результат провайдера, worker переводит публикацию в ошибку <code>write_outcome_unknown</code> и не повторяет удалённую запись автоматически.</span></div>
        </DocSection>

        <DocSection id="sync" title="Синхронизация" intro="Политика задаёт, какие данные движутся, в каком направлении и где находится источник истины.">
          <div className="docs-definition-grid"><div><dt>Политики</dt><dd>Кабинет, сущность, направление и источник истины.</dd></div><div><dt>Запуски</dt><dd>Пробный запуск, первоначальный импорт, расписание и результат.</dd></div><div><dt>Расхождения</dt><dd>Сравнение TORGNEXA с площадкой и безопасное reconciliation.</dd></div></div>
          <ol><li>Подключите кабинет в разделе «Интеграции».</li><li>Выполните пробный запуск и проверьте ожидаемые изменения.</li><li>Запустите ограниченный первоначальный импорт.</li><li>После проверки задайте период автоматической синхронизации.</li><li>Разбирайте конфликты как расхождения — не редактируйте историю вручную.</li></ol>
          <Callout title="Повторная доставка безопасна">События публикуются через transactional outbox, а потребители подавляют дубли с помощью inbox/idempotency.</Callout>
        </DocSection>

        <DocSection id="master-data" title="Контрагенты и финансы" intro="Единые справочники не позволяют разным модулям создавать противоречивые версии одной сущности.">
          <div className="docs-split"><div><h3>Контрагенты</h3><p>Ищите юридическое лицо по каноническому справочнику и проверяйте его роли: покупатель, поставщик или партнёр. ERP, ЭДО, платежи и закупки ссылаются на эту запись.</p></div><div><h3>Финансы</h3><p>Раздел показывает неизменяемый журнал продаж, комиссий, возвратов и выплат площадок. Конвертация использует сохранённый источник и точный курс; исправления оформляются корректирующими записями.</p></div></div>
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
          <div className="docs-tab-guide"><article><strong>Уведомления</strong><span>Ошибки, предупреждения и системные события с признаком прочтения.</span></article><article><strong>Отчёты</strong><span>Периоды 7, 30 и 90 дней, поиск, графики, CSV/PDF и доступная AI-аналитика.</span></article><article><strong>Аудит</strong><span>Append-only история привилегированных изменений с субъектом, временем и результатом.</span></article></div>
          <p>Экспорт отчёта сохраняет выбранные фильтры. PDF создаётся сервером и скачивается готовым файлом. Аналитические проекции могут обновляться с небольшой задержкой и не являются транзакционной истиной.</p>
        </DocSection>

        <DocSection id="settings" title="Настройки" intro="Настройки разделены на семь вкладок; состав доступных действий зависит от роли администратора.">
          <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Вкладка</th><th>Что находится внутри</th></tr></thead><tbody>
            <tr><td><strong>Основные</strong></td><td>Профиль, способы входа, роли, workspace, подписка, участники, сессии, интеграции и AI-провайдеры.</td></tr>
            <tr><td><strong>Провайдеры входа</strong></td><td>Корпоративный OIDC, discovery-проверка, правила сопоставления и активация.</td></tr>
            <tr><td><strong>Каналы и важность</strong></td><td>In-app, email и SMS, важность, категории, тихие часы и часовой пояс.</td></tr>
            <tr><td><strong>MCP-агенты</strong></td><td>Сервисные аккаунты, одноразовые токены, инструменты, политики и аварийная остановка.</td></tr>
            <tr><td><strong>Контроль и сценарии</strong></td><td>Trust controls и ограничения автоматизированных действий.</td></tr>
            <tr><td><strong>Webhooks</strong></td><td>HTTPS-получатели, типы событий, подпись, ротация и повтор доставки.</td></tr>
            <tr><td><strong>Плагины</strong></td><td>Проверенные расширения, разрешения и сетевые зависимости до активации.</td></tr>
          </tbody></table></div>
          <Callout title="Конфликт версии" tone="warning">Если другой администратор сохранил запись раньше, обновите данные, проверьте изменения и повторите операцию.</Callout>
        </DocSection>

        <DocSection id="automation" title="AI, MCP, webhooks и плагины" intro="Автоматизация получает только явно выданные возможности и никогда не обходит серверные проверки.">
          <ul><li><strong>AI-провайдер</strong> используется для аналитики в отчётах. Ключ шифруется и повторно не отображается.</li><li><strong>MCP-аккаунт</strong> получает одноразовый bearer-токен и ограниченный набор инструментов. Без политики вызовы отклоняются.</li><li><strong>Аварийная остановка</strong> блокирует всех MCP-агентов workspace до явного возобновления.</li><li><strong>Webhook</strong> доставляет выбранные события на HTTPS-адрес с подписью и защитой от повтора.</li><li><strong>Плагин</strong> показывает запрошенные права и адреса; просмотр каталога ничего не устанавливает.</li></ul>
          <Callout title="AI не является привилегированным обходом">Даже действительный токен не отменяет tenant boundary, capability, rate limit, risk class, policy и approval.</Callout>
        </DocSection>

        <DocSection id="security" title="Доступ и безопасность" intro="TORGNEXA использует модель default deny: разрешено только то, что выдано явно.">
          <ul className="docs-checklist"><li>Контекст организации и workspace берётся из проверенной сессии.</li><li>Пароли остаются у провайдера идентификации; TORGNEXA их не видит и не хранит.</li><li>Ключи, токены, приватные ключи и платёжные данные нельзя помещать в комментарии, логи и экспорт.</li><li>Не отключайте последнего активного администратора workspace.</li><li>Завершайте неизвестные активные сессии во вкладке «Основные».</li><li>Опасные действия проходят policy/approval и фиксируются в аудите.</li></ul>
          <p>Upstream OIDC-провайдер сначала создаётся как черновик: issuer должен входить в allowlist развертывания, discovery проходит проверку, и только затем конфигурацию можно активировать.</p>
        </DocSection>

        <DocSection id="environment" title="Переменные окружения .env" intro="Полный справочник Community-контура: как безопасно создать файл, что можно менять и какие значения принимает каждая переменная.">
          <h3>Как заполнить файл</h3>
          <ol className="docs-steps compact">
            <li><strong>Сгенерируйте секреты</strong><span>В корне репозитория выполните <code>make community-init</code>. Команда создаст случайные значения и установит права <code>0600</code>.</span></li>
            <li><strong>Измените только нужное</strong><span>Обычно это занятые порты, уровень журнала, OIDC allowlist, SMTP/chat и адрес ClamAV.</span></li>
            <li><strong>Проверьте конфигурацию</strong><span>Выполните <code>docker compose --env-file .env config --quiet</code> и <code>make community-check</code>.</span></li>
            <li><strong>Запустите контур</strong><span>Используйте <code>make community-up</code>, затем проверьте состояние командой <code>make community-status</code>.</span></li>
          </ol>
          <Callout title="Не копируйте .env.example как готовый файл" tone="warning">Пустые секреты в шаблоне оставлены намеренно. В работающей установке не заменяйте сгенерированные credentials отдельно от persistent volumes.</Callout>
          <h3>Форматы значений</h3>
          <ul><li>Строка записывается как <code>ИМЯ=значение</code>, без пробелов вокруг знака равенства.</li><li>Логические значения: <code>true</code> или <code>false</code>.</li><li>Длительности: <code>500ms</code>, <code>30s</code>, <code>5m</code> или <code>1h</code>.</li><li>CSV-списки разделяются запятыми и не содержат пустых элементов.</li><li>Порты — целые числа от 1 до 65535.</li></ul>
          <EnvironmentTables/>
          <h3>Как работает ClamAV</h3>
          <p>Загруженный файл сначала попадает в закрытый карантин Garage/S3. После проверки размера, MIME-типа и SHA-256 worker передаёт содержимое в ClamAV. Чистый файл можно выпустить для использования, заражённый отклоняется, а при недоступности сканера файл остаётся в карантине. Fail-open режима нет.</p>
          <Callout title="ClamAV по умолчанию выключен">Community Compose не запускает контейнер clamd автоматически. Перед <code>TORGNEXA_WORKER_UPLOADS_ENABLED=true</code> добавьте доступный scanner и замените <code>127.0.0.1:3310</code> на его адрес в Docker-сети, например <code>clamav:3310</code>.</Callout>
          <h3>Пример SMTP</h3>
          <pre><code>NOTIFICATION_SMTP_ADDRESS=smtp.company.ru:587{`\n`}NOTIFICATION_SMTP_FROM=torgnexa@company.ru{`\n`}NOTIFICATION_SMTP_USERNAME=torgnexa{`\n`}NOTIFICATION_SMTP_PASSWORD=replace-with-secret{`\n`}NOTIFICATION_SMTP_SERVER_NAME=smtp.company.ru{`\n`}NOTIFICATION_SMTP_IMPLICIT_TLS=false{`\n`}NOTIFICATION_DELIVERY_TIMEOUT=10s</code></pre>
          <h3>Существующий .env</h3>
          <p>Если volumes уже содержат данные, не запускайте генератор с <code>--force</code> и не ротируйте секреты по одному. Потерянный <code>.env</code> нельзя заменить новым без согласованной процедуры восстановления credentials в PostgreSQL, Keycloak, ClickHouse и Garage.</p>
          <Callout title="Production" tone="warning">Корневой <code>.env</code> предназначен для single-host Community-контура. В production используйте внешний secret manager или Docker/Kubernetes secrets, TLS edge и отдельные процедуры ротации.</Callout>
        </DocSection>

        <DocSection id="operations" title="Эксплуатация Community-контура" intro="Перед обновлением проверьте состояние сервисов, резервные копии и возможность восстановления.">
          <p>Создание и заполнение конфигурации подробно разобрано в разделе <a href="#environment">«Переменные окружения .env»</a>.</p>
          <pre><code>docker compose --env-file .env ps{`\n`}curl http://127.0.0.1:8080/api/v1/health{`\n`}docker compose --env-file .env logs --tail=100 api</code></pre>
          <p>PostgreSQL — операционная истина, Kafka — событийная платформа, ClickHouse — аналитика, Valkey — кеши и координация. Нормальное состояние: необходимые сервисы и frontend имеют статус healthy.</p>
          <Callout title="Перед обновлением" tone="warning">Сделайте резервную копию, проверьте миграции и отрепетируйте восстановление. Один только факт создания backup не подтверждает его пригодность.</Callout>
        </DocSection>

        <DocSection id="troubleshooting" title="Решение проблем" intro="Начните с симптома, затем проверяйте ближайшую границу: сессию, права, API или внешний коннектор.">
          <dl className="docs-faq">
            <div><dt>Слишком часто появляется экран входа</dt><dd>Access token должен обновляться автоматически. Проверьте доступность <code>/oidc/silent-callback.html</code>, Web Origins и политику встраивания провайдера. Повторный вход ожидаем после окончания SSO-сессии, выхода или отзыва.</dd></div>
            <div><dt>Раздел отсутствует в меню</dt><dd>Проверьте capability пользователя и текущий workspace. Скрытие пункта — следствие правил доступа, а не удаление данных.</dd></div>
            <div><dt>Не удалось загрузить данные</dt><dd>Проверьте API health и сессию. Ответ 403 означает отсутствие разрешения, 409 — конфликт версии, 429 — превышение лимита.</dd></div>
            <div><dt>Интеграция не активируется</dt><dd>Откройте карточку площадки и проверьте credentials, OAuth metadata, capabilities и health check.</dd></div>
            <div><dt>Синхронизация не запускается</dt><dd>Убедитесь, что кабинет активен, политика создана, направление поддерживается, а предыдущий запуск не требует решения оператора.</dd></div>
            <div><dt>Секрет случайно раскрыт</dt><dd>Немедленно отзовите его у провайдера, выполните ротацию и проверьте аудит. Удаления текста из UI недостаточно.</dd></div>
          </dl>
          <figure><img src="/docs/documentation.png" alt="Публичная документация TORGNEXA"/><figcaption>Руководство доступно до входа и адаптируется под настольный и мобильный экран.</figcaption></figure>
        </DocSection>
      </main>
    </div>
  </div>;
}

import {createContext, useContext, useEffect, type ReactNode} from "react";
import {connectorCatalog} from "../generated/connector-catalog";

export const documentationSections = [
  ["start", "Быстрый старт"],
  ["interface", "Интерфейс и навигация"],
  ["overview", "Обзор"],
  ["catalog-orders", "Каталог и заказы"],
  ["inventory-incidents", "Остатки и инциденты"],
  ["marking", "Маркировка и УПД"],
  ["integrations", "Интеграции"],
  ["integration-status", "Состояние интеграций"],
  ["social", "Публикации"],
  ["sync", "Синхронизация"],
  ["master-data", "Контрагенты и финансы"],
  ["control", "Согласования и документы"],
  ["monitoring", "Уведомления, отчёты и аудит"],
  ["settings", "Настройки"],
  ["automation", "Автоматизация и расширения"],
  ["assistant", "AI-помощник оператора"],
  ["developer", "API и расширения"],
  ["security", "Доступ и безопасность"],
  ["environment", "Переменные .env"],
  ["operations", "Эксплуатация"],
  ["troubleshooting", "Решение проблем"],
] as const;

type DocumentationNavigationItem = (typeof documentationSections)[number];
const documentationSection = (id: DocumentationNavigationItem[0]): DocumentationNavigationItem => documentationSections.find(([sectionId]) => sectionId === id)!;

const documentationNavigation = [
  {title: "Начало", items: [documentationSection("start"), documentationSection("interface"), documentationSection("overview")]},
  {title: "Операционная работа", items: [documentationSection("catalog-orders"), documentationSection("inventory-incidents"), documentationSection("marking"), documentationSection("integrations"), documentationSection("integration-status"), documentationSection("social"), documentationSection("sync"), documentationSection("master-data"), documentationSection("control"), documentationSection("monitoring")]},
  {title: "Администрирование и поддержка", items: [documentationSection("settings"), documentationSection("automation"), documentationSection("assistant"), documentationSection("security"), documentationSection("environment"), documentationSection("operations"), documentationSection("troubleshooting")]},
  {title: "Для разработчиков", items: [documentationSection("developer")]},
] as const;

export const docsTitle = "Документация TORGNEXA — интеграции, WMS и автоматизация";
export const docsDescription = "Официальная документация TORGNEXA: подключение маркетплейсов, интернет-магазинов, платежей и CRM, работа с каталогом, WMS, маркировкой, возвратами и автоматизацией.";

export const documentationPages = [
  {id: "interface", path: "/docs/getting-started", heading: "Первый вход и интерфейс", title: "Первый вход и интерфейс — документация TORGNEXA", description: "Как войти в TORGNEXA, выбрать рабочий контур и быстро найти нужный раздел интерфейса."},
  {id: "overview", path: "/docs/overview", heading: "Обзор", title: "Обзор TORGNEXA — документация", description: "Как читать операционные показатели, онбординг, состояние сервисов и задачи, требующие внимания."},
  {id: "catalog-orders", path: "/docs/catalog-and-orders", heading: "Каталог и заказы", title: "Каталог и заказы — документация TORGNEXA", description: "Как работать с товарами, изображениями, предложениями, заказами, статусами и безопасными повторными операциями."},
  {id: "inventory-incidents", path: "/docs/inventory-and-incidents", heading: "Остатки и инциденты", title: "Остатки и инциденты — документация TORGNEXA", description: "Как читать остатки и ATP, обрабатывать складские инциденты и безопасно маршрутизировать fulfillment."},
  {id: "marking", path: "/docs/marking", heading: "Маркировка и УПД", title: "Маркировка и УПД — документация TORGNEXA", description: "Как безопасно проверять коды, вести партии и упаковки, обрабатывать расхождения и готовить УПД."},
  {id: "integrations", path: "/docs/integrations", heading: "Интеграции", title: "Интеграции — документация TORGNEXA", description: "Пошаговое подключение маркетплейсов, интернет-магазинов, платежей, CRM и других внешних систем."},
  {id: "integration-status", path: "/docs/integration-status", heading: "Состояние интеграций", title: "Состояние интеграций — документация TORGNEXA", description: "Как читать фактическое состояние кабинетов, health history, ошибки, unknown и рекомендации по восстановлению."},
  {id: "social", path: "/docs/publications", heading: "Публикации", title: "Публикации — документация TORGNEXA", description: "Как создавать и планировать публикации в подключённых социальных каналах с контролем статуса и прав."},
  {id: "sync", path: "/docs/synchronization", heading: "Синхронизация", title: "Синхронизация — документация TORGNEXA", description: "Как настроить направления обмена, расписание, импорт, сверку и разбор расхождений."},
  {id: "master-data", path: "/docs/counterparties-and-finance", heading: "Контрагенты и финансы", title: "Контрагенты и финансы — документация TORGNEXA", description: "Как вести единые справочники юридических лиц, банковские реквизиты, платежи, курсы и расчёты."},
  {id: "control", path: "/docs/approvals-and-documents", heading: "Согласования и документы", title: "Согласования и документы — документация TORGNEXA", description: "Как управлять чувствительными операциями, сертификатами, ЭДО, МЧД и запросами приватности."},
  {id: "monitoring", path: "/docs/notifications-reports-audit", heading: "Уведомления, отчёты и аудит", title: "Уведомления, отчёты и аудит — документация TORGNEXA", description: "Как отслеживать ошибки, читать отчёты и подтверждать историю привилегированных действий."},
  {id: "settings", path: "/docs/settings", heading: "Настройки", title: "Настройки TORGNEXA — документация", description: "Как настроить профиль, рабочее пространство, роли, уведомления, интеграции, AI-провайдеров и безопасность."},
  {id: "automation", path: "/docs/automation", heading: "Автоматизация и расширения", title: "Автоматизация и расширения — документация TORGNEXA", description: "Как безопасно использовать AI-провайдеров, MCP, webhooks, n8n и плагины с ограниченными правами."},
  {id: "assistant", path: "/docs/ai-assistant", heading: "AI-помощник оператора", title: "AI-помощник оператора — документация TORGNEXA", description: "Как получать ответы по данным рабочего пространства с evidence, freshness и безопасными typed previews."},
  {id: "developer", path: "/docs/api-and-extensions", heading: "API и расширения", title: "API и расширения — документация TORGNEXA", description: "Как интегрировать TORGNEXA через REST API, SDK, webhooks, MCP и внешние расширения."},
  {id: "security", path: "/docs/security", heading: "Доступ и безопасность", title: "Доступ и безопасность — документация TORGNEXA", description: "Как устроены default deny, OIDC, секреты, tenant-контекст, аудит и согласования опасных действий."},
  {id: "environment", path: "/docs/environment", heading: "Переменные окружения .env", title: "Переменные окружения .env — документация TORGNEXA", description: "Справочник переменных Community-развёртывания: секреты, порты, OIDC, worker, ClamAV и уведомления."},
  {id: "operations", path: "/docs/operations", heading: "Эксплуатация Community-контура", title: "Эксплуатация TORGNEXA — документация", description: "Как запускать, проверять, обновлять и восстанавливать Community-контур TORGNEXA."},
  {id: "troubleshooting", path: "/docs/troubleshooting", heading: "Решение проблем", title: "Решение проблем TORGNEXA — документация", description: "Диагностика входа, API, интеграций, синхронизации и случайного раскрытия секрета."},
] as const;

export type DocumentationSectionId = (typeof documentationPages)[number]["id"];

const documentationPageById = new Map<string, (typeof documentationPages)[number]>(documentationPages.map(page => [page.id, page]));

export function documentationSectionIdForPath(pathname: string): DocumentationSectionId | undefined {
  const normalized = pathname.replace(/\/+$/, "") || "/";
  return documentationPages.find(page => page.path === normalized)?.id;
}

function documentationPathFor(id: string): string {
  return documentationPageById.get(id)?.path ?? "/docs";
}

function documentationPageForId(id: DocumentationSectionId) {
  return documentationPageById.get(id)!;
}

type DocumentationGuide = {
  audience: string;
  before: string;
  outcome: string;
  next?: {id: DocumentationSectionId; label: string};
};

const documentationGuides: Record<DocumentationSectionId, DocumentationGuide> = {
  interface: {
    audience: "Для нового пользователя или сотрудника поддержки",
    before: "Учётная запись и приглашение в рабочее пространство",
    outcome: "Вы войдёте, проверите рабочий контекст и найдёте нужный раздел",
    next: {id: "integrations", label: "Подключить первую интеграцию"},
  },
  overview: {
    audience: "Для руководителя и оператора",
    before: "Доступ к рабочему пространству",
    outcome: "Вы поймёте, где видны показатели, задачи и состояние сервисов",
    next: {id: "catalog-orders", label: "Перейти к каталогу и заказам"},
  },
  "catalog-orders": {
    audience: "Для контент-менеджера и оператора заказов",
    before: "Каталог и источник данных, с которым вы работаете",
    outcome: "Вы обновите товар, найдёте заказ и избежите повторной операции",
    next: {id: "inventory-incidents", label: "Проверить остатки"},
  },
  "inventory-incidents": {
    audience: "Для склада и fulfillment-команды",
    before: "Подключённый источник остатков и складские правила",
    outcome: "Вы отличите доступный остаток от резерва и разберёте инцидент",
    next: {id: "marking", label: "Проверить маркировку и УПД"},
  },
  marking: {
    audience: "Для складского оператора и специалиста по compliance",
    before: "GTIN/SKU, задание WMS, права на сканирование и активный контур маркировки",
    outcome: "Вы проверите код, поймёте причину отказа и не увеличите количество повторным сканом",
    next: {id: "integrations", label: "Подключить внешний API"},
  },
  integrations: {
    audience: "Для администратора или интеграционного специалиста",
    before: "Доступ провайдера, API/OAuth и нужные права",
    outcome: "Кабинет проверен, возможности ограничены, импорт готов к запуску",
    next: {id: "integration-status", label: "Проверить состояние кабинета"},
  },
  "integration-status": {
    audience: "Для оператора интеграций и первой линии поддержки",
    before: "Созданный кабинет, доступ к истории проверок и идентификатор внешней операции",
    outcome: "Вы отличите отключённый кабинет от временной ошибки и выберете безопасное восстановление",
    next: {id: "sync", label: "Настроить синхронизацию"},
  },
  social: {
    audience: "Для контент-команды",
    before: "Подключённый канал публикации и права на размещение",
    outcome: "Вы подготовите публикацию, запланируете её и проверите результат",
    next: {id: "monitoring", label: "Проверить доставку и аудит"},
  },
  sync: {
    audience: "Для оператора обмена и интеграций",
    before: "Активный кабинет и разрешённое направление обмена",
    outcome: "Вы запустите импорт, прочитаете сверку и поймёте причину расхождения",
    next: {id: "monitoring", label: "Настроить контроль ошибок"},
  },
  "master-data": {
    audience: "Для финансовой и ERP-команды",
    before: "Единые юридические лица, договоры и валютные правила",
    outcome: "Вы сохраните справочники и свяжете финансовые операции без дублей",
    next: {id: "control", label: "Настроить согласования"},
  },
  control: {
    audience: "Для approver и специалиста по compliance",
    before: "Роли, политика операции и исходные документы",
    outcome: "Вы проведёте чувствительную операцию через проверку и согласование",
    next: {id: "monitoring", label: "Проверить историю действий"},
  },
  monitoring: {
    audience: "Для операционной команды и поддержки",
    before: "Настроенные каналы уведомлений и доступ к аудиту",
    outcome: "Вы увидите ошибку вовремя, найдёте отчёт и подтвердите действие",
    next: {id: "troubleshooting", label: "Разобрать проблему"},
  },
  settings: {
    audience: "Для администратора рабочего пространства",
    before: "Права администратора и согласованные настройки",
    outcome: "Вы настроите роли, вход, уведомления, AI и интеграции",
    next: {id: "security", label: "Проверить безопасность"},
  },
  automation: {
    audience: "Для администратора платформы и разработчика",
    before: "Понятная задача автоматизации и минимальные права",
    outcome: "Вы подключите расширение, не превращая AI или MCP в обход контроля",
    next: {id: "assistant", label: "Открыть AI-помощника"},
  },
  assistant: {
    audience: "Для оператора, руководителя и сотрудника поддержки",
    before: "Разрешения на чтение модулей и вопрос, который можно подтвердить источниками",
    outcome: "Вы получите ответ с состоянием grounding, freshness и ссылками на первичные данные",
    next: {id: "troubleshooting", label: "Перейти к диагностике"},
  },
  developer: {
    audience: "Для backend- и integration-разработчика",
    before: "Версия API, схема данных и тестовый рабочий контур",
    outcome: "Вы спроектируете вызовы, webhooks и повторяемые операции",
    next: {id: "security", label: "Проверить границы доступа"},
  },
  security: {
    audience: "Для администратора и специалиста по безопасности",
    before: "Модель ролей, доверенные адреса и процедура ротации",
    outcome: "Вы проверите default deny, секреты, tenant-контекст и аудит",
    next: {id: "operations", label: "Перейти к эксплуатации"},
  },
  environment: {
    audience: "Для администратора Community-развёртывания",
    before: "Сервер, Docker и безопасное место для секретов",
    outcome: "Вы заполните .env, проверите конфигурацию и запустите контур",
    next: {id: "operations", label: "Проверить работу контура"},
  },
  operations: {
    audience: "Для release- и ops-инженера",
    before: "Резервная копия, точный тег и план восстановления",
    outcome: "Вы обновите Community безопасно и подтвердите доступность сервисов",
    next: {id: "troubleshooting", label: "Открыть диагностику"},
  },
  troubleshooting: {
    audience: "Для любого пользователя и первой линии поддержки",
    before: "Симптом, время ошибки и рабочее пространство",
    outcome: "Вы определите слой проблемы и соберёте безопасное свидетельство",
  },
};

export const troubleshootingFaq = [
  {question: "Слишком часто появляется экран входа", answer: "Access token должен обновляться автоматически. Проверьте доступность /oidc/silent-callback.html, Web Origins и политику встраивания провайдера. Повторный вход ожидаем после окончания SSO-сессии, выхода или отзыва."},
  {question: "Раздел отсутствует в меню", answer: "Проверьте разрешения пользователя и текущее рабочее пространство. Скрытие пункта — следствие правил доступа, а не удаление данных."},
  {question: "Не удалось загрузить данные", answer: "Проверьте API health и сессию. Ответ 403 означает отсутствие разрешения, 409 — конфликт версии, 429 — превышение лимита."},
  {question: "Интеграция не активируется", answer: "Откройте карточку площадки и проверьте учётные данные, параметры OAuth, разрешения и проверку доступности."},
  {question: "Синхронизация не запускается", answer: "Убедитесь, что кабинет активен, политика создана, направление поддерживается, а предыдущий запуск не требует решения оператора."},
  {question: "Секрет случайно раскрыт", answer: "Немедленно отзовите его у провайдера, выполните ротацию и проверьте аудит. Удаления текста из UI недостаточно."},
] as const;

const documentationGlossary = [
  ["Кабинет", "Сохранённая связь TORGNEXA с одной внешней системой. В одном рабочем пространстве может быть несколько кабинетов."],
  ["Возможность", "Конкретное действие, которое разрешено карточке: например, чтение товаров или запись цены."],
  ["Проверка доступности", "Реальный безопасный запрос к официальному API. Он подтверждает доступ, но сам по себе не включает обмен данными."],
  ["ATP", "Доступный остаток: физическое количество за вычетом резерва, карантина и уже распределённых единиц."],
  ["Сверка", "Сравнение состояния TORGNEXA и внешней системы с перечнем расхождений, которые нужно решить."],
  ["Idempotency key", "Ключ повторяемости. Он помогает безопасно повторить запрос, не создав вторую операцию."],
] as const;

const DocumentationSectionContext = createContext<DocumentationSectionId | undefined>(undefined);

function DocumentationMetadata({page}: {page: {path: string; heading: string; title: string; description: string}}) {
  useEffect(() => {
    const previousTitle = document.title;
    const managed: Array<{element: HTMLMetaElement | HTMLLinkElement; previous: string | null; attribute: "content" | "href"; created: boolean}> = [];
    const setHeadValue = (tag: "meta" | "link", selector: string, attribute: "content" | "href", value: string, attributes: Record<string, string>) => {
      let element = document.head.querySelector(selector) as HTMLMetaElement | HTMLLinkElement | null;
      const created = !element;
      if (!element) {
        element = document.createElement(tag) as HTMLMetaElement | HTMLLinkElement;
        Object.entries(attributes).forEach(([name, attributeValue]) => element!.setAttribute(name, attributeValue));
        document.head.appendChild(element);
      }
      managed.push({element, previous: element.getAttribute(attribute), attribute, created});
      element.setAttribute(attribute, value);
    };

    const canonical = `${window.location.origin}${page.path}`;
    document.title = page.title;
    setHeadValue("meta", 'meta[name="description"]', "content", page.description, {name: "description"});
    setHeadValue("meta", 'meta[property="og:title"]', "content", page.title, {property: "og:title"});
    setHeadValue("meta", 'meta[property="og:description"]', "content", page.description, {property: "og:description"});
    setHeadValue("meta", 'meta[property="og:type"]', "content", "article", {property: "og:type"});
    setHeadValue("meta", 'meta[property="og:locale"]', "content", "ru_RU", {property: "og:locale"});
    setHeadValue("meta", 'meta[property="og:url"]', "content", canonical, {property: "og:url"});
    setHeadValue("meta", 'meta[name="twitter:card"]', "content", "summary", {name: "twitter:card"});
    setHeadValue("link", 'link[rel="canonical"]', "href", canonical, {rel: "canonical"});

    const breadcrumbItems = [
      {"@type": "ListItem", position: 1, name: "TORGNEXA", item: window.location.origin},
      {"@type": "ListItem", position: 2, name: "Документация", item: `${window.location.origin}/docs`},
    ];
    if (page.path !== "/docs") breadcrumbItems.push({"@type": "ListItem", position: 3, name: page.heading, item: canonical});
    const structuredDataGraph: Array<Record<string, unknown>> = [
      {
        "@type": "TechArticle",
        "@id": `${canonical}#article`,
        headline: page.title,
        description: page.description,
        inLanguage: "ru-RU",
        url: canonical,
        mainEntityOfPage: canonical,
        isPartOf: {"@type": "TechArticle", "@id": `${window.location.origin}/docs#article`, url: `${window.location.origin}/docs`, name: docsTitle},
        publisher: {"@type": "Organization", name: "TORGNEXA"},
      },
      {
        "@type": "BreadcrumbList",
        "@id": `${canonical}#breadcrumb`,
        itemListElement: breadcrumbItems,
      },
    ];
    if (page.path === "/docs/troubleshooting") {
      structuredDataGraph.push({
        "@type": "FAQPage",
        "@id": `${canonical}#faq`,
        mainEntity: troubleshootingFaq.map(({question, answer}) => ({
          "@type": "Question",
          name: question,
          acceptedAnswer: {"@type": "Answer", text: answer},
        })),
      });
    }
    const structuredData = document.createElement("script");
    structuredData.type = "application/ld+json";
    structuredData.textContent = JSON.stringify({
      "@context": "https://schema.org",
      "@type": "TechArticle",
      "@graph": structuredDataGraph,
    });
    document.head.appendChild(structuredData);

    return () => {
      document.title = previousTitle;
      managed.reverse().forEach(({element, previous, attribute, created}) => {
        if (created) element.remove();
        else if (previous === null) element.removeAttribute(attribute);
        else element.setAttribute(attribute, previous);
      });
      structuredData.remove();
    };
  }, []);
  return null;
}

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
      <tr><td><strong>1С-Битрикс</strong></td><td><code>{`{ user_id, webhook_code }`}</code></td><td><code>store_host</code>, <code>base_path</code>, <code>catalog_iblock_id</code>, <code>store_currency</code>, <code>price_type_id</code>; товары read/write, регулярные цены на запись.</td></tr>
      <tr><td><strong>CS-Cart</strong></td><td><code>{`{ email, api_key }`}</code></td><td><code>store_host</code>, <code>base_path</code>, <code>store_currency</code>; официальный REST API 2.0, товары read/write, базовые цены, остатки, заказы read и стандартные статусы заказов write.</td></tr>
      <tr><td><strong>OpenCart</strong></td><td>Bearer token модуля TORGNEXA</td><td><code>store_host</code>, <code>base_path</code>, <code>store_currency</code>; сначала установите <code>torgnexa.ocmod.zip</code> — он добавляет <code>extension/torgnexa/api/*</code>, затем доступны товары read/write.</td></tr>
      <tr><td><strong>Shopify</strong></td><td>OAuth client JSON</td><td><code>shop_domain</code>, <code>store_currency</code>; OAuth с host-owned refresh, товары read/write.</td></tr>
      <tr><td><strong>Bitrix24 CRM</strong></td><td>OAuth client JSON</td><td><code>portal_host</code>; лиды, сделки, контакты, компании и товарные строки в отдельном CRM-контуре.</td></tr>
      <tr><td><strong>Telegram / MAX</strong></td><td>Токен бота</td><td>Числовой <code>chat_id</code> в параметрах среды; рабочий сценарий — текстовые публикации, лимиты 4096 / 4000.</td></tr>
      <tr><td><strong>СДЭК / ПЭК / Почта России</strong></td><td>Учётные данные провайдера</td><td>После <code>healthy</code> доступны отдельные read-only операции: тарифы, отслеживание и ПВЗ. У СДЭК дополнительно доступны создание, отмена и формирование PDF-этикетки; создание и отмена проходят через approval-bound worker.</td></tr>
      <tr><td><strong>Деловые Линии</strong></td><td>Учётные данные провайдера</td><td>После <code>healthy</code> доступны bounded чтение терминалов/ПВЗ, предпросмотр тарифа и отслеживание истории статусов по номеру документа; операции отправлений требуют отдельной qualification.</td></tr>
      <tr><td><strong>5Post / Ozon Доставка</strong></td><td>Учётные данные провайдера</td><td>Сейчас доступно создание кабинета и официальная проверка доступности. Заявленные в SDK отправления, тарифы, трекинг и этикетки не включаются без runtime-квалификации.</td></tr>
      <tr><td><strong>СБП / YooKassa / Robokassa</strong></td><td>JSON учётных данных провайдера</td><td>Отдельный раздел «Финансы»; разрешения различаются, возврат доступен только при <code>payments.refund</code>.</td></tr>
      <tr><td><strong>Долями</strong></td><td>Логин/пароль и mTLS-сертификат</td><td>Платёжная карточка только для проверки доступности; Create, Commit, Cancel, Info, Refund и вебхуки пока не включены.</td></tr>
    </tbody></table></div>
    <Callout title="Не угадывайте формат JSON" tone="warning">Подсказка placeholder в drawer является частью контракта карточки. Если провайдер требует поля, которых нет в подсказке, сначала обновите manifest/connector spec и квалификацию, а не обходите форму произвольным payload.</Callout>

    <h3>Что означает «только проверка доступности»</h3>
    <p>«Долями», Lamoda, М.Видео, Auto.ru, Avito, CIAN, Chestny ZNAK, Diadoc, EGAIS, Instagram, Odnoklassniki, RUTUBE, Saby EDO, Threads, VetIS/Mercury, VK и YouTube сейчас можно подключить в своей категории, сохранить учётные данные и проверить официальный адрес API. Это подтверждает доступ к API, но не включает синхронизацию товаров, цены, остатки, заказы, публикацию, сообщения, ЭДО или регулируемую запись; рабочая связка появится только после отдельной квалификации.</p>
    <Callout title="Логистика в отдельной панели">Для СДЭК, ПЭК и Почты России после успешной проверки и активации карточка показывает доступные операции тарифа, трекинга и ПВЗ. Для СДЭК при включённой capability доступны approval-bound форма создания отправления и запрос PDF-этикетки. Состав кнопок строится из текущего runtime-каталога, поэтому SDK-возможность сама по себе не означает, что операция доступна оператору.</Callout>
  </>;
}

function IntegrationQualificationGuides() {
  return <div className="docs-qualification-guides">
    <div className="docs-subsection-heading"><div><h3>Отдельные инструкции для storefront</h3><p>Эти стенды нужны для protocol smoke и qualification. Они не заменяют настройку рабочего кабинета и не должны получать боевые секреты.</p></div><span>6 инструкций</span></div>
    <details id="opencart-smoke" className="docs-details"><summary><strong>OpenCart</strong><span>OCMOD bridge, каталог, цены, остатки и заказы</span><i aria-hidden="true">+</i></summary><OpenCartDockerGuide/></details>
    <details id="woocommerce-smoke" className="docs-details"><summary><strong>WooCommerce</strong><span>REST API и проверка синтетической витрины</span><i aria-hidden="true">+</i></summary><WooCommerceDockerGuide/></details>
    <details id="prestashop-smoke" className="docs-details"><summary><strong>PrestaShop</strong><span>Webservice API и проверка каталога</span><i aria-hidden="true">+</i></summary><PrestaShopDockerGuide/></details>
    <details id="saleor-smoke" className="docs-details"><summary><strong>Saleor</strong><span>GraphQL API, channel и warehouse</span><i aria-hidden="true">+</i></summary><SaleorDockerGuide/></details>
    <details id="shopify-smoke" className="docs-details"><summary><strong>Shopify</strong><span>Protocol smoke и Dev Store qualification</span><i aria-hidden="true">+</i></summary><ShopifyDockerGuide/></details>
    <details id="shopware-smoke" className="docs-details"><summary><strong>Shopware 6</strong><span>Admin API и OAuth2 client credentials</span><i aria-hidden="true">+</i></summary><ShopwareDockerGuide/></details>
  </div>;
}

function OpenCartDockerGuide() {
  return <div className="docs-opencart-guide">
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
    <DocsScreenshot src="/docs/opencart-smoke.png" width={1265} height={712} alt="Страница документации TORGNEXA с инструкцией OpenCart Docker smoke-test" caption="Инструкция доступна в публичной документации: команды, ожидаемые проверки и очистка тестового стека."/>
    <DocsScreenshot src="/docs/opencart-store.png" width={1265} height={712} alt="Демо-магазин OpenCart с синтетическими товарами TORGNEXA" caption="Демо-магазин после seed: три синтетических товара видны через обычный поиск OpenCart."/>
    <p>Полный текст, список endpoint-проверок и совместимость со схемой OpenCart 4.1 находятся в <code>docs/connectors/opencart/docker-smoke.md</code>.</p>
  </div>;
}

function WooCommerceDockerGuide() {
  return <div className="docs-opencart-guide">
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
    <DocsScreenshot src="/docs/woocommerce-guide.png" width={1265} height={712} alt="Страница документации TORGNEXA с инструкцией WooCommerce Docker smoke-test" caption="Инструкция запуска, проверки и очистки WooCommerce-стенда."/>
    <DocsScreenshot src="/docs/woocommerce-store.png" width={1265} height={1212} alt="Демо-магазин WooCommerce с синтетическими товарами TORGNEXA" caption="Демо-витрина WooCommerce после загрузки синтетических товаров."/>
    <p>Полный текст и ограничения квалификации находятся в <code>docs/connectors/woocommerce/docker-smoke.md</code>. Рабочий процесс TORGNEXA сейчас маршрутизирует только сущность <code>products</code>; дополнительные REST-возможности не расширяют среду автоматически.</p>
  </div>;
}

function PrestaShopDockerGuide() {
  return <div className="docs-opencart-guide">
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
    <DocsScreenshot src="/docs/prestashop-guide.png" width={1265} height={712} alt="Страница документации TORGNEXA с инструкцией PrestaShop Webservice smoke-test" caption="Инструкция запуска, проверки официального Webservice API и очистки стенда."/>
    <DocsScreenshot src="/docs/prestashop-store.png" width={1265} height={712} alt="Демо-магазин PrestaShop с синтетическими товарами TORGNEXA" caption="Демо-витрина PrestaShop после seed: синтетические товары доступны через обычный storefront."/>
    <p>Полный список запросов, ограничения ключа и формат официальных JSON/XML ответов находятся в <code>docs/connectors/prestashop/docker-smoke.md</code>. Рабочий процесс TORGNEXA по-прежнему маршрутизирует только сущность <code>products</code>; возможности манифеста не превращаются в автоматическую синхронизацию.</p>
  </div>;
}

function SaleorDockerGuide() {
  return <div className="docs-opencart-guide">
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
  return <div className="docs-opencart-guide">
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

function ShopwareDockerGuide() {
  return <div className="docs-opencart-guide">
    <h3>Shopware 6: проверка Admin API в Docker Compose</h3>
    <p>Shopware использует Admin API под <code>/api/*</code> и OAuth2 <code>client_credentials</code> для Integration. Disposable Compose-стенд на loopback использует community-образ Dockware с демо-каталогом; это не production-магазин и не merchant staging.</p>
    <ol className="docs-steps compact">
      <li><strong>Запустите стенд</strong><span>Команды выполняются из корня репозитория. API будет доступен только на порту 18005.</span><pre><code>docker compose -f docker-compose.shopware-test.yml up -d; docker compose -f docker-compose.shopware-test.yml ps</code></pre></li>
      <li><strong>Создайте временную Integration</strong><span>Выведите access key и secret внутри контейнера, сохраните их только в текущем shell и не коммитьте.</span><pre><code>{`docker compose -f docker-compose.shopware-test.yml exec -T shopware bash -lc 'cd /var/www/html && php bin/console integration:create --admin --no-interaction smoke-torgnexa'`}</code></pre></li>
      <li><strong>Запустите credentialed smoke</strong><span>Проверяются OAuth, отказ без bearer, JSON:API/flat response mapping, каталог, EUR price, stock, orders, refunds, записи и read-after-write cleanup.</span><pre><code>{`SHOPWARE_BASE_URL=http://127.0.0.1:18005 SHOPWARE_ALLOW_HTTP=1 SHOPWARE_HOST_HEADER=localhost SHOPWARE_CLIENT_ID=... SHOPWARE_CLIENT_SECRET=... SHOPWARE_TEST_SKU=SWDEMO10002 SHOPWARE_STORE_CURRENCY=EUR SHOPWARE_ALLOW_WRITES=1 scripts/shopware-smoke.sh`}</code></pre></li>
      <li><strong>Удалите стенд</strong><span>После проверки удалите только Shopware-контейнер и его disposable данные.</span><pre><code>docker compose -f docker-compose.shopware-test.yml down -v</code></pre></li>
    </ol>
    <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Демо-объект</th><th>Значение</th><th>Что подтверждает</th></tr></thead><tbody>
      <tr><td><code>SWDEMO10002</code></td><td>EUR price, stock 10</td><td>catalog/detail, currency/price и inventory read</td></tr>
      <tr><td><code>search/order</code></td><td>bounded read</td><td>orders и state-machine shape</td></tr>
      <tr><td><code>order-transaction-capture-refund</code></td><td>bounded read</td><td>фактический public refunds route</td></tr>
    </tbody></table></div>
    <Callout title="Только локальная проверка" tone="warning">Порт <code>18005</code>, Integration key/secret и demo-данные синтетические. Dockware — community-supported image; для внешнего staging используйте HTTPS, scoped Integration и отдельный SKU. Product create, incoming webhooks и необратимая отмена заказа остаются закрыты.</Callout>
    <p>Полная процедура и результат находятся в <code>docs/connectors/shopware/docker-live-qualification.md</code> и <code>docs/connectors/shopware/live-qualification-status.json</code>. Docker smoke прошёл 2026-08-29; merchant qualification требует отдельного endpoint.</p>
  </div>;
}

const routes = [
  ["Обзор", "/", "Показатели, онбординг и состояние операционного контура"],
  ["Каталог", "/catalog", "Товары, предложения, категории и изображения"],
  ["Качество публикации", "/publication-quality", "Target-specific preflight, score, blockers и gate receipts"],
  ["Публикация товаров", "/marketplace-publications", "Безопасная публикация карточек на подключённые маркетплейсы"],
  ["Заказы", "/orders", "Поиск, статусы и подробности заказов"],
  ["Возвраты", "/returns", "Возвраты, отмены, инспекции и связанные refunds"],
  ["Остатки", "/inventory", "Позиции, склады, инциденты, fulfillment и импорт"],
  ["Инциденты", "/incidents", "Отклонения, сбои складов и действия оператора"],
  ["Маркировка", "/marking", "Проверка кодов, партии, упаковки и УПД"],
  ["Состояние интеграций", "/integrations/status", "Состояние подключений, проверки и история доступности"],
  ["Интеграции", "/integrations", "Подключения маркетплейсов и внешних систем"],
  ["Публикации", "/social", "Текстовые публикации в подключённые социальные каналы"],
  ["Синхронизация", "/sync", "Политики, запуски и расхождения"],
  ["Контрагенты", "/counterparties", "Единый справочник юридических лиц и ролей"],
  ["Закупки", "/procurement", "Поставщики, прайс-листы, заказы поставщикам и reconciliation"],
  ["Финансы", "/finance", "Расчёты площадок, курсы валют и платежи"],
  ["Финансовая аналитика", "/finance/analytics", "P&L, денежный поток, FIFO-себестоимость и качество данных"],
  ["Согласования", "/approvals", "Политики и выполнение чувствительных операций"],
  ["Автоматизации", "/workflows", "Версионируемые workflow, триггеры, условия, повторы и согласования"],
  ["Сертификаты и документы", "/compliance", "Разрешительные документы и запросы приватности"],
  ["Уведомления", "/notifications", "Ошибки, предупреждения и системные события"],
  ["Отчёты", "/reports", "Аналитика, фильтры и экспорт"],
  ["AI-помощник", "/assistant", "Ответы по разрешённым данным с evidence и deep links"],
  ["Безопасность", "/security", "Активные сессии, история входов и журнал изменений"],
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
      ["TORGNEXA_PUBLIC_URL", "http://127.0.0.1:5173", "Публичный HTTPS-адрес для canonical, Open Graph и sitemap. В production замените на реальный домен."],
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
  const activeSection = useContext(DocumentationSectionContext);
  if (activeSection && activeSection !== id) return null;
  const dedicatedPage = activeSection === id;
  return <section id={id}>
    {dedicatedPage ? null : <header className="docs-section-heading"><div><p className="eyebrow">РУКОВОДСТВО</p><h2>{title}</h2>{intro ? <p>{intro}</p> : null}</div><a href="/docs" aria-label="К началу документации">↑</a></header>}
    {children}
  </section>;
}

function Callout({title, tone = "info", children}: {title: string; tone?: "info" | "warning" | "success"; children: ReactNode}) {
  return <div className={`docs-callout ${tone}`}><strong>{title}</strong><span>{children}</span></div>;
}

function FeatureGrid({items}: {items: readonly (readonly [string, string])[]}) {
  return <div className="docs-feature-grid">{items.map(([title, copy]) => <article key={title}><h3>{title}</h3><p>{copy}</p></article>)}</div>;
}

function DocumentationPageGuide({guide}: {guide: DocumentationGuide}) {
  return <section className="docs-page-guide" aria-labelledby="docs-guide-title">
    <div className="docs-guide-heading"><p className="eyebrow">КОРОТКО</p><h2 id="docs-guide-title">Как пройти этот раздел</h2><p>Сначала проверьте исходные условия, затем выполните действия по порядку. Так проще отличить настройку от результата проверки.</p></div>
    <div className="docs-guide-grid">
      <article><span>Для кого</span><strong>{guide.audience}</strong></article>
      <article><span>Перед началом</span><strong>{guide.before}</strong></article>
      <article><span>В результате</span><strong>{guide.outcome}</strong></article>
    </div>
    {guide.next ? <a className="docs-next-step" href={documentationPathFor(guide.next.id)}><span>Следующий шаг</span><strong>{guide.next.label}</strong><span aria-hidden="true">→</span></a> : <p className="docs-guide-finish"><strong>Если результат другой</strong><span>Откройте симптом ниже и зафиксируйте время, рабочее пространство и идентификатор операции.</span></p>}
  </section>;
}

function DocumentationGlossary() {
  return <div className="docs-glossary" aria-label="Короткий словарь терминов">
    {documentationGlossary.map(([term, explanation]) => <article key={term}><h3>{term}</h3><p>{explanation}</p></article>)}
  </div>;
}

function DocsScreenshot({src, alt, caption, width, height}: {src: string; alt: string; caption: string; width: number; height: number}) {
  return <figure className="docs-screenshot"><img src={src} alt={alt} width={width} height={height} loading="lazy" decoding="async"/><figcaption>{caption}</figcaption></figure>;
}

function EnvironmentTables() {
  return <div className="docs-env-groups">{environmentGroups.map((group) => <div className="docs-env-group" key={group.title}>
    <h3>{group.title}</h3>
    <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Переменная</th><th>По умолчанию</th><th>Назначение и ограничения</th></tr></thead><tbody>
      {group.rows.map(([name, fallback, description]) => <tr key={name}><td><code>{name}</code></td><td><code>{fallback}</code></td><td>{description}</td></tr>)}
    </tbody></table></div>
  </div>)}</div>;
}

export function PublicDocumentationPage({sectionId}: {sectionId?: DocumentationSectionId} = {}) {
  const activeSection = sectionId ?? (typeof window === "undefined" ? undefined : documentationSectionIdForPath(window.location.pathname));
  const activePage = activeSection ? documentationPageForId(activeSection) : undefined;
  const activeGuide = activeSection ? documentationGuides[activeSection] : undefined;
  const page = activePage ?? {path: "/docs", title: docsTitle, description: docsDescription, heading: "Документация TORGNEXA"};
  return <div className="docs-shell">
    <DocumentationMetadata page={page}/>
    <header className="docs-header">
      <a className="docs-brand" href="/"><span className="docs-brand-logo"><img src="/brand/torgnexa-logo.png" alt="TORGNEXA" width="985" height="145" loading="lazy" decoding="async"/></span><span><small>Документация</small></span></a>
      <nav aria-label="Навигация документации"><a href="/docs">Руководство</a><a className="docs-login-link" href="/">Войти</a></nav>
    </header>
    <div className="docs-layout">
      <nav className="docs-toc" aria-label="Разделы документации"><strong><a href="/docs" aria-current={!activeSection ? "page" : undefined}>Содержание</a></strong>{documentationNavigation.map(group => <div className="docs-toc-group" key={group.title}><span>{group.title}</span>{group.items.map(([id, label]) => <a key={id} href={documentationPathFor(id)} aria-current={activeSection === id ? "page" : undefined}>{label}</a>)}</div>)}</nav>
      <main className="docs-content">
        {activePage && activeGuide ? <>
          <section className="docs-subpage-intro">
            <nav className="docs-breadcrumbs" aria-label="Хлебные крошки"><a href="/">TORGNEXA</a><span aria-hidden="true">›</span><a href="/docs">Документация</a><span aria-hidden="true">›</span><span>{activePage.heading}</span></nav>
            <div className="docs-version"><span>Руководство пользователя</span><span>Тематический раздел</span></div>
            <h1>{activePage.heading}</h1>
            <p className="docs-lead">{activePage.description}</p>
          </section>
          <DocumentationPageGuide guide={activeGuide}/>
        </> : <section className="docs-hero" id="start">
          <nav className="docs-breadcrumbs" aria-label="Хлебные крошки"><a href="/">TORGNEXA</a><span aria-hidden="true">›</span><span>Документация</span></nav>
          <div className="docs-version"><span>Руководство пользователя</span><span>Текущий интерфейс</span></div>
          <h1>Документация TORGNEXA</h1>
          <p className="docs-lead">Понятное руководство для e-commerce-команд: как подключить маркетплейс, интернет-магазин, платежи или CRM, работать с каталогом и заказами и безопасно запускать синхронизацию.</p>
          <div className="docs-hero-actions"><a className="button primary" href="/">Войти в TORGNEXA</a><a className="button secondary" href={documentationPathFor("interface")}>С чего начать</a></div>
          <div className="docs-reading-paths" aria-label="Сценарии чтения"><a href={documentationPathFor("interface")}><strong>Я впервые в TORGNEXA</strong><span>Вход, роли и навигация</span></a><a href={documentationPathFor("integrations")}><strong>Подключаю интеграцию</strong><span>Кабинет, проверка и импорт</span></a><a href={documentationPathFor("environment")}><strong>Разворачиваю Community</strong><span>.env, Docker и эксплуатация</span></a><a href={documentationPathFor("developer")}><strong>Интегрирую по API</strong><span>Контракты и расширения</span></a></div>
          <div className="docs-install-address"><span>Адрес локальной установки</span><code>http://127.0.0.1:5173</code></div>
        </section>}

        <DocumentationSectionContext.Provider value={activeSection}>
        <DocSection id="interface" title="Первый вход и интерфейс" intro="Публичная документация открывается без авторизации. Рабочие разделы становятся доступны после входа.">
          <ol className="docs-steps">
            <li><strong>Откройте приложение</strong><span>На стартовом экране нажмите «Войти». Технический термин OIDC пользователю в кнопке не показывается.</span></li>
            <li><strong>Пройдите проверку личности</strong><span>Используйте учётную запись, выданную администратором. Пароль обрабатывает провайдер входа, а не TORGNEXA.</span></li>
            <li><strong>Проверьте рабочий контекст</strong><span>В верхней панели отображаются организация и рабочее пространство, полученные из проверенной сессии.</span></li>
            <li><strong>Начните с «Обзора»</strong><span>Интерфейс покажет доступные шаги и проблемы, требующие внимания.</span></li>
          </ol>
          <DocsScreenshot src="/docs/login.png" width={1280} height={720} alt="Экран входа TORGNEXA с кнопкой «Войти»" caption="Публичный экран входа: одна понятная кнопка «Войти» и ссылка на это руководство."/>
          <DocsScreenshot src="/docs/mobile.png" width={415} height={830} alt="Мобильная версия интерфейса TORGNEXA с компактной навигацией" caption="На узком экране навигация сворачивается, а таблицы и пошаговые карточки остаются прокручиваемыми."/>
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
          <h3>Словарь без лишнего жаргона</h3>
          <p>В документации встречаются технические слова, которые описывают реальные проверки и ограничения. Ниже — короткий перевод на язык ежедневной работы.</p>
          <DocumentationGlossary/>
          <p>Тема, плотность таблиц и мобильное меню сохраняются в интерфейсе без превращения браузера в источник бизнес-состояния. Плашка состояния подключения показывает только состояние SSE-канала инвалидаций: данные всё равно перечитываются обычными API с проверкой прав.</p>
          <Callout title="Данные не появились сразу?">После подключения кабинета сначала запустите первоначальный импорт в «Синхронизации». Пустой обзор до импорта не означает потерю данных.</Callout>
        </DocSection>

        <DocSection id="catalog-orders" title="Каталог и заказы" intro="Карточки и заказы хранятся в канонической модели, независимо от форматов конкретной площадки.">
          <div className="docs-split">
            <div><h3>Каталог</h3><ul><li>Используйте серверный поиск и курсорную пагинацию по 25 строк.</li><li>При создании товара код, название, описание и изображение заполняются во вкладке «Основные данные»; после сохранения доступны «Предложения», «Категории» и отдельное управление изображениями.</li><li>Загруженное изображение проходит карантин и проверку; HTTPS-ссылка проверяется до публикации.</li><li>Публикация учитывает обязательные поля, возможности коннектора и требования compliance.</li></ul></div>
            <div><h3>Заказы</h3><ul><li>Фильтруйте по поиску и нормализованному статусу через серверный API.</li><li>В боковой панели проверяйте источник, состав, суммы и историю заказа.</li><li>Не создавайте повторную операцию с новым idempotency key, пока результат первой неизвестен.</li><li>Ошибки доставки данных разбирайте через синхронизацию и инциденты.</li></ul></div>
            <div><h3>Возвраты</h3><ul><li>Раздел «Возвраты» показывает жизненный цикл от запроса до закрытия, позиции, quantities и disposition.</li><li>Переходы статуса используют optimistic version и ключ идемпотентности; конфликт версии требует обновить карточку.</li><li>Инспекция и refund allocation фиксируются отдельными API-операциями и не переписывают историю.</li></ul></div>
          </div>
          <h3>Возврат, отмена и возврат денег</h3>
          <p>Это три разные операции: отмена заказа меняет состояние заказа, физический возврат проходит через приёмку и инспекцию, а refund относится к уже захваченной оплате. Поэтому refund не доказывает, что товар принят, а disposition не должен менять платёжную историю задним числом.</p>
          <ol className="docs-steps compact">
            <li><strong>Откройте карточку возврата</strong><span>Проверьте заказ, причину, валюту, requested/received/accepted quantity и связанные позиции.</span></li>
            <li><strong>Переведите возврат по state machine</strong><span>Допустимый путь: requested → approved → authorized → in_transit → received → inspecting → accepted, partially_accepted или rejected → closed.</span></li>
            <li><strong>Зафиксируйте результат инспекции</strong><span>Выберите disposition: restock, quarantine, scrap или replace. Частичный возврат сохраняет точное decimal-количество.</span></li>
            <li><strong>Создайте refund allocation отдельно</strong><span>Сумма не может превысить захваченную оплату; sensitive-операция проходит capability, policy, approval и аудит.</span></li>
          </ol>
          <DocsScreenshot src="/docs/returns.png" width={1265} height={712} alt="Карточка возврата TORGNEXA со статусом, позициями и действиями инспекции" caption="Карточка возврата: оператор видит допустимое следующее состояние и не смешивает физическую приёмку с возвратом денег."/>
          <h3>Качество публикации</h3>
          <p>Перед записью в канал откройте <code>/publication-quality</code> и выберите target: товар/offer, кабинет, connector, канал, locale и jurisdiction. Проверка сохраняет snapshot/profile digest, score, категории проблем и gate receipt. Статус <code>ready</code> разрешает запись; <code>ready_with_warnings</code> показывает предупреждения, а <code>blocked</code>, <code>approval_required</code>, <code>stale</code>, <code>unsupported</code> и <code>unknown</code> останавливают публикацию.</p>
          <ul><li><strong>Сначала исправьте issues</strong> — обязательное поле, media, цена, остаток, capability или compliance evidence.</li><li><strong>Затем повторите preflight</strong> — старый receipt нельзя использовать после изменения товара, профиля или runtime capability.</li><li><strong>Для remediation используйте предложение исправления</strong> — оно содержит expected snapshot digest, а применение проходит обычной Product/PIM-командой с optimistic version.</li></ul>
          <DocsScreenshot src="/docs/publication-quality.png" width={1265} height={712} alt="Центр качества публикации TORGNEXA со score, решением и проблемами карточки" caption="Центр качества: решение публикации и причины блокировки видны до внешней записи."/>
          <Callout title="Деньги и количества">Суммы передаются в минимальных единицах вместе с валютой. Дробные количества используют точное decimal-представление.</Callout>
        </DocSection>

        <DocSection id="inventory-incidents" title="Остатки и инциденты" intro="Остатки отражают доступное количество, резервы, состояние складов и исключения fulfillment. Для кодов маркировки предусмотрен отдельный раздел «Маркировка» со сканированием и контролем партий.">
          <div className="docs-tab-guide">
            <article><strong>Позиции</strong><span>Физический остаток, резерв и доступный ATP по товару и складу.</span></article>
            <article><strong>Склады</strong><span>Операционное состояние и приоритетный резервный склад.</span></article>
            <article><strong>Инциденты</strong><span>Нарушения маршрута и задачи, которые нельзя завершить автоматически.</span></article>
            <article><strong>Fulfillment</strong><span>Ручной резерв позиции заказа — только для исключительных случаев.</span></article>
            <article><strong>Импорт</strong><span>Безопасное массовое обновление из подготовленного источника.</span></article>
          </div>
          <p>Перевод склада в недоступное или утраченное состояние открывает инцидент и пытается перенести обязательства на резервный склад. Физический товар при этом не «перемещается» автоматически. Заказы без маршрута остаются видимыми оператору. В «Инцидентах» объединяются warehouse incidents, открытые расхождения, проблемные кабинеты и ожидающие согласования; строки ведут в соответствующий рабочий раздел.</p>
          <h3>Операторские задачи WMS</h3>
          <div className="docs-feature-grid">
            <article><h3>Подбор и упаковка</h3><p>Задача связывается с заказом, складом и точным количеством. Шаги можно взять в работу, сканировать, завершить или перевести в исключение.</p></article>
            <article><h3>Приёмка и размещение</h3><p>Отдельные задачи фиксируют фактическое количество, место и оператора; расхождение не исправляет остаток задним числом.</p></article>
            <article><h3>Пересчёт</h3><p>Cycle count создаёт проверяемое свидетельство по позиции и локации, а корректировка проходит как новая запись журнала.</p></article>
            <article><h3>Передача в pack area</h3><p>До 50 завершённых задач подбора можно собрать в видимую аудируемую batch-передачу. Marketplace shipment и автоматическое списание не заявляются этим контуром.</p></article>
          </div>
          <p>Сканирование сохраняет только SHA-256 отпечаток штрихкода, локацию, точное количество, исполнителя и время UTC. Это позволяет подтвердить операцию, не складывая необязательные данные о товаре в событие.</p>
          <p>В очереди WMS оператор может отфильтровать задачу по состоянию и типу, открыть карточку, взять её в работу, выполнить сканирование, завершить, отменить или зафиксировать exception. Передача в pack area объединяет до 50 завершённых pick-задач одного склада, но не подтверждает отгрузку на маркетплейсе и не списывает остаток автоматически.</p>
          <DocsScreenshot src="/docs/wms-task.png" width={1265} height={712} alt="Публичная инструкция TORGNEXA по остаткам и заданиям WMS" caption="Публичная инструкция по WMS: остатки, задания оператора, сканирование и ограничения текущего fulfillment-контура."/>
          <Callout title="Корректировки требуют причины" tone="warning">История остатков ведётся как журнал. Исправление создаёт новую запись, а не переписывает прошлое.</Callout>
        </DocSection>

        <DocSection id="marking" title="Маркировка и УПД" intro="Раздел «Маркировка» помогает оператору проверить код, связать его с GTIN/SKU и заданием WMS и безопасно обработать расхождение. Исходный Data Matrix в приложении не сохраняется.">
          <ol className="docs-steps">
            <li><strong>Откройте партию</strong><span>Проверьте SKU, GTIN, запрошенное и зарезервированное количество, открытые расхождения и текущее состояние партии.</span></li>
            <li><strong>Выберите операцию WMS</strong><span>Перед сканированием укажите receiving, put-away, pick, pack, cycle count или return receiving — действие должно соответствовать заданию.</span></li>
            <li><strong>Передайте код и количество</strong><span>Система принимает barcode, GTIN, SKU и точное ожидаемое количество. Повторный запрос используйте с новым ключом только после понятного результата предыдущего.</span></li>
            <li><strong>Разберите результат</strong><span>Принятый код обновляет проверяемое состояние, а отказ показывает reason code. Не закрывайте задание вручную, если количество не совпало.</span></li>
            <li><strong>Проведите УПД через контроль</strong><span>Печать, агрегация, ЭДО, УКЭП и МЧД выполняются отдельными согласованными операциями; истёкший сертификат или отсутствие МЧД блокируют отправку.</span></li>
          </ol>
          <div className="docs-feature-grid">
            <article><h3><code>gtin_mismatch</code></h3><p>GTIN кода не совпал с партией или SKU. Проверьте этикетку и исходное задание, не повторяйте скан вслепую.</p></article>
            <article><h3><code>duplicate</code></h3><p>Код уже принимался. Количество не увеличивается, поэтому повторная отправка не создаёт скрытый излишек.</p></article>
            <article><h3><code>overflow</code></h3><p>Сканов больше, чем разрешено заданием. Операция остаётся открытой для проверки, а остаток не исправляется автоматически.</p></article>
            <article><h3><code>unknown</code></h3><p>Удалённый результат неясен после timeout. Сначала выполните status read/reconciliation, затем принимайте решение о повторе.</p></article>
          </div>
          <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Что проверяется</th><th>Что сохраняется</th><th>Чего нет в приложении</th></tr></thead><tbody>
            <tr><td>GTIN, SKU, задание и точное количество</td><td>SHA-256 fingerprint, location, quantity, actor и UTC time</td><td>Исходный Data Matrix и секреты провайдера</td></tr>
            <tr><td>Состояние партии и открытые drifts</td><td>Неизменяемая история операции и reason code</td><td>Автоматическое подтверждение поставки маркетплейса</td></tr>
            <tr><td>Сертификат, МЧД и результат ЭДО</td><td>Bounded evidence и audit-событие</td><td>Автоматический retry неизвестной удалённой записи</td></tr>
          </tbody></table></div>
          <DocsScreenshot src="/docs/marking.png" width={1265} height={712} alt="Публичная инструкция TORGNEXA по разделу «Маркировка и УПД»" caption="Публичная страница инструкции: перед сканированием оператор видит условия, порядок действий и правила обработки отказа."/>
          <Callout title="Безопасность кодов" tone="warning">Исходный код не кладётся в логи, события или screenshots. Любая запись capability маркировки проходит права, policy, approval и audit; live qualification провайдера не заменяется synthetic-тестом.</Callout>
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
          <DocsScreenshot src="/docs/integrations.png" width={1265} height={712} alt="Раздел «Интеграции» TORGNEXA с карточками подключения" caption="Экран каталога: карточка провайдера ведёт к подключению кабинета и проверке доступа."/>
          <IntegrationConnectionGuide/>
          <IntegrationQualificationGuides/>
          <DocsScreenshot src="/docs/integration-connection.png" width={1265} height={712} alt="Пошаговое подключение кабинета интеграции TORGNEXA" caption="Визуальная шпаргалка к панели подключения: кабинет, учётные данные, проверка, возможности и запуск импорта."/>
          <p>Для OAuth-подключения нажмите «Войти». Токен доступа обновляется сервером автоматически до истечения срока. Повторный вход требуется только если площадка отозвала доступ, отклонила токен обновления или не выдала его; карточка кабинета покажет «Войти снова».</p>
          <p>К текущим готовым storefront-маршрутам относятся 1С‑Битрикс, CS-Cart, Magento, Medusa, OpenCart, Shopify и Shopware; для них рабочий контур включает товары, базовые цены, остатки, заказы и явно указанные направления синхронизации. Для CS-Cart дополнительно доступна стандартная смена статуса заказа. Bitrix24 — отдельный CRM-контур: лиды, сделки, контакты, компании и товарные строки не превращаются в product sync.</p>
          <p>В разделе «Качество публикации» (<code>/publication-quality</code>) оператор видит target-specific score, проблемы карточки и срок действия evidence. `ready` допускает запись, а `blocked`, `approval_required`, `stale`, `unsupported` и `unknown` останавливают её; старый receipt не может пройти preflight после изменения товара или capability.</p>
          <Callout title="Боевые учётные данные" tone="warning">Не используйте боевые ключи в тестовом контуре. Выдавайте минимальные права, а при раскрытии немедленно отзывайте ключ у провайдера и выполняйте его ротацию.</Callout>
        </DocSection>

        <DocSection id="integration-status" title="Состояние интеграций" intro="Центр состояния показывает единый снимок кабинетов, runtime, доступности, операций и синхронизации. Чтение снимка не выполняет удалённую проверку и не изменяет кабинет.">
          <ol className="docs-steps">
            <li><strong>Начните со сводки</strong><span>Счётчики «Работают», «Внимание», «Заблокированы», «Устарели» и «Синхронизация» показывают масштаб проблемы, но не заменяют открытие конкретного кабинета.</span></li>
            <li><strong>Отфильтруйте очередь</strong><span>Используйте состояние, рабочий контур, семейство, capability или причину. Фильтры меняют только чтение снимка и сохраняют cursor пагинации.</span></li>
            <li><strong>Откройте кабинет</strong><span>В карточке доступны измерения Runtime, кабинета, credentials, configuration, health, capability, sync, reconciliation, webhook и rate limit.</span></li>
            <li><strong>Отделите проблему от отсутствия данных</strong><span>Красный статус не означает одно и то же: <code>unknown</code> — наблюдение недоступно, <code>stale</code> — оно устарело, <code>reauthorization_required</code> — нужна повторная авторизация.</span></li>
            <li><strong>Выполните следующее действие</strong><span>Используйте только предложенное действие с ожидаемой версией кабинета. Если источники неполны, состояние не считается зелёным.</span></li>
          </ol>
          <div className="docs-table-wrap"><table className="docs-route-table"><thead><tr><th>Состояние</th><th>Как читать</th><th>Безопасная реакция</th></tr></thead><tbody>
            <tr><td><strong>Работает</strong> / <code>healthy</code></td><td>Снимок подтверждён доступными источниками и активными операциями.</td><td>Проверить дату evidence и продолжить обычный обмен.</td></tr>
            <tr><td><strong>Требует внимания</strong> / <code>attention</code></td><td>Есть проблема или неполное измерение, но причина уже известна.</td><td>Открыть issue, исправить причину и повторить bounded health-check.</td></tr>
            <tr><td><strong>Устарело</strong> / <code>stale</code></td><td>Последнее evidence старше допустимого TTL.</td><td>Не считать кабинет исправным; проверить worker, расписание и источник.</td></tr>
            <tr><td><strong>Нет данных</strong> / <code>unknown</code></td><td>Система не смогла доказательно определить состояние.</td><td>Не выполнять запись и не повторять внешний вызов вслепую; начать с reconciliation.</td></tr>
            <tr><td><strong>Заблокировано</strong> / <code>blocked</code></td><td>Сработала policy, capability, approval или security boundary.</td><td>Исправить условие и пройти обычный approval, а не обходить gate.</td></tr>
          </tbody></table></div>
          <DocsScreenshot src="/docs/integration-status.png" width={1265} height={712} alt="Центр состояния интеграций TORGNEXA со сводкой и фильтрами" caption="Центр состояния: сначала сводка и фильтры, затем карточка конкретного кабинета с измерениями и рекомендациями."/>
          <Callout title="Не путайте чтение и проверку" tone="info">Центр показывает сохранённый snapshot с generated_at и digest. Кнопку реальной проверки запускайте в карточке «Интеграции» только после проверки scope и параметров среды.</Callout>
        </DocSection>

        <DocSection id="social" title="Публикации" intro="Social Core хранит контент, канал, расписание и историю статусов независимо от конкретной социальной сети.">
          <p>В текущей версии рабочей среды подключены Telegram и MAX для текстовых сообщений. Лимит Telegram — 4096 символов, MAX — 4000. В «Интеграциях» создайте кабинет нужного провайдера, сохраните токен бота, заполните <code>chat_id</code> по шаблону, выполните проверку, включите <code>social.post.text</code> и активируйте кабинет. Затем создайте активный канал и публикацию в разделе «Публикации».</p>
          <p>Для Telegram в карточке «Webhook канала» можно включить или отключить доставку входящих событий. Укажите публичный HTTPS-адрес и заранее сохранённую callback-ссылку на секрет. Операция идемпотентна, а сервер принимает только публикации настроенного канала; callback-запросы, личные сообщения и другие update-типы остаются закрыты.</p>
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
          <p>В текущей рабочей среде платёжные операции доступны для СБП, YooKassa и Robokassa: создание, статус и сверка. Возврат разрешается только шлюзам с правом <code>payments.refund</code>; у Robokassa полный и частичный возврат требуют Password3 и возвращают асинхронный идентификатор заявки. Ozon Pay пока ограничен проверкой Seller API, а «Долями» — проверкой настроенного API endpoint; для «Долями» дополнительно требуется mTLS-сертификат.</p>
          <p>Для входящих уведомлений используйте <code>POST /api/v1/webhooks/payments/&#123;connector_id&#125;/&#123;organization&#95;id&#125;/&#123;workspace&#95;id&#125;/&#123;account_id&#125;</code>. Это публичный callback без пользовательской сессии: сервер проверяет активный платёжный кабинет, повторно подтверждает состояние у провайдера, записывает свидетельство и применяет переход ровно один раз. Провайдер получает унифицированный <code>200</code> при ошибке до проверки, а тело callback не считается источником статуса.</p>
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
          <div className="docs-tab-guide"><article><strong>Уведомления</strong><span>Ошибки, предупреждения и системные события с признаком прочтения.</span></article><article><strong>Отчёты</strong><span>Периоды 7, 30 и 90 дней, поиск, графики, CSV/PDF и доступная AI-аналитика.</span></article><article><strong>Аудит</strong><span>Append-only история привилегированных изменений с субъектом, временем и результатом.</span></article><article><strong>Realtime</strong><span>Аутентифицированный SSE передаёт только heartbeat и сигналы инвалидации; данные перечитываются обычными API.</span></article></div>
          <p>Экспорт отчёта сохраняет выбранные фильтры. PDF создаётся сервером и скачивается готовым файлом. Аналитические проекции могут обновляться с небольшой задержкой и не являются транзакционной истиной.</p>
          <p>Отчёт «Юнит-экономика по каналам» показывает фактическую contribution margin по <code>channel_ref</code>: чистую выручку, комиссии, логистику, рекламу, возвраты, COGS, payout и покрытие источников. База признания выбирается явно (<code>order_accrual</code>, <code>settlement</code> или <code>cash</code>); статусы <code>partial</code>, <code>unmatched</code>, <code>conflict</code> и <code>mixed_currency</code> не превращаются в нулевые значения. Для подробной формулы и диагностики откройте <a href="/docs/operations">руководство эксплуатации</a>.</p>
          <div className="docs-split"><div><h3>Как читать сумму</h3><p>GMV за вычетом скидок, отмен и возвратов даёт net revenue. После комиссий, payment fee, fulfilment, хранения, рекламы, промо, COGS и штрафов получается contribution profit.</p></div><div><h3>Как читать качество</h3><p><code>complete</code> означает полное покрытие источников, <code>partial</code> — неполное, <code>unmatched</code> — неуверенное сопоставление, <code>conflict</code> — спорную запись, а <code>mixed_currency</code> — отсутствие безопасной конвертации.</p></div></div>
          <p>В фильтре «База» выберите одну дату признания: <code>order_accrual</code>, <code>settlement</code> или <code>cash</code>. Для каждой строки проверяйте <code>channel_ref</code>, покрытие и источник; отсутствующий факт не должен выглядеть как нулевой расход. CSV/PDF повторяет тот же bounded snapshot и не пересчитывает его новым курсом.</p>
          <DocsScreenshot src="/docs/unit-economics.png" width={1265} height={712} alt="Публичная инструкция TORGNEXA по отчёту юнит-экономики" caption="Публичная инструкция по юнит-экономике: база признания, формула, качество покрытия и безопасное чтение результата."/>
          <p>Поток <code>GET /api/v1/realtime</code> доступен только при разрешении <code>operations.realtime.read</code>. Browser coalesces burst-инвалидации в окне 150 мс и не помещает в SSE payload товары, заказы, аудит или PII; после сигнала интерфейс повторно запрашивает только разрешённые данные.</p>
        </DocSection>

        <DocSection id="settings" title="Настройки" intro="Настройки разделены на семь вкладок; состав доступных действий зависит от роли администратора.">
          <FeatureGrid items={[
            ["Профиль пользователя", "Имя, фамилия, дата рождения, должность, отдел и телефон редактируются версионно; username и email остаются данными провайдера входа."],
            ["Фото профиля", "PNG, JPEG или GIF до 5 МБ проходят карантин и проверку безопасности до привязки к профилю."],
            ["Команда workspace", "Администратор приглашает участников, назначает роли, блокирует доступ и открывает профиль участника без раскрытия OIDC subject в интерфейсе."],
            ["Privacy workflow", "Запросы доступа, выгрузки и удаления ставятся в защищённую очередь и не выполняются скрытой браузерной операцией."],
          ]}/>
          <p>Во вкладке «Основные» текущий пользователь видит роль, рабочее пространство, срок сессии и профиль. Изменение личных полей использует optimistic version и ключ идемпотентности; конфликт версии означает, что сначала нужно перечитать профиль.</p>
          <p>Кнопка «Загрузить фото» принимает только PNG, JPEG или GIF размером до 5 МБ. Файл сначала получает статус карантина, затем проходит MIME/размер/содержимое-проверки; к профилю привязывается только выпущенный файл. «Удалить фото» снимает связь, но не обходит журнал и проверку версии.</p>
          <p>Блок «Пользователи и роли» доступен при <code>settings.members.read</code>. Пользователь с <code>settings.members.write</code> может пригласить участника с ролью viewer, operator, manager или admin, изменить роль/статус и открыть его профиль. Поля username и email у участника доступны только для чтения; изменение профиля и блокировка последнего активного администратора защищены серверными правами и проверкой версии.</p>
          <p>Запрос «Выгрузка» или «Удаление» персональных данных создаётся с ключом идемпотентности и попадает в durable privacy-workflow. Удаление требует явного подтверждения; успешный ответ означает постановку запроса в очередь, а не мгновенное удаление.</p>
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

        <DocSection id="automation" title="Автоматизация и расширения" intro="ИИ, MCP, webhooks и плагины получают только явно выданные возможности и никогда не обходят серверные проверки.">
          <h3>Конструктор автоматизаций</h3>
          <FeatureGrid items={[
            ["Типизированная схема", "Черновик собирается из события-триггера, условия и действия из allowlist. Произвольный код, SQL и HTTP-вызовы в конструктор не попадают."],
            ["Проверка до публикации", "Кнопка «Проверить схему» показывает ошибки до сохранения; лимиты v1 — до 64 узлов и 128 связей."],
            ["Неизменяемые версии", "Публикация создаёт новую версию с optimistic version и ключом идемпотентности. Изменённый черновик не меняет уже опубликованный запуск."],
            ["Контролируемый запуск", "Тестовый запуск доступен только опубликованной версии. Для failed и cancelled запусков оператор может запросить повтор."],
            ["Хронология и evidence", "В карточке запуска видны узлы, попытки, статусы и машинные коды ошибок; payload внешних данных не отображается."],
            ["Отмена и повтор", "Worker хранит доступность и число попыток, а отмена и повтор проходят те же права, политику и аудит, что и исходное действие."],
          ]}/>
          <Callout title="Автоматизация не расширяет права">Workflow запускается в границах рабочего пространства и текущей версии. Если действие требует approval или capability, схема не может обойти эту проверку.</Callout>
          <p>Практический путь в интерфейсе: создайте draft, добавьте событие или расписание, соедините его с condition и allowlisted action, нажмите «Проверить схему», затем «Опубликовать» и «Тестовый запуск». По идентификатору run открывается timeline шагов и evidence; для <code>failed</code> или <code>cancelled</code> доступен retry с новой run identity, а незавершённый запуск можно отменить.</p>
          <DocsScreenshot src="/docs/workflow-builder.png" width={1265} height={712} alt="Публичная инструкция TORGNEXA по конструктору автоматизаций" caption="Публичная инструкция по workflow: схема, проверка, публикация, тестовый запуск и безопасное восстановление."/>
          <ul><li><strong>Провайдеры ИИ</strong> включают Claude, DeepSeek, GigaChat, Google Gemini, Grok (xAI), Kimi, OpenAI-совместимый, Qwen, YandexGPT и локальные Ollama, LM Studio, Open WebUI. Gemini использует официальный <code>generateContent</code> с ключом <code>x-goog-api-key</code>, Grok — xAI Chat Completions с Bearer; локальные серверы уже должны быть запущены.</li><li><strong>Ограничения ИИ</strong> одинаковы для hosted и local providers: только bounded non-streaming text completion, без скачивания моделей и без вызовов инструментов из этого контура.</li><li><strong>Политика передачи данных ИИ</strong> ограничивает классы данных, провайдеров, моделей, размер запроса и месячный лимит. Предварительная проверка редактирует чувствительные фрагменты и не отправляет тестовый запрос наружу.</li><li><strong>MCP-аккаунт</strong> получает одноразовый токен Bearer и ограниченный набор инструментов. В базовой сборке без настроенной политики управления <code>tools/list</code> пуст, а <code>tools/call</code> отклоняется.</li><li><strong>Аварийная остановка</strong> блокирует всех MCP-агентов рабочего пространства до явного возобновления.</li><li><strong>Вебхук</strong> доставляет выбранные события на HTTPS-адрес с подписью, повторными попытками и очередью ошибок, историей попыток и ручным повтором по идентификатору доставки.</li><li><strong>Плагин</strong> показывает запрошенные права, классы секретов и сетевые адреса; просмотр каталога ничего не устанавливает.</li></ul>
          <Callout title="ИИ не является привилегированным обходом">Даже действительный токен не отменяет границу рабочего пространства, разрешения, лимит запросов, класс риска, политику и согласование.</Callout>
        </DocSection>

        <DocSection id="assistant" title="AI-помощник оператора" intro="Помощник отвечает по разрешённым данным рабочего пространства, показывает evidence и ссылки на первоисточники и не выполняет доменные записи напрямую.">
          <ol className="docs-steps">
            <li><strong>Создайте сессию</strong><span>Откройте «AI-помощник» и нажмите «Новая сессия». Сессия ограничена вашим actor, organization и workspace-контекстом.</span></li>
            <li><strong>Задайте операционный вопрос</strong><span>Например: «Что требует внимания в интеграциях?», «Почему товар не публикуется?», «Какие каналы просели?» или «Что будет с остатком?».</span></li>
            <li><strong>Проверьте grounding</strong><span>Смотрите <code>grounding_state</code>, freshness, digest и deep links. Без evidence ответ остаётся partial, stale, unavailable или refused.</span></li>
            <li><strong>Используйте план как preview</strong><span>«Сформируй план исправления» возвращает typed preview. Кнопка не выполняет запись; чувствительное действие уходит в обычный approval/domain worker.</span></li>
          </ol>
          <div className="docs-feature-grid">
            <article><h3>Только чтение</h3><p>Помощник не получает секреты, raw provider payload, chain-of-thought и прямой доступ к connector packages.</p></article>
            <article><h3>Доказуемый ответ</h3><p><code>grounded</code> разрешён только при актуальном evidence; устаревший или недоступный источник явно обозначается.</p></article>
            <article><h3>Untrusted data</h3><p>Тексты товара, отзывы, webhooks и ответы провайдеров считаются недоверенными данными и не превращаются в инструкции для модели.</p></article>
            <article><h3>Безопасный preview</h3><p>Любая чувствительная запись требует capability, policy, approval, version и idempotency вне AI-контуры.</p></article>
          </div>
          <h3>Если ответ кажется неполным</h3>
          <p><code>source_unavailable</code> означает, что источник интеграций или отчёта недоступен; <code>insufficient_data</code> — что фактов недостаточно; <code>stale_data</code> — что evidence старше допустимого TTL. В этих случаях откройте deep link исходного раздела и проверьте состояние там, не воспринимая ответ как подтверждение операции.</p>
          <pre><code>POST /api/v1/assistant/sessions{`\n`}POST /api/v1/assistant/sessions/&#123;session_id&#125;/messages{`\n`}GET  /api/v1/assistant/runs/&#123;run_id&#125;</code></pre>
          <DocsScreenshot src="/docs/ai-assistant.png" width={1265} height={712} alt="Публичная инструкция TORGNEXA по AI-помощнику оператора" caption="Публичная инструкция по AI-помощнику: вопросы, evidence, freshness и границы typed preview."/>
          <Callout title="AI не является привилегированным обходом" tone="warning">Даже действительный токен не отменяет границу рабочего пространства, разрешения, лимит запросов, класс риска, policy и согласование.</Callout>
        </DocSection>

        <DocSection id="developer" title="API и расширения" intro="Публичная поверхность для интеграторов строится вокруг версионированных контрактов, а не внутренних таблиц.">
          <FeatureGrid items={[
            ["REST API", "Основной HTTP-контур находится под /api/v1; организация и рабочее пространство выводятся из проверенной авторизации, а не из произвольных данных запроса."],
            ["SDK", "OpenAPI генерирует поддерживаемые клиенты Go, TypeScript и Python с политикой совместимости."],
            ["Вебхуки", "Исходящие события подписываются, доставляются с повторными попытками и сохраняют неизменяемую историю попыток."],
            ["n8n и MCP", "n8n остаётся внешней интеграцией; MCP/OpenClaw получают только scoped-инструменты и governed доступ."],
            ["Профиль и privacy", "Профиль, аватар и запросы персональных данных доступны через tenant-scoped API с версией и идемпотентностью."],
          ]}/>
          <pre><code>GET  /api/v1/health{`\n`}GET  /api/v1/products?limit=25&amp;q=SKU-42{`\n`}POST /api/v1/webhook-subscriptions{`\n`}POST /mcp</code></pre>
          <p>Для мутаций используйте idempotency key и обрабатывайте 401/403/409/429 явно. В событиях и webhook envelope не передавайте access token, приватные ключи, полные платёжные данные или лишние PII.</p>
        </DocSection>

        <DocSection id="security" title="Доступ и безопасность" intro="TORGNEXA использует модель default deny: разрешено только то, что выдано явно.">
          <ul className="docs-checklist"><li>Контекст организации и рабочего пространства берётся из проверенной сессии.</li><li>Пароли остаются у провайдера идентификации; TORGNEXA их не видит и не хранит.</li><li>Ключи, токены, приватные ключи и платёжные данные нельзя помещать в комментарии, логи и экспорт.</li><li>Не отключайте последнего активного администратора рабочего пространства.</li><li>Завершайте неизвестные активные сессии в разделе «Безопасность».</li><li>Опасные действия проходят проверку политики и согласования и фиксируются в аудите.</li></ul>
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
          <p>Создание и заполнение конфигурации подробно разобрано в разделе <a href={documentationPathFor("environment")}>«Переменные окружения .env»</a>.</p>
          <pre><code>docker compose --env-file .env ps{`\n`}curl http://127.0.0.1:8080/api/v1/health{`\n`}docker compose --env-file .env logs --tail=100 api</code></pre>
          <p>PostgreSQL — операционная истина, Kafka — событийная платформа, ClickHouse — аналитика, Valkey — кеши и координация. Нормальное состояние: необходимые сервисы и frontend имеют статус healthy.</p>
          <h3>Рабочая среда на отдельном VPS</h3>
          <p>Для рабочей среды используйте защищённый ручной процесс SSH по точному тегу: GitHub Environment с проверяющим, закреплённый <code>known_hosts</code>, отдельный рабочий слой, TLS-шлюз, внешнее хранилище секретов и проверку доступности после переключения релиза. Community Compose остаётся локальным эталоном для одного узла и не превращается в HA- или CDN-топологию.</p>
          <ol><li>Подготовьте рабочий <code>.env</code> с правами <code>0600</code> и без идентификаторов разработки OIDC.</li><li>Проверьте резервное копирование, восстановление и откат до первого развёртывания.</li><li>Запустите только вручную одобренный точный тег и дождитесь проверки API.</li><li>При неуспешной проверке доступности вернитесь на предыдущий выпуск и сохраните свидетельство.</li></ol>
          <Callout title="Перед обновлением" tone="warning">Сделайте резервную копию, проверьте миграции и отрепетируйте восстановление. Один только факт создания backup не подтверждает его пригодность.</Callout>
          <h3>Авторизованная проверка Community через браузер</h3>
          <p>После запуска стека выполните <code>make community-e2e</code>. Проверка использует чистый браузерный профиль и синтетического пользователя <code>demo</code>, проходит настоящий authorization-code flow через Keycloak, проверяет каталог, изображения товара, заказы, редактирование и восстановление профиля участника, а также постановку privacy export-запроса в очередь.</p>
          <pre><code>make community-up{`\n`}make community-e2e{`\n`}make community-down</code></pre>
          <Callout title="E2E не удаляет демо-профиль" tone="info">Повторяемая проверка намеренно не запускает destructive deletion: после неё синтетическая учётная запись нужна для остальных assertions. При ошибке диагностический screenshot сохраняется вне репозитория.</Callout>
        </DocSection>

        <DocSection id="troubleshooting" title="Решение проблем" intro="Начните с симптома, затем проверяйте ближайшую границу: сессию, права, API или внешний коннектор.">
          <dl className="docs-faq">
            {troubleshootingFaq.map(({question, answer}) => <div key={question}><dt>{question}</dt><dd>{answer}</dd></div>)}
          </dl>
          <DocsScreenshot src="/docs/documentation.png" width={1265} height={712} alt="Публичная документация TORGNEXA" caption="Руководство доступно до входа и адаптируется под настольный и мобильный экран."/>
        </DocSection>
        </DocumentationSectionContext.Provider>
      </main>
    </div>
  </div>;
}

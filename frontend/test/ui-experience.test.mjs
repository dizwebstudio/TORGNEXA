import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";

const read = (name) => readFileSync(new URL(`../src/${name}`, import.meta.url), "utf8");
const readRoot = (name) => readFileSync(new URL(`../../${name}`, import.meta.url), "utf8");

test("shell exposes icon navigation, command search, theme and activity center", () => {
  const shell = read("shell/AppShell.tsx");
  assert.match(shell, /aria-current=/);
  assert.match(shell, /CommandPalette/);
  assert.match(shell, /ActivityCenter/);
  assert.match(shell, /toggleTheme/);
  assert.match(shell, /metaKey\|\|event\.ctrlKey/);
  assert.match(shell, /Управление торговлей/);
  assert.doesNotMatch(shell, /Commerce Orchestration/);
  assert.match(shell, /isKnownPath/);
  assert.match(shell, /Страница не найдена/);
  assert.match(shell, /Не удалось загрузить часть активности/);
  assert.match(shell, /Повторить/);
  assert.match(shell, /Все разделы/);
  assert.match(shell, /secondary-navigation/);
  assert.match(shell, /primaryNavigation/);
  assert.match(read("shell/navigation.ts"), /navigationSections/);
  assert.match(read("styles.css"), /\.nav-more-panel/);
});

test("dashboard is operational, permission-aware and loads metrics independently", () => {
  const dashboard = read("pages/DashboardPage.tsx");
  assert.match(dashboard, /Расхождения/);
  assert.match(dashboard, /Проблемы на складе/);
  assert.match(dashboard, /Ждут решения/);
  assert.match(dashboard, /Первые шаги/);
  assert.match(dashboard, /enabled: canReadStock/);
  assert.match(dashboard, /kpi-value-skeleton/);
  assert.equal((dashboard.match(/listConnectorAccounts/g) ?? []).length, 1);
  assert.equal((dashboard.match(/getSyncStatus/g) ?? []).length, 1);
  assert.match(dashboard, /href="\/approvals"/);
  assert.doesNotMatch(dashboard, /Оркестрация Commerce/);
  assert.doesNotMatch(dashboard, /рабочий контур/);
  assert.match(dashboard, /DemoDatasetButton/);
  assert.match(dashboard, /Быстро заполнить систему/);
});

test("data table provides search sort pagination columns selection and bookmarkable views without browser persistence", () => {
  const table = read("components/DataTable.tsx");
  for (const token of ["searchParams", "sort-button", "Выбрано", "Колонки", "Сохранить вид", "pageSize"]) assert.ok(table.includes(token), token);
  assert.doesNotMatch(table.toLowerCase(), /localstorage|sessionstorage|document\.cookie/);
});

test("inventory exposes warehouse incidents and fulfillment allocation lineage", () => {
  const inventory = read("pages/InventoryPage.tsx");
  assert.match(inventory, /Остатки и исполнение заказов/);
  assert.match(inventory, /Исполнение заказов/);
  assert.match(inventory, /listWarehouseIncidents/);
  assert.match(inventory, /listFulfillmentAllocations/);
  assert.match(inventory, /Автоматическое переключение/);
  assert.match(inventory, /ReplacesID/);
  assert.match(inventory, /Задания WMS/);
  assert.match(inventory, /listWarehouseTasks/);
  assert.match(inventory, /createWarehouseTaskBatch/);
  assert.match(inventory, /Сканирование сохраняет только digest/);
});

test("integration settings use overview cards and a focused drawer", () => {
  const integrations = read("features/settings/IntegrationCatalog.tsx");
  const css = read("styles.css");
  assert.match(integrations, /integration-summary-card/);
  assert.match(integrations, /integration-market-card/);
  assert.match(integrations, /integration-card-visual/);
  assert.match(integrations, /entry\.presentation\.logo/);
  assert.match(integrations, /integration-capability-tags/);
  assert.match(integrations, /connectionState/);
  assert.match(integrations, /<Drawer/);
  assert.match(integrations, /Разрешённые возможности/);
  assert.match(css, /--connector-surface/);
  assert.match(css, /connector-logo-branded/);
  assert.match(css, /integration-card-hit-target:focus-visible/);
  assert.match(css, /\.integration-toolbar \{[^}]*display:grid/);
  assert.match(css, /\.integration-toolbar \{[^}]*grid-template-columns:minmax\(0,420px\)/);
  assert.match(css, /\.integration-toolbar \.family-tabs \{[^}]*flex-wrap:wrap/);
  assert.match(css, /\.integration-catalog-heading > div:first-child \{[^}]*min-width:0/);
});

test("integration catalog groups every connector by an explicit runtime surface", () => {
  const integrations = read("features/settings/IntegrationCatalog.tsx");
  const generated = read("generated/connector-catalog.ts");
  assert.equal([...generated.matchAll(/^      stage: "ready"/gm)].length, 18);
  assert.equal([...generated.matchAll(/^      stage: "separate_surface"/gm)].length, 43);
  assert.equal([...generated.matchAll(/^      stage: "planned"/gm)].length, 0);
  assert.match(integrations, /Подключение пока недоступно/);
  assert.match(integrations, /Создать кабинет или включить заявленные возможности нельзя/);
  assert.match(integrations, /Перейти к провайдерам ИИ/);
  assert.match(integrations, /navigate\("\/settings#ai-provider-settings"\)/);
  assert.match(read("pages/SettingsPage.tsx"), /ai-provider-settings/);
  assert.match(read("pages/SettingsPage.tsx"), /hashchange/);
  assert.match(read("shell/useLocationPath.ts"), /window\.location\.search\}\$\{window\.location\.hash/);
  assert.match(generated, /id: "pochta-russia"/);
  assert.match(integrations, /Почты России/);
  assert.match(read("features/settings/AIProviderSettings.tsx"), /Claude \(Anthropic\)/);
  assert.match(read("features/settings/AIProviderSettings.tsx"), /Ollama/);
  assert.match(read("features/settings/AIProviderSettings.tsx"), /LM Studio/);
  assert.match(read("features/settings/AIProviderSettings.tsx"), /Open WebUI/);
  assert.match(read("features/settings/AIProviderSettings.tsx"), /Google Gemini/);
  assert.match(read("features/settings/AIProviderSettings.tsx"), /Grok \(xAI\)/);
  assert.match(read("features/settings/AIProviderSettings.tsx"), /<h2>Провайдеры ИИ<\/h2>/);
  assert.match(read("features/reports/AskAI.tsx"), /Провайдер ИИ/);
  assert.match(read("features/reports/AskAI.tsx"), /Google Gemini/);
  assert.match(read("features/reports/AskAI.tsx"), /Grok \(xAI\)/);
  assert.match(read("features/reports/AskAI.tsx"), /Claude \(Anthropic\)/);
  assert.match(read("features/reports/AskAI.tsx"), /Ollama/);
  assert.match(read("features/reports/AskAI.tsx"), /LM Studio/);
  assert.match(read("features/reports/AskAI.tsx"), /Open WebUI/);
  assert.match(integrations, /Перейти к курсам валют/);
  assert.match(integrations, /Текстовые публикации работают/);
  assert.match(integrations, /CRM-контур работает/);
  assert.match(integrations, /Подключение доступно в категории/);
  assert.match(integrations, /integration-family-heading/);
  assert.match(integrations, /runtime\.operationalCapabilities/);
  assert.match(integrations, /runtimeConfigTemplate/);
  assert.match(generated, /name: "1С-Битрикс"/);
  assert.match(integrations, /официальный REST-модуль/);
  assert.match(integrations, /JSON вида \{ user_id, webhook_code \}/);
  assert.match(generated, /name: "CS-Cart"/);
  assert.match(integrations, /официальный REST API 2\.0/);
  assert.match(integrations, /JSON вида \{ email, api_key \}/);
  assert.match(generated, /id: "lamoda"/);
  assert.match(generated, /id: "mvideo"/);
  assert.match(generated, /id: "dolyami"/);
  assert.match(generated, /name: "Долями"/);
  assert.match(integrations, /certificate_pem, private_key_pem/);
  assert.match(generated, /surface: "marketplace"/);
  assert.match(generated, /healthOnly: true/);
  const docs = read("pages/PublicDocumentationPage.tsx");
  for (const token of ["Lamoda", "Долями", "Google Gemini", "Grok (xAI)", "operations.realtime.read", "Профиль пользователя", "Фото профиля", "make community-e2e", "gtin_mismatch", "health history", "grounding_state"]) assert.ok(docs.includes(token), token);
  for (const token of ["docsTitle", "docsDescription", "application/ld+json", "canonical", "docs-reading-paths", "docs-details", "Автоматизация и расширения", "Маркировка и УПД", "Состояние интеграций", "AI-помощник оператора"]) assert.ok(docs.includes(token), token);
});

test("AI provider settings keep form controls aligned and separated", () => {
  const settings = read("features/settings/AIProviderSettings.tsx");
  const css = read("styles.css");
  assert.match(settings, /settings-grid ai-provider-form/);
  assert.match(settings, /account-actions ai-provider-form-actions/);
  assert.match(css, /\.settings-grid > \* \{ min-width: 0; \}/);
  assert.match(css, /\.field input,\.field select,\.field textarea/);
  assert.match(css, /\.ai-provider-form-actions \{ margin-top: 14px; \}/);
});

test("profile card presents stored profile details and an offline demo avatar", () => {
  const settings = read("pages/SettingsPage.tsx");
  const model = read("auth/session-model.ts");
  const adapter = read("auth/keycloak-adapter.ts");
  const avatar = read("components/UserAvatar.tsx");
  const css = read("styles.css");
  for (const token of ["Профиль пользователя", "Должность", "Подразделение", "Дата рождения", "Электронная почта", "Телефон", "хранятся в TORGNEXA", "getCurrentUserProfile", "uploadCurrentUserAvatar", "createCurrentUserProfilePrivacyRequest", "profileDisplayName", "Изменить профиль", "Удалить фото", "Запросить выгрузку", "Запросить удаление"]) assert.match(settings, new RegExp(token));
  const members = read("features/settings/MemberSettings.tsx");
  for (const token of ["getWorkspaceMemberProfile", "updateWorkspaceMemberProfile", "Профиль", "Сохранить профиль"]) assert.match(members, new RegExp(token));
  for (const token of ["jobTitle", "department", "birthdate", "phoneNumber", "picture"]) { assert.match(model, new RegExp(token)); assert.match(adapter, new RegExp(token)); }
  assert.match(avatar, /demo-avatar\.svg/);
  assert.match(css, /\.profile-hero/);
  assert.match(css, /\.profile-facts/);
});

test("catalog product creation starts from a real product card", () => {
  const catalog = read("features/catalog/ProductList.tsx");
  const demoButton = read("features/DemoDatasetButton.tsx");
  const demoCache = read("features/demoDataset.ts");
  const css = read("styles.css");
  for (const token of ["product-create-card", "Карточка товара", "product-create-card-tabs", "Создать карточку", "product-create-description"]) assert.ok(catalog.includes(token), token);
  for (const token of [".product-create-card-header", ".product-create-card-tabs", ".product-create-form", ".product-create-card-footer"]) assert.ok(css.includes(token), token);
  assert.match(catalog, /api\.createProduct\(\{body:\{code,title,description\}\}\)/);
  assert.match(catalog, /useState\("main"\)/);
  assert.match(catalog, /\[\["main","Основные данные"\],\["offers","Предложения и цены"\]/);
  assert.match(catalog, /product-create-image-section/);
  assert.match(catalog, /type="file" accept="image\/\*"/);
  assert.match(catalog, /uploadAndAttachImage/);
  assert.match(catalog, /catalog-primary-image/);
  assert.match(catalog, /role="tablist"/);
  assert.match(catalog, /type="button" role="tab"/);
  assert.match(catalog, /api\.createProductImage\(\{productId:productID/);
  assert.doesNotMatch(catalog, /Заполнить демо-каталог/);
  assert.doesNotMatch(catalog, /createDemoOrders/);
  assert.match(demoButton, /Добавить демо-данные/);
  assert.match(demoButton, /demo-dataset:all/);
  assert.match(demoCache, /cache\.invalidateQueries\(\)/);
  assert.match(catalog, /catalog-product-thumbnail/);
  assert.match(catalog, /image_url/);
  for (const token of [".product-create-image-form", ".product-create-image-preview", ".product-create-image-empty"]) assert.ok(css.includes(token), token);
  for (const token of [".catalog-product-cell", ".catalog-product-thumbnail", ".catalog-product-copy"]) assert.ok(css.includes(token), token);
});

test("read errors always expose retry and catalog offer mutations report failures", () => {
  const apiState = read("components/ApiState.tsx");
  const catalog = read("features/catalog/ProductList.tsx");
  assert.match(apiState, /window\.location\.reload/);
  for (const token of ["Не удалось добавить предложение", "Не удалось сохранить предложение", "Не удалось добавить цену", "Не удалось изменить цену", "Не удалось назначить категорию", "Не удалось создать категорию"]) assert.match(catalog, new RegExp(token));
});

test("visual system includes dark mode mobile labels focus and reduced motion", () => {
  const css = read("styles.css");
  for (const token of ['data-theme="dark"', ".nav-label", ":focus-visible", "prefers-reduced-motion", ".toast-region", ".drawer-layer", ".skeleton"]) assert.ok(css.includes(token), token);
});

test("technical status values are localized in badges", () => {
  const badge = read("components/StatusBadge.tsx");
  assert.match(badge, /critical: "Критический"/);
  assert.match(badge, /needs_attention: "Требует внимания"/);
  assert.match(badge, /statusLabels\[normalized\]/);
  for (const token of ["disputed: \"Оспорен\"", "expired: \"Срок истёк\"", "revoked: \"Отозван\""]) assert.match(badge, new RegExp(token));
});

test("connector capabilities are presented in Russian", () => {
  const settings = read("features/settings/IntegrationCatalog.tsx");
  const labels = read("components/labels.ts");
  for (const token of ["Просмотр товаров", "Управление кабинетами интеграций", "Просмотр политик ИИ", "Управление участниками рабочего пространства", "Просмотр состояния безопасности"]) assert.match(settings, new RegExp(token));
  assert.doesNotMatch(settings, /Получать данные ·/);
  for (const token of ["ai.completion.generate", "Генерация ответов ИИ", "payments.webhooks", "Получение уведомлений о платежах", "social.post.buttons", "Интерактивные кнопки публикаций"]) assert.match(labels, new RegExp(token));
});

test("audit, security and documentation technical values are localized for operators", () => {
  const audit = read("pages/AuditPage.tsx");
  const security = read("features/settings/SecuritySettings.tsx");
  const trust = read("features/settings/TrustControlSettings.tsx");
  const providers = read("features/settings/AIProviderSettings.tsx");
  const docs = read("pages/PublicDocumentationPage.tsx");
  assert.match(audit, /auditActionLabel\(item\.action\)/);
  assert.match(audit, /auditResourceLabel\(item\.resource_type\)/);
  assert.match(audit, /auditSourceLabel\(item\.source\)/);
  for (const token of ["auditActionLabel", "auditResourceLabel", "Субъект и связь операции"]) assert.match(security, new RegExp(token));
  for (const token of ["Свидетельства безопасности", "Маржинальная прибыль", "Безопасная предварительная проверка", "Сервис не запускается при ошибке проверки"]) assert.match(trust, new RegExp(token));
  for (const token of ["Идентификатор папки", "Адрес API"]) assert.match(providers, new RegExp(token));
  for (const token of ["права", "учётные данные", "рабочее пространство", "возможности манифеста"]) assert.match(docs, new RegExp(token));
  assert.doesNotMatch(trust, />[^<]*(Contribution profit|Marketplace fee|Security Evidence|Connector Replay Lab|Synthetic fixture)[^<]*</);
});

test("storefront connector logos use official marks and brand palettes", () => {
  const bitrix = readRoot("frontend/public/connector-logos/bitrix.svg");
  const csCart = readRoot("frontend/public/connector-logos/cs-cart.svg");
  const catalog = read("generated/connector-catalog.ts");
  assert.match(bitrix, /width="344" height="67"/);
  assert.match(bitrix, /fill="#D91935"/);
  assert.match(bitrix, /fill="#231F20"/);
  assert.doesNotMatch(bitrix, /#F58220|#E04A16/);
  assert.match(csCart, /width="139" height="34"/);
  assert.match(csCart, /#7381FD/);
  assert.match(csCart, /#76C7FF/);
  assert.match(csCart, /#1B2032/);
  assert.doesNotMatch(csCart, /#202A44|CS-CART/);
  assert.match(catalog, /surface: "#FFF1F3"/);
  assert.match(catalog, /surface: "#F0F1FF"/);
  assert.match(catalog, /accent: "#D91935"/);
  assert.match(catalog, /accent: "#7381FD"/);
});

test("orders expose guarded lifecycle actions and modal focus management", () => {
  const orders = read("features/orders/OrderList.tsx");
  const dialog = read("components/Dialog.tsx");
  const drawer = read("components/Drawer.tsx");
  const palette = read("components/CommandPalette.tsx");
  assert.match(orders, /changeOrderStatus/);
  for (const token of ["Подтвердить заказ", "Передать в обработку", "Передать в исполнение", "Отменить заказ"]) assert.match(orders, new RegExp(token));
  for (const modal of [dialog, drawer, palette]) {
    assert.match(modal, /useFocusTrap/);
    assert.match(modal, /aria-modal="true"/);
    assert.match(modal, /tabIndex=\{-1\}/);
  }
});

test("frontend image policy permits catalog HTTPS thumbnails", () => {
  const server = readRoot("frontend/serve.mjs");
  assert.match(server, /img-src 'self' data: https:/);
});

test("public documentation is prerendered and served before the SPA fallback", () => {
  const packageJSON = JSON.parse(readRoot("frontend/package.json"));
  const server = readRoot("frontend/serve.mjs");
  const docs = read("pages/PublicDocumentationPage.tsx");
  const compose = readRoot("docker-compose.production.yml");
  const dockerfile = readRoot("frontend/Dockerfile.production");
  const robots = readRoot("frontend/public/robots.txt");
  assert.match(packageJSON.scripts.build, /npm run build:docs/);
  assert.equal(packageJSON.scripts["build:docs"], "vite build --ssr src/ssr/docs-entry.tsx --outDir .prerender && node scripts/prerender-docs.mjs");
  assert.equal(packageJSON.scripts["test:docs"], "node scripts/check-public-docs.mjs");
  assert.match(server, /directoryIndex/);
  assert.match(server, /\.txt.*text\/plain/);
  assert.match(server, /\.xml.*application\/xml/);
  assert.match(docs, /documentationPages/);
  assert.match(docs, /documentationSectionIdForPath/);
  assert.match(docs, /\/docs\/integrations/);
  assert.doesNotMatch(docs, /href="#/);
  assert.match(compose, /TORGNEXA_PUBLIC_URL: \$\{TORGNEXA_PUBLIC_URL:\?set TORGNEXA_PUBLIC_URL/);
  assert.match(dockerfile, /TORGNEXA_PUBLIC_URL/);
  assert.match(robots, /Allow: \/docs/);
  assert.match(robots, /Disallow: \/api\//);
});

test("task 120 uses server-side grids with cursor pagination for core commerce lists", () => {
  const grid = read("components/ServerDataGrid.tsx");
  const catalog = read("features/catalog/ProductList.tsx");
  const orders = read("features/orders/OrderList.tsx");
  assert.match(grid, /на сервере/);
  assert.match(catalog, /next_cursor/);
  assert.match(catalog, /listProducts\(\{limit:25,q:/);
  assert.match(orders, /next_cursor/);
  assert.match(orders, /listOrders\(\{limit:25,q:/);
});

test("orders show product card thumbnails in list and detail", () => {
  const orders = read("features/orders/OrderList.tsx");
  const image = read("components/ProductImage.tsx");
  const css = read("styles.css");
  assert.match(orders, /order-product-thumbnail/);
  assert.match(orders, /ProductImage/);
  assert.match(orders, /product_image_url/);
  assert.match(orders, /product_title\|\|v\.product_sku/);
  assert.match(image, /getUploadContent/);
  assert.match(image, /URL\.createObjectURL/);
  assert.match(css, /\.order-product-cell/);
  assert.match(css, /\.line-item \.order-product-thumbnail/);
});

test("product images support deletion, failed-load state and offline demo assets", () => {
  const catalog = read("features/catalog/ProductList.tsx");
  const image = read("components/ProductImage.tsx");
  const css = read("styles.css");
  const seed = readRoot("internal/platform/postgres/searchrepo/repository.go");
  const demoAsset = readRoot("frontend/public/demo-images/demo-01.svg");
  assert.match(catalog, /deleteProductImage/);
  assert.match(catalog, /Удалить/);
  assert.match(catalog, /window\.confirm/);
  assert.match(image, /Изображение не загрузилось/);
  assert.match(image, /onError/);
  assert.match(image, /query\.isError/);
  assert.match(css, /\.image-placeholder-failed/);
  assert.match(seed, /\/demo-images\/demo-01\.svg/);
  assert.match(seed, /\/demo-images\/demo-26\.svg/);
  assert.match(demoAsset, /<svg/);
});

test("task 120 realtime is an authenticated SSE invalidation channel", () => {
  const hook = read("app/useRealtime.ts");
  const shell = read("shell/AppShell.tsx");
  assert.match(hook, /\/api\/v1\/realtime/);
  assert.match(hook, /Authorization:/);
  assert.match(hook, /text\/event-stream/);
  assert.match(hook, /invalidateQueries/);
  assert.match(shell, /realtime-pill/);
});

test("OIDC sessions renew in memory and recover silently without browser persistence", () => {
  const adapter = read("auth/keycloak-adapter.ts");
  const provider = read("auth/AuthProvider.tsx");
  assert.match(adapter, /grant_type: "refresh_token"/);
  assert.match(adapter, /parameters\.prompt = "none"/);
  assert.match(adapter, /oidc\/silent-callback\.html/);
  assert.match(provider, /forceRefresh: true/);
  assert.doesNotMatch(`${adapter}\n${provider}`.toLowerCase(), /localstorage|sessionstorage|document\.cookie/);
});

test("public documentation follows current navigation, settings and sign-in behavior", () => {
  const docs = read("pages/PublicDocumentationPage.tsx");
  const navigation = read("shell/navigation.ts");
  const settings = read("features/settings/settings-tabs.ts");
  const envExample = readRoot(".env.example");
  const navigationItems = navigation.slice(navigation.indexOf("export const navigationItems"));
  const navigationLabels = [...navigationItems.matchAll(/label: "([^"]+)"/g)].map((match) => match[1]);
  const settingsLabels = [...settings.matchAll(/label: "([^"]+)"/g)].map((match) => match[1]);
  assert.equal(navigationLabels.length, 23);
  assert.equal(settingsLabels.length, 7);
  for (const label of [...navigationLabels, ...settingsLabels]) assert.ok(docs.includes(label), label);
  for (const route of ["/catalog", "/publication-quality", "/orders", "/returns", "/inventory", "/incidents", "/integrations", "/social", "/sync", "/counterparties", "/finance", "/approvals", "/workflows", "/compliance", "/notifications", "/reports", "/security", "/audit", "/settings"]) {
    assert.ok(docs.includes(route), route);
  }
  assert.match(docs, /oidc\/silent-callback\.html/);
  assert.match(docs, /Карточки площадок/);
  assert.match(docs, /make community-init/);
  const environmentVariables = [...envExample.matchAll(/^([A-Z][A-Z0-9_]*)=/gm)].map((match) => match[1]);
  assert.equal(environmentVariables.length, 56);
  for (const variable of environmentVariables) assert.ok(docs.includes(variable), variable);
  assert.match(docs, /Как работает ClamAV/);
  assert.match(docs, /Fail-open режима нет/);
  assert.doesNotMatch(docs, /Войти через OIDC/);
  assert.doesNotMatch(docs, /Настройки» → «Интеграции/);
});

test("workflow operator view exposes step evidence timeline and safe recovery", () => {
  const workflows = read("pages/WorkflowsPage.tsx");
  for (const token of ["listWorkflowRunSteps", "listWorkflowRunEvidence", "Хронология запуска", "Свидетельства", "Повторить", "Отменить"]) assert.match(workflows, new RegExp(token));
});

test("task 120 provides a unified incident center and deep links", () => {
  const center = read("pages/IncidentCenterPage.tsx");
  const nav = read("shell/navigation.ts");
  assert.match(center, /eyebrow="Операции"/);
  assert.match(center, /title="Центр инцидентов"/);
  assert.match(center, /kindName/);
  assert.match(center, /Доступные данные всё равно показаны/);
  assert.match(center, /listWarehouseIncidents/);
  assert.match(center, /getSyncStatus/);
  assert.match(center, /listConnectorAccounts/);
  assert.match(center, /listApprovals/);
  assert.match(center, /История/);
  assert.match(center, /historyRows/);
  assert.match(center, /terminal/);
  assert.match(center, /\/incidents\//);
  assert.match(nav, /normalized\.startsWith\(item\.path \+ "\/"\)/);
});

test("demo catalog includes an actionable warehouse incident and stock position", () => {
  const repository = readRoot("internal/platform/postgres/searchrepo/repository.go");
  assert.match(repository, /DEMO-INCIDENT-WH/);
  assert.match(repository, /Демо-склад для инцидентов/);
  assert.match(repository, /warehouse_incidents/);
  assert.match(repository, /demo_outage/);
  assert.match(repository, /seedDemoInventoryPosition\(ctx, tx, org, ws, offerID, warehouseID, 18, 0/);
  assert.match(repository, /SELECT EXISTS\(SELECT 1 FROM inventory_positions/);
  assert.match(repository, /seedDemoCatalogMerchandising/);
  assert.match(repository, /pim_categories/);
  assert.match(repository, /VALUES\(\$1,\$2,\$3,\$4,\$5,'draft',1/);
  assert.match(repository, /activate demo category/);
  assert.match(repository, /INSERT INTO prices/);
  assert.match(repository, /seedDemoFulfillmentAllocations/);
  assert.match(repository, /fulfillment_allocations/);
});

test("community browser checks have a synthetic Keycloak user and upload scanner", () => {
  const realm = readRoot("deploy/keycloak/torgnexa-realm.json");
  const compose = readRoot("docker-compose.yml");
  const helper = readRoot("scripts/ensure-community-demo-user.sh");
  assert.match(realm, /"username": "demo"/);
  assert.match(realm, /"realmRoles": \["admin"\]/);
  assert.match(compose, /clamav\/clamav:1\.4\.3@sha256:/);
  assert.match(compose, /TORGNEXA_WORKER_UPLOADS_ENABLED: \$\{TORGNEXA_WORKER_UPLOADS_ENABLED:-true\}/);
  assert.match(helper, /set-password/);
  assert.match(helper, /add-roles/);
  assert.match(helper, /community-demo-member\.sql/);
  assert.match(readRoot("scripts/community-demo-member.sql"), /demo_subject/);
});

test("authorized community e2e covers catalog orders and product images", () => {
  const runner = readRoot("scripts/community-e2e.mjs");
  const wrapper = readRoot("scripts/community-e2e.sh");
  const makefile = readRoot("Makefile");
  for (const token of ["Keycloak realm", "TORGNEXA_DEMO_PASSWORD", "google-chrome", "Runtime.evaluate", "profile-card", "профиль пользователя", "catalog-product-thumbnail", "catalog-primary-image", "catalog-image-editor", "order-product-thumbnail", "Подтвердить заказ"]) assert.match(runner, new RegExp(token.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), token);
  assert.match(runner, /user-data-dir=/);
  assert.match(runner, /Page\.captureScreenshot/);
  assert.match(wrapper, /ensure-community-demo-user\.sh/);
  assert.match(makefile, /community-e2e: community-up/);
});

test("task 120 command palette searches server-side and opens entities directly", () => {
  const command = read("components/CommandPalette.tsx");
  assert.match(command, /listProducts\(\{q:serverQuery/);
  assert.match(command, /listOrders\(\{q:serverQuery/);
  assert.match(command, /\/catalog\/\$\{encodeURIComponent/);
  assert.match(command, /\/orders\/\$\{encodeURIComponent/);
});

test("task 120 reports use professional SVG analytics with presets and KPIs", () => {
  const reports = read("pages/ReportsPage.tsx");
  const chart = read("components/AnalyticsChart.tsx");
  assert.match(reports, /7 дней/);
  assert.match(reports, /30 дней/);
  assert.match(reports, /analytics-kpis/);
  assert.match(chart, /<svg/);
  assert.match(chart, /chart-grid-line/);
  assert.match(chart, /chart-legend/);
});

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
});

test("dashboard is operational and includes onboarding and orchestration state", () => {
  const dashboard = read("pages/DashboardPage.tsx");
  assert.match(dashboard, /Открытые расхождения/);
  assert.match(dashboard, /Складские инциденты/);
  assert.match(dashboard, /Требуют человека/);
  assert.match(dashboard, /Первый запуск/);
  assert.match(dashboard, /Commerce Orchestration/);
});

test("data table provides search sort pagination columns selection and bookmarkable views without browser persistence", () => {
  const table = read("components/DataTable.tsx");
  for (const token of ["searchParams", "sort-button", "Выбрано", "Колонки", "Сохранить вид", "pageSize"]) assert.ok(table.includes(token), token);
  assert.doesNotMatch(table.toLowerCase(), /localstorage|sessionstorage|document\.cookie/);
});

test("inventory exposes warehouse incidents and fulfillment allocation lineage", () => {
  const inventory = read("pages/InventoryPage.tsx");
  assert.match(inventory, /listWarehouseIncidents/);
  assert.match(inventory, /listFulfillmentAllocations/);
  assert.match(inventory, /Автоматический failover/);
  assert.match(inventory, /ReplacesID/);
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
});

test("integration catalog distinguishes executable, separate and planned runtime stages", () => {
  const integrations = read("features/settings/IntegrationCatalog.tsx");
  const generated = read("generated/connector-catalog.ts");
  assert.equal([...generated.matchAll(/^      stage: "ready"/gm)].length, 11);
  assert.equal([...generated.matchAll(/^      stage: "separate_surface"/gm)].length, 8);
  assert.equal([...generated.matchAll(/^      stage: "planned"/gm)].length, 19);
  assert.match(integrations, /Подключение пока недоступно/);
  assert.match(integrations, /Создать кабинет или включить заявленные возможности нельзя/);
  assert.match(integrations, /Перейти к AI-провайдерам/);
  assert.match(integrations, /Перейти к курсам валют/);
  assert.match(integrations, /runtime\.operationalCapabilities/);
  assert.match(integrations, /runtimeConfigTemplate/);
});

test("visual system includes dark mode mobile labels focus and reduced motion", () => {
  const css = read("styles.css");
  for (const token of ['data-theme="dark"', ".nav-label", ":focus-visible", "prefers-reduced-motion", ".toast-region", ".drawer-layer", ".skeleton"]) assert.ok(css.includes(token), token);
});

test("task 120 uses server-side grids with cursor pagination for core commerce lists", () => {
  const grid = read("components/ServerDataGrid.tsx");
  const catalog = read("features/catalog/ProductList.tsx");
  const orders = read("features/orders/OrderList.tsx");
  assert.match(grid, /server-side/);
  assert.match(catalog, /next_cursor/);
  assert.match(catalog, /listProducts\(\{limit:25,q:/);
  assert.match(orders, /next_cursor/);
  assert.match(orders, /listOrders\(\{limit:25,q:/);
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
  const navigationLabels = [...navigation.matchAll(/label: "([^"]+)"/g)].map((match) => match[1]);
  const settingsLabels = [...settings.matchAll(/label: "([^"]+)"/g)].map((match) => match[1]);
  assert.equal(navigationLabels.length, 16);
  assert.equal(settingsLabels.length, 7);
  for (const label of [...navigationLabels, ...settingsLabels]) assert.ok(docs.includes(label), label);
  for (const route of ["/catalog", "/orders", "/inventory", "/incidents", "/integrations", "/social", "/sync", "/counterparties", "/finance", "/approvals", "/compliance", "/notifications", "/reports", "/audit", "/settings"]) {
    assert.ok(docs.includes(route), route);
  }
  assert.match(docs, /oidc\/silent-callback\.html/);
  assert.match(docs, /Карточки площадок/);
  assert.match(docs, /make community-init/);
  const environmentVariables = [...envExample.matchAll(/^([A-Z][A-Z0-9_]*)=/gm)].map((match) => match[1]);
  assert.equal(environmentVariables.length, 54);
  for (const variable of environmentVariables) assert.ok(docs.includes(variable), variable);
  assert.match(docs, /Как работает ClamAV/);
  assert.match(docs, /Fail-open режима нет/);
  assert.doesNotMatch(docs, /Войти через OIDC/);
  assert.doesNotMatch(docs, /Настройки» → «Интеграции/);
});

test("task 120 provides a unified incident center and deep links", () => {
  const center = read("pages/IncidentCenterPage.tsx");
  const nav = read("shell/navigation.ts");
  assert.match(center, /listWarehouseIncidents/);
  assert.match(center, /getSyncStatus/);
  assert.match(center, /listConnectorAccounts/);
  assert.match(center, /listApprovals/);
  assert.match(center, /\/incidents\//);
  assert.match(nav, /normalized\.startsWith\(item\.path \+ "\/"\)/);
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

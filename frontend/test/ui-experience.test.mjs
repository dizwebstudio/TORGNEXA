import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";

const read = (name) => readFileSync(new URL(`../src/${name}`, import.meta.url), "utf8");

test("shell exposes icon navigation, command search, theme and activity center", () => {
  const shell = read("shell/AppShell.tsx");
  assert.match(shell, /aria-current=/);
  assert.match(shell, /CommandPalette/);
  assert.match(shell, /ActivityCenter/);
  assert.match(shell, /toggleTheme/);
  assert.match(shell, /metaKey\|\|event\.ctrlKey/);
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
  assert.match(integrations, /integration-summary-card/);
  assert.match(integrations, /<Drawer/);
  assert.match(integrations, /Разрешённые возможности/);
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

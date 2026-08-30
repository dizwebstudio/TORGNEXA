import {useQuery} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {allowedNavigation} from "../shell/navigation";
import {useAuth} from "../auth/AuthProvider";
import {navigate} from "../shell/useLocationPath";
import {Page} from "./Page";
import {Icon} from "../components/Icon";
import {StatusBadge} from "../components/StatusBadge";
import {DemoDatasetButton} from "../features/DemoDatasetButton";

type AnyRecord = Record<string, any>;

function arr(body: unknown): AnyRecord[] {
  if (!body || typeof body !== "object") return [];
  const items = (body as {items?: unknown}).items;
  return Array.isArray(items)
    ? items.filter((item): item is AnyRecord => Boolean(item) && typeof item === "object")
    : [];
}

function numberValue(value: unknown): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function formatMoney(minorUnits: number, currency: string): string {
  try {
    return new Intl.NumberFormat("ru-RU", {style: "currency", currency, maximumFractionDigits: 0}).format(minorUnits / 100);
  } catch {
    return `${Math.round(minorUnits / 100).toLocaleString("ru-RU")} ${currency}`;
  }
}

function useDashboardQueries() {
  const api = useApi();
  const {session} = useAuth();
  const capabilities = session?.capabilities ?? [];
  const canReadStock = capabilities.includes("stock.read");
  const canReadConnectors = capabilities.includes("connectors.read");
  const canReadSync = capabilities.includes("sync.read");
  const canReadApprovals = capabilities.includes("approvals.read");
  const canReadReports = capabilities.includes("reports.read");
  const queryOptions = {staleTime: 20_000, gcTime: 5 * 60_000};

  const inventory = useQuery({
    ...queryOptions,
    queryKey: ["dashboard", "inventory"],
    enabled: canReadStock,
    queryFn: async () => arr((await api.listInventoryPositions()).body),
  });
  const connectors = useQuery({
    ...queryOptions,
    queryKey: ["dashboard", "connectors"],
    enabled: canReadConnectors,
    queryFn: async () => arr((await api.listConnectorAccounts({limit: 100})).body),
  });
  const sync = useQuery({
    ...queryOptions,
    queryKey: ["dashboard", "sync"],
    enabled: canReadSync,
    queryFn: async () => (await api.getSyncStatus()).body as AnyRecord,
  });
  const approvals = useQuery({
    ...queryOptions,
    queryKey: ["dashboard", "approvals"],
    enabled: canReadApprovals,
    queryFn: async () => arr((await api.listApprovals()).body),
  });
  const incidents = useQuery({
    ...queryOptions,
    queryKey: ["dashboard", "incidents"],
    enabled: canReadStock,
    queryFn: async () => arr((await api.listWarehouseIncidents()).body),
  });
  const warehouses = useQuery({
    ...queryOptions,
    queryKey: ["dashboard", "warehouses"],
    enabled: canReadStock,
    queryFn: async () => arr((await api.listInventoryWarehouses()).body),
  });
  const sales = useQuery({
    ...queryOptions,
    queryKey: ["dashboard", "sales", "7d"],
    enabled: canReadReports,
    queryFn: async () => {
      const to = new Date();
      const from = new Date(to);
      from.setUTCDate(to.getUTCDate() - 6);
      return (await api.getReportData({
        reportId: "sales_daily",
        from: from.toISOString(),
        to: new Date(to.getTime() + 86_400_000).toISOString(),
        limit: 50,
      })).body as AnyRecord;
    },
  });

  return {capabilities, canReadStock, canReadConnectors, canReadSync, canReadApprovals, canReadReports, inventory, connectors, sync, approvals, incidents, warehouses, sales};
}

type DashboardQueries = ReturnType<typeof useDashboardQueries>;

type MetricCardProps = {
  label: string;
  value: string | number;
  detail: string;
  icon: "orders" | "connectors" | "sync" | "incident" | "approvals";
  query: {isPending: boolean; isError: boolean};
  enabled: boolean;
  href?: string;
  tone?: "accent" | "warning" | "danger";
};

function MetricCard({label, value, detail, icon, query, enabled, href, tone}: MetricCardProps) {
  const state = !enabled ? "unavailable" : query.isPending ? "loading" : query.isError ? "error" : "ready";
  const className = ["kpi-card", tone ? `kpi-${tone}` : "", state === "error" ? "kpi-error" : ""].filter(Boolean).join(" ");
  const content = <>
    <span className="kpi-icon"><Icon name={icon}/></span>
    <span className="kpi-copy">
      <small>{label}</small>
      {state === "loading" ? <span className="skeleton kpi-value-skeleton" aria-label="Загрузка"/> : <strong>{state === "error" ? "—" : state === "unavailable" ? "Нет доступа" : value}</strong>}
      <span className="kpi-detail">{state === "loading" ? "Загрузка…" : state === "error" ? "Не удалось обновить" : state === "unavailable" ? "Недоступно для вашей роли" : detail}</span>
    </span>
  </>;
  if (!href || state === "unavailable") return <article className={className}>{content}</article>;
  return <button type="button" className={className} onClick={() => navigate(href)}>{content}</button>;
}

function DashboardMetrics({queries}: {queries: DashboardQueries}) {
  const inventory = queries.inventory.data ?? [];
  const connectors = queries.connectors.data ?? [];
  const sync = queries.sync.data ?? {};
  const approvals = queries.approvals.data ?? [];
  const incidents = queries.incidents.data ?? [];
  const salesRows = Array.isArray(queries.sales.data?.rows) ? queries.sales.data.rows : [];
  const orders = salesRows.reduce((total: number, row: any[]) => total + numberValue(row[2]), 0);
  const gross = salesRows.reduce((total: number, row: any[]) => total + numberValue(row[5]), 0);
  const currency = typeof salesRows[0]?.[1] === "string" ? salesRows[0][1] : "RUB";
  const pending = approvals.filter(item => item.state === "pending").length;
  const openIncidents = incidents.filter(item => !["resolved", "closed", "completed"].includes(String(item.status ?? item.Status ?? "").toLowerCase())).length;
  const degraded = connectors.filter(item => !["healthy", "unknown"].includes(String(item.health_status ?? "unknown")) || String(item.status) === "disabled").length;
  const low = inventory.filter(item => numberValue(item.Available ?? item.available) <= numberValue(item.Reserved ?? item.reserved) && numberValue(item.OnHand ?? item.on_hand) > 0).length;
  const openDrifts = numberValue(sync.summary?.open_drifts);
  const salesDetail = gross ? formatMoney(gross, currency) : "Нет продаж за 7 дней";

  return <>
    <div className="operations-grid">
      <MetricCard label="Заказы за 7 дней" value={orders} detail={salesDetail} icon="orders" query={queries.sales} enabled={queries.canReadReports} href="/reports" tone="accent"/>
      <MetricCard label="Проблемы с интеграциями" value={degraded} detail={`${connectors.length} подключено`} icon="connectors" query={queries.connectors} enabled={queries.canReadConnectors} href="/integrations" tone={degraded ? "danger" : undefined}/>
      <MetricCard label="Расхождения" value={openDrifts} detail="Открыть синхронизацию" icon="sync" query={queries.sync} enabled={queries.canReadSync} href="/sync" tone={openDrifts ? "warning" : undefined}/>
      <MetricCard label="Проблемы на складе" value={openIncidents} detail={`${low} позиций с низким остатком`} icon="incident" query={queries.incidents} enabled={queries.canReadStock} href="/incidents" tone={openIncidents ? "danger" : undefined}/>
      <MetricCard label="Ждут решения" value={pending} detail="Открыть согласования" icon="approvals" query={queries.approvals} enabled={queries.canReadApprovals} href="/approvals" tone={pending ? "warning" : undefined}/>
    </div>
    <OperationalFlow connectors={connectors} query={queries.connectors} enabled={queries.canReadConnectors} incidents={openIncidents} drift={openDrifts}/>
  </>;
}

function OperationalFlow({connectors, query, enabled, incidents, drift}: {connectors: AnyRecord[]; query: {isPending: boolean; isError: boolean}; enabled: boolean; incidents: number; drift: number}) {
  const active = connectors.filter(item => item.status === "active").length;
  const healthy = connectors.filter(item => item.health_status === "healthy").length;
  const state = !enabled ? "unknown" : query.isPending ? "unknown" : query.isError || incidents || drift ? "degraded" : "healthy";
  const channelSummary = query.isPending ? "Проверяем…" : query.isError ? "Не удалось проверить" : enabled ? `${healthy}/${active || connectors.length} работают` : "Нет доступа";
  return <section className="panel orchestration-panel">
    <div className="section-heading">
      <div>
        <p className="eyebrow">Состояние системы</p>
        <h2>Что работает сейчас</h2>
        <p>Продажи, синхронизация и склад — на одном экране.</p>
      </div>
      <StatusBadge value={state}/>
    </div>
    <div className="flow-map">
      <div className="flow-node"><Icon name="connectors"/><strong>Продажи</strong><small>{channelSummary}</small></div>
      <span className="flow-arrow"><Icon name="arrowRight"/></span>
      <div className="flow-node flow-core"><span className="brand-mark small">TN</span><strong>TORGNEXA</strong><small>синхронизация и контроль</small></div>
      <span className="flow-arrow"><Icon name="arrowRight"/></span>
      <div className={`flow-node ${incidents ? "flow-danger" : ""}`}><Icon name="warehouse"/><strong>Склад</strong><small>{incidents ? `${incidents} требуют внимания` : "критичных проблем нет"}</small></div>
    </div>
  </section>;
}

function Onboarding({queries}: {queries: DashboardQueries}) {
  const {capabilities} = queries;
  const steps = [
    capabilities.includes("connectors.read") ? {label: "Подключить продажи", done: (queries.connectors.data?.length ?? 0) > 0, query: queries.connectors, path: "/integrations"} : null,
    capabilities.includes("stock.read") ? {label: "Добавить склад", done: (queries.warehouses.data?.length ?? 0) > 0, query: queries.warehouses, path: "/inventory"} : null,
    capabilities.includes("sync.read") ? {label: "Настроить синхронизацию", done: numberValue(queries.sync.data?.summary?.enabled_policies) > 0, query: queries.sync, path: "/sync"} : null,
    capabilities.includes("sync.read") ? {label: "Запустить первую синхронизацию", done: (queries.sync.data?.runs?.length ?? 0) > 0, query: queries.sync, path: "/sync"} : null,
  ].filter((step): step is NonNullable<typeof step> => Boolean(step));
  if (!steps.length) return null;
  const done = steps.filter(step => step.done).length;
  if (done === steps.length && steps.every(step => !step.query.isPending)) return null;
  const checking = steps.some(step => step.query.isPending);
  return <section className="panel onboarding">
    <div className="onboarding-copy">
      <p className="eyebrow">Первые шаги</p>
      <h2>Подключите основные разделы</h2>
      <p>{checking ? "Проверяем текущие настройки…" : `${done} из ${steps.length} шагов завершено.`}</p>
      <div className="progress"><span style={{width: `${done / steps.length * 100}%`}}/></div>
    </div>
    <div className="onboarding-steps">{steps.map((step, index) => <button type="button" key={step.label} className={step.done ? "done" : ""} onClick={() => navigate(step.path)}><span>{step.done ? <Icon name="check"/> : index + 1}</span><strong>{step.label}</strong><Icon name="chevron"/></button>)}</div>
  </section>;
}

export function DashboardPage() {
  const {session} = useAuth();
  const queries = useDashboardQueries();
  const items = allowedNavigation(session?.capabilities ?? []).filter(item => item.path !== "/");
  return <Page eyebrow="Сегодня" title="Что нужно сделать сегодня" description="Заказы, проблемы и остатки — на одном экране.">
    <section className="panel demo-dataset-panel"><div><p className="eyebrow">Демо-данные</p><h2>Быстро заполнить систему</h2><p>Добавьте товары, заказы, остатки и примеры операций для знакомства с TORGNEXA.</p></div><DemoDatasetButton/></section>
    <Onboarding queries={queries}/>
    <DashboardMetrics queries={queries}/>
    <section><div className="section-heading"><div><h2>Разделы</h2><p>Быстрый переход к ежедневным задачам.</p></div></div><div className="module-grid modern-modules">{items.map(item => <button type="button" className="module-card" key={item.id} onClick={() => navigate(item.path)}><span className="module-glyph"><Icon name={item.icon}/></span><span><strong>{item.label}</strong><small>{item.risk === "READ" ? "Просмотр" : "Изменения"}</small></span><Icon name="arrowRight" className="arrow"/></button>)}</div></section>
  </Page>;
}

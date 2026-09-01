import type {IconName} from "../components/icon-names.js";

export type RiskClass = "READ" | "WRITE_SAFE" | "WRITE_SENSITIVE" | "LEGALLY_SIGNIFICANT";

export interface NavigationItem {
  readonly id: string;
  readonly label: string;
  readonly path: string;
  readonly capability?: string;
  readonly risk: RiskClass;
  readonly icon: IconName;
  readonly shortcut?: string;
}

export const primaryNavigationIDs: ReadonlySet<string> = new Set([
  "dashboard",
  "catalog",
  "orders",
  "inventory",
  "connectors",
  "sync",
  "notifications",
  "security",
  "settings",
]);

export const navigationSections = [
  {id: "operations", label: "Операционная работа", itemIds: ["publication-quality", "marketplace-publication", "returns", "incidents", "marking", "replenishment", "pricing"]},
  {id: "integrations", label: "Интеграции и каналы", itemIds: ["integration-status", "marketplace-operations", "social"]},
  {id: "control", label: "Контроль и данные", itemIds: ["counterparties", "procurement", "finance", "financial-analytics", "advertising", "approvals", "compliance", "reports", "audit"]},
  {id: "automation", label: "Автоматизация", itemIds: ["workflows", "operator-assistant"]},
] as const;

export const navigationItems: readonly NavigationItem[] = [
  {id: "dashboard", label: "Обзор", path: "/", risk: "READ", icon: "dashboard", shortcut: "G D"},
  {id: "catalog", label: "Каталог", path: "/catalog", capability: "products.read", risk: "READ", icon: "catalog", shortcut: "G C"},
  {id: "publication-quality", label: "Качество публикации", path: "/publication-quality", capability: "products.read", risk: "READ", icon: "compliance", shortcut: "G Q"},
  {id: "marketplace-publication", label: "Публикация товаров", path: "/marketplace-publications", capability: "products.read", risk: "WRITE_SENSITIVE", icon: "sync"},
  {id: "orders", label: "Заказы", path: "/orders", capability: "orders.read", risk: "READ", icon: "orders", shortcut: "G O"},
  {id: "returns", label: "Возвраты", path: "/returns", capability: "orders.returns.read", risk: "WRITE_SENSITIVE", icon: "orders"},
  {id: "inventory", label: "Остатки", path: "/inventory", capability: "stock.read", risk: "READ", icon: "inventory", shortcut: "G I"},
  {id: "replenishment", label: "Прогноз и пополнение", path: "/replenishment", capability: "stock.read", risk: "WRITE_SENSITIVE", icon: "inventory"},
  {id: "pricing", label: "Цены и repricing", path: "/pricing", capability: "products.read", risk: "WRITE_SENSITIVE", icon: "finance"},
  {id: "incidents", label: "Инциденты", path: "/incidents", capability: "stock.read", risk: "READ", icon: "incident", shortcut: "G E"},
  {id: "marking", label: "Маркировка", path: "/marking", capability: "stock.read", risk: "WRITE_SENSITIVE", icon: "compliance"},
  {id: "integration-status", label: "Состояние интеграций", path: "/integrations/status", capability: "connectors.read", risk: "READ", icon: "connectors"},
  {id: "marketplace-operations", label: "Операции marketplace", path: "/marketplace-operations", capability: "integrations.center.read", risk: "READ", icon: "connectors"},
  {id: "connectors", label: "Интеграции", path: "/integrations", capability: "connectors.read", risk: "READ", icon: "connectors", shortcut: "G X"},
  {id: "social", label: "Публикации", path: "/social", capability: "connectors.read", risk: "WRITE_SAFE", icon: "notifications", shortcut: "G M"},
  {id: "sync", label: "Синхронизация", path: "/sync", capability: "sync.read", risk: "READ", icon: "sync", shortcut: "G S"},
  {id: "counterparties", label: "Контрагенты", path: "/counterparties", capability: "counterparties.read", risk: "READ", icon: "counterparties", shortcut: "G P"},
  {id: "procurement", label: "Закупки", path: "/procurement", capability: "procurement.suppliers.read", risk: "WRITE_SENSITIVE", icon: "inventory"},
  {id: "finance", label: "Финансы", path: "/finance", capability: "settlements.read", risk: "READ", icon: "finance", shortcut: "G F"},
  {id: "financial-analytics", label: "Финансовая аналитика", path: "/finance/analytics", capability: "finance.reports.read", risk: "READ", icon: "finance"},
  {id: "advertising", label: "Реклама", path: "/advertising", capability: "ads.read", risk: "READ", icon: "finance"},
  {id: "approvals", label: "Согласования", path: "/approvals", capability: "approvals.read", risk: "WRITE_SENSITIVE", icon: "approvals", shortcut: "G A"},
  {id: "workflows", label: "Автоматизации", path: "/workflows", capability: "workflows.read", risk: "WRITE_SAFE", icon: "sync", shortcut: "G W"},
  {id: "compliance", label: "Сертификаты и документы", path: "/compliance", capability: "compliance.read", risk: "WRITE_SENSITIVE", icon: "compliance", shortcut: "G L"},
  {id: "notifications", label: "Уведомления", path: "/notifications", capability: "notifications.read", risk: "READ", icon: "notifications", shortcut: "G N"},
  {id: "reports", label: "Отчёты", path: "/reports", capability: "reports.read", risk: "READ", icon: "reports", shortcut: "G R"},
  {id: "operator-assistant", label: "AI-помощник", path: "/assistant", capability: "assistant.read", risk: "READ", icon: "reports"},
  {id: "security", label: "Безопасность", path: "/security", capability: "settings.security.read", risk: "READ", icon: "security", shortcut: "G Y"},
  {id: "audit", label: "Аудит", path: "/audit", capability: "audit.read", risk: "READ", icon: "audit", shortcut: "G U"},
  {id: "settings", label: "Настройки", path: "/settings", capability: "settings.read", risk: "WRITE_SAFE", icon: "settings", shortcut: "G T"}
] as const;

export function hasCapability(capabilities: readonly string[], required?: string): boolean {
  if (!required) return true;
  return capabilities.includes(required);
}

export function allowedNavigation(capabilities: readonly string[]): NavigationItem[] {
  return navigationItems.filter((item) => hasCapability(capabilities, item.capability));
}

export function routeForPath(pathname: string): NavigationItem | undefined {
  const normalized = pathname !== "/" ? pathname.replace(/\/+$/, "") : pathname;
  const exact = navigationItems.find((item) => item.path === normalized);
  if (exact) return exact;
  return navigationItems.filter((item) => item.path !== "/" && normalized.startsWith(item.path + "/")).reduce<NavigationItem | undefined>((best, item) => !best || item.path.length > best.path.length ? item : best, undefined);
}

export function canOpenPath(pathname: string, capabilities: readonly string[]): boolean {
  const route = routeForPath(pathname);
  return Boolean(route && hasCapability(capabilities, route.capability));
}

export function isKnownPath(pathname: string): boolean {
  const normalized = pathname !== "/" ? pathname.replace(/\/+$/, "") : pathname;
  if (normalized === "/" || navigationItems.some((item) => item.path === normalized)) return true;
  if (/^\/catalog\/[^/]+$/.test(normalized) || /^\/(orders|returns)\/[^/]+$/.test(normalized) || /^\/integrations\/status\/[^/]+$/.test(normalized) || /^\/marketplace-operations\/[^/]+$/.test(normalized)) return true;
  if (normalized === "/oauth/connectors/callback") return true;
  if (normalized === "/finance/analytics") return true;
  return /^\/incidents\/(warehouse|drift|connector|approval)\/[^/]+$/.test(normalized);
}

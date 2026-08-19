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

export const navigationItems: readonly NavigationItem[] = [
  {id: "dashboard", label: "Обзор", path: "/", risk: "READ", icon: "dashboard", shortcut: "G D"},
  {id: "catalog", label: "Каталог", path: "/catalog", capability: "products.read", risk: "READ", icon: "catalog", shortcut: "G C"},
  {id: "orders", label: "Заказы", path: "/orders", capability: "orders.read", risk: "READ", icon: "orders", shortcut: "G D"},
  {id: "inventory", label: "Остатки", path: "/inventory", capability: "stock.read", risk: "READ", icon: "inventory", shortcut: "G I"},
  {id: "incidents", label: "Инциденты", path: "/incidents", capability: "stock.read", risk: "READ", icon: "incident", shortcut: "G E"},
  {id: "connectors", label: "Интеграции", path: "/integrations", capability: "connectors.read", risk: "READ", icon: "connectors", shortcut: "G X"},
  {id: "sync", label: "Синхронизация", path: "/sync", capability: "sync.read", risk: "READ", icon: "sync", shortcut: "G S"},
  {id: "approvals", label: "Согласования", path: "/approvals", capability: "approvals.read", risk: "WRITE_SENSITIVE", icon: "approvals", shortcut: "G A"},
  {id: "compliance", label: "Сертификаты и документы", path: "/compliance", capability: "compliance.read", risk: "WRITE_SENSITIVE", icon: "compliance", shortcut: "G L"},
  {id: "notifications", label: "Уведомления", path: "/notifications", capability: "notifications.read", risk: "READ", icon: "notifications", shortcut: "G N"},
  {id: "reports", label: "Отчёты", path: "/reports", capability: "reports.read", risk: "READ", icon: "reports", shortcut: "G R"},
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
  return navigationItems.find((item) => item.path === normalized || (item.path !== "/" && normalized.startsWith(item.path + "/")));
}

export function canOpenPath(pathname: string, capabilities: readonly string[]): boolean {
  const route = routeForPath(pathname);
  return Boolean(route && hasCapability(capabilities, route.capability));
}

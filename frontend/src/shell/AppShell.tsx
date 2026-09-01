import {lazy,Suspense,useEffect,useMemo,useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useAuth} from "../auth/AuthProvider";
import {useApi} from "../api/ApiProvider";
import {allowedNavigation, canOpenPath, isKnownPath, navigationSections, primaryNavigationIDs, routeForPath, type NavigationItem} from "./navigation";
import {navigate, useLocationPath} from "./useLocationPath";
import {useRealtimeInvalidation} from "../app/useRealtime";
import {Icon} from "../components/Icon";
import {CommandPalette} from "../components/CommandPalette";
import {Drawer} from "../components/Drawer";
import {StatusBadge} from "../components/StatusBadge";
import {useUi} from "../app/UiProvider";
import {UserAvatar} from "../components/UserAvatar";

// Pages are loaded on demand so the initial shell remains small on low-bandwidth
// VPS deployments. Each route keeps its own data/UI dependencies out of the
// critical bundle until the operator actually opens it.
const DashboardPage = lazy(() => import("../pages/DashboardPage").then(module => ({default: module.DashboardPage})));
const CatalogPage = lazy(() => import("../pages/CatalogPage").then(module => ({default: module.CatalogPage})));
const MassCatalogPage = lazy(() => import("../pages/MassCatalogPage").then(module => ({default: module.MassCatalogPage})));
const PublicationQualityPage = lazy(() => import("../pages/PublicationQualityPage").then(module => ({default: module.PublicationQualityPage})));
const MarketplacePublicationPage = lazy(() => import("../pages/MarketplacePublicationPage").then(module => ({default: module.MarketplacePublicationPage})));
const MarketplaceListingPage = lazy(() => import("../pages/MarketplaceListingPage").then(module => ({default: module.MarketplaceListingPage})));
const OrdersPage = lazy(() => import("../pages/OrdersPage").then(module => ({default: module.OrdersPage})));
const ReturnsPage = lazy(() => import("../pages/ReturnsPage").then(module => ({default: module.ReturnsPage})));
const NotificationsPage = lazy(() => import("../pages/NotificationsPage").then(module => ({default: module.NotificationsPage})));
const InventoryPage = lazy(() => import("../pages/InventoryPage").then(module => ({default: module.InventoryPage})));
const MobileWarehousePage = lazy(() => import("../pages/MobileWarehousePage").then(module => ({default: module.MobileWarehousePage})));
const ReplenishmentPage = lazy(() => import("../pages/ReplenishmentPage").then(module => ({default: module.ReplenishmentPage})));
const PricingPage = lazy(() => import("../pages/PricingPage").then(module => ({default: module.PricingPage})));
const CounterpartiesPage = lazy(() => import("../pages/CounterpartiesPage").then(module => ({default: module.CounterpartiesPage})));
const ProcurementPage = lazy(() => import("../pages/ProcurementPage").then(module => ({default: module.ProcurementPage})));
const FinancePage = lazy(() => import("../pages/FinancePage").then(module => ({default: module.FinancePage})));
const FinancialAnalyticsPage = lazy(() => import("../pages/FinancialAnalyticsPage").then(module => ({default: module.FinancialAnalyticsPage})));
const CustomerServicePage = lazy(() => import("../pages/CustomerServicePage").then(module => ({default: module.CustomerServicePage})));
const AdvertisingPage = lazy(() => import("../pages/AdvertisingPage").then(module => ({default: module.AdvertisingPage})));
const CompliancePage = lazy(() => import("../pages/CompliancePage").then(module => ({default: module.CompliancePage})));
const SettingsPage = lazy(() => import("../pages/SettingsPage").then(module => ({default: module.SettingsPage})));
const ReportsPage = lazy(() => import("../pages/ReportsPage").then(module => ({default: module.ReportsPage})));
const OperatorAssistantPage = lazy(() => import("../pages/OperatorAssistantPage").then(module => ({default: module.OperatorAssistantPage})));
const AuditPage = lazy(() => import("../pages/AuditPage").then(module => ({default: module.AuditPage})));
const SecurityPage = lazy(() => import("../pages/SecurityPage").then(module => ({default: module.SecurityPage})));
const SyncPage = lazy(() => import("../pages/SyncPage").then(module => ({default: module.SyncPage})));
const ApprovalsPage = lazy(() => import("../pages/ApprovalsPage").then(module => ({default: module.ApprovalsPage})));
const WorkflowsPage = lazy(() => import("../pages/WorkflowsPage").then(module => ({default: module.WorkflowsPage})));
const IntegrationsPage = lazy(() => import("../pages/IntegrationsPage").then(module => ({default: module.IntegrationsPage})));
const IntegrationStatusPage = lazy(() => import("../pages/IntegrationStatusPage").then(module => ({default: module.IntegrationStatusPage})));
const MarketplaceOperationsPage = lazy(() => import("../pages/MarketplaceOperationsPage").then(module => ({default: module.MarketplaceOperationsPage})));
const SocialPage = lazy(() => import("../pages/SocialPage").then(module => ({default: module.SocialPage})));
const PlaceholderPage = lazy(() => import("../pages/PlaceholderPage").then(module => ({default: module.PlaceholderPage})));
const IncidentCenterPage = lazy(() => import("../pages/IncidentCenterPage").then(module => ({default: module.IncidentCenterPage})));
const MarkingPage = lazy(() => import("../pages/MarkingPage").then(module => ({default: module.MarkingPage})));
const ConnectorOAuthCallbackPage = lazy(() => import("../pages/ConnectorOAuthCallbackPage").then(module => ({default: module.ConnectorOAuthCallbackPage})));
const EcosystemPage = lazy(() => import("../pages/EcosystemPage").then(module => ({default: module.EcosystemPage})));

const realtimeLabels: Readonly<Record<string, string>> = {live: "Подключено", connecting: "Подключение…", offline: "Недоступно"};

function content(path: string) {
  if (path === "/") return <DashboardPage />;
  if (path === "/catalog/bulk") return <MassCatalogPage />;
  if (path === "/catalog" || path.startsWith("/catalog/")) return <CatalogPage />;
  if (path === "/publication-quality") return <PublicationQualityPage />;
  if (path === "/marketplace-publications") return <MarketplacePublicationPage />;
  if (path === "/marketplace-listings") return <MarketplaceListingPage />;
  if (path === "/orders" || path.startsWith("/orders/")) return <OrdersPage />;
  if (path === "/returns" || path.startsWith("/returns/")) return <ReturnsPage />;
  if (path === "/inventory") return <InventoryPage />;
  if (path === "/warehouse/mobile") return <MobileWarehousePage />;
  if (path === "/replenishment") return <ReplenishmentPage />;
  if (path === "/pricing") return <PricingPage />;
  if (path === "/incidents" || path.startsWith("/incidents/")) return <IncidentCenterPage />;
  if (path === "/marking") return <MarkingPage />;
  if (path === "/compliance") return <CompliancePage />;
  if (path === "/notifications") return <NotificationsPage />;
  if (path === "/sync") return <SyncPage />;
  if (path === "/counterparties") return <CounterpartiesPage />;
  if (path === "/procurement") return <ProcurementPage />;
  if (path === "/finance") return <FinancePage />;
  if (path === "/finance/analytics") return <FinancialAnalyticsPage />;
  if (path === "/customer-service" || path.startsWith("/customer-service/")) return <CustomerServicePage />;
  if (path === "/advertising") return <AdvertisingPage />;
  if (path === "/approvals") return <ApprovalsPage />;
  if (path === "/workflows") return <WorkflowsPage />;
  if (path === "/integrations") return <IntegrationsPage />;
  if (path === "/integrations/status" || path.startsWith("/integrations/status/")) return <IntegrationStatusPage />;
  if (path === "/marketplace-operations" || path.startsWith("/marketplace-operations/")) return <MarketplaceOperationsPage />;
  if (path === "/ecosystem") return <EcosystemPage />;
  if (path === "/social") return <SocialPage />;
  if (path === "/reports") return <ReportsPage />;
  if (path === "/assistant") return <OperatorAssistantPage />;
  if (path === "/security") return <SecurityPage />;
  if (path === "/audit") return <AuditPage />;
  if (path === "/settings") return <SettingsPage />;
  if (path === "/oauth/connectors/callback") return <ConnectorOAuthCallbackPage />;
  const item = routeForPath(path);
  return item ? <PlaceholderPage item={item} /> : null;
}

function ActivityCenter({open,onClose}:{open:boolean;onClose:()=>void}){const api=useApi();const notifications=useQuery({queryKey:["shell","activity","notifications"],queryFn:async()=>((await api.listNotifications({limit:8})).body as {items?:any[]}).items??[],enabled:open});const approvals=useQuery({queryKey:["shell","activity","approvals"],queryFn:async()=>((await api.listApprovals()).body as {items?:any[]}).items??[],enabled:open});const unread=(notifications.data??[]).filter(v=>!v.read_at);const pending=(approvals.data??[]).filter(v=>v.state==="pending");const failed=notifications.isError||approvals.isError;const retry=()=>{void Promise.all([notifications.refetch(),approvals.refetch()])};return <Drawer open={open} title="Центр активности" subtitle="События, требующие внимания" onClose={onClose}><div className="activity-summary"><div><strong>{unread.length}</strong><span>непрочитанных</span></div><div><strong>{pending.length}</strong><span>согласований</span></div></div>{failed?<div className="activity-error"><div className="panel alert error" role="alert"><Icon name="error"/><span>Не удалось загрузить часть активности.</span></div><button className="button ghost" onClick={retry}>Повторить</button></div>:<div className="activity-section"><div className="drawer-section-heading"><h3>Сейчас важно</h3><button className="button ghost" onClick={()=>{navigate("/notifications");onClose()}}>Все события</button></div>{[...pending.map(v=>({id:v.id,title:"Требуется согласование",body:v.action,severity:"warning"})),...unread.map(v=>({id:v.id,title:v.title,body:v.body,severity:v.severity}))].slice(0,8).map(item=><button className="activity-item" key={item.id} onClick={()=>{navigate(item.title==="Требуется согласование"?"/approvals":"/notifications");onClose()}}><StatusBadge value={item.severity}/><span><strong>{item.title}</strong><small>{item.body}</small></span><Icon name="chevron"/></button>)}</div>}</Drawer>}

function NavigationLink({item,path,secondary=false}:{item:NavigationItem;path:string;secondary?:boolean}) {
  return <button aria-current={path===item.path?"page":undefined} title={item.label} className={`nav-item ${secondary?"nav-item-secondary ":""}${path===item.path?"active":""}`} onClick={()=>navigate(item.path)}><span className="nav-icon"><Icon name={item.icon}/></span><span className="nav-label">{item.label}</span></button>;
}

export function AppShell() {
  const auth = useAuth();
  const path = useLocationPath();
  const {theme,toggleTheme,compact,toggleCompact}=useUi();
  const realtime=useRealtimeInvalidation();
  const [commandOpen,setCommandOpen]=useState(false),[activityOpen,setActivityOpen]=useState(false),[mobileOpen,setMobileOpen]=useState(false);
  const capabilities = auth.session?.capabilities ?? [];
  const navigation = useMemo(() => allowedNavigation(capabilities), [capabilities]);
  const primaryNavigation = useMemo(() => navigation.filter((item) => primaryNavigationIDs.has(item.id)), [navigation]);
  const secondaryNavigation = useMemo(() => navigationSections.map((section) => ({...section, items: navigation.filter((item) => section.itemIds.some((id) => id === item.id))})).filter((section) => section.items.length > 0), [navigation]);
  const secondaryRouteActive = secondaryNavigation.some((section) => section.items.some((item) => path === item.path || path.startsWith(`${item.path}/`)));
  const [sectionsOpen,setSectionsOpen]=useState(false);
  const knownPath = isKnownPath(path);
  const allowed = knownPath && (path === "/oauth/connectors/callback" ? capabilities.includes("connectors.accounts.write") : canOpenPath(path, capabilities));
  const current = routeForPath(path);
  useEffect(()=>{let prefix=false;let timer=0;const handler=(event:KeyboardEvent)=>{const target=event.target as HTMLElement|null;if(target?.closest("input,textarea,select,[contenteditable=true]"))return;const key=event.key.toLowerCase();if((event.metaKey||event.ctrlKey)&&key==="k"){event.preventDefault();setCommandOpen(true);return}if(event.key==="Escape"){setCommandOpen(false);setMobileOpen(false);prefix=false;return}if(key==="g"){prefix=true;window.clearTimeout(timer);timer=window.setTimeout(()=>{prefix=false},900);return}if(prefix){prefix=false;const item=navigation.find(value=>value.shortcut?.toLowerCase()===`g ${key}`);if(item){event.preventDefault();navigate(item.path)}}};window.addEventListener("keydown",handler);return()=>{window.removeEventListener("keydown",handler);window.clearTimeout(timer)}},[navigation]);
  useEffect(()=>{setMobileOpen(false);setSectionsOpen(false)},[path]);

  return <div className={`app-shell ${mobileOpen?"nav-open":""}`}>
    <aside className="sidebar">
      <button className="brand" onClick={() => navigate("/")} aria-label="TORGNEXA — обзор"><span className="brand-logo-frame"><img className="brand-logo-full" src="/brand/torgnexa-logo.png" alt="TORGNEXA"/><img className="brand-logo-symbol" src="/brand/torgnexa-symbol.png" alt="" aria-hidden="true"/></span><span className="brand-copy"><small>Управление торговлей</small></span></button>
      <nav aria-label="Основная навигация">{primaryNavigation.map((item) => <NavigationLink item={item} path={path} key={item.id}/>)}{secondaryNavigation.length ? <div className={`nav-more ${sectionsOpen?"open":""}`}><button type="button" className={`nav-item nav-more-toggle ${secondaryRouteActive?"active":""}`} aria-expanded={sectionsOpen} aria-controls="secondary-navigation" onClick={()=>setSectionsOpen((value)=>!value)}><span className="nav-icon"><Icon name="menu"/></span><span className="nav-label">Все разделы</span><Icon className="nav-more-chevron" name="chevron" size={15}/></button>{sectionsOpen ? <div id="secondary-navigation" className="nav-more-panel">{secondaryNavigation.map((section)=><section className="nav-more-group" key={section.id}><p>{section.label}</p>{section.items.map((item)=><NavigationLink item={item} path={path} secondary key={item.id}/>)}</section>)}</div> : null}</div> : null}</nav>
      <div className="sidebar-footer"><div className="profile"><UserAvatar session={auth.session!}/><span className="profile-copy"><strong>{auth.session?.displayName}</strong><small>Защищённая сессия</small></span></div><button className="icon-button" onClick={() => void auth.logout()} title="Выйти" aria-label="Выйти"><Icon name="logout"/></button></div>
    </aside>
    <main className="main-column">
      <header className="topbar"><div className="topbar-context"><button className="mobile-menu" onClick={()=>setMobileOpen(v=>!v)} aria-label="Меню"><Icon name="menu"/></button><div><span className="workspace-label">TORGNEXA</span><strong>{current?.label??"Текущий контур"}</strong></div></div><div className="topbar-right"><button className="quick-search" onClick={()=>setCommandOpen(true)}><Icon name="search"/><span>Поиск и переход</span><kbd>⌘ K</kbd></button><button className="icon-button topbar-icon" onClick={()=>setActivityOpen(true)} aria-label="Центр активности"><Icon name="activity"/></button><button className={`icon-button topbar-icon ${compact?"active":""}`} onClick={toggleCompact} aria-label="Сменить плотность таблиц" title="Плотность интерфейса"><Icon name="columns"/></button><button className="icon-button topbar-icon" onClick={toggleTheme} aria-label="Сменить тему"><Icon name={theme==="dark"?"sun":"moon"}/></button><span className={`realtime-pill ${realtime}`} title={realtime==="live"?"Поток обновлений подключён":realtime==="connecting"?"Подключаем поток обновлений":"Поток обновлений временно недоступен"}><span/>{realtimeLabels[realtime]}</span><span className="security-pill"><span/>защищено</span></div></header>
      <div className="content"><Suspense fallback={<section className="page-loading" aria-busy="true">Загрузка раздела…</section>}>{!knownPath?<section className="denied"><span className="denied-code">404</span><h1>Страница не найдена</h1><p>Такого раздела или вложенного маршрута нет.</p><button className="button primary" onClick={() => navigate("/")}>Вернуться в обзор</button></section>:allowed ? content(path) : <section className="denied"><span className="denied-code">403</span><h1>Раздел недоступен</h1><p>Для раздела «{current?.label??"текущего маршрута"}» не хватает прав.</p><button className="button primary" onClick={() => navigate("/")}>Вернуться в обзор</button></section>}</Suspense></div>
    </main>
    <CommandPalette open={commandOpen} onClose={()=>setCommandOpen(false)}/><ActivityCenter open={activityOpen} onClose={()=>setActivityOpen(false)}/>
  </div>;
}

import {useEffect,useMemo,useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useAuth} from "../auth/AuthProvider";
import {useApi} from "../api/ApiProvider";
import {allowedNavigation, canOpenPath, routeForPath} from "./navigation";
import {navigate, useLocationPath} from "./useLocationPath";
import {DashboardPage} from "../pages/DashboardPage";
import {CatalogPage} from "../pages/CatalogPage";
import {OrdersPage} from "../pages/OrdersPage";
import {NotificationsPage} from "../pages/NotificationsPage";
import {InventoryPage} from "../pages/InventoryPage";
import {CompliancePage} from "../pages/CompliancePage";
import {SettingsPage} from "../pages/SettingsPage";
import {ReportsPage} from "../pages/ReportsPage";
import {AuditPage} from "../pages/AuditPage";
import {SyncPage} from "../pages/SyncPage";
import {ApprovalsPage} from "../pages/ApprovalsPage";
import {IntegrationsPage} from "../pages/IntegrationsPage";
import {PlaceholderPage} from "../pages/PlaceholderPage";
import {IncidentCenterPage} from "../pages/IncidentCenterPage";
import {useRealtimeInvalidation} from "../app/useRealtime";
import {ConnectorOAuthCallbackPage} from "../pages/ConnectorOAuthCallbackPage";
import {Icon} from "../components/Icon";
import {CommandPalette} from "../components/CommandPalette";
import {Drawer} from "../components/Drawer";
import {StatusBadge} from "../components/StatusBadge";
import {useUi} from "../app/UiProvider";

function content(path: string) {
  if (path === "/") return <DashboardPage />;
  if (path === "/catalog" || path.startsWith("/catalog/")) return <CatalogPage />;
  if (path === "/orders" || path.startsWith("/orders/")) return <OrdersPage />;
  if (path === "/inventory") return <InventoryPage />;
  if (path === "/incidents" || path.startsWith("/incidents/")) return <IncidentCenterPage />;
  if (path === "/compliance") return <CompliancePage />;
  if (path === "/notifications") return <NotificationsPage />;
  if (path === "/sync") return <SyncPage />;
  if (path === "/approvals") return <ApprovalsPage />;
  if (path === "/integrations") return <IntegrationsPage />;
  if (path === "/reports") return <ReportsPage />;
  if (path === "/audit") return <AuditPage />;
  if (path === "/settings") return <SettingsPage />;
  if (path === "/oauth/connectors/callback") return <ConnectorOAuthCallbackPage />;
  const item = routeForPath(path);
  return item ? <PlaceholderPage item={item} /> : null;
}

function ActivityCenter({open,onClose}:{open:boolean;onClose:()=>void}){const api=useApi();const notifications=useQuery({queryKey:["shell","activity","notifications"],queryFn:async()=>((await api.listNotifications({limit:8})).body as {items?:any[]}).items??[],enabled:open});const approvals=useQuery({queryKey:["shell","activity","approvals"],queryFn:async()=>((await api.listApprovals()).body as {items?:any[]}).items??[],enabled:open});const unread=(notifications.data??[]).filter(v=>!v.read_at);const pending=(approvals.data??[]).filter(v=>v.state==="pending");return <Drawer open={open} title="Центр активности" subtitle="События, требующие внимания" onClose={onClose}><div className="activity-summary"><div><strong>{unread.length}</strong><span>непрочитанных</span></div><div><strong>{pending.length}</strong><span>согласований</span></div></div><div className="activity-section"><div className="drawer-section-heading"><h3>Сейчас важно</h3><button className="button ghost" onClick={()=>{navigate("/notifications");onClose()}}>Все события</button></div>{[...pending.map(v=>({id:v.id,title:"Требуется согласование",body:v.action,severity:"warning"})),...unread.map(v=>({id:v.id,title:v.title,body:v.body,severity:v.severity}))].slice(0,8).map(item=><button className="activity-item" key={item.id} onClick={()=>{navigate(item.title==="Требуется согласование"?"/approvals":"/notifications");onClose()}}><StatusBadge value={item.severity}/><span><strong>{item.title}</strong><small>{item.body}</small></span><Icon name="chevron"/></button>)}</div></Drawer>}

export function AppShell() {
  const auth = useAuth();
  const path = useLocationPath();
  const {theme,toggleTheme,compact,toggleCompact}=useUi();
  const realtime=useRealtimeInvalidation();
  const [commandOpen,setCommandOpen]=useState(false),[activityOpen,setActivityOpen]=useState(false),[mobileOpen,setMobileOpen]=useState(false);
  const capabilities = auth.session?.capabilities ?? [];
  const navigation = useMemo(() => allowedNavigation(capabilities), [capabilities]);
  const allowed = path === "/oauth/connectors/callback" ? capabilities.includes("connectors.accounts.write") : canOpenPath(path, capabilities);
  const current = routeForPath(path);
  useEffect(()=>{let prefix=false;let timer=0;const handler=(event:KeyboardEvent)=>{const target=event.target as HTMLElement|null;if(target?.closest("input,textarea,select,[contenteditable=true]"))return;const key=event.key.toLowerCase();if((event.metaKey||event.ctrlKey)&&key==="k"){event.preventDefault();setCommandOpen(true);return}if(event.key==="Escape"){setCommandOpen(false);setMobileOpen(false);prefix=false;return}if(key==="g"){prefix=true;window.clearTimeout(timer);timer=window.setTimeout(()=>{prefix=false},900);return}if(prefix){prefix=false;const item=navigation.find(value=>value.shortcut?.toLowerCase()===`g ${key}`);if(item){event.preventDefault();navigate(item.path)}}};window.addEventListener("keydown",handler);return()=>{window.removeEventListener("keydown",handler);window.clearTimeout(timer)}},[navigation]);
  useEffect(()=>setMobileOpen(false),[path]);

  return <div className={`app-shell ${mobileOpen?"nav-open":""}`}>
    <aside className="sidebar">
      <button className="brand" onClick={() => navigate("/")} aria-label="TORGNEXA — обзор"><span className="brand-mark small">TN</span><span><strong>TORGNEXA</strong><small>Commerce Orchestration</small></span></button>
      <nav aria-label="Основная навигация">{navigation.map((item) => <button key={item.id} aria-current={path===item.path?"page":undefined} title={item.label} className={`nav-item ${path === item.path ? "active" : ""}`} onClick={() => navigate(item.path)}><span className="nav-icon"><Icon name={item.icon}/></span><span className="nav-label">{item.label}</span></button>)}</nav>
      <div className="sidebar-footer"><div className="profile"><span className="avatar">{auth.session?.displayName.slice(0, 1).toUpperCase()}</span><span className="profile-copy"><strong>{auth.session?.displayName}</strong><small>Защищённая сессия</small></span></div><button className="icon-button" onClick={() => void auth.logout()} title="Выйти" aria-label="Выйти"><Icon name="logout"/></button></div>
    </aside>
    <main className="main-column">
      <header className="topbar"><div className="topbar-context"><button className="mobile-menu" onClick={()=>setMobileOpen(v=>!v)} aria-label="Меню"><Icon name="menu"/></button><div><span className="workspace-label">TORGNEXA</span><strong>{current?.label??"Текущий контур"}</strong></div></div><div className="topbar-right"><button className="quick-search" onClick={()=>setCommandOpen(true)}><Icon name="search"/><span>Поиск и переход</span><kbd>⌘ K</kbd></button><button className="icon-button topbar-icon" onClick={()=>setActivityOpen(true)} aria-label="Центр активности"><Icon name="activity"/></button><button className={`icon-button topbar-icon ${compact?"active":""}`} onClick={toggleCompact} aria-label="Сменить плотность таблиц" title="Плотность интерфейса"><Icon name="columns"/></button><button className="icon-button topbar-icon" onClick={toggleTheme} aria-label="Сменить тему"><Icon name={theme==="dark"?"sun":"moon"}/></button><span className={`realtime-pill ${realtime}`} title={realtime==="live"?"Realtime stream подключён":realtime==="connecting"?"Подключаем realtime":"Realtime временно недоступен"}><span/>{realtime==="live"?"live":realtime==="connecting"?"connecting":"offline"}</span><span className="security-pill"><span/>защищено</span></div></header>
      <div className="content">{allowed ? content(path) : <section className="denied"><span className="denied-code">403</span><h1>Раздел недоступен</h1><p>{current ? `Для раздела «${current.label}» не хватает capability.` : "Такого маршрута нет в frontend shell."}</p><button className="button primary" onClick={() => navigate("/")}>Вернуться в обзор</button></section>}</div>
    </main>
    <CommandPalette open={commandOpen} onClose={()=>setCommandOpen(false)}/><ActivityCenter open={activityOpen} onClose={()=>setActivityOpen(false)}/>
  </div>;
}

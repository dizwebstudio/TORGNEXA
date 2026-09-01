import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {decodeItems as decode} from "../../api/decoders";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock,LoadingBlock} from "../../components/ApiState";
import {Dialog} from "../../components/Dialog";
import {EmptyState} from "../../components/EmptyState";
import {Icon} from "../../components/Icon";
import {StatusBadge} from "../../components/StatusBadge";
import {useToast} from "../../components/Toast";

type Permission="commerce.products.read"|"commerce.orders.read"|"party.counterparties.read"|"commerce.price.change.request";
interface Account{id:string;label:string;agent_id:string;model_id:string;integration_id:string;permissions:Permission[];enabled:boolean;version:number;credential_status:"active"|"expired"|"revoked";expires_at:string;rotated_from_id?:string;revoked_at?:string;last_used_at?:string;use_count:number;created_at:string;updated_at:string}
type R={body:any};type Client={listMCPAccounts():Promise<R>;createMCPAccount(x:object):Promise<R>;disableMCPAccount(x:object):Promise<R>;rotateMCPAccount(x:object):Promise<R>;getMCPAccountPolicy(x:object):Promise<R>;installMCPAccountPolicy(x:object):Promise<R>;getMCPAgentKillSwitch():Promise<R>;setMCPAgentKillSwitch(x:object):Promise<R>};
const permissionLabels:Readonly<Record<Permission,string>>={"commerce.products.read":"Поиск товаров","commerce.orders.read":"Список заказов","party.counterparties.read":"Поиск контрагентов","commerce.price.change.request":"Заявка на изменение цены"};
const permissionKeys=Object.keys(permissionLabels) as Permission[];
function decodeCreated(value:unknown):{account:Account;token?:string}{const root=value as {account?:unknown;token?:unknown};if(!root?.account||(root.token!==undefined&&typeof root.token!=="string"))throw new Error("invalid MCP account create response");return {account:root.account as Account,token:root.token as string|undefined}}

interface PolicyRuleMoney{currency:string;minor_units:number}
interface PolicyRule{tool:string;permission:string;risk:string;approval_required:boolean;money?:PolicyRuleMoney[];max_calls?:number;window_seconds?:number}
interface Policy{installed:boolean;policy_id?:string;version?:number;rules?:PolicyRule[];effective_from?:string;effective_until?:string}
function decodePolicy(value:unknown):Policy{const root=value as {installed?:unknown};if(typeof root?.installed!=="boolean")throw new Error("invalid policy response");return value as Policy}
interface KillSwitch{disabled:boolean;version:number}
function decodeKillSwitch(value:unknown):KillSwitch{const root=value as {disabled?:unknown;version?:unknown};if(typeof root?.disabled!=="boolean"||typeof root?.version!=="number")throw new Error("invalid kill switch response");return {disabled:root.disabled,version:root.version}}

export function MCPAccountSettings(){
 const api=useApi() as unknown as Client,auth=useAuth(),cache=useQueryClient(),toast=useToast();
 const canRead=auth.session?.capabilities.includes("settings.mcp_accounts.read")??false;
 const canWrite=auth.session?.capabilities.includes("settings.mcp_accounts.write")??false;
 const [label,setLabel]=useState(""),[agentId,setAgentId]=useState(""),[modelId,setModelId]=useState(""),[integrationId,setIntegrationId]=useState(""),[permissions,setPermissions]=useState<Permission[]>([]);
 const [revealed,setRevealed]=useState<{label:string;token:string}|null>(null);
 const [expanded,setExpanded]=useState<string|null>(null);
 const query=useQuery({queryKey:["settings","mcp-accounts"],queryFn:async()=>decode((await api.listMCPAccounts()).body),enabled:canRead,staleTime:10_000});
 const refresh=()=>cache.invalidateQueries({queryKey:["settings","mcp-accounts"]});
 const reset=()=>{setLabel("");setAgentId("");setModelId("");setIntegrationId("");setPermissions([])};
 const togglePermission=(key:Permission)=>setPermissions(current=>current.includes(key)?current.filter(value=>value!==key):[...current,key]);
 const reveal=(created:{account:Account;token?:string},title:string)=>{if(created.token)setRevealed({label:created.account.label,token:created.token});else toast.push({kind:"error",title:"Операция уже была выполнена",body:"Токен не хранится и не возвращается повторно. Выполните новую ротацию."});toast.push({kind:"success",title})};
 const create=useMutation({mutationFn:()=>api.createMCPAccount({idempotencyKey:crypto.randomUUID(),body:{label:label.trim(),agent_id:agentId.trim(),model_id:modelId.trim(),integration_id:integrationId.trim(),permissions,expires_in_days:90}}),onSuccess:async result=>{reveal(decodeCreated(result.body),"MCP-аккаунт создан");reset();await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось создать аккаунт",body:"Проверьте название, agent_id, model_id, integration_id и выбранные инструменты."})});
 const disable=useMutation({mutationFn:(account:Account)=>api.disableMCPAccount({idempotencyKey:crypto.randomUUID(),body:{account_id:account.id,expected_version:account.version}}),onSuccess:async()=>{toast.push({kind:"success",title:"MCP-аккаунт отозван"});await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось отозвать аккаунт"})});
 const rotate=useMutation({mutationFn:(account:Account)=>api.rotateMCPAccount({idempotencyKey:crypto.randomUUID(),body:{account_id:account.id,expected_version:account.version,expires_in_days:90}}),onSuccess:async result=>{reveal(decodeCreated(result.body),"Токен ротирован");await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось ротировать токен"})});
 if(!canRead)return null;
 const valid=!!label.trim()&&!!agentId.trim()&&!!modelId.trim()&&!!integrationId.trim()&&permissions.length>0;
 return <section className="panel settings-card">
  <div className="settings-card-heading"><div><p className="eyebrow">MCP</p><h2>Доступ AI-агентов</h2><p className="settings-copy">Учётные записи внешних MCP-клиентов. Токен показывается один раз, действует 90 дней и может быть ротирован; использование и отзыв отслеживаются без хранения исходного секрета.</p></div>{query.data?<StatusBadge value={`${query.data.length}`}/>:null}</div>
  <AgentKillSwitch api={api} canWrite={canWrite}/>
  {query.isPending?<LoadingBlock/>:query.isError?<ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить MCP-аккаунты.</ErrorBlock>:query.data.length===0?<EmptyState title="MCP-аккаунты не настроены" text="Добавьте первый аккаунт в форме ниже."/>:<div className="settings-grid">{query.data.map(account=><article className="connector-account" key={account.id}><header><div><strong>{account.label}</strong><small>{account.agent_id} · {account.model_id} · {account.integration_id}</small></div><StatusBadge value={account.credential_status}/></header><small>Истекает: {new Date(account.expires_at).toLocaleString("ru-RU")} · вызовов: {account.use_count} · последнее использование: {account.last_used_at?new Date(account.last_used_at).toLocaleString("ru-RU"):"ещё не использовался"}</small><div className="chip-list">{account.permissions.map((permission: Permission)=><span className="chip" key={permission}>{permissionLabels[permission]}</span>)}</div><div className="account-actions"><button className="button ghost" onClick={()=>setExpanded(current=>current===account.id?null:account.id)}>{expanded===account.id?"Скрыть политику":"Политика доступа"}</button>{account.credential_status==="active"&&canWrite?<><button className="button ghost" disabled={rotate.isPending} onClick={()=>rotate.mutate(account)}>Ротировать токен</button><button className="button ghost danger-text" disabled={disable.isPending} onClick={()=>disable.mutate(account)}>Отозвать</button></>:null}</div>{expanded===account.id?<AccountPolicyPanel api={api} account={account} canWrite={canWrite}/>:null}</article>)}</div>}
  {canWrite?<section className="drawer-section">
    <h3>Добавить аккаунт</h3>
    <div className="settings-grid">
     <label className="field"><span>Название</span><input value={label} maxLength={120} placeholder="Например, «n8n-воркфлоу»" onChange={e=>setLabel(e.target.value)}/></label>
     <label className="field"><span>Agent ID</span><input value={agentId} maxLength={160} placeholder="agent-1" onChange={e=>setAgentId(e.target.value)}/></label>
     <label className="field"><span>Model ID</span><input value={modelId} maxLength={160} placeholder="claude-opus-5" onChange={e=>setModelId(e.target.value)}/></label>
     <label className="field"><span>Integration ID</span><input value={integrationId} maxLength={160} placeholder="n8n" onChange={e=>setIntegrationId(e.target.value)}/></label>
    </div>
    <fieldset><legend>Разрешённые инструменты</legend>{permissionKeys.map(key=><label className="check-row" key={key}><input type="checkbox" checked={permissions.includes(key)} onChange={()=>togglePermission(key)}/><span>{permissionLabels[key]}</span></label>)}</fieldset>
    <div className="account-actions"><button className="button primary" disabled={create.isPending||!valid} onClick={()=>create.mutate()}>{create.isPending?"Создаём…":"Создать аккаунт"}</button></div>
  </section>:null}
  <Dialog open={!!revealed} title="Токен MCP-аккаунта" description={revealed?`«${revealed.label}» — сохраните токен сейчас, он больше не будет показан.`:undefined} onClose={()=>setRevealed(null)}>
   {revealed?<div className="settings-grid"><code className="token-reveal">{revealed.token}</code><div className="account-actions"><button className="button primary" onClick={()=>{void navigator.clipboard?.writeText(revealed.token);toast.push({kind:"success",title:"Токен скопирован"})}}>Скопировать</button><button className="button ghost" onClick={()=>setRevealed(null)}>Закрыть</button></div></div>:null}
  </Dialog>
 </section>
}

function AgentKillSwitch({api,canWrite}:{api:Client;canWrite:boolean}){
 const toast=useToast();
 const query=useQuery({queryKey:["settings","mcp-agents","kill-switch"],queryFn:async()=>decodeKillSwitch((await api.getMCPAgentKillSwitch()).body),staleTime:10_000});
 const [confirming,setConfirming]=useState(false),[reason,setReason]=useState("");
 const toggle=useMutation({mutationFn:()=>api.setMCPAgentKillSwitch({body:{disabled:!query.data?.disabled,reason:reason.trim()}}),onSuccess:async()=>{const wasDisabled=query.data?.disabled;setConfirming(false);setReason("");toast.push({kind:"success",title:wasDisabled?"AI-агенты снова разрешены":"AI-агенты остановлены"});await query.refetch()},onError:()=>toast.push({kind:"error",title:"Не удалось изменить состояние выключателя"})});
 if(query.isPending)return null;
 if(query.isError)return <ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить состояние выключателя.</ErrorBlock>;
 const disabled=query.data.disabled;
 return <>
  {disabled?<div className="incident-banner"><Icon name="incident"/><div><strong>Все AI-агенты остановлены аварийным выключателем.</strong><p>Вызовы MCP-инструментов блокируются для всех агентов этого рабочего пространства независимо от их индивидуальных политик, пока выключатель не будет снят.</p></div>{canWrite?<button className="button primary" onClick={()=>setConfirming(true)}>Возобновить работу</button>:null}</div>
   :canWrite?<div className="settings-card-heading"><div><p className="eyebrow">Аварийный выключатель</p><span className="settings-copy">Мгновенно останавливает вызовы всех AI-агентов этого рабочего пространства, независимо от их политик.</span></div><button className="button ghost danger-text" onClick={()=>setConfirming(true)}>Остановить всех агентов</button></div>:null}
  <Dialog open={confirming} title={disabled?"Возобновить работу AI-агентов":"Аварийная остановка AI-агентов"} description={disabled?undefined:"Каждый вызов MCP-инструмента любым агентом этого workspace будет немедленно отклонён."} onClose={()=>setConfirming(false)}>
   <div className="settings-grid">
    <label className="field"><span>Причина</span><input value={reason} maxLength={256} placeholder={disabled?"Инцидент устранён":"Например, подозрение на компрометацию токена"} onChange={e=>setReason(e.target.value)}/></label>
    <div className="account-actions"><button className={`button ${disabled?"primary":"danger"}`} disabled={!reason.trim()||toggle.isPending} onClick={()=>toggle.mutate()}>{toggle.isPending?"Применяем…":disabled?"Возобновить":"Остановить"}</button><button className="button ghost" onClick={()=>setConfirming(false)}>Отмена</button></div>
   </div>
  </Dialog>
 </>
}

function AccountPolicyPanel({api,account,canWrite}:{api:Client;account:Account;canWrite:boolean}){
 const toast=useToast();
 const query=useQuery({queryKey:["settings","mcp-accounts",account.id,"policy"],queryFn:async()=>decodePolicy((await api.getMCPAccountPolicy({accountId:account.id})).body),staleTime:10_000});
 const needsPriceLimit=account.permissions.includes("commerce.price.change.request");
 const [currency,setCurrency]=useState("RUB"),[maxAmount,setMaxAmount]=useState(""),[maxCalls,setMaxCalls]=useState(""),[windowSeconds,setWindowSeconds]=useState("3600");
 const install=useMutation({mutationFn:()=>api.installMCPAccountPolicy({accountId:account.id,body:needsPriceLimit?{price_change_money_limits:[{currency:currency.toUpperCase(),max_minor_units:Math.round(Number(maxAmount)*100)}],price_change_max_calls:maxCalls?Number(maxCalls):undefined,price_change_window_seconds:maxCalls?Number(windowSeconds):undefined}:{}}),onSuccess:async()=>{toast.push({kind:"success",title:"Политика установлена"});await query.refetch()},onError:()=>toast.push({kind:"error",title:"Не удалось установить политику",body:needsPriceLimit?"Для заявок на изменение цены нужен лимит суммы больше нуля.":"Проверьте, что аккаунту разрешён хотя бы один инструмент."})});
 if(query.isPending)return <LoadingBlock/>;
 if(query.isError)return <ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить политику.</ErrorBlock>;
 return <div className="drawer-section">
  {query.data.installed?<>
   <p className="settings-note">Версия {query.data.version} · действует с {query.data.effective_from?new Date(query.data.effective_from).toLocaleString("ru-RU"):"—"}</p>
   <div className="chip-list">{(query.data.rules??[]).map(rule=><span className="chip" key={rule.tool}>{rule.tool}{rule.money?.length?` · до ${rule.money.map(m=>`${(m.minor_units/100).toLocaleString("ru-RU")} ${m.currency}`).join(", ")}`:""}</span>)}</div>
  </>:<p className="settings-note">Политика не установлена — вызовы всех инструментов этого аккаунта будут отклонены governance-сервисом.</p>}
  {canWrite?<>
   {needsPriceLimit?<div className="settings-grid">
    <label className="field"><span>Лимит на заявку об изменении цены</span><input type="number" min="0" step="0.01" value={maxAmount} onChange={e=>setMaxAmount(e.target.value)}/></label>
    <label className="field"><span>Валюта</span><input value={currency} maxLength={3} onChange={e=>setCurrency(e.target.value)}/></label>
    <label className="field"><span>Лимит вызовов за окно (опционально)</span><input type="number" min="0" value={maxCalls} onChange={e=>setMaxCalls(e.target.value)}/></label>
    <label className="field"><span>Окно, сек</span><input type="number" min="300" value={windowSeconds} onChange={e=>setWindowSeconds(e.target.value)}/></label>
   </div>:null}
   <div className="account-actions"><button className="button ghost" disabled={install.isPending||(needsPriceLimit&&!(Number(maxAmount)>0))} onClick={()=>install.mutate()}>{install.isPending?"Устанавливаем…":query.data.installed?"Обновить политику":"Установить политику"}</button></div>
  </>:null}
 </div>
}

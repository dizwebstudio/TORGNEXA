import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock,LoadingBlock} from "../../components/ApiState";
import {Dialog} from "../../components/Dialog";
import {EmptyState} from "../../components/EmptyState";
import {StatusBadge} from "../../components/StatusBadge";
import {useToast} from "../../components/Toast";

type Permission="commerce.products.read"|"commerce.orders.read"|"party.counterparties.read"|"commerce.price.change.request";
interface Account{id:string;label:string;agent_id:string;model_id:string;integration_id:string;permissions:Permission[];enabled:boolean;version:number;created_at:string;updated_at:string}
const permissionLabels:Readonly<Record<Permission,string>>={"commerce.products.read":"Поиск товаров","commerce.orders.read":"Список заказов","party.counterparties.read":"Поиск контрагентов","commerce.price.change.request":"Заявка на изменение цены"};
const permissionKeys=Object.keys(permissionLabels) as Permission[];
function decode(value:unknown):Account[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid MCP account response");return root.items as Account[]}
function decodeCreated(value:unknown):{account:Account;token:string}{const root=value as {account?:unknown;token?:unknown};if(!root?.account||typeof root.token!=="string")throw new Error("invalid MCP account create response");return {account:root.account as Account,token:root.token}}

export function MCPAccountSettings(){
 const api=useApi(),auth=useAuth(),cache=useQueryClient(),toast=useToast();
 const canRead=auth.session?.capabilities.includes("settings.mcp_accounts.read")??false;
 const canWrite=auth.session?.capabilities.includes("settings.mcp_accounts.write")??false;
 const [label,setLabel]=useState(""),[agentId,setAgentId]=useState(""),[modelId,setModelId]=useState(""),[integrationId,setIntegrationId]=useState(""),[permissions,setPermissions]=useState<Permission[]>([]);
 const [revealed,setRevealed]=useState<{label:string;token:string}|null>(null);
 const query=useQuery({queryKey:["settings","mcp-accounts"],queryFn:async()=>decode((await api.listMCPAccounts()).body),enabled:canRead,staleTime:10_000});
 const refresh=()=>cache.invalidateQueries({queryKey:["settings","mcp-accounts"]});
 const reset=()=>{setLabel("");setAgentId("");setModelId("");setIntegrationId("");setPermissions([])};
 const togglePermission=(key:Permission)=>setPermissions(current=>current.includes(key)?current.filter(value=>value!==key):[...current,key]);
 const create=useMutation({mutationFn:()=>api.createMCPAccount({idempotencyKey:crypto.randomUUID(),body:{label:label.trim(),agent_id:agentId.trim(),model_id:modelId.trim(),integration_id:integrationId.trim(),permissions}}),onSuccess:async result=>{const created=decodeCreated(result.body);setRevealed({label:created.account.label,token:created.token});toast.push({kind:"success",title:"MCP-аккаунт создан"});reset();await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось создать аккаунт",body:"Проверьте название, agent_id, model_id, integration_id и выбранные инструменты."})});
 const disable=useMutation({mutationFn:(account:Account)=>api.disableMCPAccount({body:{account_id:account.id,expected_version:account.version}}),onSuccess:async()=>{toast.push({kind:"success",title:"MCP-аккаунт отключён"});await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось отключить аккаунт"})});
 if(!canRead)return null;
 const valid=!!label.trim()&&!!agentId.trim()&&!!modelId.trim()&&!!integrationId.trim()&&permissions.length>0;
 return <section className="panel settings-card">
  <div className="settings-card-heading"><div><p className="eyebrow">MCP</p><h2>Доступ AI-агентов</h2><p className="settings-copy">Учётные записи внешних MCP-клиентов (AI-агентов), которым разрешено вызывать инструменты TORGNEXA. Токен доступа показывается один раз при создании и больше не сохраняется — если он утерян, отключите аккаунт и создайте новый.</p></div>{query.data?<StatusBadge value={`${query.data.length}`}/>:null}</div>
  {query.isPending?<LoadingBlock/>:query.isError?<ErrorBlock>Не удалось загрузить MCP-аккаунты.</ErrorBlock>:query.data.length===0?<EmptyState title="MCP-аккаунты не настроены" text="Добавьте первый аккаунт в форме ниже."/>:<div className="settings-grid">{query.data.map(account=><article className="connector-account" key={account.id}><header><div><strong>{account.label}</strong><small>{account.agent_id} · {account.model_id} · {account.integration_id}</small></div><StatusBadge value={account.enabled?"active":"disabled"}/></header><div className="chip-list">{account.permissions.map(permission=><span className="chip" key={permission}>{permissionLabels[permission]}</span>)}</div>{account.enabled&&canWrite?<div className="account-actions"><button className="button ghost danger-text" disabled={disable.isPending} onClick={()=>disable.mutate(account)}>Отключить</button></div>:null}</article>)}</div>}
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

import {useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock, LoadingBlock} from "../../components/ApiState";
import {EmptyState} from "../../components/EmptyState";
import {StatusBadge} from "../../components/StatusBadge";

interface Provider {
  provider_id:string; protocol:"oidc"; display_name:string; issuer_url:string; client_id:string; callback_url:string;
  secret_configured:boolean; revision:number; version:number; active_revision?:number; enabled:boolean;
  validation_status:"not_validated"|"valid"|"invalid"; validation_reason:string; validated_at?:string; updated_at:string;
}

const decode=(value:unknown):Provider[]=>{
  if(!value||typeof value!=="object"||!Array.isArray((value as {items?:unknown}).items))throw new Error("invalid identity provider response");
  return (value as {items:Provider[]}).items.map((item)=>{
    if(!item||typeof item.provider_id!=="string"||item.protocol!=="oidc"||typeof item.version!=="number"||typeof item.revision!=="number")throw new Error("invalid identity provider");
    return item;
  });
};

export function IdentityProviderSettings(){
 const api=useApi(),auth=useAuth(),cache=useQueryClient();
 const canRead=auth.session?.capabilities.includes("settings.identity_providers.read")??false;
 const canWrite=auth.session?.capabilities.includes("settings.identity_providers.write")??false;
 const callback=`${window.location.origin}/oidc/callback`;
 const [providerID,setProviderID]=useState(""),[displayName,setDisplayName]=useState(""),[issuerURL,setIssuerURL]=useState(""),[clientID,setClientID]=useState(""),[clientSecret,setClientSecret]=useState("");
 const [rollback,setRollback]=useState<Record<string,string>>({});
 const query=useQuery({queryKey:["settings","identity-providers"],enabled:canRead,queryFn:async()=>decode((await api.listSettingsIdentityProviders()).body)});
 const current=query.data?.find((item)=>item.provider_id===providerID);
 const refresh=()=>cache.invalidateQueries({queryKey:["settings","identity-providers"]});
 const save=useMutation({mutationFn:()=>api.saveSettingsIdentityProviderDraft({providerId:providerID,idempotencyKey:crypto.randomUUID(),body:{protocol:"oidc",display_name:displayName,issuer_url:issuerURL,client_id:clientID,callback_url:callback,client_secret:clientSecret||undefined,expected_version:current?.version??0}}),onSuccess:async()=>{setClientSecret("");await refresh();}});
 const action=useMutation({mutationFn:({item,kind}:{item:Provider;kind:"validate"|"activate"|"disable"|"rollback"})=>{
  const base={providerId:item.provider_id,idempotencyKey:crypto.randomUUID(),body:{expected_version:item.version}};
  if(kind==="validate")return api.validateSettingsIdentityProvider(base);
  if(kind==="activate")return api.activateSettingsIdentityProvider(base);
  if(kind==="disable")return api.disableSettingsIdentityProvider(base);
  return api.rollbackSettingsIdentityProvider({...base,body:{expected_version:item.version,target_revision:Number(rollback[item.provider_id])}});
 },onSettled:refresh});
 const edit=(item:Provider)=>{setProviderID(item.provider_id);setDisplayName(item.display_name);setIssuerURL(item.issuer_url);setClientID(item.client_id);setClientSecret("");};
 if(!canRead)return null;
 return <section className="panel settings-card identity-provider-settings">
  <div className="settings-card-heading"><div><p className="eyebrow">Identity boundary</p><h2>Провайдеры входа</h2></div><span className="status status-active">Default deny</span></div>
  <p className="settings-note">Любой OIDC-провайдер, включая VK ID, настраивается одинаково. Issuer должен входить в deployment allowlist. Секрет сохраняется один раз и больше не возвращается в браузер.</p>
  {canWrite?<form className="catalog-form" onSubmit={(event)=>{event.preventDefault();save.mutate()}}>
   <label className="field">Ключ провайдера<input required pattern="[a-z0-9][a-z0-9._-]{0,63}" value={providerID} onChange={(event)=>setProviderID(event.target.value.trim().toLowerCase())} placeholder="например, vk или corporate"/></label>
   <label className="field">Название<input required maxLength={160} value={displayName} onChange={(event)=>setDisplayName(event.target.value)} placeholder="VK ID"/></label>
   <label className="field">OIDC issuer<input required type="url" value={issuerURL} onChange={(event)=>setIssuerURL(event.target.value)} placeholder="https://id.example.ru"/></label>
   <label className="field">Client ID<input required maxLength={256} value={clientID} onChange={(event)=>setClientID(event.target.value)} /></label>
   <label className="field">Client secret<input type="password" autoComplete="new-password" maxLength={65536} value={clientSecret} onChange={(event)=>setClientSecret(event.target.value)} placeholder={current?.secret_configured?"Оставьте пустым, чтобы сохранить текущий":"Введите секрет клиента"}/></label>
   <label className="field">Callback URL<input readOnly value={callback}/></label>
   <button className="button primary" disabled={save.isPending}>{save.isPending?"Сохраняем…":current?"Создать новую версию":"Сохранить черновик"}</button>
  </form>:null}
  {save.isError?<ErrorBlock>Черновик отклонён. Проверьте allowlist issuer, callback и актуальную версию.</ErrorBlock>:null}
  {query.isPending?<LoadingBlock/>:query.isError?<ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить провайдеров входа.</ErrorBlock>:query.data?.length===0?<EmptyState title="Провайдеры не настроены" text="Добавьте OIDC-конфигурацию, проверьте discovery и только затем активируйте её."/>:<div className="integration-grid">{query.data?.map((item)=><article className="panel integration-card" key={item.provider_id}>
   <div className="settings-card-heading"><div><strong>{item.display_name}</strong><small className="mono">{item.provider_id} · revision {item.revision}</small></div><StatusBadge value={item.enabled?"active":"disabled"}/></div>
   <div className="integration-meta"><span>Issuer</span><strong>{item.issuer_url}</strong></div><div className="integration-meta"><span>Валидация</span><strong>{item.validation_status}</strong></div><div className="integration-meta"><span>Активная версия</span><strong>{item.active_revision??"—"}</strong></div>
   <div className="integration-actions"><button className="button ghost" onClick={()=>edit(item)}>Редактировать</button>{canWrite?<><button className="button ghost" disabled={action.isPending} onClick={()=>action.mutate({item,kind:"validate"})}>Проверить discovery</button>{item.validation_status==="valid"&&!item.enabled?<button className="button primary" disabled={action.isPending} onClick={()=>action.mutate({item,kind:"activate"})}>Активировать</button>:null}{item.enabled?<button className="button danger" disabled={action.isPending} onClick={()=>action.mutate({item,kind:"disable"})}>Отключить</button>:null}</>:null}</div>
   {canWrite&&item.revision>1?<div className="catalog-inline"><input type="number" min={1} max={item.revision-1} value={rollback[item.provider_id]??""} onChange={(event)=>setRollback((current)=>({...current,[item.provider_id]:event.target.value}))} placeholder="Версия для отката"/><button className="button ghost" disabled={action.isPending||!rollback[item.provider_id]} onClick={()=>action.mutate({item,kind:"rollback"})}>Безопасный откат</button></div>:null}
  </article>)}</div>}
  {action.isError?<ErrorBlock>Операция не выполнена: обновите данные и проверьте подтверждение проверки.</ErrorBlock>:null}
 </section>;
}

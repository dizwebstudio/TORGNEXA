import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock,LoadingBlock} from "../../components/ApiState";
import {EmptyState} from "../../components/EmptyState";
import {StatusBadge} from "../../components/StatusBadge";
import {useToast} from "../../components/Toast";

type ProviderID="openai-compatible"|"gigachat"|"yandexgpt"|"kimi"|"qwen"|"deepseek"|"claude"|"ollama"|"lm-studio"|"open-webui";
interface Account{id:string;provider:ProviderID;label:string;model:string;base_url?:string;folder_id?:string;enabled:boolean;version:number;created_at:string;updated_at:string}
const providerLabels:Readonly<Record<ProviderID,string>>={"openai-compatible":"OpenAI-совместимый","gigachat":"GigaChat (Sber)","yandexgpt":"YandexGPT","kimi":"Kimi (Moonshot AI)","qwen":"Qwen (Alibaba Cloud)","deepseek":"DeepSeek","claude":"Claude (Anthropic)","ollama":"Ollama","lm-studio":"LM Studio","open-webui":"Open WebUI"};
const providerKeys=Object.keys(providerLabels) as ProviderID[];
const hostOverrideProviders:ReadonlySet<ProviderID>=new Set(["openai-compatible","kimi","qwen","deepseek","claude","ollama","lm-studio","open-webui"]);
const localBaseURLPlaceholders:Readonly<Record<"ollama"|"lm-studio"|"open-webui",string>>={ollama:"http://ollama:11434/v1","lm-studio":"http://host.docker.internal:1234/v1","open-webui":"http://open-webui:3000/api"};
function decode(value:unknown):Account[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid AI provider account response");return root.items as Account[]}

export function AIProviderSettings(){
 const api=useApi(),auth=useAuth(),cache=useQueryClient(),toast=useToast();
 const canRead=auth.session?.capabilities.includes("settings.ai_providers.read")??false;
 const canWrite=auth.session?.capabilities.includes("settings.ai_providers.write")??false;
 const [provider,setProvider]=useState<ProviderID>("openai-compatible"),[label,setLabel]=useState(""),[model,setModel]=useState(""),[baseUrl,setBaseUrl]=useState(""),[folderId,setFolderId]=useState(""),[credential,setCredential]=useState("");
 const query=useQuery({queryKey:["settings","ai-providers"],queryFn:async()=>decode((await api.listAIProviderAccounts()).body),enabled:canRead,staleTime:10_000});
 const refresh=()=>cache.invalidateQueries({queryKey:["settings","ai-providers"]});
 const reset=()=>{setLabel("");setModel("");setBaseUrl("");setFolderId("");setCredential("")};
 const create=useMutation({mutationFn:()=>api.createAIProviderAccount({idempotencyKey:crypto.randomUUID(),body:{provider,label:label.trim(),model:model.trim(),base_url:baseUrl.trim()||undefined,folder_id:folderId.trim()||undefined,credential}}),onSuccess:async()=>{toast.push({kind:"success",title:"Аккаунт AI-провайдера добавлен"});reset();await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось добавить аккаунт",body:"Проверьте провайдера, модель, ключ и обязательные поля."})});
 const disable=useMutation({mutationFn:(account:Account)=>api.disableAIProviderAccount({idempotencyKey:crypto.randomUUID(),body:{account_id:account.id,expected_version:account.version}}),onSuccess:async()=>{toast.push({kind:"success",title:"Аккаунт отключён"});await refresh()},onError:()=>toast.push({kind:"error",title:"Не удалось отключить аккаунт"})});
 if(!canRead)return null;
 const valid=!!label.trim()&&!!model.trim()&&!!credential&&(provider!=="yandexgpt"||!!folderId.trim());
 return <section id="ai-provider-settings" className="panel settings-card">
  <div className="settings-card-heading"><div><p className="eyebrow">AI</p><h2>Провайдеры аналитики</h2><p className="settings-copy">Подключите внешнюю LLM, чтобы отправлять ей аналитические запросы из отчётов. Ключ шифруется и хранится только в SecretProvider — TORGNEXA его не показывает повторно.</p></div>{query.data?<StatusBadge value={`${query.data.length}`}/>:null}</div>
  {query.isPending?<LoadingBlock/>:query.isError?<ErrorBlock>Не удалось загрузить AI-провайдеров.</ErrorBlock>:query.data.length===0?<EmptyState title="AI-провайдеры не настроены" text="Добавьте первый аккаунт в форме ниже."/>:<div className="settings-grid">{query.data.map(account=><article className="connector-account" key={account.id}><header><div><strong>{account.label}</strong><small>{providerLabels[account.provider]} · {account.model}</small></div><StatusBadge value={account.enabled?"active":"disabled"}/></header>{account.folder_id?<small>folder_id: {account.folder_id}</small>:null}{account.base_url?<small>{account.base_url}</small>:null}{account.enabled&&canWrite?<div className="account-actions"><button className="button ghost danger-text" disabled={disable.isPending} onClick={()=>disable.mutate(account)}>Отключить</button></div>:null}</article>)}</div>}
  {canWrite?<section className="drawer-section">
    <h3>Добавить аккаунт</h3>
    <div className="settings-grid">
     <label className="field"><span>Провайдер</span><select value={provider} onChange={e=>setProvider(e.target.value as ProviderID)}>{providerKeys.map(key=><option value={key} key={key}>{providerLabels[key]}</option>)}</select></label>
     <label className="field"><span>Название</span><input value={label} maxLength={120} placeholder="Например, «Сводка по продажам»" onChange={e=>setLabel(e.target.value)}/></label>
     <label className="field"><span>Модель</span><input value={model} maxLength={120} placeholder={provider==="claude"?"claude-sonnet-4-20250514":"gpt-4o-mini"} onChange={e=>setModel(e.target.value)}/></label>
     {hostOverrideProviders.has(provider)?<label className="field"><span>Base URL (необязательно)</span><input value={baseUrl} maxLength={2039} placeholder={provider==="ollama"||provider==="lm-studio"||provider==="open-webui"?localBaseURLPlaceholders[provider]:provider==="claude"?"https://api.anthropic.com":"https://api.moonshot.ai"} onChange={e=>setBaseUrl(e.target.value)}/></label>:null}
     {provider==="yandexgpt"?<label className="field"><span>Folder ID</span><input value={folderId} maxLength={120} placeholder="b1gxxxxxxxxxxxxxxxx" onChange={e=>setFolderId(e.target.value)}/></label>:null}
     <label className="field"><span>API-ключ / токен</span><input type="password" value={credential} maxLength={65536} autoComplete="off" placeholder={provider==="ollama"?"ollama (локальный сервер не проверяет ключ)":provider==="lm-studio"?"lm-studio (если ключ не включён)":"Токен или API-ключ"} onChange={e=>setCredential(e.target.value)}/></label>
    </div>
    <div className="account-actions"><button className="button primary" disabled={create.isPending||!valid} onClick={()=>create.mutate()}>{create.isPending?"Добавляем…":"Добавить аккаунт"}</button></div>
  </section>:null}
 </section>
}

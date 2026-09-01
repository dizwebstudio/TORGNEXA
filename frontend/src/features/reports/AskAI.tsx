import {useState} from "react";
import {useMutation,useQuery} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {decodeItems as decode} from "../../api/decoders";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock} from "../../components/ApiState";
import {Icon} from "../../components/Icon";

interface Column{key:string;label:string}
interface ReportData{generated_at:string;columns:Column[];rows:string[][]}
interface Account{id:string;label:string;provider:string;model:string;enabled:boolean}
const providerLabels:Readonly<Record<string,string>>={"openai-compatible":"OpenAI-совместимый",gigachat:"GigaChat (Sber)",yandexgpt:"YandexGPT",kimi:"Kimi (Moonshot AI)",qwen:"Qwen (Alibaba Cloud)",deepseek:"DeepSeek",claude:"Claude (Anthropic)",gemini:"Google Gemini",grok:"Grok (xAI)",ollama:"Ollama", "lm-studio":"LM Studio", "open-webui":"Open WebUI"};
const maxDigestLength=6000,maxDigestRows=150;
const systemPrompt="Ты аналитик e-commerce платформы TORGNEXA. Отвечай кратко и по-русски, опираясь только на предоставленные данные отчёта. Если данных недостаточно для вывода — так и скажи.";
function digest(report:ReportData):string{
 const header=report.columns.map(c=>c.label).join(" | ");
 const rows=report.rows.slice(0,maxDigestRows).map(row=>row.join(" | ")).join("\n");
 const body=`${header}\n${rows}`;
 return body.length>maxDigestLength?`${body.slice(0,maxDigestLength)}\n…(усечено)`:body;
}

export function AskAIPanel({reportTitle,report}:{reportTitle:string;report:ReportData}){
 const api=useApi(),auth=useAuth();
 const canAnalyze=auth.session?.capabilities.includes("ai.analyze")??false;
 const canListAccounts=auth.session?.capabilities.includes("settings.ai_providers.read")??false;
 const [accountId,setAccountId]=useState(""),[question,setQuestion]=useState(""),[answer,setAnswer]=useState<{text:string;provider:string;model:string}>();
 const accounts=useQuery({queryKey:["settings","ai-providers"],queryFn:async()=>decode((await api.listAIProviderAccounts()).body),enabled:canAnalyze&&canListAccounts,staleTime:10_000});
 const enabled=(accounts.data??[]).filter(account=>account.enabled);
 const ask=useMutation({mutationFn:async()=>{const body=(await api.analyzeWithAIProvider({idempotencyKey:crypto.randomUUID(),body:{account_id:accountId,system_prompt:systemPrompt,data_classes:["aggregate"],prompt:`Отчёт «${reportTitle}», сформирован ${new Date(report.generated_at).toLocaleString("ru-RU")}.\n\nДанные (первые ${Math.min(report.rows.length,maxDigestRows)} из ${report.rows.length} строк):\n${digest(report)}\n\nВопрос: ${question.trim()||"Дай краткую сводку и главные наблюдения по этим данным."}`}})).body as {text?:unknown;provider?:unknown;model?:unknown};if(!body||typeof body.text!=="string"||typeof body.provider!=="string"||typeof body.model!=="string")throw new Error("invalid analyze response");return {text:body.text,provider:body.provider,model:body.model}},onSuccess:setAnswer});
 if(!canAnalyze)return null;
 return <section className="drawer-section ask-ai-panel">
  <h3><Icon name="activity"/> Спросить ИИ об этом отчёте</h3>
  {!canListAccounts?<p className="settings-note">Нет доступа к списку провайдеров ИИ.</p>:accounts.isError?<ErrorBlock retry={()=>void accounts.refetch()}>Не удалось загрузить список провайдеров ИИ.</ErrorBlock>:enabled.length===0?<p className="settings-note">Нет включённых провайдеров ИИ. Добавьте аккаунт в разделе «Настройки → Провайдеры ИИ».</p>:<>
   <div className="settings-grid">
    <label className="field"><span>Провайдер ИИ</span><select value={accountId} onChange={e=>setAccountId(e.target.value)}><option value="">Выберите аккаунт…</option>{enabled.map(account=><option value={account.id} key={account.id}>{account.label} · {providerLabels[account.provider]??account.provider}</option>)}</select></label>
    <label className="field"><span>Вопрос (необязательно)</span><input value={question} maxLength={2000} placeholder="Что выросло сильнее всего за период?" onChange={e=>setQuestion(e.target.value)}/></label>
   </div>
   <div className="account-actions"><button className="button primary" disabled={!accountId||ask.isPending} onClick={()=>ask.mutate()}>{ask.isPending?"Спрашиваем…":"Спросить"}</button></div>
   {ask.isError?<ErrorBlock>Не удалось получить ответ от провайдера ИИ. Проверьте ключ и доступность сервиса.</ErrorBlock>:null}
   {answer?<div className="panel ask-ai-answer"><p className="settings-note">Ответ провайдера {providerLabels[answer.provider]??answer.provider} · модель {answer.model}</p><p>{answer.text}</p></div>:null}
  </>}
 </section>
}

import {useEffect, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock, LoadingBlock} from "../../components/ApiState";
import {StatusBadge} from "../../components/StatusBadge";
import {labelFor, workspaceStatusLabels} from "../../components/labels";

interface WorkspaceProfile {organization_name: string; workspace_name: string; organization_status: string; workspace_status: string; organization_version: number; workspace_version: number; updated_at: string}
interface CloudSubscription{id:string;plan_id:string;plan_version:number;state:string;current_period_start:string;current_period_end:string}
interface CloudSubscriptionState{mode:string;subscription:CloudSubscription|null}
function decodeSubscription(value:unknown):CloudSubscriptionState{const root=value as {mode?:unknown;subscription?:unknown};if(typeof root?.mode!=="string")throw new Error("invalid cloud subscription response");return {mode:root.mode,subscription:(root.subscription??null) as CloudSubscription|null}}
const subscriptionStateLabels:Record<string,string>={trial:"Пробный период",active:"Активна",past_due:"Просрочена оплата",grace:"Льготный период",suspended:"Приостановлена",cancelled:"Отменена"};

function CloudSubscriptionCard(){
 const api=useApi(),auth=useAuth();
 const canRead=auth.session?.capabilities.includes("cloud.subscription.read")??false;
 const query=useQuery({queryKey:["settings","cloud-subscription"],queryFn:async()=>decodeSubscription((await api.getCloudSubscription()).body),enabled:canRead,staleTime:30_000});
 if(!canRead)return null;
 if(query.isPending)return <LoadingBlock/>;
 if(query.isError)return <ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить состояние подписки.</ErrorBlock>;
 return <section className="panel settings-card">
  <div className="settings-card-heading"><div><p className="eyebrow">Тариф</p><h2>Подписка</h2></div><StatusBadge value={query.data.mode==="community"?"Community":"Cloud"}/></div>
  {query.data.subscription?<dl className="settings-facts">
   <div><dt>План</dt><dd>{query.data.subscription.plan_id} · версия {query.data.subscription.plan_version}</dd></div>
   <div><dt>Статус</dt><dd><StatusBadge value={subscriptionStateLabels[query.data.subscription.state]??query.data.subscription.state}/></dd></div>
   <div><dt>Текущий период</dt><dd>{new Date(query.data.subscription.current_period_start).toLocaleDateString("ru-RU")} – {new Date(query.data.subscription.current_period_end).toLocaleDateString("ru-RU")}</dd></div>
  </dl>:<p className="settings-copy">Community-режим работает без биллинговой зависимости — платной подписки не требуется.</p>}
 </section>
}

function decodeProfile(value: unknown): WorkspaceProfile {
  if (!value || typeof value !== "object") throw new Error("invalid workspace settings");
  const row = value as Record<string, unknown>;
  for (const key of ["organization_name", "workspace_name", "organization_status", "workspace_status", "updated_at"]) if (typeof row[key] !== "string") throw new Error("invalid workspace settings");
  if (!Number.isSafeInteger(row.organization_version) || !Number.isSafeInteger(row.workspace_version)) throw new Error("invalid workspace settings");
  return row as unknown as WorkspaceProfile;
}

export function WorkspaceSettings() {
  const api = useApi();
  const cache = useQueryClient();
  const query = useQuery({queryKey: ["settings", "workspace"], queryFn: async () => decodeProfile((await api.getWorkspaceSettings()).body), staleTime: 10_000});
  const [organizationName, setOrganizationName] = useState("");
  const [workspaceName, setWorkspaceName] = useState("");
  useEffect(() => { if (query.data) { setOrganizationName(query.data.organization_name); setWorkspaceName(query.data.workspace_name); } }, [query.data]);
  const mutation = useMutation({mutationFn: async () => {
    if (!query.data) throw new Error("Профиль не загружен");
    return api.updateWorkspaceSettings({idempotencyKey: `workspace:${query.data.organization_version}:${query.data.workspace_version}`, body: {organization_name: organizationName.trim(), workspace_name: workspaceName.trim(), organization_version: query.data.organization_version, workspace_version: query.data.workspace_version}});
  }, onSuccess: async () => { await cache.invalidateQueries({queryKey: ["settings", "workspace"]}); }});
  if (query.isPending) return <LoadingBlock />;
  if (query.isError) return <ErrorBlock retry={()=>void query.refetch()}>Не удалось загрузить настройки рабочего пространства.</ErrorBlock>;
  return <>
  <section className="panel settings-card">
    <div className="settings-card-heading"><div><p className="eyebrow">Организация</p><h2>Рабочее пространство</h2></div><StatusBadge value={labelFor(query.data.workspace_status,workspaceStatusLabels)}/></div>
    <div className="settings-form">
      <label className="field"><span>Название организации</span><input value={organizationName} maxLength={200} autoComplete="organization" onChange={(event: {target: {value: string}}) => setOrganizationName(event.target.value)} /></label>
      <label className="field"><span>Название рабочего пространства</span><input value={workspaceName} maxLength={200} autoComplete="off" onChange={(event: {target: {value: string}}) => setWorkspaceName(event.target.value)} /></label>
    </div>
    {mutation.isError ? <ErrorBlock>Настройки изменились параллельно или не прошли проверку. Обновите данные и повторите.</ErrorBlock> : null}
    <button className="button primary" disabled={mutation.isPending || !organizationName.trim() || !workspaceName.trim()} onClick={() => mutation.mutate()}>Сохранить настройки</button>
  </section>
  <CloudSubscriptionCard />
  </>;
}

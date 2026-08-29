import {useEffect, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {labelFor, bootstrapJobStatusLabels} from "../../components/labels";

interface Account {id: string; version: number; status: string; health_status: string}
interface Preview {id: string; account_id: string; account_version: number; policy_count: number; read_count: number; write_count: number; created_at: string; expires_at: string; consumed_at?: string}
interface Schedule {account_id: string; mode: "incremental"|"scheduled_full"; interval_minutes: number; enabled: boolean; next_run_at?: string; last_enqueued_at?: string; last_job_id?: string; version: number; updated_at: string}
interface Job {id: string; account_id: string; kind: "initial_import"|"scheduled_sync"; mode: "incremental"|"scheduled_full"; status: "pending"|"running"|"retry_wait"|"completed"|"failed"; checkpoint_policy_id?: string; started_runs: number; attempt_count: number; last_error_code?: string; created_at: string; updated_at: string}
interface BootstrapState {previews: Preview[]; schedules: Schedule[]; jobs: Job[]}

function decodeState(value: unknown): BootstrapState {
  if (!value || typeof value !== "object") throw new Error("invalid bootstrap state");
  const row=value as Record<string,unknown>;
  if(!Array.isArray(row.previews)||!Array.isArray(row.schedules)||!Array.isArray(row.jobs)) throw new Error("invalid bootstrap state");
  const previews=row.previews.map((item)=>{if(!item||typeof item!=="object")throw new Error("invalid preview");const v=item as Record<string,unknown>;if(typeof v.id!=="string"||typeof v.account_id!=="string"||typeof v.account_version!=="number"||typeof v.policy_count!=="number"||typeof v.read_count!=="number"||typeof v.write_count!=="number"||typeof v.created_at!=="string"||typeof v.expires_at!=="string")throw new Error("invalid preview");return v as unknown as Preview;});
  const schedules=row.schedules.map((item)=>{if(!item||typeof item!=="object")throw new Error("invalid schedule");const v=item as Record<string,unknown>;if(typeof v.account_id!=="string"||(v.mode!=="incremental"&&v.mode!=="scheduled_full")||typeof v.interval_minutes!=="number"||typeof v.enabled!=="boolean"||typeof v.version!=="number"||typeof v.updated_at!=="string")throw new Error("invalid schedule");return v as unknown as Schedule;});
  const jobs=row.jobs.map((item)=>{if(!item||typeof item!=="object")throw new Error("invalid job");const v=item as Record<string,unknown>;if(typeof v.id!=="string"||typeof v.account_id!=="string"||(v.kind!=="initial_import"&&v.kind!=="scheduled_sync")||(v.mode!=="incremental"&&v.mode!=="scheduled_full")||!(["pending","running","retry_wait","completed","failed"] as unknown[]).includes(v.status)||typeof v.started_runs!=="number"||typeof v.attempt_count!=="number"||typeof v.created_at!=="string"||typeof v.updated_at!=="string")throw new Error("invalid job");return v as unknown as Job;});
  return {previews,schedules,jobs};
}

function formatTime(value?:string):string {return value?new Intl.DateTimeFormat("ru-RU",{dateStyle:"short",timeStyle:"short"}).format(new Date(value)):"—";}

export function ConnectorBootstrapControls({account}:{account:Account}) {
  const api=useApi();
  const cache=useQueryClient();
  const state=useQuery({queryKey:["connector-bootstrap-state"],queryFn:async()=>decodeState((await api.getConnectorBootstrapState()).body),staleTime:5_000,refetchInterval:15_000});
  const schedule=(state.data?.schedules??[]).find((item)=>item.account_id===account.id);
  const preview=[...(state.data?.previews??[])].find((item)=>item.account_id===account.id&&item.account_version===account.version&&Date.parse(item.expires_at)>Date.now());
  const job=(state.data?.jobs??[]).find((item)=>item.account_id===account.id);
  const [mode,setMode]=useState<"incremental"|"scheduled_full">("incremental");
  const [interval,setInterval]=useState(60);
  const [enabled,setEnabled]=useState(true);
  useEffect(()=>{if(schedule){setMode(schedule.mode);setInterval(schedule.interval_minutes);setEnabled(schedule.enabled);}},[schedule?.version]);
  const refresh=()=>cache.invalidateQueries({queryKey:["connector-bootstrap-state"]});
  const previewMutation=useMutation({mutationFn:()=>api.previewConnectorBootstrap({idempotencyKey:`bootstrap-preview:${account.id}:${account.version}:${Date.now()}`,body:{account_id:account.id,expected_version:account.version}}),onSuccess:refresh});
  const startMutation=useMutation({mutationFn:()=>{if(!preview)throw new Error("preview required");return api.startConnectorBootstrap({idempotencyKey:`bootstrap:${account.id}:${account.version}:${Date.now()}`,body:{preview_id:preview.id}});},onSuccess:refresh});
  const scheduleMutation=useMutation({mutationFn:()=>api.putConnectorSyncSchedule({idempotencyKey:`schedule:${account.id}:${schedule?.version??0}:${Date.now()}`,body:{account_id:account.id,account_version:account.version,mode,interval_minutes:interval,enabled,expected_version:schedule?.version??0}}),onSuccess:refresh});
  const activeHealthy=account.status==="active"&&account.health_status==="healthy";
  return <fieldset className="bootstrap-settings"><legend>Импорт и расписание</legend>
    <small>Предпросмотр ничего не меняет во внешней системе и действует 30 минут. Импорт и расписание выполняются сервером, даже если браузер закрыт.</small>
    <div className="bootstrap-summary"><span>Предпросмотр</span><strong>{preview?`${preview.policy_count} политик · чтение ${preview.read_count} · запись ${preview.write_count}`:"не выполнен"}</strong><span>Последняя задача</span><strong>{job?`${labelFor(job.status,bootstrapJobStatusLabels)} · запусков ${job.started_runs}`:"—"}</strong></div>
    <div className="integration-actions"><button className="button ghost" disabled={!activeHealthy||previewMutation.isPending} onClick={()=>previewMutation.mutate()}>{previewMutation.isPending?"Проверяем…":"Пробный запуск"}</button><button className="button primary" disabled={!preview||Boolean(preview.consumed_at)||startMutation.isPending} onClick={()=>startMutation.mutate()}>{startMutation.isPending?"Ставим…":"Первоначальный импорт"}</button></div>
    <div className="bootstrap-schedule-grid"><label className="field"><span>Режим</span><select value={mode} onChange={(event)=>setMode(event.target.value as "incremental"|"scheduled_full")}><option value="incremental">Инкрементальный</option><option value="scheduled_full">Полная сверка</option></select></label><label className="field"><span>Период, минут</span><input type="number" min={15} max={10080} value={interval} onChange={(event)=>setInterval(Number(event.target.value))}/></label><label className="capability-option"><input type="checkbox" checked={enabled} onChange={(event)=>setEnabled(event.target.checked)}/><span><strong>Расписание включено</strong><small>Следующий запуск: {formatTime(schedule?.next_run_at)}</small></span></label></div>
    <button className="button ghost" disabled={scheduleMutation.isPending||interval<15||interval>10080||(enabled&&!preview)} onClick={()=>scheduleMutation.mutate()}>{scheduleMutation.isPending?"Сохраняем…":"Сохранить расписание"}</button>
    {job?.last_error_code?<small className="error-text">Последняя ошибка: {job.last_error_code}</small>:null}
    {previewMutation.isError||startMutation.isError||scheduleMutation.isError?<small className="error-text" role="alert">Операция не выполнена. Обновите предпросмотр и проверьте версию кабинета.</small>:null}
    {state.isError?<div className="bootstrap-error"><small className="error-text">Не удалось загрузить состояние импорта.</small><button type="button" className="button ghost" onClick={()=>void state.refetch()}>Повторить</button></div>:null}
  </fieldset>;
}

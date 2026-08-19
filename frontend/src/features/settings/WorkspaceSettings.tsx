import {useEffect, useState} from "react";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {ErrorBlock, LoadingBlock} from "../../components/ApiState";

interface WorkspaceProfile {organization_name: string; workspace_name: string; organization_status: string; workspace_status: string; organization_version: number; workspace_version: number; updated_at: string}

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
  if (query.isError) return <ErrorBlock>Не удалось загрузить настройки workspace.</ErrorBlock>;
  return <section className="panel settings-card">
    <div className="settings-card-heading"><div><p className="eyebrow">Организация</p><h2>Рабочее пространство</h2></div><span className="status status-active">{query.data.workspace_status}</span></div>
    <div className="settings-form">
      <label className="field"><span>Название организации</span><input value={organizationName} maxLength={200} autoComplete="organization" onChange={(event: {target: {value: string}}) => setOrganizationName(event.target.value)} /></label>
      <label className="field"><span>Название workspace</span><input value={workspaceName} maxLength={200} autoComplete="off" onChange={(event: {target: {value: string}}) => setWorkspaceName(event.target.value)} /></label>
    </div>
    {mutation.isError ? <ErrorBlock>Настройки изменились параллельно или не прошли проверку. Обновите данные и повторите.</ErrorBlock> : null}
    <button className="button primary" disabled={mutation.isPending || !organizationName.trim() || !workspaceName.trim()} onClick={() => mutation.mutate()}>Сохранить настройки</button>
  </section>;
}

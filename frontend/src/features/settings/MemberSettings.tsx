import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../../api/ApiProvider";
import {useAuth} from "../../auth/AuthProvider";
import {ErrorBlock,LoadingBlock} from "../../components/ApiState";
import {EmptyState} from "../../components/EmptyState";
import {StatusBadge} from "../../components/StatusBadge";

interface Member{id:string;email:string;display_name:string;oidc_subject?:string;role:"admin"|"manager"|"operator"|"viewer";status:"invited"|"active"|"disabled";version:number;invited_at:string;updated_at:string}
const roles={admin:"Администратор",manager:"Менеджер",operator:"Оператор",viewer:"Наблюдатель"};
const statuses={invited:"Приглашён",active:"Активен",disabled:"Заблокирован"};
function decode(value:unknown):Member[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw Error("invalid members");return root.items as Member[]}

export function MemberSettings(){
 const api=useApi(),auth=useAuth(),cache=useQueryClient(),canWrite=auth.session?.capabilities.includes("settings.members.write")??false;
 const [email,setEmail]=useState(""),[name,setName]=useState(""),[role,setRole]=useState<keyof typeof roles>("viewer");
 const query=useQuery({queryKey:["settings","members"],queryFn:async()=>decode((await api.listWorkspaceMembers({limit:100})).body),enabled:auth.session?.capabilities.includes("settings.members.read")??false});
 const invite=useMutation({mutationFn:()=>api.inviteWorkspaceMember({idempotencyKey:crypto.randomUUID(),body:{email,display_name:name,role}}),onSuccess:async()=>{setEmail("");setName("");await cache.invalidateQueries({queryKey:["settings","members"]})}});
 const update=useMutation({mutationFn:({member,changes}:{member:Member;changes:Partial<Pick<Member,"role"|"status">>})=>api.updateWorkspaceMember({memberId:member.id,idempotencyKey:crypto.randomUUID(),body:{role:changes.role??member.role,status:changes.status??member.status,expected_version:member.version}}),onSuccess:async()=>cache.invalidateQueries({queryKey:["settings","members"]})});
 if(!(auth.session?.capabilities.includes("settings.members.read")??false))return null;
 return <section className="panel settings-card member-settings"><div className="settings-card-heading"><div><p className="eyebrow">Команда</p><h2>Пользователи и роли</h2><p className="settings-copy">Локальные роли workspace выдаются явно и не наследуются автоматически из внешних групп.</p></div></div>
 {canWrite?<form className="member-invite" onSubmit={event=>{event.preventDefault();invite.mutate()}}><label className="field"><span>Email</span><input required type="email" maxLength={254} value={email} onChange={event=>setEmail(event.target.value)} placeholder="user@example.com"/></label><label className="field"><span>Имя</span><input maxLength={160} value={name} onChange={event=>setName(event.target.value)} placeholder="Имя пользователя"/></label><label className="field"><span>Роль</span><select value={role} onChange={event=>setRole(event.target.value as keyof typeof roles)}>{Object.entries(roles).map(([key,label])=><option key={key} value={key}>{label}</option>)}</select></label><button className="button primary" disabled={invite.isPending}>{invite.isPending?"Приглашаем…":"Пригласить"}</button></form>:null}
 {invite.isError?<ErrorBlock>Не удалось пригласить: email уже используется или данные некорректны.</ErrorBlock>:null}{update.isError?<ErrorBlock>Не удалось изменить участника. Нельзя заблокировать или понизить последнего активного администратора.</ErrorBlock>:null}
 {query.isPending?<LoadingBlock/>:query.isError?<ErrorBlock>Не удалось загрузить пользователей workspace.</ErrorBlock>:query.data.length===0?<EmptyState title="Пользователей пока нет" text="Пригласите первого участника и назначьте минимально необходимую роль."/>:<div className="table-wrap"><table><thead><tr><th>Пользователь</th><th>Роль</th><th>Статус</th><th>Приглашён</th><th>Действия</th></tr></thead><tbody>{query.data.map(member=><tr key={member.id}><td><strong>{member.display_name||member.email}</strong><br/><span>{member.email}</span>{member.oidc_subject?<><br/><span className="mono">{member.oidc_subject}</span></>:null}</td><td>{canWrite?<select disabled={update.isPending} value={member.role} onChange={event=>update.mutate({member,changes:{role:event.target.value as Member["role"]}})}>{Object.entries(roles).map(([key,label])=><option key={key} value={key}>{label}</option>)}</select>:roles[member.role]}</td><td><StatusBadge value={statuses[member.status]}/></td><td>{new Date(member.invited_at).toLocaleString("ru-RU")}</td><td>{canWrite?<button className={member.status==="disabled"?"button primary":"button danger"} disabled={update.isPending} onClick={()=>update.mutate({member,changes:{status:member.status==="disabled"?"active":"disabled"}})}>{member.status==="disabled"?"Разблокировать":"Заблокировать"}</button>:"—"}</td></tr>)}</tbody></table></div>}</section>
}

import {useState} from "react";
import {useMutation,useQuery,useQueryClient} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {useAuth} from "../auth/AuthProvider";
import {DataTable} from "../components/DataTable";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {EmptyState} from "../components/EmptyState";
import {StatusBadge} from "../components/StatusBadge";
import {useToast} from "../components/Toast";
import {Page} from "./Page";
import {idempotencyKey as idempotency} from "../lib/ids";

interface Counterparty{id:string;code:string;party_type:string;party_id:string;role:string;status:string;version:number;created_at:string;updated_at:string}
interface LegalParty{party_type:string;party_id:string;code:string;display_name:string;inn?:string;registration_id?:string;status:string}
type Response={body:any};
type Client={
 listCounterparties(input?:object):Promise<Response>;
 searchLegalParties(input:object):Promise<Response>;
 createLegalParty(input:{idempotencyKey:string;body:object}):Promise<Response>;
 createCounterparty(input:{idempotencyKey:string;body:object}):Promise<Response>;
};

function decodeCounterparties(value:unknown):Counterparty[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid counterparty response");return root.items as Counterparty[]}
function decodeLegalParties(value:unknown):LegalParty[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid legal party response");return root.items as LegalParty[]}
const partyTypeLabels:Record<string,string>={legal_entity:"Юридическое лицо",individual_entrepreneur:"Индивидуальный предприниматель",branch:"Филиал"};
const roleLabels:Record<string,string>={customer:"Покупатель",supplier:"Поставщик",partner:"Партнёр",other:"Другое"};
const statusLabels:Record<string,string>={draft:"Черновик",active:"Активен",archived:"В архиве"};

export function CounterpartiesPage(){
 const api=useApi() as unknown as Client;
 const auth=useAuth();
 const canWrite=auth.session?.capabilities.includes("counterparties.write")??false;
 const [tab,setTab]=useState<"roles"|"search">("roles");
 const [rolePreset,setRolePreset]=useState<LegalParty|null>(null);
 const assign=(party:LegalParty)=>{setRolePreset(party);setTab("roles")};
 return <Page eyebrow="Контрагенты" title="Контрагенты" description="Канонический справочник юридических лиц и назначенные роли: покупатель, поставщик или партнёр.">
  <div className="catalog-tabs" role="tablist">
   <button type="button" role="tab" aria-selected={tab==="roles"} className={tab==="roles"?"active":""} onClick={()=>setTab("roles")}>Роли</button>
   <button type="button" role="tab" aria-selected={tab==="search"} className={tab==="search"?"active":""} onClick={()=>setTab("search")}>Справочник лиц</button>
  </div>
  {tab==="roles"?<Roles key={rolePreset?.party_id??"empty"} api={api} canWrite={canWrite} preset={rolePreset}/>:<Search api={api} canWrite={canWrite} onAssign={assign}/>}
 </Page>
}

function Roles({api,canWrite,preset}:{api:Client;canWrite:boolean;preset:LegalParty|null}){
 const qc=useQueryClient(),toast=useToast();
 const q=useQuery({queryKey:["counterparties"],queryFn:async()=>decodeCounterparties((await api.listCounterparties({limit:100})).body),staleTime:15_000});
 const [code,setCode]=useState(""),[partyType,setPartyType]=useState(preset?.party_type??"legal_entity"),[partyID,setPartyID]=useState(preset?.party_id??""),[role,setRole]=useState("customer");
 const create=useMutation({mutationFn:()=>api.createCounterparty({idempotencyKey:idempotency(),body:{code:code.trim(),party_type:partyType,party_id:partyID.trim(),role}}),onSuccess:async()=>{setCode("");setPartyID("");toast.push({kind:"success",title:"Роль контрагента добавлена",body:"Связь сохранена в каноническом справочнике."});await qc.invalidateQueries({queryKey:["counterparties"]})},onError:()=>toast.push({kind:"error",title:"Не удалось добавить роль",body:"Сначала создайте юридическое лицо во вкладке «Справочник лиц» и проверьте его ID."})});
 const valid=!!code.trim()&&!!partyID.trim()&&!!partyType&&!!role;
 const columns=[
  {key:"code",label:"Код",value:(v:Counterparty)=>v.code,render:(v:Counterparty)=><strong className="mono">{v.code}</strong>},
  {key:"role",label:"Роль",value:(v:Counterparty)=>roleLabels[v.role]??v.role,render:(v:Counterparty)=><StatusBadge value={roleLabels[v.role]??v.role}/>},
  {key:"party_type",label:"Тип лица",value:(v:Counterparty)=>partyTypeLabels[v.party_type]??v.party_type},
  {key:"party_id",label:"ID лица",value:(v:Counterparty)=>v.party_id,render:(v:Counterparty)=><span className="mono">{v.party_id}</span>},
  {key:"status",label:"Статус",value:(v:Counterparty)=>statusLabels[v.status]??v.status,render:(v:Counterparty)=><StatusBadge value={statusLabels[v.status]??v.status}/>},
 ];
 return <div className="catalog-stack">
  {canWrite?<section className="panel inline-create"><div><p className="eyebrow">Новая связь</p><h2>Назначить роль контрагента</h2><p>Роль не копирует реквизиты: она ссылается на одну запись из канонического справочника лиц.</p></div><form className="settings-grid" onSubmit={event=>{event.preventDefault();if(!create.isPending)create.mutate()}}><label className="field"><span>Код связи</span><input required maxLength={128} value={code} onChange={event=>setCode(event.target.value)} placeholder="supplier-acme"/></label><label className="field"><span>Тип лица</span><select value={partyType} onChange={event=>setPartyType(event.target.value)}><option value="legal_entity">Юридическое лицо</option><option value="individual_entrepreneur">ИП</option><option value="branch">Филиал</option></select></label><label className="field"><span>ID записи справочника</span><input required value={partyID} onChange={event=>setPartyID(event.target.value)} placeholder="Выберите лицо во вкладке «Справочник лиц»"/></label><label className="field"><span>Роль</span><select value={role} onChange={event=>setRole(event.target.value)}><option value="customer">Покупатель</option><option value="supplier">Поставщик</option><option value="partner">Партнёр</option><option value="other">Другое</option></select></label><div className="account-actions"><button className="button primary" disabled={!valid||create.isPending}>{create.isPending?"Сохраняем…":"Добавить роль"}</button></div></form></section>:null}
  {q.isPending?<LoadingBlock/>:q.isError?<ErrorBlock retry={()=>void q.refetch()}>Не удалось загрузить роли контрагентов.</ErrorBlock>:q.data.length===0?<EmptyState title="Ролей пока нет" text={canWrite?"Создайте юридическое лицо во вкладке «Справочник лиц», затем назначьте ему роль.":"В этом рабочем пространстве ещё нет назначенных ролей."}/>:<section className="panel"><div className="drawer-section-heading"><div><h2>Назначенные роли</h2><p>Одна каноническая запись может использоваться в разных бизнес-процессах без дублирования реквизитов.</p></div></div><DataTable rows={q.data} columns={columns} rowKey={value=>value.id} searchPlaceholder="Код, роль или статус…"/></section>}
 </div>
}

function Search({api,canWrite,onAssign}:{api:Client;canWrite:boolean;onAssign:(party:LegalParty)=>void}){
 const qc=useQueryClient(),toast=useToast();
 const [partyType,setPartyType]=useState("legal_entity"),[code,setCode]=useState(""),[legalName,setLegalName]=useState(""),[shortName,setShortName]=useState(""),[fullName,setFullName]=useState(""),[name,setName]=useState(""),[countryCode,setCountryCode]=useState("RU"),[inn,setINN]=useState(""),[kpp,setKPP]=useState(""),[ogrn,setOGRN]=useState(""),[ogrnip,setOGRNIP]=useState(""),[parentID,setParentID]=useState("");
 const [query,setQuery]=useState(""),[searchINN,setSearchINN]=useState(""),[searchType,setSearchType]=useState(""),[submitted,setSubmitted]=useState<{q:string;inn:string;partyType:string}|null>(null);
 const search=useQuery({queryKey:["legal-parties","search",submitted],enabled:!!submitted,queryFn:async()=>decodeLegalParties((await api.searchLegalParties({q:submitted?.q||undefined,inn:submitted?.inn||undefined,partyType:submitted?.partyType||undefined})).body)});
 const resetMaster=()=>{setCode("");setLegalName("");setShortName("");setFullName("");setName("");setINN("");setKPP("");setOGRN("");setOGRNIP("");setParentID("")};
 const create=useMutation({mutationFn:()=>{const body=partyType==="legal_entity"?{party_type:partyType,code:code.trim(),legal_name:legalName.trim(),short_name:shortName.trim()||undefined,country_code:countryCode,inn:inn.trim()||undefined,kpp:kpp.trim()||undefined,ogrn:ogrn.trim()||undefined}:partyType==="individual_entrepreneur"?{party_type:partyType,code:code.trim(),full_name:fullName.trim(),country_code:countryCode,inn:inn.trim()||undefined,ogrnip:ogrnip.trim()||undefined}:{party_type:partyType,code:code.trim(),name:name.trim(),country_code:countryCode,kpp:kpp.trim()||undefined,legal_entity_id:parentID.trim()};return api.createLegalParty({idempotencyKey:idempotency(),body})},onSuccess:async result=>{const party=result.body as LegalParty;resetMaster();toast.push({kind:"success",title:"Запись справочника добавлена",body:`ID: ${party.party_id}`});setQuery(party.code);setSubmitted({q:party.code,inn:"",partyType:party.party_type});await qc.invalidateQueries({queryKey:["legal-parties","search"]})},onError:()=>toast.push({kind:"error",title:"Не удалось добавить запись",body:"Для российского лица нужны корректные ИНН и регистрационные реквизиты; проверьте код и права."})});
 const validName=partyType==="legal_entity"?!!legalName.trim():partyType==="individual_entrepreneur"?!!fullName.trim():!!name.trim()&&!!parentID.trim();
 const validRussian=countryCode!=="RU"||(partyType==="legal_entity"?!!inn.trim()&&!!kpp.trim()&&!!ogrn.trim():partyType==="individual_entrepreneur"?!!inn.trim()&&!!ogrnip.trim():!!kpp.trim());
 const valid=canWrite&&!!code.trim()&&!!countryCode&&validName&&validRussian;
 return <div className="catalog-stack">
  {canWrite?<section className="panel inline-create"><div><p className="eyebrow">Канонический мастер</p><h2>Добавить запись в справочник</h2><p>Сначала создаётся юридическое лицо, ИП или филиал. После этого его можно назначить покупателем, поставщиком или партнёром.</p></div><form className="settings-grid" onSubmit={event=>{event.preventDefault();if(!create.isPending)create.mutate()}}><label className="field"><span>Тип записи</span><select value={partyType} onChange={event=>setPartyType(event.target.value)}><option value="legal_entity">Юридическое лицо</option><option value="individual_entrepreneur">ИП</option><option value="branch">Филиал</option></select></label><label className="field"><span>Код</span><input required maxLength={128} value={code} onChange={event=>setCode(event.target.value)} placeholder="acme-ru"/></label>{partyType==="legal_entity"?<><label className="field"><span>Полное юридическое название</span><input required maxLength={500} value={legalName} onChange={event=>setLegalName(event.target.value)} placeholder="ООО «Ромашка»"/></label><label className="field"><span>Краткое название</span><input maxLength={300} value={shortName} onChange={event=>setShortName(event.target.value)} placeholder="Ромашка"/></label></>:partyType==="individual_entrepreneur"?<label className="field"><span>ФИО предпринимателя</span><input required maxLength={500} value={fullName} onChange={event=>setFullName(event.target.value)} placeholder="Иванов Иван Иванович"/></label>:<><label className="field"><span>Название филиала</span><input required maxLength={500} value={name} onChange={event=>setName(event.target.value)} placeholder="Филиал на Складской"/></label><label className="field"><span>ID головного юрлица</span><input required value={parentID} onChange={event=>setParentID(event.target.value)} placeholder="ID записи юрлица"/></label></>}<label className="field"><span>Страна, код ISO</span><input required maxLength={2} value={countryCode} onChange={event=>setCountryCode(event.target.value.toUpperCase())} placeholder="RU"/></label><label className="field"><span>ИНН</span><input maxLength={12} value={inn} onChange={event=>setINN(event.target.value)} placeholder={countryCode==="RU"?"10 или 12 цифр":"Необязательно"}/></label>{partyType==="legal_entity"||partyType==="branch"?<label className="field"><span>КПП</span><input maxLength={16} value={kpp} onChange={event=>setKPP(event.target.value)} placeholder={countryCode==="RU"?"9 цифр":"Необязательно"}/></label>:null}{partyType==="legal_entity"?<label className="field"><span>ОГРН</span><input maxLength={32} value={ogrn} onChange={event=>setOGRN(event.target.value)} placeholder={countryCode==="RU"?"13 цифр":"Необязательно"}/></label>:partyType==="individual_entrepreneur"?<label className="field"><span>ОГРНИП</span><input maxLength={32} value={ogrnip} onChange={event=>setOGRNIP(event.target.value)} placeholder={countryCode==="RU"?"15 цифр":"Необязательно"}/></label>:null}<div className="account-actions"><button className="button primary" disabled={!valid||create.isPending}>{create.isPending?"Создаём…":"Добавить в справочник"}</button></div></form><p className="settings-note">Для страны RU сервер проверяет контрольные суммы ИНН, КПП, ОГРН и ОГРНИП. Реквизиты не дублируются в ролях.</p></section>:null}
  <section className="panel inline-create"><div><p className="eyebrow">Поиск</p><h2>Найти запись справочника</h2><p>Используйте найденный ID при назначении роли или нажмите «Назначить роль» прямо в результате.</p></div><form onSubmit={event=>{event.preventDefault();setSubmitted({q:query.trim(),inn:searchINN.trim(),partyType:searchType})}}><input value={query} onChange={event=>setQuery(event.target.value)} placeholder="Название или код"/><input value={searchINN} onChange={event=>setSearchINN(event.target.value)} placeholder="ИНН" maxLength={12}/><select value={searchType} onChange={event=>setSearchType(event.target.value)}><option value="">Любой тип</option><option value="legal_entity">Юридическое лицо</option><option value="individual_entrepreneur">ИП</option><option value="branch">Филиал</option></select><button className="button primary">Искать</button></form></section>
  {!submitted?null:search.isPending?<LoadingBlock/>:search.isError?<ErrorBlock retry={()=>void search.refetch()}>Не удалось выполнить поиск по справочнику.</ErrorBlock>:search.data.length===0?<EmptyState title="Записей не найдено" text="Уточните название, код, ИНН или тип лица."/>:<div className="settings-grid">{search.data.map(p=><article className="connector-account" key={p.party_id}><header><div><strong>{p.display_name}</strong><small className="mono">{p.code}</small></div><StatusBadge value={statusLabels[p.status]??p.status}/></header><dl className="detail-list"><div><dt>Тип</dt><dd>{partyTypeLabels[p.party_type]??p.party_type}</dd></div><div><dt>ID</dt><dd className="mono">{p.party_id}</dd></div>{p.inn?<div><dt>ИНН</dt><dd className="mono">{p.inn}</dd></div>:null}{p.registration_id?<div><dt>Регистрационный номер</dt><dd className="mono">{p.registration_id}</dd></div>:null}</dl>{canWrite?<div className="account-actions"><button type="button" className="button ghost" onClick={()=>onAssign(p)}>Назначить роль</button></div>:null}</article>)}</div>}
 </div>
}

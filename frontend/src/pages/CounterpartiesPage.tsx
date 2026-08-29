import {useState} from "react";
import {useQuery} from "@tanstack/react-query";
import {useApi} from "../api/ApiProvider";
import {ErrorBlock,LoadingBlock} from "../components/ApiState";
import {DataTable} from "../components/DataTable";
import {EmptyState} from "../components/EmptyState";
import {StatusBadge} from "../components/StatusBadge";
import {Page} from "./Page";

interface Counterparty{id:string;code:string;party_type:string;party_id:string;role:string;status:string;version:number;created_at:string;updated_at:string}
interface LegalParty{party_type:string;party_id:string;code:string;display_name:string;inn?:string;registration_id?:string;status:string}
type R={body:any};type Client={listCounterparties(x?:object):Promise<R>;searchLegalParties(x:object):Promise<R>};

function decodeCounterparties(value:unknown):Counterparty[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid counterparty response");return root.items as Counterparty[]}
function decodeLegalParties(value:unknown):LegalParty[]{const root=value as {items?:unknown};if(!Array.isArray(root?.items))throw new Error("invalid legal party response");return root.items as LegalParty[]}

const partyTypeLabels:Record<string,string>={legal_entity:"Юрлицо",individual_entrepreneur:"ИП",branch:"Филиал"};
const roleLabels:Record<string,string>={customer:"Покупатель",supplier:"Поставщик",partner:"Партнёр",other:"Другое"};

export function CounterpartiesPage(){
 const api=useApi() as unknown as Client;
 const [tab,setTab]=useState<"roles"|"search">("roles");
 return <Page eyebrow="Контрагенты" title="Контрагенты" description="Роли контрагентов (покупатель/поставщик/партнёр) и канонический справочник юридических лиц.">
  <div className="catalog-tabs" role="tablist">
   <button role="tab" aria-selected={tab==="roles"} className={tab==="roles"?"active":""} onClick={()=>setTab("roles")}>Роли</button>
   <button role="tab" aria-selected={tab==="search"} className={tab==="search"?"active":""} onClick={()=>setTab("search")}>Поиск по реестру</button>
  </div>
  {tab==="roles"?<Roles api={api}/>:<Search api={api}/>}
 </Page>
}

function Roles({api}:{api:Client}){
 const q=useQuery({queryKey:["counterparties"],queryFn:async()=>decodeCounterparties((await api.listCounterparties({limit:100})).body),staleTime:15_000});
 if(q.isPending)return <LoadingBlock/>;
 if(q.isError)return <ErrorBlock>Не удалось загрузить контрагентов.</ErrorBlock>;
 if(q.data.length===0)return <EmptyState title="Контрагенты не назначены" text="Роли появляются автоматически при оформлении заказов и подключении поставщиков."/>;
 const columns=[
  {key:"code",label:"Код",value:(v:Counterparty)=>v.code,render:(v:Counterparty)=><strong className="mono">{v.code}</strong>},
  {key:"role",label:"Роль",value:(v:Counterparty)=>roleLabels[v.role]??v.role,render:(v:Counterparty)=><StatusBadge value={roleLabels[v.role]??v.role}/>},
  {key:"party_type",label:"Тип лица",value:(v:Counterparty)=>partyTypeLabels[v.party_type]??v.party_type},
  {key:"party_id",label:"ID лица",value:(v:Counterparty)=>v.party_id,render:(v:Counterparty)=><span className="mono">{v.party_id}</span>},
  {key:"status",label:"Статус",value:(v:Counterparty)=>v.status,render:(v:Counterparty)=><StatusBadge value={v.status}/>},
 ];
 return <DataTable rows={q.data} columns={columns} rowKey={v=>v.id} searchPlaceholder="Код, роль, статус…"/>;
}

function Search({api}:{api:Client}){
 const [query,setQuery]=useState(""),[inn,setInn]=useState(""),[partyType,setPartyType]=useState("");
 const [submitted,setSubmitted]=useState<{q:string;inn:string;partyType:string}|null>(null);
 const q=useQuery({
  queryKey:["legal-parties","search",submitted],
  enabled:!!submitted,
  queryFn:async()=>decodeLegalParties((await api.searchLegalParties({q:submitted?.q||undefined,inn:submitted?.inn||undefined,partyType:submitted?.partyType||undefined})).body),
 });
 return <div className="catalog-stack">
  <section className="panel inline-create">
   <div><h2>Поиск в реестре юридических лиц</h2><p>Канонический справочник — используется инструментом «Поиск контрагентов» и обработкой заказов.</p></div>
   <form onSubmit={e=>{e.preventDefault();setSubmitted({q:query.trim(),inn:inn.trim(),partyType})}}>
    <input value={query} onChange={e=>setQuery(e.target.value)} placeholder="Название или код"/>
    <input value={inn} onChange={e=>setInn(e.target.value)} placeholder="ИНН" maxLength={12}/>
    <select value={partyType} onChange={e=>setPartyType(e.target.value)}>
     <option value="">Любой тип</option>
     <option value="legal_entity">Юрлицо</option>
     <option value="individual_entrepreneur">ИП</option>
     <option value="branch">Филиал</option>
    </select>
    <button className="button primary">Искать</button>
   </form>
  </section>
  {!submitted?null:q.isPending?<LoadingBlock/>:q.isError?<ErrorBlock>Не удалось выполнить поиск.</ErrorBlock>:q.data.length===0?<EmptyState title="Ничего не найдено" text="Уточните запрос, ИНН или тип лица."/>:<div className="settings-grid">{q.data.map(p=><article className="connector-account" key={p.party_id}><header><div><strong>{p.display_name}</strong><small className="mono">{p.code}</small></div><StatusBadge value={p.status}/></header><dl className="detail-list"><div><dt>Тип</dt><dd>{partyTypeLabels[p.party_type]??p.party_type}</dd></div>{p.inn?<div><dt>ИНН</dt><dd className="mono">{p.inn}</dd></div>:null}{p.registration_id?<div><dt>Рег. номер</dt><dd className="mono">{p.registration_id}</dd></div>:null}</dl></article>)}</div>}
 </div>
}

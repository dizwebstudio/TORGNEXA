import {useEffect,useState} from "react";
import {useQueryClient} from "@tanstack/react-query";
import {useAuth} from "../auth/AuthProvider";
import {shouldInvalidateRealtimeEvent} from "./realtime-events";

export type RealtimeState="connecting"|"live"|"offline";

export function useRealtimeInvalidation():RealtimeState{
 const {session}=useAuth();const cache=useQueryClient();const [state,setState]=useState<RealtimeState>("connecting");
 useEffect(()=>{
  if(!session?.accessToken||!session.capabilities.includes("operations.realtime.read")){setState("offline");return}
  const controller=new AbortController();let retry=0,timer=0,invalidationTimer=0;
  const scheduleInvalidation=()=>{
   // A worker batch can append several audit records in one burst. Coalesce
   // those signals so one SSE burst does not fan out into a request storm
   // across every mounted query.
   if(invalidationTimer)return;
   invalidationTimer=window.setTimeout(()=>{invalidationTimer=0;void cache.invalidateQueries({predicate:(q:any)=>!String(q.queryKey[0]??"").startsWith("static")});},150);
  };
  const connect=async()=>{setState(retry?"offline":"connecting");try{
   const response=await fetch(new URL("/api/v1/realtime",window.location.origin),{headers:{Authorization:`Bearer ${session.accessToken}`,Accept:"text/event-stream"},credentials:"same-origin",redirect:"error",signal:controller.signal});
   if(!response.ok||!response.body)throw new Error("realtime unavailable");setState("live");retry=0;const reader=response.body.getReader(),decoder=new TextDecoder();let buffer="";
   while(!controller.signal.aborted){const {value,done}=await reader.read();if(done)break;buffer+=decoder.decode(value,{stream:true});let split;while((split=buffer.indexOf("\n\n"))>=0){const frame=buffer.slice(0,split);buffer=buffer.slice(split+2);const event=frame.split("\n").find(v=>v.startsWith("event:"))?.slice(6).trim();if(shouldInvalidateRealtimeEvent(event))scheduleInvalidation();}}
   if(!controller.signal.aborted)throw new Error("stream ended");
  }catch{if(controller.signal.aborted)return;setState("offline");retry=Math.min(retry+1,6);timer=window.setTimeout(()=>void connect(),Math.min(30_000,1000*2**retry));}};
  void connect();return()=>{controller.abort();window.clearTimeout(timer);window.clearTimeout(invalidationTimer)};
 },[session?.accessToken,session?.capabilities,cache]);
 return state;
}

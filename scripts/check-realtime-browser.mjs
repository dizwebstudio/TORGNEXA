#!/usr/bin/env node
// A10: real browser/React/Query/SDK, synthetic auth/API/SSE transport. The Go
// HTTP tests separately validate the server frames and network deadlines.
import assert from "node:assert/strict";
import {spawn} from "node:child_process";
import {mkdtemp, readFile, rm} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve} from "node:path";
import {pathToFileURL} from "node:url";

const root=resolve(import.meta.dirname,"..");
const {createServer}=await import(pathToFileURL(join(root,"frontend/node_modules/vite/dist/node/index.js")));
const {default:react}=await import(pathToFileURL(join(root,"frontend/node_modules/@vitejs/plugin-react/dist/index.js")));
const scratch=await mkdtemp(join(tmpdir(),"torgnexa-realtime-"));
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
let server,chrome,cdp;
async function until(label,operation){const end=Date.now()+20_000;while(Date.now()<end){if(await operation())return;await delay(50);}throw new Error(`Timed out: ${label}`);}

class CDP {
 constructor(url){this.id=0;this.pending=new Map();this.socket=new WebSocket(url);this.ready=new Promise((resolve,reject)=>{this.socket.addEventListener("open",resolve,{once:true});this.socket.addEventListener("error",reject,{once:true});});this.socket.addEventListener("message",event=>{const message=JSON.parse(String(event.data));const pending=this.pending.get(message.id);if(!pending)return;this.pending.delete(message.id);clearTimeout(pending.timer);message.error?pending.reject(new Error(message.error.message)):pending.resolve(message.result);});}
 async send(method,params={}){await this.ready;const id=++this.id;return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{this.pending.delete(id);reject(new Error(`CDP timed out: ${method}`));},20_000);this.pending.set(id,{resolve,reject,timer});this.socket.send(JSON.stringify({id,method,params}));});}
 async evaluate(expression){const response=await this.send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true,userGesture:true});if(response.exceptionDetails)throw new Error(response.exceptionDetails.exception?.description??response.exceptionDetails.text);return response.result?.value;}
 async wait(expression,label){await until(label,()=>this.evaluate(expression));}
}

function syntheticHost(){
 const listeners=new Set();
 let session={subject:"synthetic-viewer",displayName:"Synthetic viewer",accessToken:"synthetic-token",cacheScope:"synthetic-scope",capabilities:["orders.read","operations.realtime.read"],roles:["viewer"]};
 let version=1,orderReads=0,connections=0,aborts=0,active;
 const encoder=new TextEncoder();
 const frame=(event,reason)=>`event: ${event}\ndata: ${JSON.stringify({reason,cursor:"synthetic-unchanged-head",at:new Date().toISOString()})}\n\n`;
 const send=(event,reason)=>{if(!active?.closed)active.controller.enqueue(encoder.encode(frame(event,reason)));};
 window.__TORGNEXA_AUTH_ADAPTER__={async getSession(){return session;},async login(){},async logout(){session=null;listeners.forEach(fn=>fn());},subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn);}};
 const original=window.fetch.bind(window);
 window.fetch=async(input,init)=>{
  const request=new Request(input,init),url=new URL(request.url);
  if(url.origin!==location.origin)throw new Error("External network disabled in realtime regression");
  if(!url.pathname.startsWith("/api/"))return original(request);
  if(request.signal.aborted)throw new DOMException("Aborted","AbortError");
  if(url.pathname==="/api/v1/realtime"){
   connections++;const stream={closed:false,controller:null};active=stream;
   const body=new ReadableStream({start(controller){stream.controller=controller;const initial=frame("ready","connected")+frame("invalidate","connected");controller.enqueue(encoder.encode(initial.slice(0,17)));controller.enqueue(encoder.encode(initial.slice(17)));}});
   request.signal.addEventListener("abort",()=>{aborts++;if(!stream.closed){stream.closed=true;stream.controller.error(new DOMException("Aborted","AbortError"));}},{once:true});
   return new Response(body,{headers:{"Content-Type":"text/event-stream"}});
  }
  if(url.pathname==="/api/v1/orders"){
   orderReads++;
   return Response.json({items:[{id:"synthetic-order",order_number:`SSE_VERSION_${version}`,status:"pending",currency:"RUB",grand_minor_units:12500,placed_at:"2026-09-10T00:00:00Z",updated_at:"2026-09-10T00:00:00Z",product_title:"Synthetic product"}]});
  }
  return Response.json({items:[]});
 };
 window.__realtimeTest={stats:()=>({orderReads,connections,aborts}),send,
  gap(){active.closed=true;active.controller.close();version=2;},
  burst(){version=3;for(let i=0;i<8;i++)send("invalidate","audit");},
 };
}

try{
 server=await createServer({root:join(root,"frontend"),configFile:false,cacheDir:join(scratch,"vite"),plugins:[react(),{name:"no-live-api",configureServer(server){server.middlewares.use((req,res,next)=>{if(req.url?.startsWith("/api/")){res.statusCode=500;res.end("Synthetic interception required");}else next();});}}],server:{host:"127.0.0.1",port:0,strictPort:false},logLevel:"error"});
 await server.listen();const port=server.httpServer.address().port;
 const profile=join(scratch,"chrome");
 chrome=spawn(process.env.CHROME_BIN??"google-chrome",["--headless=new","--window-size=1440,1100","--no-sandbox","--disable-gpu","--disable-dev-shm-usage","--disable-background-networking","--no-first-run","--no-default-browser-check","--password-store=basic","--remote-debugging-port=0",`--user-data-dir=${profile}`,"about:blank"],{stdio:"ignore"});
 let startupError;chrome.once("error",error=>{startupError=error;});let debugPort;
 await until("Chrome startup",async()=>{if(startupError)throw startupError;if(chrome.exitCode!==null)throw new Error("Chrome stopped");try{debugPort=(await readFile(join(profile,"DevToolsActivePort"),"utf8")).split("\n")[0];return true;}catch{return false;}});
 const targets=await(await fetch(`http://127.0.0.1:${debugPort}/json/list`)).json();
 cdp=new CDP(targets.find(target=>target.type==="page").webSocketDebuggerUrl);
 await cdp.send("Page.enable");await cdp.send("Runtime.enable");
 await cdp.send("Page.addScriptToEvaluateOnNewDocument",{source:`(${syntheticHost.toString()})()`});
 await cdp.send("Page.navigate",{url:`http://127.0.0.1:${port}/orders`});
 await cdp.wait(`document.body.innerText.includes('SSE_VERSION_1') && document.querySelector('.realtime-pill.live')!==null`,"initial stream/data");
 await delay(400);
 let before=await cdp.evaluate(`__realtimeTest.stats()`);
 await cdp.evaluate(`for(let i=0;i<4;i++){__realtimeTest.send('heartbeat','heartbeat');__realtimeTest.send('ready','connected');}`);
 await delay(400);
 assert.equal((await cdp.evaluate(`__realtimeTest.stats()`)).orderReads,before.orderReads,"liveness frames must not refetch");
 console.log("PASS ready/heartbeat frames do not cause periodic query refresh");

 await cdp.evaluate(`__realtimeTest.gap()`);
 await cdp.wait(`document.querySelector('.realtime-pill.offline')!==null`,"stream disconnect");
 assert.equal(await cdp.evaluate(`document.body.innerText.includes('SSE_VERSION_1')`),true,"old value remains until reconnect refresh");
 await cdp.wait(`document.body.innerText.includes('SSE_VERSION_2')`,"data changed during disconnect is reloaded");
 await delay(250);
 let after=await cdp.evaluate(`__realtimeTest.stats()`);
 assert.equal(after.connections,before.connections+1,"one reconnect");
 assert.equal(after.orderReads,before.orderReads+1,"one connected invalidation refetch even when cursor is unchanged");
 console.log("PASS reconnect reloads missed state via connected invalidation, including unchanged cursor and split frames");

 before=after;await cdp.evaluate(`__realtimeTest.burst()`);
 await cdp.wait(`document.body.innerText.includes('SSE_VERSION_3')`,"audit invalidation burst");await delay(250);
 after=await cdp.evaluate(`__realtimeTest.stats()`);
 assert.equal(after.orderReads,before.orderReads+1,"eight frames coalesce into one refetch");
 console.log("PASS eight audit invalidations coalesce into one authorized API refetch");

 await cdp.evaluate(`document.querySelector('button[aria-label="Выйти"]').click()`);
 await cdp.wait(`document.body.innerText.includes('Войти') && !document.body.innerText.includes('SSE_VERSION_3')`,"logout cleanup");
 await delay(2300);
 const stopped=await cdp.evaluate(`__realtimeTest.stats()`);
 assert.ok(stopped.aborts>after.aborts,"logout cancels the active stream");
 assert.equal(stopped.connections,after.connections,"logout does not reconnect");
 assert.equal(stopped.orderReads,after.orderReads,"logout does not refetch retired queries");
 console.log("PASS logout aborts SSE and leaves no reconnect/refetch timer");
 console.log("A10 realtime browser regression: PASS");
}finally{
 cdp?.socket.close();
 if(chrome&&chrome.exitCode===null){chrome.kill("SIGTERM");await Promise.race([new Promise(resolve=>chrome.once("exit",resolve)),delay(3000)]);if(chrome.exitCode===null){chrome.kill("SIGKILL");await delay(200);}}
 await server?.close();await rm(scratch,{recursive:true,force:true});
}

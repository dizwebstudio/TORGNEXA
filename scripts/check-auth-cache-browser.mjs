#!/usr/bin/env node
// A01 regression: real React/Query/SDK and rendered Orders UI; only the host
// auth adapter and remote API responses are synthetic. No live stack is used.
import assert from "node:assert/strict";
import {spawn} from "node:child_process";
import {mkdtemp, readFile, rm, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve} from "node:path";
import {pathToFileURL} from "node:url";

const root=resolve(import.meta.dirname,"..");
const {createServer}=await import(pathToFileURL(join(root,"frontend/node_modules/vite/dist/node/index.js")));
const {default:react}=await import(pathToFileURL(join(root,"frontend/node_modules/@vitejs/plugin-react/dist/index.js")));
const scratch=await mkdtemp(join(tmpdir(),"torgnexa-auth-cache-"));
let server,chrome,cdp;
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function until(label,operation){const end=Date.now()+20_000;while(Date.now()<end){if(await operation())return;await delay(50);}throw new Error(`Timed out: ${label}`);}

class CDP {
 constructor(url){this.id=0;this.pending=new Map();this.socket=new WebSocket(url);this.ready=new Promise((resolve,reject)=>{this.socket.addEventListener("open",resolve,{once:true});this.socket.addEventListener("error",reject,{once:true});});this.socket.addEventListener("message",event=>{const message=JSON.parse(String(event.data));const pending=this.pending.get(message.id);if(!pending)return;this.pending.delete(message.id);clearTimeout(pending.timer);message.error?pending.reject(new Error(message.error.message)):pending.resolve(message.result);});}
 async send(method,params={}){await this.ready;const id=++this.id;return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{this.pending.delete(id);reject(new Error(`CDP timed out: ${method}`));},20_000);this.pending.set(id,{resolve,reject,timer});this.socket.send(JSON.stringify({id,method,params}));});}
 async evaluate(expression){const response=await this.send("Runtime.evaluate",{expression,awaitPromise:true,returnByValue:true,userGesture:true});if(response.exceptionDetails)throw new Error(response.exceptionDetails.exception?.description??response.exceptionDetails.text);return response.result?.value;}
 async wait(expression,label){await until(label,()=>this.evaluate(expression));}
}

function syntheticHost(){
 const listeners=new Set();
 const pending=[];
 const modes=new Map();
 const calls=[];
 const leaks=[];
 let refreshes=0,logouts=0,aborts=0;
 const make=(id,scope="scope-one",extra={})=>({subject:id,displayName:`Synthetic ${id}`,accessToken:`synthetic-${id}-${scope}`,cacheScope:scope,capabilities:["orders.read"],roles:["viewer"],...extra});
 let session=make("A"),next=make("B");
 const notify=()=>listeners.forEach(fn=>fn());
 const marker=value=>`ORDER_${value.replaceAll("-","_")}`;
 window.__TORGNEXA_AUTH_ADAPTER__={
  async getSession(){refreshes++;return session;},
  async login(){session=next;notify();},
  async logout(){logouts++;session=null;notify();},
  subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn);},
 };
 const original=window.fetch.bind(window);
 window.fetch=async(input,init)=>{
  const request=new Request(input,init),url=new URL(request.url);
  if(url.origin!==location.origin)throw new Error("External network disabled in auth-cache regression");
  if(!url.pathname.startsWith("/api/"))return original(request);
  const token=request.headers.get("Authorization")?.slice(7)??"anonymous";
  calls.push({token,path:url.pathname});
  if(url.pathname!=="/api/v1/orders")return Response.json({items:[]});
  const response=status=>status===200?Response.json({items:[{id:"synthetic-order",order_number:marker(token),status:"pending",currency:"RUB",grand_minor_units:12500,placed_at:"2026-09-09T00:00:00Z",updated_at:"2026-09-09T00:00:00Z",product_title:"Synthetic product"}]}):new Response("{}",{status,headers:{"Content-Type":"application/json"}});
  const mode=modes.get(token)??200;
  if(mode!=="hold")return response(mode);
  request.signal.addEventListener("abort",()=>{aborts++;},{once:true});
  // Deliberately complete despite abort: late-response fencing must still work.
  return new Promise(resolve=>pending.push({token,resolve:status=>resolve(response(status))}));
 };
 window.__authCacheTest={make,marker,leaks,calls,
  next(value){next=value;},mode(value,mode){modes.set(value.accessToken,mode);},
  change(value,notification=true){session=value;if(notification)notify();else document.dispatchEvent(new Event("visibilitychange"));},
  release(value,status=200){modes.set(value.accessToken,status);for(const item of pending.splice(0)){if(item.token===value.accessToken)item.resolve(status);else pending.push(item);}},
  pending(value){return pending.filter(item=>item.token===value.accessToken).length;},
  stats(){return{refreshes,logouts,aborts};},
 };
 const observe=()=>{if(document.querySelector(".profile-copy strong")?.textContent==="Synthetic B"&&document.body.textContent.includes(marker(make("A").accessToken)))leaks.push("previous user's order rendered under B");};
 document.addEventListener("DOMContentLoaded",()=>new MutationObserver(observe).observe(document.body,{subtree:true,childList:true,characterData:true}));
}

async function check(expression,label){assert.equal(await cdp.evaluate(expression),true,label);}
async function has(text){await cdp.wait(`document.body.innerText.includes(${JSON.stringify(text)})`,text);}
async function change(code){await cdp.evaluate(`__authCacheTest.change(${code})`);}
async function clickText(text){await cdp.evaluate(`(()=>{const button=[...document.querySelectorAll('button')].find(v=>v.textContent.trim()===${JSON.stringify(text)});if(!button)throw new Error('button missing');button.click();})()`);}
const marker=(id,scope="scope-one")=>`ORDER_synthetic_${id}_${scope.replaceAll("-","_")}`;
try{
 server=await createServer({root:join(root,"frontend"),configFile:false,cacheDir:join(scratch,"vite"),plugins:[react(),{name:"no-live-api",configureServer(server){server.middlewares.use((req,res,next)=>{if(req.url?.startsWith("/api/")){res.statusCode=500;res.end("Synthetic API interception required");}else next();});}}],server:{host:"127.0.0.1",port:0,strictPort:false},logLevel:"error"});
 await server.listen();
 const port=server.httpServer.address().port;
 const profile=join(scratch,"chrome");
 chrome=spawn(process.env.CHROME_BIN??"google-chrome",["--headless=new","--window-size=1440,1100","--no-sandbox","--disable-gpu","--disable-dev-shm-usage","--disable-background-networking","--no-first-run","--no-default-browser-check","--password-store=basic","--remote-debugging-port=0",`--user-data-dir=${profile}`,"about:blank"],{stdio:"ignore"});
 let startupError;chrome.once("error",error=>{startupError=error;});
 let debugPort;
 await until("Chrome startup",async()=>{if(startupError)throw startupError;if(chrome.exitCode!==null)throw new Error("Chrome stopped");try{debugPort=(await readFile(join(profile,"DevToolsActivePort"),"utf8")).split("\n")[0];return true;}catch{return false;}});
 const targets=await(await fetch(`http://127.0.0.1:${debugPort}/json/list`)).json();
 cdp=new CDP(targets.find(target=>target.type==="page").webSocketDebuggerUrl);
 await cdp.send("Page.enable");await cdp.send("Runtime.enable");
 await cdp.send("Page.addScriptToEvaluateOnNewDocument",{source:`(${syntheticHost.toString()})()`});
 await cdp.send("Page.navigate",{url:`http://127.0.0.1:${port}/orders`});
 await has(marker("A"));
 await cdp.evaluate(`document.querySelector('button[aria-label="Выйти"]').click()`);
 await has("Войти");await check(`!document.body.innerText.includes(${JSON.stringify(marker("A"))})`,"logout hides A");
 await cdp.evaluate(`__authCacheTest.mode(__authCacheTest.make('B'),'hold')`);
 await clickText("Войти");
 await cdp.wait(`__authCacheTest.pending(__authCacheTest.make('B'))>0`,"B request started");
 await has("Synthetic B");await check(`!document.body.innerText.includes(${JSON.stringify(marker("A"))})`,"B loading has no A cache");
 await cdp.evaluate(`__authCacheTest.release(__authCacheTest.make('B'),500)`);
 await has("Не удалось загрузить заказы.");await check(`!document.body.innerText.includes(${JSON.stringify(marker("A"))})`,"B failure has no A cache");
 await cdp.evaluate(`__authCacheTest.mode(__authCacheTest.make('B'),200)`);await clickText("Повторить");await has(marker("B"));
 console.log("PASS A -> logout -> B: loading, API failure, recovery; no old data frame");

 for(const status of [200,401]){
  await cdp.evaluate(`__authCacheTest.mode(__authCacheTest.make('A'),'hold')`);
  await change(`__authCacheTest.make('A')`);
  await cdp.wait(`__authCacheTest.pending(__authCacheTest.make('A'))>0`,"old request pending");
  await change(`__authCacheTest.make('B')`);await has(marker("B"));
  const before=await cdp.evaluate(`__authCacheTest.stats()`);
  await cdp.evaluate(`__authCacheTest.release(__authCacheTest.make('A'),${status})`);await delay(250);
  await has(marker("B"));
  const after=await cdp.evaluate(`__authCacheTest.stats()`);
  assert.equal(after.refreshes,before.refreshes,"late response must not refresh B");assert.equal(after.logouts,before.logouts,"late response must not log B out");assert.ok(after.aborts>0,"old fetch signal must abort");
  console.log(`PASS late A HTTP ${status}: aborted, cannot populate B cache or alter B session`);
 }
 await change(`__authCacheTest.make('B','scope-two',{displayName:'Synthetic workspace two'})`);await has(marker("B","scope-two"));
 await check(`!document.body.innerText.includes(${JSON.stringify(marker("B"))})`,"same subject workspace change has fresh cache");
 await cdp.evaluate(`__authCacheTest.change(__authCacheTest.make('B','scope-two',{displayName:'Synthetic restricted',capabilities:[]}),false)`);
 await has("Раздел недоступен");await check(`!document.body.innerText.includes(${JSON.stringify(marker("B","scope-two"))})`,"permission reduction hides cached orders");
 console.log("PASS workspace and permissions change: old cached orders and UI state discarded");
 await change(`__authCacheTest.make('B','expiring',{expiresAt:new Date(Date.now()+1200).toISOString()})`);await has(marker("B","expiring"));
 await has("Войти");await check(`!document.body.innerText.includes(${JSON.stringify(marker("B","expiring"))})`,"expiry clears cached orders");
 console.log("PASS hard session expiry: authenticated subtree removed");
 await check(`__authCacheTest.leaks.length===0`,"no cross-identity DOM frame");
 console.log("A01 browser regression: PASS (real React StrictMode, QueryClient, generated SDK, Orders UI; synthetic auth/API)");
}catch(error){
 if(cdp){try{const screenshot=await cdp.send("Page.captureScreenshot",{format:"png"});await writeFile(join(tmpdir(),"torgnexa-a01-browser-failure.png"),Buffer.from(screenshot.data,"base64"));console.error("Diagnostic screenshot: /tmp/torgnexa-a01-browser-failure.png");}catch{}}
 console.error(error);process.exitCode=1;
}finally{
 cdp?.socket.close();
 if(chrome&&chrome.exitCode===null){chrome.kill("SIGTERM");await Promise.race([new Promise(resolve=>chrome.once("exit",resolve)),delay(3000)]);if(chrome.exitCode===null){chrome.kill("SIGKILL");await delay(200);}}
 await server?.close();await rm(scratch,{recursive:true,force:true});
}

import test from "node:test";
import assert from "node:assert/strict";
import {createKeycloakAdapter} from "../.repository-test/auth/keycloak-adapter.js";
import {SessionController} from "../.repository-test/auth/session-controller.js";

const issuer="https://identity.example.test/realms/synthetic";
const deferred=()=>{let resolve;const promise=new Promise(r=>{resolve=r;});return{promise,resolve};};
const tokenResponse=(subject,workspace="workspace-a")=>new Response(JSON.stringify({
 access_token:`synthetic.${Buffer.from(JSON.stringify({iss:issuer,sub:subject,exp:Math.floor(Date.now()/1000)+300,organization_id:"synthetic-tenant",workspace_id:workspace,sid:"synthetic-login",realm_access:{roles:["viewer"]}})).toString("base64url")}.synthetic`,
 refresh_token:"synthetic-refresh",
}),{headers:{"Content-Type":"application/json"}});

function browserGlobals(t, transport){
 const previous={window:globalThis.window,document:globalThis.document,fetch:globalThis.fetch};
 const origin="https://app.example.test";
 const callback=url=>`${origin}/oidc/callback?code=synthetic-code&state=${new URL(url).searchParams.get("state")}`;
 globalThis.window={location:{origin},setTimeout,history:{replaceState(){}},open:url=>({closed:false,location:{href:callback(url)},close(){this.closed=true;}})};
 globalThis.document={body:{appendChild(){}},createElement(){return {contentWindow:{location:{href:"about:blank"}},setAttribute(){},remove(){},set src(url){this.contentWindow.location.href=callback(url);}};}};
 globalThis.fetch=transport;
 t.after(()=>Object.assign(globalThis,previous));
}

test("OIDC cache scope changes with workspace while a token renewal keeps the same scope",async t=>{
 let workspace="workspace-a";
 browserGlobals(t,async()=>tokenResponse("A",workspace));
 const adapter=createKeycloakAdapter({issuer,clientId:"synthetic"});
 const a=await adapter.getSession();
 const renewed=await adapter.getSession({forceRefresh:true});
 assert.equal(a.cacheScope,renewed.cacheScope);
 workspace="workspace-b";
 const switched=await adapter.getSession({forceRefresh:true});
 assert.notEqual(a.cacheScope,switched.cacheScope);
});

test("concurrent initial lookups join silent sign-in instead of publishing anonymous", async t => {
  const pending = deferred();
  const started = deferred();
  let calls = 0;
  browserGlobals(t, async () => { calls++; started.resolve(); return pending.promise; });
  const adapter = createKeycloakAdapter({issuer, clientId: "synthetic"});
  const first = adapter.getSession();
  const second = adapter.getSession();
  await started.promise;
  pending.resolve(tokenResponse("A"));
  const sessions = await Promise.all([first, second]);
  assert.equal(calls, 1);
  assert.deepEqual(sessions.map(session => session?.subject), ["A", "A"]);
});

test("StrictMode start-cleanup-start keeps the result of the joined OIDC lookup", {timeout: 5000}, async t => {
  const pending = deferred();
  const started = deferred();
  const authenticated = deferred();
  browserGlobals(t, async () => { started.resolve(); return pending.promise; });
  const controller = new SessionController(createKeycloakAdapter({issuer, clientId: "synthetic"}));
  controller.subscribe(() => {
    if (controller.getSnapshot().status === "authenticated") authenticated.resolve(controller.getSnapshot());
  });
  const stopFirst = controller.start();
  stopFirst();
  const stopSecond = controller.start();
  t.after(stopSecond);
  await started.promise;
  pending.resolve(tokenResponse("A"));
  const snapshot = await authenticated.promise;
  assert.equal(snapshot.session.subject, "A");
  assert.equal(snapshot.lifetime.signal.aborted, false);
});

test("token refresh finishing after logout cannot restore the adapter session",async t=>{
 const pending=deferred();let calls=0;
 browserGlobals(t,async()=>++calls===1?tokenResponse("A"):pending.promise);
 const adapter=createKeycloakAdapter({issuer,clientId:"synthetic"});
 await adapter.getSession();
 const refresh=adapter.getSession({forceRefresh:true});
 await adapter.logout();pending.resolve(tokenResponse("A"));
 assert.equal(await refresh,null);
 assert.equal(await adapter.getSession(),null);
});

test("old refresh finishing after login B cannot overwrite the new adapter session",async t=>{
 const pending=deferred();let calls=0;
 browserGlobals(t,async()=>{calls++;return calls===1?tokenResponse("A"):calls===2?pending.promise:tokenResponse("B");});
 const adapter=createKeycloakAdapter({issuer,clientId:"synthetic"});
 await adapter.getSession();
 const refresh=adapter.getSession({forceRefresh:true});
 await adapter.logout();await adapter.login("/orders");
 pending.resolve(tokenResponse("A"));
 assert.equal(await refresh,null);
 assert.equal((await adapter.getSession()).subject,"B");
});

test("an authorization-code response after logout cannot restore the session", async t => {
  const started = deferred();
  const pending = deferred();
  browserGlobals(t, async () => { started.resolve(); return pending.promise; });
  const adapter = createKeycloakAdapter({issuer, clientId: "synthetic"});
  const login = adapter.login("/orders");
  const rejected = assert.rejects(login, {name: "AbortError"});
  await started.promise;
  await adapter.logout();
  pending.resolve(tokenResponse("A"));
  await rejected;
  assert.equal(await adapter.getSession(), null);
});

test("a newer login wins over an older authorization-code response", async t => {
  const started = deferred();
  const pending = deferred();
  let calls = 0;
  browserGlobals(t, async () => {
    if (++calls === 1) { started.resolve(); return pending.promise; }
    return tokenResponse("B");
  });
  const adapter = createKeycloakAdapter({issuer, clientId: "synthetic"});
  const oldLogin = adapter.login("/orders");
  const rejected = assert.rejects(oldLogin, {name: "AbortError"});
  await started.promise;
  await adapter.login("/orders");
  pending.resolve(tokenResponse("A"));
  await rejected;
  assert.equal((await adapter.getSession()).subject, "B");
});

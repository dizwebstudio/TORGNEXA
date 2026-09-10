import test from "node:test";
import assert from "node:assert/strict";
import {fetchWithSessionRefresh} from "../.repository-test/api/session-fetch.js";

const refreshedSession = {
  subject: "user-1",
  displayName: "Оператор",
  accessToken: "new-token",
  capabilities: [],
  cacheScope: "synthetic-scope-1",
};

test("a 401 refreshes the session and retries the request exactly once", async () => {
  const authorizations = [];
  const bodies = [];
  const transport = async (request) => {
    authorizations.push(request.headers.get("Authorization"));
    bodies.push(await request.text());
    return new Response(null, {status: authorizations.length === 1 ? 401 : 200});
  };
  let rejected = 0;

  const response = await fetchWithSessionRefresh(
    "https://torgnexa.test/api/v1/orders",
    {method: "POST", headers: {Authorization: "Bearer old-token"}, body: "payload"},
    {...refreshedSession, accessToken: "old-token"},
    async () => refreshedSession,
    async () => { rejected += 1; },
    transport,
  );

  assert.equal(response.status, 200);
  assert.deepEqual(authorizations, ["Bearer old-token", "Bearer new-token"]);
  assert.deepEqual(bodies, ["payload", "payload"]);
  assert.equal(rejected, 0);
});

test("a second 401 rejects the refreshed application session", async () => {
  let requests = 0;
  let rejected = 0;
  const response = await fetchWithSessionRefresh(
    "https://torgnexa.test/api/v1/orders",
    {headers: {Authorization: "Bearer old-token"}},
    {...refreshedSession, accessToken: "old-token"},
    async () => refreshedSession,
    async () => { rejected += 1; },
    async () => { requests += 1; return new Response(null, {status: 401}); },
  );

  assert.equal(response.status, 401);
  assert.equal(requests, 2);
  assert.equal(rejected, 1);
});

test("an old 401 cannot refresh, log out or replay a request in a retired session", async () => {
  const lifetime = new AbortController();
  let finish;
  let refreshes=0, rejections=0;
  const pending=fetchWithSessionRefresh("https://torgnexa.test/api/v1/orders", {method:"POST",body:"old-command"},
    {...refreshedSession,accessToken:"old-token"},async()=>{refreshes++;return refreshedSession;},async()=>{rejections++;},
    ()=>new Promise(resolve=>{finish=resolve;}), lifetime.signal);
  lifetime.abort(); finish(new Response(null,{status:401}));
  await assert.rejects(pending,{name:"AbortError"});
  assert.equal(refreshes,0);assert.equal(rejections,0);
});

test("renewal cannot replay an old command with another identity or workspace", async () => {
  for (const changed of [{subject:"other-user"},{cacheScope:"other-workspace"},{capabilities:["orders.write"]}]) {
    let calls=0;
    await assert.rejects(fetchWithSessionRefresh("https://torgnexa.test/api/v1/orders", {method:"POST",body:"old-command"},
      {...refreshedSession,accessToken:"old-token"},async()=>({...refreshedSession,...changed}),async()=>{},
      async()=>{calls++;return new Response(null,{status:401});}),{name:"AbortError"});
    assert.equal(calls,1);
  }
});

test("successful late responses and a late second 401 remain fenced after retirement", async () => {
  for (const status of [200,401]) {
    const lifetime=new AbortController();let calls=0,rejected=0;
    await assert.rejects(fetchWithSessionRefresh("https://torgnexa.test/api/v1/orders",undefined,
      {...refreshedSession,accessToken:"old-token"},async()=>refreshedSession,async()=>{rejected++;},
      async()=>{calls++;if(calls===1)return new Response(null,{status:401});lifetime.abort();return new Response(null,{status});},lifetime.signal),{name:"AbortError"});
    assert.equal(rejected,0);
  }
});

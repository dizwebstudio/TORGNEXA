import test from "node:test";
import assert from "node:assert/strict";
import {fetchWithSessionRefresh} from "../.repository-test/api/session-fetch.js";

const refreshedSession = {
  subject: "user-1",
  displayName: "Оператор",
  accessToken: "new-token",
  capabilities: [],
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
    "old-token",
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
    "old-token",
    async () => refreshedSession,
    async () => { rejected += 1; },
    async () => { requests += 1; return new Response(null, {status: 401}); },
  );

  assert.equal(response.status, 401);
  assert.equal(requests, 2);
  assert.equal(rejected, 1);
});

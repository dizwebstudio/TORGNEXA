import assert from "node:assert/strict";
import { APIError, TorgnexaClient } from "../src/client.gen.mjs";

let captured;
const client = new TorgnexaClient({
  baseURL: "https://api.example.test/api/v1",
  bearerToken: "token",
  fetch: async (url, init) => {
    captured = { url: String(url), init };
    return { status: 200, headers: new Headers(), text: async () => '{"items":[]}' };
  },
});
const response = await client.listProducts({ q: "bolt", status: "active", limit: 10, cursor: "v1.abc" });
assert.equal(response.statusCode, 200);
assert.match(captured.url, /\/api\/v1\/products\?/);
assert.match(captured.url, /q=bolt/);
assert.equal(captured.init.headers.Authorization, "Bearer token");
assert.doesNotMatch(captured.url, /organization_id|workspace_id/);

const failing = new TorgnexaClient({
  baseURL: "https://api.example.test/api/v1",
  fetch: async () => ({ status: 404, headers: new Headers(), text: async () => '{"error":"missing"}' }),
});
await assert.rejects(() => failing.markNotificationRead({ notificationId: "n/1" }), (error) => error instanceof APIError && error.statusCode === 404);
await assert.rejects(() => failing.getNotificationPreference({}), /channel is required/);

const pdfBytes = new TextEncoder().encode("%PDF-1.7\n%%EOF").buffer;
const binary = new TorgnexaClient({
  baseURL: "https://api.example.test/api/v1",
  fetch: async () => ({
    status: 200,
    headers: new Headers({ "Content-Type": "application/pdf" }),
    arrayBuffer: async () => pdfBytes,
    text: async () => { throw new Error("binary response must not be decoded as text"); },
  }),
});
const pdf = await binary.getReportData({ reportId: "sales_daily", format: "pdf" });
assert.equal(pdf.body, pdfBytes);
console.log("TypeScript SDK runtime tests: PASS");

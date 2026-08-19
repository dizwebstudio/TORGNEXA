const test = require('node:test');
const assert = require('node:assert/strict');
const { createHmac } = require('node:crypto');
const genericFunctions = require('../.offline-dist/nodes/Torgnexa/GenericFunctions.js');
const { normalizeBaseUrl, cleanQuery, torgnexaApiRequest } = genericFunctions;
const { TorgnexaTrigger } = require('../.offline-dist/nodes/TorgnexaTrigger/TorgnexaTrigger.node.js');
const { normalizeEventTypes, sameEventTypes } = require('../.offline-dist/nodes/TorgnexaTrigger/Events.js');
const { verifyWebhookSignature } = require('../.offline-dist/nodes/TorgnexaTrigger/Signature.js');

test('base URL is constrained to API v1 and safe schemes', () => {
  assert.equal(normalizeBaseUrl('https://merchant.example/api/v1/'), 'https://merchant.example/api/v1');
  assert.equal(normalizeBaseUrl('http://127.0.0.1:8080/api/v1'), 'http://127.0.0.1:8080/api/v1');
  assert.throws(() => normalizeBaseUrl('http://merchant.example/api/v1'), /HTTPS/);
  assert.throws(() => normalizeBaseUrl('https://user:pass@merchant.example/api/v1'), /credentials/);
  assert.throws(() => normalizeBaseUrl('https://merchant.example/api/v2'), /api\/v1/);
});

test('tenant selectors are never accepted by the client helper', () => {
  assert.deepEqual(cleanQuery({ q: 'shoe', limit: 10, empty: '' }), { q: 'shoe', limit: 10 });
  assert.throws(() => cleanQuery({ workspace_id: 'forbidden' }), /forbidden/);
  assert.throws(() => cleanQuery({ organizationId: 'forbidden' }), /forbidden/);
});

test('API helper uses credential authentication and rejects redirects/status drift', async () => {
  let captured;
  const context = {
    async getCredentials(name) {
      assert.equal(name, 'torgnexaApi');
      return { baseUrl: 'https://merchant.example/api/v1', accessToken: 'not-exposed-to-helper' };
    },
    helpers: {
      async httpRequestWithAuthentication(name, options) {
        assert.equal(name, 'torgnexaApi');
        captured = options;
        return { statusCode: 200, body: { items: [] } };
      },
    },
  };
  const page = await torgnexaApiRequest(context, 'GET', '/products', { qs: { q: 'x', limit: 1 } });
  assert.deepEqual(page, { items: [] });
  assert.equal(captured.url, 'https://merchant.example/api/v1/products');
  assert.equal(captured.maxRedirects, 0);
  assert.deepEqual(captured.qs, { q: 'x', limit: 1 });
  assert.equal('organization_id' in captured.qs, false);
  assert.equal('workspace_id' in captured.qs, false);
});

test('event type normalization is unique, sorted, and forward compatible', () => {
  const events = normalizeEventTypes(
    ['commerce.orders.order_changed.v1', 'commerce.orders.order_changed.v1'],
    'commerce.pricing.price_changed.v1\nsecurity.upload.quarantined.v1',
  );
  assert.deepEqual(events, [
    'commerce.orders.order_changed.v1',
    'commerce.pricing.price_changed.v1',
    'security.upload.quarantined.v1',
  ]);
  assert.equal(sameEventTypes([...events].reverse(), events), true);
  assert.throws(() => normalizeEventTypes([], 'not-an-event'), /Invalid/);
});

test('webhook signature verifies exact raw body and replay window', () => {
  const secret = '0123456789abcdef0123456789abcdef';
  const rawBody = Buffer.from('{"delivery_id":"whd_1","event_type":"commerce.orders.order_changed.v1"}');
  const timestamp = '1786435200';
  const deliveryId = 'whd_1';
  const hmac = createHmac('sha256', secret).update(`${timestamp}.`).update(rawBody).digest('hex');
  const base = { deliveryId, timestamp, signature: `v1=${hmac}`, rawBody, secret, nowMs: 1786435200 * 1000 };
  assert.equal(verifyWebhookSignature(base), true);
  assert.equal(verifyWebhookSignature({ ...base, rawBody: Buffer.from(rawBody.toString().replace('whd_1', 'whd_2')) }), false);
  assert.equal(verifyWebhookSignature({ ...base, nowMs: 1786435601 * 1000 }), false);
  assert.equal(verifyWebhookSignature({ ...base, signature: `v1=${'0'.repeat(64)}` }), false);
});


test('trigger retains stale subscription state when remote disable fails', async () => {
  const state = {
    subscriptionId: 'n8n_existing',
    signingSecret: 'a'.repeat(64),
    eventTypes: ['commerce.orders.order_changed.v1'],
    endpoint: 'https://n8n.example/webhook',
  };
  const context = {
    getWorkflowStaticData() { return state; },
    getNodeParameter(name, fallback) {
      if (name === 'events') return ['commerce.orders.order_changed.v1'];
      if (name === 'additionalEventTypes') return '';
      return fallback;
    },
    getNodeWebhookUrl() { return 'https://n8n.example/webhook'; },
  };
  const original = genericFunctions.torgnexaApiRequest;
  genericFunctions.torgnexaApiRequest = async () => { throw new Error('network unavailable'); };
  try {
    const trigger = new TorgnexaTrigger();
    await assert.rejects(
      () => trigger.webhookMethods.default.create.call(context),
      /network unavailable/,
    );
    assert.equal(state.subscriptionId, 'n8n_existing');
    assert.equal(state.signingSecret, 'a'.repeat(64));
  } finally {
    genericFunctions.torgnexaApiRequest = original;
  }
});

test('trigger delete retains state until TORGNEXA confirms disable', async () => {
  const state = {
    subscriptionId: 'n8n_existing',
    signingSecret: 'b'.repeat(64),
  };
  const context = { getWorkflowStaticData() { return state; } };
  const original = genericFunctions.torgnexaApiRequest;
  genericFunctions.torgnexaApiRequest = async () => { throw new Error('network unavailable'); };
  try {
    const trigger = new TorgnexaTrigger();
    assert.equal(await trigger.webhookMethods.default.delete.call(context), false);
    assert.equal(state.subscriptionId, 'n8n_existing');
    assert.equal(state.signingSecret, 'b'.repeat(64));
  } finally {
    genericFunctions.torgnexaApiRequest = original;
  }
});

test('trigger acknowledges a valid duplicate delivery without running workflow twice', async () => {
  const secret = '0123456789abcdef0123456789abcdef';
  const deliveryId = 'whd_replay_1';
  const eventType = 'commerce.orders.order_changed.v1';
  const body = { delivery_id: deliveryId, event_type: eventType };
  const rawBody = Buffer.from(JSON.stringify(body));
  const timestamp = String(Math.floor(Date.now() / 1000));
  const signature = `v1=${createHmac('sha256', secret).update(`${timestamp}.`).update(rawBody).digest('hex')}`;
  const state = { signingSecret: secret };
  const responses = [];
  const context = {
    getWorkflowStaticData() { return state; },
    getNodeParameter(name, fallback) {
      if (name === 'events') return [eventType];
      if (name === 'additionalEventTypes') return '';
      return fallback;
    },
    getRequestObject() { return { rawBody }; },
    getHeaderData() {
      return {
        'TORGNEXA-Delivery-Id': deliveryId,
        'TORGNEXA-Timestamp': timestamp,
        'TORGNEXA-Signature': signature,
      };
    },
    getBodyData() { return body; },
    getResponseObject() {
      const entry = { statusCode: 0, body: '' };
      responses.push(entry);
      return {
        status(code) { entry.statusCode = code; return this; },
        send(value) { entry.body = String(value); return this; },
        end() { return this; },
      };
    },
  };

  const trigger = new TorgnexaTrigger();
  const first = await trigger.webhook.call(context);
  const duplicate = await trigger.webhook.call(context);
  assert.equal(Boolean(first.workflowData), true);
  assert.equal(Boolean(duplicate.workflowData), false);
  assert.equal(duplicate.noWebhookResponse, true);
  assert.deepEqual(responses.at(-1), { statusCode: 200, body: 'OK' });
  assert.equal(Object.keys(state.recentDeliveryHashes).length, 1);
});

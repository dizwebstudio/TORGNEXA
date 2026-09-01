const test = require('node:test');
const assert = require('node:assert/strict');
const { createHmac } = require('node:crypto');
const { execFileSync } = require('node:child_process');
const { mkdtemp, mkdir, readFile, rm, writeFile } = require('node:fs/promises');
const { createServer } = require('node:http');
const { tmpdir } = require('node:os');
const { join, resolve } = require('node:path');

const root = resolve(__dirname, '..');

function run(command, args, cwd) {
  return execFileSync(command, args, {
    cwd,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, npm_config_update_notifier: 'false' },
  });
}

async function startApiDouble() {
  const requests = [];
  const server = createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const rawBody = Buffer.concat(chunks).toString('utf8');
    let body;
    try {
      body = rawBody ? JSON.parse(rawBody) : undefined;
    } catch {
      body = rawBody;
    }
    requests.push({ method: request.method, url: request.url, headers: request.headers, body });

    const requestedURL = new URL(request.url, 'http://127.0.0.1');
    if (request.method === 'GET' && requestedURL.pathname === '/api/v1/products' && requestedURL.searchParams.get('limit') === '50') {
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ items: [{ id: 'product-1', code: 'DEMO-001', title: 'Demo product' }], next_cursor: '' }));
      return;
    }
    if (request.method === 'PATCH' && request.url === '/api/v1/orders/order-1/status') {
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ id: 'order-1', status: body.status, version: body.version + 1 }));
      return;
    }
    if (request.method === 'POST' && request.url === '/api/v1/webhook-subscriptions') {
      response.writeHead(201, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ id: body.id, endpoint: body.endpoint, event_types: body.event_types, status: 'active' }));
      return;
    }
    if (request.method === 'GET' && request.url === '/api/v1/webhook-subscriptions') {
      const created = requests.find((item) => item.method === 'POST' && item.url === '/api/v1/webhook-subscriptions');
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ items: created ? [{ id: created.body.id, endpoint: created.body.endpoint, event_types: created.body.event_types, status: 'active' }] : [] }));
      return;
    }
    response.writeHead(request.method === 'DELETE' ? 204 : 404, { 'content-type': 'application/json' });
    response.end(request.method === 'DELETE' ? undefined : JSON.stringify({ title: 'Not found' }));
  });
  await new Promise((resolvePromise) => server.listen(0, '127.0.0.1', resolvePromise));
  const address = server.address();
  return { server, requests, baseUrl: `http://127.0.0.1:${address.port}/api/v1` };
}

function contextFor(baseUrl, parameters) {
  return {
    getInputData() { return [{}]; },
    getNodeParameter(name, _index, fallback) { return Object.prototype.hasOwnProperty.call(parameters, name) ? parameters[name] : fallback; },
    async getCredentials(name) { assert.equal(name, 'torgnexaApi'); return { baseUrl, accessToken: 'e2e-token' }; },
    getNode() { return { name: 'TORGNEXA E2E' }; },
    continueOnFail() { return false; },
    helpers: {
      async httpRequestWithAuthentication(name, options) {
        assert.equal(name, 'torgnexaApi');
        const requestedURL = new URL(options.url);
        for (const [key, value] of Object.entries(options.qs || {})) requestedURL.searchParams.set(key, String(value));
        const response = await fetch(requestedURL, {
          method: options.method,
          headers: { ...(options.headers || {}), authorization: 'Bearer e2e-token', 'content-type': 'application/json' },
          body: options.method === 'GET' || options.method === 'DELETE' ? undefined : JSON.stringify(options.body || {}),
          redirect: 'manual',
        });
        const text = await response.text();
        return { statusCode: response.status, body: text ? JSON.parse(text) : undefined };
      },
    },
  };
}

test('published artifact installs and runs a representative n8n workflow', async () => {
  const temp = await mkdtemp(join(tmpdir(), 'torgnexa-n8n-e2e-'));
  const api = await startApiDouble();
  try {
    run('npm', ['run', 'build'], root);
    const packDir = join(temp, 'pack');
    const hostDir = join(temp, 'host');
    await mkdir(packDir);
    await mkdir(hostDir);
    const packOutput = run('npm', ['pack', '--json', '--ignore-scripts', '--pack-destination', packDir], root);
    const tarballName = JSON.parse(packOutput)[0].filename;
    const tarball = join(packDir, tarballName);

    await writeFile(join(hostDir, 'package.json'), JSON.stringify({ name: 'torgnexa-n8n-e2e-host', private: true }));
    run('npm', ['install', '--offline', '--ignore-scripts', '--no-audit', '--no-fund', '--package-lock=false', tarball], hostDir);

    const installedPackage = JSON.parse(await readFile(join(hostDir, 'node_modules/n8n-nodes-torgnexa/package.json'), 'utf8'));
    assert.equal(installedPackage.version, '0.2.0');
    for (const relativePath of installedPackage.n8n.nodes.concat(installedPackage.n8n.credentials)) {
      await readFile(join(hostDir, 'node_modules/n8n-nodes-torgnexa', relativePath));
    }

    const { Torgnexa } = require(join(hostDir, 'node_modules/n8n-nodes-torgnexa', installedPackage.n8n.nodes[0]));
    const { TorgnexaTrigger } = require(join(hostDir, 'node_modules/n8n-nodes-torgnexa', installedPackage.n8n.nodes[1]));
    const actionNode = new Torgnexa();
    const resourceProperty = actionNode.description.properties.find((property) => property.name === 'resource');
    assert.deepEqual(resourceProperty.options.map((item) => item.value), ['product', 'catalog', 'order', 'inventory', 'fulfillment', 'sync', 'pricing']);

    const listResult = await actionNode.execute.call(contextFor(api.baseUrl, {
      resource: 'product', operation: 'list', limit: 50, q: '', productStatus: '', cursor: '',
    }));
    assert.equal(listResult[0][0].json.items[0].code, 'DEMO-001');

    const statusResult = await actionNode.execute.call(contextFor(api.baseUrl, {
      resource: 'order', operation: 'changeStatus', orderId: 'order-1', orderStatus: 'confirmed',
      expectedVersion: 1, idempotencyKey: 'e2e-order-status-1',
    }));
    assert.equal(statusResult[0][0].json.status, 'confirmed');
    const statusRequest = api.requests.find((item) => item.url === '/api/v1/orders/order-1/status');
    assert.equal(statusRequest.headers['idempotency-key'], 'e2e-order-status-1');
    assert.deepEqual(statusRequest.body, { status: 'confirmed', version: 1 });

    const trigger = new TorgnexaTrigger();
    const triggerState = {};
    const triggerContext = contextFor(api.baseUrl, {
      events: ['commerce.fulfillment.shipment_changed.v1'], additionalEventTypes: '',
    });
    Object.assign(triggerContext, {
      getWorkflowStaticData() { return triggerState; },
      getNodeWebhookUrl() { return 'https://n8n.example/webhook/torgnexa-e2e'; },
    });
    assert.equal(await trigger.webhookMethods.default.create.call(triggerContext), true);
    assert.equal(typeof triggerState.signingSecret, 'string');
    const event = { delivery_id: 'e2e-delivery-1', event_type: 'commerce.fulfillment.shipment_changed.v1', data: { shipment_id: 'shipment-1' } };
    const rawBody = Buffer.from(JSON.stringify(event));
    const timestamp = String(Math.floor(Date.now() / 1000));
    const signature = createHmac('sha256', triggerState.signingSecret).update(`${timestamp}.`).update(rawBody).digest('hex');
    Object.assign(triggerContext, {
      getRequestObject() { return { rawBody }; },
      getHeaderData() { return { 'TORGNEXA-Delivery-Id': event.delivery_id, 'TORGNEXA-Timestamp': timestamp, 'TORGNEXA-Signature': `v1=${signature}` }; },
      getBodyData() { return event; },
    });
    const triggerResult = await trigger.webhook.call(triggerContext);
    assert.equal(triggerResult.workflowData[0][0].json.event_type, event.event_type);
    assert.ok(api.requests.some((item) => item.method === 'POST' && item.url === '/api/v1/webhook-subscriptions'));
  } finally {
    await new Promise((resolvePromise) => api.server.close(resolvePromise));
    await rm(temp, { recursive: true, force: true });
  }
});

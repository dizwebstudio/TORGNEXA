const test = require('node:test');
const assert = require('node:assert/strict');
const { createHmac } = require('node:crypto');
const { execFileSync } = require('node:child_process');
const { access, mkdtemp, mkdir, readdir, readFile, rm, writeFile } = require('node:fs/promises');
const { tmpdir } = require('node:os');
const { join, resolve } = require('node:path');

const root = resolve(__dirname, '..');

function run(command, args, cwd, extraEnv = {}, stdio = ['ignore', 'pipe', 'pipe']) {
  return execFileSync(command, args, {
    cwd,
    encoding: 'utf8',
    stdio,
    env: { ...process.env, npm_config_update_notifier: 'false', ...extraEnv },
  });
}

function createApiDouble() {
  const requests = [];
  return {
    requests,
    baseUrl: 'http://127.0.0.1:8080/api/v1',
    async request(options) {
      const requestedURL = new URL(options.url);
      for (const [key, value] of Object.entries(options.qs || {})) requestedURL.searchParams.set(key, String(value));
      const body = options.body;
      const item = { method: options.method, url: `${requestedURL.pathname}${requestedURL.search}`, headers: options.headers || {}, body };
      requests.push(item);
      if (options.method === 'GET' && requestedURL.pathname === '/api/v1/products' && requestedURL.searchParams.get('limit') === '50') {
        return { statusCode: 200, body: { items: [{ id: 'product-1', code: 'DEMO-001', title: 'Demo product' }], next_cursor: '' } };
      }
      if (options.method === 'PATCH' && item.url === '/api/v1/orders/order-1/status') {
        return { statusCode: 200, body: { id: 'order-1', status: body.status, version: body.version + 1 } };
      }
      if (options.method === 'POST' && item.url === '/api/v1/webhook-subscriptions') {
        return { statusCode: 201, body: { id: body.id, endpoint: body.endpoint, event_types: body.event_types, status: 'active' } };
      }
      if (options.method === 'GET' && item.url === '/api/v1/webhook-subscriptions') {
        const created = requests.find((entry) => entry.method === 'POST' && entry.url === '/api/v1/webhook-subscriptions');
        return { statusCode: 200, body: { items: created ? [{ id: created.body.id, endpoint: created.body.endpoint, event_types: created.body.event_types, status: 'active' }] : [] } };
      }
      if (options.method === 'DELETE') return { statusCode: 204, body: undefined };
      return { statusCode: 404, body: { title: 'Not found' } };
    },
  };
}

function contextFor(baseUrl, parameters, requestHandler) {
  return {
    getInputData() { return [{}]; },
    getNodeParameter(name, _index, fallback) { return Object.prototype.hasOwnProperty.call(parameters, name) ? parameters[name] : fallback; },
    async getCredentials(name) { assert.equal(name, 'torgnexaApi'); return { baseUrl, accessToken: 'e2e-token' }; },
    getNode() { return { name: 'TORGNEXA E2E' }; },
    continueOnFail() { return false; },
    helpers: {
      async httpRequestWithAuthentication(name, options) {
        assert.equal(name, 'torgnexaApi');
        return requestHandler(options);
      },
    },
  };
}

test('published artifact installs and runs a representative n8n workflow', async () => {
  const temp = await mkdtemp(join(tmpdir(), 'torgnexa-n8n-e2e-'));
  try {
    await access(join(root, 'dist/nodes/Torgnexa/Torgnexa.node.js'));
    const packDir = join(temp, 'pack');
    const hostDir = join(temp, 'host');
    await mkdir(packDir);
    await mkdir(hostDir);
    const npmEnv = { npm_config_cache: join(temp, 'npm-cache') };
    run('npm', ['pack', '--ignore-scripts', '--pack-destination', packDir], root, npmEnv, 'inherit');
    const tarballName = (await readdir(packDir)).find((entry) => entry.endsWith('.tgz'));
    assert.ok(tarballName, 'npm pack did not produce an artifact');
    const tarball = join(packDir, tarballName);

    await writeFile(join(hostDir, 'package.json'), JSON.stringify({ name: 'torgnexa-n8n-e2e-host', private: true }));
    run('npm', ['install', '--offline', '--ignore-scripts', '--no-audit', '--no-fund', '--package-lock=false', tarball], hostDir, npmEnv);

    const api = createApiDouble();

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
    }, api.request));
    assert.equal(listResult[0][0].json.items[0].code, 'DEMO-001');

    const statusResult = await actionNode.execute.call(contextFor(api.baseUrl, {
      resource: 'order', operation: 'changeStatus', orderId: 'order-1', orderStatus: 'confirmed',
      expectedVersion: 1, idempotencyKey: 'e2e-order-status-1',
    }, api.request));
    assert.equal(statusResult[0][0].json.status, 'confirmed');
    const statusRequest = api.requests.find((item) => item.url === '/api/v1/orders/order-1/status');
    assert.equal(statusRequest.headers['Idempotency-Key'], 'e2e-order-status-1');
    assert.deepEqual(statusRequest.body, { status: 'confirmed', version: 1 });

    const trigger = new TorgnexaTrigger();
    const triggerState = {};
    const triggerContext = contextFor(api.baseUrl, {
      events: ['commerce.fulfillment.shipment_changed.v1'], additionalEventTypes: '',
    }, api.request);
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
    await rm(temp, { recursive: true, force: true });
  }
});

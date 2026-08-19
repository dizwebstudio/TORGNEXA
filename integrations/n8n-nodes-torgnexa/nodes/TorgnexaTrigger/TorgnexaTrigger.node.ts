import { createHash, randomBytes } from 'node:crypto';
import type { IHookFunctions, INodeType, INodeTypeDescription, IWebhookFunctions, IWebhookResponseData } from 'n8n-workflow';
import { torgnexaApiRequest } from '../Torgnexa/GenericFunctions';
import { commonEventOptions, normalizeEventTypes, sameEventTypes } from './Events';
import { headerValue, verifyWebhookSignature } from './Signature';

interface Subscription {
  id: string;
  endpoint: string;
  event_types: string[];
  status: 'active' | 'disabled';
}
interface SubscriptionList { items: Subscription[] }

const replayWindowMs = 5 * 60 * 1000;
const replaySweepMs = 30 * 1000;
const maxReplayEntries = 20_000;

type ReplayClaim = 'accepted' | 'replay' | 'capacity';

function claimDelivery(state: Record<string, any>, deliveryId: string, nowMs: number): ReplayClaim {
  let entries: Record<string, number>;
  const candidate = state.recentDeliveryHashes;
  if (candidate && typeof candidate === 'object' && !Array.isArray(candidate)) {
    entries = candidate as Record<string, number>;
  } else {
    entries = Object.create(null) as Record<string, number>;
  }

  const lastSweep = typeof state.replayLastSweepMs === 'number' ? state.replayLastSweepMs : 0;
  const sweep = nowMs - lastSweep >= replaySweepMs || Object.keys(entries).length >= maxReplayEntries;
  if (sweep) {
    for (const [key, seenAt] of Object.entries(entries)) {
      if (!Number.isFinite(seenAt) || nowMs - seenAt > replayWindowMs) delete entries[key];
    }
    state.replayLastSweepMs = nowMs;
  }

  const digest = createHash('sha256').update(deliveryId).digest('hex');
  if (Object.prototype.hasOwnProperty.call(entries, digest)) return 'replay';
  if (Object.keys(entries).length >= maxReplayEntries) return 'capacity';

  entries[digest] = nowMs;
  state.recentDeliveryHashes = entries;
  return 'accepted';
}

function subscriptionState(context: IHookFunctions | IWebhookFunctions): Record<string, any> {
  return context.getWorkflowStaticData('node');
}

function configuredEvents(context: IHookFunctions | IWebhookFunctions): string[] {
  return normalizeEventTypes(
    context.getNodeParameter('events', []),
    context.getNodeParameter('additionalEventTypes', ''),
  );
}

function clearState(state: Record<string, any>): void {
  delete state.subscriptionId;
  delete state.signingSecret;
  delete state.eventTypes;
  delete state.endpoint;
  delete state.recentDeliveryHashes;
  delete state.replayLastSweepMs;
}

export class TorgnexaTrigger implements INodeType {
  description: INodeTypeDescription = {
    displayName: 'TORGNEXA Trigger',
    name: 'torgnexaTrigger',
    icon: 'file:torgnexa.svg',
    group: ['trigger'],
    version: 1,
    description: 'Receive verified TORGNEXA signed webhook events',
    defaults: { name: 'TORGNEXA Trigger' },
    inputs: [],
    outputs: ['main'],
    credentials: [{ name: 'torgnexaApi', required: true }],
    webhooks: [{ name: 'default', httpMethod: 'POST', responseMode: 'onReceived', path: 'webhook' }],
    properties: [
      {
        displayName: 'Events',
        name: 'events',
        type: 'multiOptions',
        default: ['commerce.orders.order_changed.v1'],
        options: commonEventOptions,
        required: true,
        description: 'Canonical TORGNEXA event types to subscribe to',
      },
      {
        displayName: 'Additional Event Types',
        name: 'additionalEventTypes',
        type: 'string',
        default: '',
        typeOptions: { rows: 3 },
        description: 'Optional comma/newline-separated canonical event types for forward-compatible generic domains',
      },
    ],
  };

  webhookMethods = {
    default: {
      async checkExists(this: IHookFunctions): Promise<boolean> {
        const state = subscriptionState(this);
        if (typeof state.subscriptionId !== 'string' || typeof state.signingSecret !== 'string') return false;
        const endpoint = this.getNodeWebhookUrl('default');
        if (!endpoint) return false;
        const events = configuredEvents(this);
        const page = await torgnexaApiRequest<SubscriptionList>(this, 'GET', '/webhook-subscriptions');
        const found = Array.isArray(page.items) ? page.items.find((item) => item.id === state.subscriptionId) : undefined;
        return Boolean(found && found.status === 'active' && found.endpoint === endpoint && sameEventTypes(found.event_types, events));
      },

      async create(this: IHookFunctions): Promise<boolean> {
        const state = subscriptionState(this);
        if (typeof state.subscriptionId === 'string') {
          await torgnexaApiRequest(this, 'DELETE', `/webhook-subscriptions/${encodeURIComponent(state.subscriptionId)}`, { expectedStatuses: [204, 404] });
          clearState(state);
        }
        const endpoint = this.getNodeWebhookUrl('default');
        if (!endpoint || !endpoint.startsWith('https://')) throw new Error('TORGNEXA Trigger requires an externally reachable HTTPS n8n webhook URL');
        const events = configuredEvents(this);
        const subscriptionId = `n8n_${randomBytes(16).toString('hex')}`;
        const signingSecret = randomBytes(32).toString('hex');
        const created = await torgnexaApiRequest<Subscription>(this, 'POST', '/webhook-subscriptions', {
          expectedStatuses: [201],
          body: { id: subscriptionId, endpoint, event_types: events, signing_secret: signingSecret },
        });
        if (created.id !== subscriptionId || created.status !== 'active') throw new Error('TORGNEXA webhook subscription was not activated');
        state.subscriptionId = subscriptionId;
        state.signingSecret = signingSecret;
        state.eventTypes = events;
        state.endpoint = endpoint;
        return true;
      },

      async delete(this: IHookFunctions): Promise<boolean> {
        const state = subscriptionState(this);
        if (typeof state.subscriptionId !== 'string') {
          clearState(state);
          return true;
        }
        try {
          await torgnexaApiRequest(this, 'DELETE', `/webhook-subscriptions/${encodeURIComponent(state.subscriptionId)}`, { expectedStatuses: [204, 404] });
        } catch {
          return false;
        }
        clearState(state);
        return true;
      },
    },
  };

  async webhook(this: IWebhookFunctions): Promise<IWebhookResponseData> {
    const state = subscriptionState(this);
    const secret = typeof state.signingSecret === 'string' ? state.signingSecret : '';
    const request = this.getRequestObject();
    const rawBody = request?.rawBody;
    if (!Buffer.isBuffer(rawBody)) {
      const res = this.getResponseObject();
      res.status(400).send('Raw webhook body required').end();
      return { noWebhookResponse: true };
    }
    const headers = this.getHeaderData();
    const deliveryId = headerValue(headers, 'TORGNEXA-Delivery-Id');
    const timestamp = headerValue(headers, 'TORGNEXA-Timestamp');
    const signature = headerValue(headers, 'TORGNEXA-Signature');
    const nowMs = Date.now();
    if (!verifyWebhookSignature({ deliveryId, timestamp, signature, rawBody, secret, nowMs })) {
      const res = this.getResponseObject();
      res.status(401).send('Unauthorized').end();
      return { noWebhookResponse: true };
    }
    const body = this.getBodyData();
    const events = configuredEvents(this);
    if (body.delivery_id !== deliveryId || typeof body.event_type !== 'string' || !events.includes(body.event_type)) {
      const res = this.getResponseObject();
      res.status(400).send('Invalid TORGNEXA webhook envelope').end();
      return { noWebhookResponse: true };
    }
    const replay = claimDelivery(state, deliveryId, nowMs);
    if (replay === 'replay') {
      // A valid duplicate is acknowledged but must never trigger the workflow twice.
      const res = this.getResponseObject();
      res.status(200).send('OK').end();
      return { noWebhookResponse: true };
    }
    if (replay === 'capacity') {
      // Fail closed rather than evicting a still-live delivery ID and reopening a
      // replay window. A retry can succeed after old entries expire.
      const res = this.getResponseObject();
      res.status(503).send('Webhook replay protection busy').end();
      return { noWebhookResponse: true };
    }
    return { workflowData: [[{ json: body }]] };
  }
}

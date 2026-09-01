import type { IExecuteFunctions, INodeExecutionData, INodeType, INodeTypeDescription } from 'n8n-workflow';
import { ensureSearchPage, torgnexaApiRequest } from './GenericFunctions';

type Field = Record<string, any>;

const show = (resource: string, operationNames: string[]) => ({ show: { resource: [resource], operation: operationNames } });
const option = (name: string, value: string, action: string) => ({ name, value, action });

const resources = [
  option('Product', 'product', 'Work with product cards, offers, prices and images'),
  option('Catalog', 'catalog', 'Work with catalog categories'),
  option('Order', 'order', 'Read orders and change their lifecycle status'),
  option('Inventory', 'inventory', 'Read positions, warehouses, allocations and incidents'),
  option('Fulfillment / WMS', 'fulfillment', 'Create and process warehouse fulfillment tasks and batches'),
  option('Synchronization', 'sync', 'Read and operate synchronization policies and drifts'),
  option('Pricing', 'pricing', 'Run deterministic pricing previews'),
];

const operations: Record<string, Field[]> = {
  product: [
    option('List / Search', 'list', 'List or search product cards'),
    option('Get', 'get', 'Get a product card'),
    option('Create', 'create', 'Create a product card'),
    option('Update', 'update', 'Update a product card'),
    option('Create Offer', 'createOffer', 'Create an offer for a product'),
    option('Update Offer', 'updateOffer', 'Update an offer'),
    option('Create Price', 'createPrice', 'Create an offer price'),
    option('Update Price', 'updatePrice', 'Update an offer price'),
    option('Assign Category', 'assignCategory', 'Assign a catalog category'),
    option('Create Image', 'createImage', 'Add an image reference'),
    option('Update Image', 'updateImage', 'Update an image reference'),
    option('Delete Image', 'deleteImage', 'Delete an image reference'),
  ],
  catalog: [
    option('List Categories', 'list', 'List catalog categories'),
    option('Create Category', 'create', 'Create a catalog category'),
  ],
  order: [
    option('List / Search', 'list', 'List or search orders'),
    option('Get', 'get', 'Get an order'),
    option('Change Status', 'changeStatus', 'Change an order lifecycle status'),
  ],
  inventory: [
    option('List Positions', 'listPositions', 'List inventory positions'),
    option('List Warehouses', 'listWarehouses', 'List warehouses'),
    option('Create Warehouse', 'createWarehouse', 'Create a warehouse'),
    option('Update Warehouse', 'updateWarehouse', 'Update a warehouse'),
    option('List Fulfillment Allocations', 'listAllocations', 'List order-item reservations'),
    option('Reserve Fulfillment Allocation', 'reserveAllocation', 'Reserve an order item at a warehouse'),
    option('List Warehouse Incidents', 'listIncidents', 'List warehouse incident history'),
  ],
  fulfillment: [
    option('List Tasks', 'listTasks', 'List warehouse tasks'),
    option('Create Task', 'createTask', 'Create a warehouse task'),
    option('Create Tasks From Order', 'createTasksFromOrder', 'Reserve an order and create pick tasks'),
    option('Get Task', 'getTask', 'Get a warehouse task'),
    option('Claim Task', 'claimTask', 'Claim a warehouse task'),
    option('Start Task', 'startTask', 'Start a warehouse task'),
    option('Scan Task', 'scanTask', 'Apply a barcode scan to a warehouse task'),
    option('Complete Task', 'completeTask', 'Complete a warehouse task'),
    option('Exception Task', 'exceptionTask', 'Move a warehouse task to exception handling'),
    option('Cancel Task', 'cancelTask', 'Cancel a warehouse task'),
    option('List Task History', 'listTaskHistory', 'List immutable task history'),
    option('Create Task Batch', 'createBatch', 'Create a packing handoff batch'),
    option('Get Task Batch', 'getBatch', 'Get a task batch'),
    option('Handoff Task Batch', 'handoffBatch', 'Hand off a task batch'),
  ],
  sync: [
    option('Get Status', 'status', 'Read synchronization status'),
    option('Create Policy', 'createPolicy', 'Create a synchronization policy'),
    option('Update Policy', 'updatePolicy', 'Update a synchronization policy'),
    option('Run Policy', 'runPolicy', 'Queue an on-demand synchronization run'),
    option('Resolve Drift', 'resolveDrift', 'Resolve an open synchronization drift'),
  ],
  pricing: [
    option('Preview Repricing', 'previewRepricing', 'Run a deterministic repricing preview without remote writes'),
  ],
};

const readPageOperations = ['list', 'listTasks'];
const productIdOperations = ['get', 'update', 'createOffer', 'updateOffer', 'createPrice', 'updatePrice', 'assignCategory', 'createImage', 'updateImage', 'deleteImage'];
const offerIdOperations = ['updateOffer'];
const priceIdOperations = ['createPrice', 'updatePrice'];
const imageIdOperations = ['updateImage', 'deleteImage'];
const writeOperations = [
  'create', 'update', 'createOffer', 'updateOffer', 'createPrice', 'updatePrice', 'assignCategory',
  'createImage', 'updateImage', 'deleteImage', 'changeStatus', 'reserveAllocation', 'createTask',
  'createTasksFromOrder', 'claimTask', 'startTask', 'scanTask', 'completeTask', 'exceptionTask',
  'cancelTask', 'createBatch', 'handoffBatch', 'createWarehouse', 'updateWarehouse', 'createPolicy', 'updatePolicy', 'runPolicy', 'resolveDrift',
];
const taskIdOperations = ['getTask', 'claimTask', 'startTask', 'scanTask', 'completeTask', 'exceptionTask', 'cancelTask', 'listTaskHistory'];
const batchIdOperations = ['getBatch', 'handoffBatch'];

const fields: Field[] = [
  {
    displayName: 'Limit', name: 'limit', type: 'number', default: 50,
    typeOptions: { minValue: 1, maxValue: 100 },
    displayOptions: { show: { operation: readPageOperations } },
  },
  {
    displayName: 'Query', name: 'q', type: 'string', default: '',
    displayOptions: show('product', ['list']),
  },
  {
    displayName: 'Product Status', name: 'productStatus', type: 'options', default: '',
    options: [{ name: 'Any', value: '' }, { name: 'Draft', value: 'draft' }, { name: 'Active', value: 'active' }, { name: 'Archived', value: 'archived' }],
    displayOptions: show('product', ['list']),
  },
  {
    displayName: 'Order Status Filter', name: 'orderStatusFilter', type: 'options', default: '',
    options: [{ name: 'Any', value: '' }, { name: 'Pending', value: 'pending' }, { name: 'Confirmed', value: 'confirmed' }, { name: 'Processing', value: 'processing' }, { name: 'Fulfilled', value: 'fulfilled' }, { name: 'Cancelled', value: 'cancelled' }],
    displayOptions: show('order', ['list']),
  },
  {
    displayName: 'Placed From', name: 'placedFrom', type: 'dateTime', default: '',
    displayOptions: show('order', ['list']),
  },
  {
    displayName: 'Placed To', name: 'placedTo', type: 'dateTime', default: '',
    displayOptions: show('order', ['list']),
  },
  {
    displayName: 'Task State', name: 'taskState', type: 'options', default: '',
    options: [{ name: 'Any', value: '' }, { name: 'Pending', value: 'pending' }, { name: 'In Progress', value: 'in_progress' }, { name: 'Completed', value: 'completed' }, { name: 'Cancelled', value: 'cancelled' }, { name: 'Exception', value: 'exception' }],
    displayOptions: show('fulfillment', ['listTasks']),
  },
  {
    displayName: 'Task Type', name: 'taskTypeFilter', type: 'options', default: '',
    options: [{ name: 'Any', value: '' }, { name: 'Receiving', value: 'receiving' }, { name: 'Put Away', value: 'put_away' }, { name: 'Pick', value: 'pick' }, { name: 'Pack', value: 'pack' }, { name: 'Cycle Count', value: 'cycle_count' }, { name: 'Transfer', value: 'transfer' }, { name: 'Return Receiving', value: 'return_receiving' }],
    displayOptions: show('fulfillment', ['listTasks']),
  },
  {
    displayName: 'Cursor', name: 'cursor', type: 'string', default: '',
    displayOptions: { show: { operation: readPageOperations } },
  },
  {
    displayName: 'Product ID', name: 'productId', type: 'string', required: true,
    displayOptions: show('product', productIdOperations),
  },
  {
    displayName: 'Offer ID', name: 'offerId', type: 'string', required: true,
    displayOptions: show('product', offerIdOperations),
  },
  {
    displayName: 'Offer or Price ID', name: 'offerOrPriceId', type: 'string', required: true,
    displayOptions: show('product', priceIdOperations),
  },
  {
    displayName: 'Category ID', name: 'categoryId', type: 'string', required: true,
    displayOptions: show('product', ['assignCategory']),
  },
  {
    displayName: 'Image ID', name: 'imageId', type: 'string', required: true,
    displayOptions: show('product', imageIdOperations),
  },
  {
    displayName: 'Order ID', name: 'orderId', type: 'string', required: true,
    displayOptions: { show: { resource: ['order', 'fulfillment'], operation: ['get', 'changeStatus', 'createTasksFromOrder'] } },
  },
  {
    displayName: 'Order Status', name: 'orderStatus', type: 'options', default: 'confirmed', required: true,
    options: [{ name: 'Pending', value: 'pending' }, { name: 'Confirmed', value: 'confirmed' }, { name: 'Processing', value: 'processing' }, { name: 'Fulfilled', value: 'fulfilled' }, { name: 'Cancelled', value: 'cancelled' }],
    displayOptions: show('order', ['changeStatus']),
  },
  {
    displayName: 'Expected Version', name: 'expectedVersion', type: 'number', default: 1, required: true,
    typeOptions: { minValue: 1 },
    displayOptions: { show: { resource: ['order', 'fulfillment', 'sync'], operation: ['changeStatus', 'claimTask', 'startTask', 'scanTask', 'completeTask', 'exceptionTask', 'cancelTask', 'updatePolicy', 'resolveDrift'] } },
  },
  {
    displayName: 'Warehouse ID', name: 'warehouseId', type: 'string', required: true,
    displayOptions: { show: { resource: ['inventory', 'fulfillment'], operation: ['reserveAllocation', 'updateWarehouse', 'listTasks', 'createTask', 'createTasksFromOrder', 'createBatch'] } },
  },
  {
    displayName: 'Order Item ID', name: 'orderItemId', type: 'string', required: true,
    displayOptions: show('inventory', ['reserveAllocation']),
  },
  {
    displayName: 'Task ID', name: 'taskId', type: 'string', required: true,
    displayOptions: show('fulfillment', taskIdOperations),
  },
  {
    displayName: 'Batch ID', name: 'batchId', type: 'string', required: true,
    displayOptions: show('fulfillment', batchIdOperations),
  },
  {
    displayName: 'Policy ID', name: 'policyId', type: 'string', required: true,
    displayOptions: show('sync', ['updatePolicy', 'runPolicy']),
  },
  {
    displayName: 'Drift ID', name: 'driftId', type: 'string', required: true,
    displayOptions: show('sync', ['resolveDrift']),
  },
  {
    displayName: 'JSON Body', name: 'jsonBody', type: 'json', default: '{}', required: true,
    typeOptions: { rows: 8 },
    description: 'Object matching the public TORGNEXA OpenAPI request schema. Tenant and workspace selectors are not allowed.',
    displayOptions: { show: {
      resource: ['product', 'catalog', 'inventory', 'fulfillment', 'sync', 'pricing'],
      operation: ['create', 'update', 'createOffer', 'updateOffer', 'createPrice', 'updatePrice', 'createImage', 'updateImage', 'createWarehouse', 'updateWarehouse', 'createTask', 'scanTask', 'createBatch', 'createPolicy', 'updatePolicy', 'resolveDrift', 'handoffBatch'],
    } },
  },
  {
    displayName: 'Idempotency Key', name: 'idempotencyKey', type: 'string',
    default: '={{$execution.id + "-" + $itemIndex}}', required: true,
    description: 'Stable key for this business mutation. Reusing it safely replays the same request instead of creating a second side effect.',
    displayOptions: { show: { operation: writeOperations } },
  },
];

function requiredString(value: unknown, label: string): string {
  const result = String(value ?? '').trim();
  if (result === '') throw new Error(`${label} is required`);
  return result;
}

function pathPart(value: unknown, label: string): string {
  return encodeURIComponent(requiredString(value, label));
}

function idempotencyHeaders(context: IExecuteFunctions, itemIndex: number): Record<string, string> {
  const key = requiredString(context.getNodeParameter('idempotencyKey', itemIndex, ''), 'Idempotency Key');
  if (key.length > 128 || !/^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/.test(key)) {
    throw new Error('Idempotency Key must match the public TORGNEXA contract');
  }
  return { 'Idempotency-Key': key };
}

function jsonObject(context: IExecuteFunctions, itemIndex: number): Record<string, unknown> {
  const raw = context.getNodeParameter('jsonBody', itemIndex, '{}');
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) return raw as Record<string, unknown>;
  if (typeof raw !== 'string') throw new Error('JSON Body must be an object');
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error('JSON Body is not valid JSON');
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('JSON Body must be an object');
  return parsed as Record<string, unknown>;
}

function jsonObjectIfPresent(context: IExecuteFunctions, itemIndex: number): Record<string, unknown> {
  const raw = context.getNodeParameter('jsonBody', itemIndex, '{}');
  if (raw === undefined || raw === null || raw === '') return {};
  return jsonObject(context, itemIndex);
}

function outputRecord(value: unknown): Record<string, any> {
  if (value && typeof value === 'object' && !Array.isArray(value)) return value as Record<string, any>;
  return { data: value };
}

function expectedWriteStatuses(operation: string): number[] {
  if (operation === 'deleteImage') return [204];
  if (operation === 'runPolicy') return [202];
  return [200, 201];
}

export class Torgnexa implements INodeType {
  description: INodeTypeDescription = {
    displayName: 'TORGNEXA',
    name: 'torgnexa',
    icon: 'file:torgnexa.svg',
    group: ['transform'],
    version: 2,
    subtitle: '={{$parameter["resource"] + ": " + $parameter["operation"]}}',
    description: 'Use TORGNEXA public REST resources with tenant-scoped IAM, idempotency, approval and audit policy',
    defaults: { name: 'TORGNEXA' },
    inputs: ['main'],
    outputs: ['main'],
    credentials: [{ name: 'torgnexaApi', required: true }],
    properties: [
      { displayName: 'Resource', name: 'resource', type: 'options', default: 'product', options: resources },
      { displayName: 'Operation', name: 'operation', type: 'options', default: 'list', options: operations.product, displayOptions: { show: { resource: ['product'] } } },
      { displayName: 'Operation', name: 'operation', type: 'options', default: 'list', options: operations.catalog, displayOptions: { show: { resource: ['catalog'] } } },
      { displayName: 'Operation', name: 'operation', type: 'options', default: 'list', options: operations.order, displayOptions: { show: { resource: ['order'] } } },
      { displayName: 'Operation', name: 'operation', type: 'options', default: 'listPositions', options: operations.inventory, displayOptions: { show: { resource: ['inventory'] } } },
      { displayName: 'Operation', name: 'operation', type: 'options', default: 'listTasks', options: operations.fulfillment, displayOptions: { show: { resource: ['fulfillment'] } } },
      { displayName: 'Operation', name: 'operation', type: 'options', default: 'status', options: operations.sync, displayOptions: { show: { resource: ['sync'] } } },
      { displayName: 'Operation', name: 'operation', type: 'options', default: 'previewRepricing', options: operations.pricing, displayOptions: { show: { resource: ['pricing'] } } },
      ...fields,
    ],
  };

  async execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
    const input = this.getInputData();
    const output: INodeExecutionData[] = [];
    const iterations = Math.max(input.length, 1);

    for (let itemIndex = 0; itemIndex < iterations; itemIndex++) {
      try {
        const resource = requiredString(this.getNodeParameter('resource', itemIndex, 'product'), 'Resource');
        const operation = requiredString(this.getNodeParameter('operation', itemIndex, 'list'), 'Operation');
        const limit = this.getNodeParameter('limit', itemIndex, 50) as number;
        const cursor = this.getNodeParameter('cursor', itemIndex, '');
        const headers = writeOperations.includes(operation) ? idempotencyHeaders(this, itemIndex) : undefined;
        let response: unknown;

        if (resource === 'product' && operation === 'list') {
          response = ensureSearchPage(await torgnexaApiRequest(this, 'GET', '/products', { qs: {
            q: this.getNodeParameter('q', itemIndex, ''),
            status: this.getNodeParameter('productStatus', itemIndex, ''),
            limit,
            cursor,
          } }));
        } else if (resource === 'product' && operation === 'get') {
          response = await torgnexaApiRequest(this, 'GET', `/products/${pathPart(this.getNodeParameter('productId', itemIndex, ''), 'Product ID')}`);
        } else if (resource === 'product' && operation === 'create') {
          response = await torgnexaApiRequest(this, 'POST', '/products', { body: jsonObject(this, itemIndex), headers, expectedStatuses: [201] });
        } else if (resource === 'product' && operation === 'update') {
          response = await torgnexaApiRequest(this, 'PATCH', `/products/${pathPart(this.getNodeParameter('productId', itemIndex, ''), 'Product ID')}`, { body: jsonObject(this, itemIndex), headers });
        } else if (resource === 'product' && operation === 'createOffer') {
          response = await torgnexaApiRequest(this, 'POST', `/products/${pathPart(this.getNodeParameter('productId', itemIndex, ''), 'Product ID')}/offers`, { body: jsonObject(this, itemIndex), headers, expectedStatuses: [201] });
        } else if (resource === 'product' && operation === 'updateOffer') {
          response = await torgnexaApiRequest(this, 'PATCH', `/products/${pathPart(this.getNodeParameter('productId', itemIndex, ''), 'Product ID')}/offers/${pathPart(this.getNodeParameter('offerId', itemIndex, ''), 'Offer ID')}`, { body: jsonObject(this, itemIndex), headers });
        } else if (resource === 'product' && (operation === 'createPrice' || operation === 'updatePrice')) {
          response = await torgnexaApiRequest(this, operation === 'createPrice' ? 'POST' : 'PATCH', `/products/${pathPart(this.getNodeParameter('productId', itemIndex, ''), 'Product ID')}/prices/${pathPart(this.getNodeParameter('offerOrPriceId', itemIndex, ''), 'Offer or Price ID')}`, { body: jsonObject(this, itemIndex), headers, expectedStatuses: expectedWriteStatuses(operation) });
        } else if (resource === 'product' && operation === 'assignCategory') {
          response = await torgnexaApiRequest(this, 'POST', `/products/${pathPart(this.getNodeParameter('productId', itemIndex, ''), 'Product ID')}/categories/${pathPart(this.getNodeParameter('categoryId', itemIndex, ''), 'Category ID')}`, { headers });
        } else if (resource === 'product' && (operation === 'createImage' || operation === 'updateImage' || operation === 'deleteImage')) {
          const productPath = `/products/${pathPart(this.getNodeParameter('productId', itemIndex, ''), 'Product ID')}/images`;
          const imagePath = operation === 'createImage' ? productPath : `${productPath}/${pathPart(this.getNodeParameter('imageId', itemIndex, ''), 'Image ID')}`;
          response = await torgnexaApiRequest(this, operation === 'createImage' ? 'POST' : operation === 'updateImage' ? 'PATCH' : 'DELETE', imagePath, {
            body: operation === 'deleteImage' ? undefined : jsonObject(this, itemIndex),
            headers,
            expectedStatuses: expectedWriteStatuses(operation),
          });
          if (operation === 'deleteImage' && response === undefined) response = { success: true };
        } else if (resource === 'catalog' && operation === 'list') {
          response = await torgnexaApiRequest(this, 'GET', '/catalog/categories');
        } else if (resource === 'catalog' && operation === 'create') {
          response = await torgnexaApiRequest(this, 'POST', '/catalog/categories', { body: jsonObject(this, itemIndex), headers, expectedStatuses: [201] });
        } else if (resource === 'order' && operation === 'list') {
          response = ensureSearchPage(await torgnexaApiRequest(this, 'GET', '/orders', { qs: {
            q: this.getNodeParameter('q', itemIndex, ''),
            status: this.getNodeParameter('orderStatusFilter', itemIndex, ''),
            placed_from: this.getNodeParameter('placedFrom', itemIndex, ''),
            placed_to: this.getNodeParameter('placedTo', itemIndex, ''),
            limit,
            cursor,
          } }));
        } else if (resource === 'order' && operation === 'get') {
          response = await torgnexaApiRequest(this, 'GET', `/orders/${pathPart(this.getNodeParameter('orderId', itemIndex, ''), 'Order ID')}`);
        } else if (resource === 'order' && operation === 'changeStatus') {
          response = await torgnexaApiRequest(this, 'PATCH', `/orders/${pathPart(this.getNodeParameter('orderId', itemIndex, ''), 'Order ID')}/status`, {
            body: { status: requiredString(this.getNodeParameter('orderStatus', itemIndex, ''), 'Order Status'), version: Number(this.getNodeParameter('expectedVersion', itemIndex, 1)) },
            headers,
          });
        } else if (resource === 'inventory' && operation === 'listPositions') {
          response = await torgnexaApiRequest(this, 'GET', '/inventory/positions');
        } else if (resource === 'inventory' && operation === 'listWarehouses') {
          response = await torgnexaApiRequest(this, 'GET', '/inventory/warehouses');
        } else if (resource === 'inventory' && operation === 'createWarehouse') {
          response = await torgnexaApiRequest(this, 'POST', '/inventory/warehouses', { body: jsonObject(this, itemIndex), headers, expectedStatuses: [201] });
        } else if (resource === 'inventory' && operation === 'updateWarehouse') {
          response = await torgnexaApiRequest(this, 'PATCH', `/inventory/warehouses/${pathPart(this.getNodeParameter('warehouseId', itemIndex, ''), 'Warehouse ID')}`, { body: jsonObject(this, itemIndex), headers });
        } else if (resource === 'inventory' && operation === 'listAllocations') {
          response = await torgnexaApiRequest(this, 'GET', '/inventory/fulfillment-allocations');
        } else if (resource === 'inventory' && operation === 'reserveAllocation') {
          response = await torgnexaApiRequest(this, 'POST', '/inventory/fulfillment-allocations', { body: {
            order_item_id: requiredString(this.getNodeParameter('orderItemId', itemIndex, ''), 'Order Item ID'),
            warehouse_id: requiredString(this.getNodeParameter('warehouseId', itemIndex, ''), 'Warehouse ID'),
          }, headers, expectedStatuses: [201] });
        } else if (resource === 'inventory' && operation === 'listIncidents') {
          response = await torgnexaApiRequest(this, 'GET', '/inventory/warehouse-incidents');
        } else if (resource === 'fulfillment' && operation === 'listTasks') {
          response = await torgnexaApiRequest(this, 'GET', '/warehouse-tasks', { qs: {
            state: this.getNodeParameter('taskState', itemIndex, ''),
            task_type: this.getNodeParameter('taskTypeFilter', itemIndex, ''),
            warehouse_id: this.getNodeParameter('warehouseId', itemIndex, ''),
            limit,
            cursor,
          } });
        } else if (resource === 'fulfillment' && operation === 'createTask') {
          response = await torgnexaApiRequest(this, 'POST', '/warehouse-tasks', { body: jsonObject(this, itemIndex), headers, expectedStatuses: [200, 201] });
        } else if (resource === 'fulfillment' && operation === 'createTasksFromOrder') {
          response = await torgnexaApiRequest(this, 'POST', '/warehouse-tasks/from-order', { body: {
            order_id: requiredString(this.getNodeParameter('orderId', itemIndex, ''), 'Order ID'),
            warehouse_id: requiredString(this.getNodeParameter('warehouseId', itemIndex, ''), 'Warehouse ID'),
          }, headers, expectedStatuses: [201] });
        } else if (resource === 'fulfillment' && operation === 'getTask') {
          response = await torgnexaApiRequest(this, 'GET', `/warehouse-tasks/${pathPart(this.getNodeParameter('taskId', itemIndex, ''), 'Task ID')}`);
        } else if (resource === 'fulfillment' && ['claimTask', 'startTask', 'scanTask', 'completeTask', 'exceptionTask', 'cancelTask'].includes(operation)) {
          const action = operation.replace('Task', '');
          response = await torgnexaApiRequest(this, 'POST', `/warehouse-tasks/${pathPart(this.getNodeParameter('taskId', itemIndex, ''), 'Task ID')}/${action}`, {
            body: { ...jsonObjectIfPresent(this, itemIndex), version: Number(this.getNodeParameter('expectedVersion', itemIndex, 1)) },
            headers,
            expectedStatuses: [200],
          });
        } else if (resource === 'fulfillment' && operation === 'listTaskHistory') {
          response = await torgnexaApiRequest(this, 'GET', `/warehouse-tasks/${pathPart(this.getNodeParameter('taskId', itemIndex, ''), 'Task ID')}/history`);
        } else if (resource === 'fulfillment' && operation === 'createBatch') {
          response = await torgnexaApiRequest(this, 'POST', '/warehouse-task-batches', { body: jsonObject(this, itemIndex), headers, expectedStatuses: [201] });
        } else if (resource === 'fulfillment' && operation === 'getBatch') {
          response = await torgnexaApiRequest(this, 'GET', `/warehouse-task-batches/${pathPart(this.getNodeParameter('batchId', itemIndex, ''), 'Batch ID')}`);
        } else if (resource === 'fulfillment' && operation === 'handoffBatch') {
          response = await torgnexaApiRequest(this, 'POST', `/warehouse-task-batches/${pathPart(this.getNodeParameter('batchId', itemIndex, ''), 'Batch ID')}/handoff`, { body: jsonObjectIfPresent(this, itemIndex), headers, expectedStatuses: [200] });
        } else if (resource === 'sync' && operation === 'status') {
          response = await torgnexaApiRequest(this, 'GET', '/sync/status');
        } else if (resource === 'sync' && operation === 'createPolicy') {
          response = await torgnexaApiRequest(this, 'POST', '/sync/policies', { body: jsonObject(this, itemIndex), headers, expectedStatuses: [201] });
        } else if (resource === 'sync' && operation === 'updatePolicy') {
          response = await torgnexaApiRequest(this, 'PATCH', `/sync/policies/${pathPart(this.getNodeParameter('policyId', itemIndex, ''), 'Policy ID')}`, { body: jsonObject(this, itemIndex), headers });
        } else if (resource === 'sync' && operation === 'runPolicy') {
          response = await torgnexaApiRequest(this, 'POST', `/sync/policies/${pathPart(this.getNodeParameter('policyId', itemIndex, ''), 'Policy ID')}/run`, { headers, expectedStatuses: [202] });
        } else if (resource === 'sync' && operation === 'resolveDrift') {
          response = await torgnexaApiRequest(this, 'POST', `/sync/drifts/${pathPart(this.getNodeParameter('driftId', itemIndex, ''), 'Drift ID')}/actions`, { body: jsonObject(this, itemIndex), headers });
        } else if (resource === 'pricing' && operation === 'previewRepricing') {
          response = await torgnexaApiRequest(this, 'POST', '/pricing/repricing/preview', { body: jsonObject(this, itemIndex) });
        } else {
          throw new Error(`Unsupported TORGNEXA resource/operation: ${resource}/${operation}`);
        }

        output.push({ json: outputRecord(response), pairedItem: input.length ? itemIndex : undefined });
      } catch (error) {
        if (!this.continueOnFail()) throw error;
        output.push({ json: {}, error, pairedItem: input.length ? itemIndex : undefined });
      }
    }
    return [output];
  }
}

import type { IExecuteFunctions, INodeExecutionData, INodeType, INodeTypeDescription } from 'n8n-workflow';
import { ensureSearchPage, torgnexaApiRequest } from './GenericFunctions';

const productFields = [
  { displayName: 'Query', name: 'q', type: 'string', default: '', displayOptions: { show: { resource: ['product'], operation: ['list'] } } },
  { displayName: 'Status', name: 'status', type: 'options', default: '', options: [
    { name: 'Any', value: '' }, { name: 'Draft', value: 'draft' }, { name: 'Active', value: 'active' }, { name: 'Archived', value: 'archived' },
  ], displayOptions: { show: { resource: ['product'], operation: ['list'] } } },
  { displayName: 'Cursor', name: 'cursor', type: 'string', default: '', displayOptions: { show: { resource: ['product'], operation: ['list'] } } },
];

const orderFields = [
  { displayName: 'Query', name: 'q', type: 'string', default: '', displayOptions: { show: { resource: ['order'], operation: ['list'] } } },
  { displayName: 'Status', name: 'status', type: 'options', default: '', options: [
    { name: 'Any', value: '' }, { name: 'Pending', value: 'pending' }, { name: 'Confirmed', value: 'confirmed' },
    { name: 'Processing', value: 'processing' }, { name: 'Fulfilled', value: 'fulfilled' }, { name: 'Cancelled', value: 'cancelled' },
  ], displayOptions: { show: { resource: ['order'], operation: ['list'] } } },
  { displayName: 'Placed From', name: 'placedFrom', type: 'dateTime', default: '', displayOptions: { show: { resource: ['order'], operation: ['list'] } } },
  { displayName: 'Placed To', name: 'placedTo', type: 'dateTime', default: '', displayOptions: { show: { resource: ['order'], operation: ['list'] } } },
  { displayName: 'Cursor', name: 'cursor', type: 'string', default: '', displayOptions: { show: { resource: ['order'], operation: ['list'] } } },
];

export class Torgnexa implements INodeType {
  description: INodeTypeDescription = {
    displayName: 'TORGNEXA',
    name: 'torgnexa',
    icon: 'file:torgnexa.svg',
    group: ['transform'],
    version: 1,
    subtitle: '={{$parameter["resource"] + ": " + $parameter["operation"]}}',
    description: 'Use TORGNEXA public REST resources without bypassing IAM, approval, or audit policy',
    defaults: { name: 'TORGNEXA' },
    inputs: ['main'],
    outputs: ['main'],
    credentials: [{ name: 'torgnexaApi', required: true }],
    properties: [
      { displayName: 'Resource', name: 'resource', type: 'options', default: 'product', options: [
        { name: 'Product', value: 'product' }, { name: 'Order', value: 'order' },
      ] },
      { displayName: 'Operation', name: 'operation', type: 'options', default: 'list', options: [{ name: 'List / Search', value: 'list', action: 'List or search records' }] },
      { displayName: 'Limit', name: 'limit', type: 'number', default: 50, typeOptions: { minValue: 1, maxValue: 100 }, displayOptions: { show: { operation: ['list'] } } },
      ...productFields,
      ...orderFields,
    ],
  };

  async execute(this: IExecuteFunctions): Promise<INodeExecutionData[][]> {
    const input = this.getInputData();
    const output: INodeExecutionData[] = [];
    const iterations = Math.max(input.length, 1);
    for (let itemIndex = 0; itemIndex < iterations; itemIndex++) {
      try {
        const resource = this.getNodeParameter('resource', itemIndex, 'product') as string;
        const limit = this.getNodeParameter('limit', itemIndex, 50) as number;
        if (resource === 'product') {
          const page = ensureSearchPage(await torgnexaApiRequest(this, 'GET', '/products', { qs: {
            q: this.getNodeParameter('q', itemIndex, ''),
            status: this.getNodeParameter('status', itemIndex, ''),
            limit,
            cursor: this.getNodeParameter('cursor', itemIndex, ''),
          } }));
          output.push({ json: page as unknown as Record<string, any>, pairedItem: input.length ? itemIndex : undefined });
        } else if (resource === 'order') {
          const page = ensureSearchPage(await torgnexaApiRequest(this, 'GET', '/orders', { qs: {
            q: this.getNodeParameter('q', itemIndex, ''),
            status: this.getNodeParameter('status', itemIndex, ''),
            placed_from: this.getNodeParameter('placedFrom', itemIndex, ''),
            placed_to: this.getNodeParameter('placedTo', itemIndex, ''),
            limit,
            cursor: this.getNodeParameter('cursor', itemIndex, ''),
          } }));
          output.push({ json: page as unknown as Record<string, any>, pairedItem: input.length ? itemIndex : undefined });
        } else {
          throw new Error(`Unsupported TORGNEXA resource: ${resource}`);
        }
      } catch (error) {
        if (!this.continueOnFail()) throw error;
        output.push({ json: {}, error, pairedItem: input.length ? itemIndex : undefined });
      }
    }
    return [output];
  }
}

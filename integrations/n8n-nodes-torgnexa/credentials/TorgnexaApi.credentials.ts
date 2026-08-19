import type { IAuthenticateGeneric, ICredentialTestRequest, ICredentialType, INodeProperties } from 'n8n-workflow';

export class TorgnexaApi implements ICredentialType {
  name = 'torgnexaApi';
  displayName = 'TORGNEXA API';
  documentationUrl = 'https://docs.torgnexa.local/developer/n8n';

  properties: INodeProperties[] = [
    {
      displayName: 'API Base URL',
      name: 'baseUrl',
      type: 'string',
      default: 'https://api.example.com/api/v1',
      required: true,
      description: 'Absolute TORGNEXA API URL ending in /api/v1. HTTPS is required except loopback development.',
    },
    {
      displayName: 'Scoped Access Token',
      name: 'accessToken',
      type: 'string',
      typeOptions: { password: true },
      default: '',
      required: true,
      description: 'OIDC/API bearer credential scoped by TORGNEXA IAM. Tenant/workspace is derived by the server from this identity.',
    },
  ];

  authenticate: IAuthenticateGeneric = {
    type: 'generic',
    properties: {
      headers: {
        Authorization: '=Bearer {{$credentials.accessToken}}',
        Accept: 'application/json',
      },
    },
  };

  test: ICredentialTestRequest = {
    request: {
      baseURL: '={{$credentials.baseUrl}}',
      url: '/products',
      method: 'GET',
      qs: { limit: 1 },
    },
  };
}

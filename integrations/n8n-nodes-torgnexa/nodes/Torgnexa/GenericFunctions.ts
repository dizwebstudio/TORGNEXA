import type { IExecuteFunctions, IHookFunctions } from 'n8n-workflow';

export type ApiContext = IExecuteFunctions | IHookFunctions;
export type HttpMethod = 'GET' | 'POST' | 'DELETE';

export interface SearchPage<T = Record<string, unknown>> {
  items: T[];
  next_cursor?: string;
}

export function normalizeBaseUrl(raw: unknown): string {
  if (typeof raw !== 'string' || raw.length === 0 || raw.length > 2048) {
    throw new Error('TORGNEXA API base URL is required');
  }
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error('TORGNEXA API base URL must be absolute');
  }
  if (parsed.username || parsed.password || parsed.search || parsed.hash) {
    throw new Error('TORGNEXA API base URL must not contain credentials, query, or fragment');
  }
  const loopback = parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1' || parsed.hostname === '[::1]' || parsed.hostname === '::1';
  if (parsed.protocol !== 'https:' && !(parsed.protocol === 'http:' && loopback)) {
    throw new Error('TORGNEXA API base URL must use HTTPS except loopback development');
  }
  const normalizedPath = parsed.pathname.replace(/\/+$/, '');
  if (!normalizedPath.endsWith('/api/v1')) {
    throw new Error('TORGNEXA API base URL must end with /api/v1');
  }
  parsed.pathname = normalizedPath;
  return parsed.toString().replace(/\/$/, '');
}

export function cleanQuery(input: Record<string, unknown>): Record<string, string | number | boolean> {
  const out: Record<string, string | number | boolean> = {};
  for (const [key, value] of Object.entries(input)) {
    if (value === undefined || value === null || value === '') continue;
    if (key === 'organization_id' || key === 'workspace_id' || key === 'organizationId' || key === 'workspaceId') {
      throw new Error('Client tenant/workspace selectors are forbidden');
    }
    if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') out[key] = value;
  }
  return out;
}

export async function torgnexaApiRequest<T>(
  context: ApiContext,
  method: HttpMethod,
  path: string,
  options: { qs?: Record<string, unknown>; body?: Record<string, unknown>; expectedStatuses?: number[] } = {},
): Promise<T> {
  if (!path.startsWith('/') || path.includes('://')) throw new Error('TORGNEXA API path must be relative to /api/v1');
  const credentials = await context.getCredentials('torgnexaApi');
  const baseUrl = normalizeBaseUrl(credentials.baseUrl);
  const request: Record<string, unknown> = {
    method,
    url: `${baseUrl}${path}`,
    json: true,
    timeout: 30_000,
    maxRedirects: 0,
    returnFullResponse: true,
    ignoreHttpStatusErrors: true,
  };
  if (options.qs) request.qs = cleanQuery(options.qs);
  if (options.body !== undefined) request.body = options.body;

  const response = await context.helpers.httpRequestWithAuthentication.call(context, 'torgnexaApi', request) as {
    statusCode?: number;
    body?: T | Record<string, unknown>;
  };
  const status = Number(response.statusCode ?? 0);
  const expected = options.expectedStatuses ?? [200];
  if (!expected.includes(status)) {
    const problem = response.body && typeof response.body === 'object' ? response.body as Record<string, unknown> : {};
    const title = typeof problem.title === 'string' ? problem.title : 'TORGNEXA API request failed';
    throw new Error(`${title} (HTTP ${status || 'unknown'})`);
  }
  return response.body as T;
}

export function ensureSearchPage(value: unknown): SearchPage {
  if (!value || typeof value !== 'object' || !Array.isArray((value as SearchPage).items)) {
    throw new Error('TORGNEXA API returned an invalid search page');
  }
  return value as SearchPage;
}

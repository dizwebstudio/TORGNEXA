import { createHmac, timingSafeEqual } from 'node:crypto';

export interface SignatureInput {
  deliveryId: string;
  timestamp: string;
  signature: string;
  rawBody: Uint8Array;
  secret: string;
  nowMs?: number;
  replayWindowSeconds?: number;
}

export function verifyWebhookSignature(input: SignatureInput): boolean {
  const replayWindowSeconds = input.replayWindowSeconds ?? 300;
  if (!/^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/.test(input.deliveryId)) return false;
  if (!/^\d{1,12}$/.test(input.timestamp)) return false;
  if (!/^v1=[0-9a-f]{64}$/.test(input.signature)) return false;
  if (input.secret.length < 32 || input.secret.length > 64 || input.rawBody.length === 0) return false;

  const unixSeconds = Number(input.timestamp);
  if (!Number.isSafeInteger(unixSeconds)) return false;
  const nowMs = input.nowMs ?? Date.now();
  if (Math.abs(nowMs - unixSeconds * 1000) > replayWindowSeconds * 1000) return false;

  const mac = createHmac('sha256', input.secret);
  mac.update(`${input.timestamp}.`);
  mac.update(input.rawBody);
  const expected = mac.digest();
  const provided = Buffer.from(input.signature.slice(3), 'hex');
  return provided.length === expected.length && timingSafeEqual(provided, expected);
}

export function headerValue(headers: Record<string, unknown>, name: string): string {
  const target = name.toLowerCase();
  for (const [key, value] of Object.entries(headers)) {
    if (key.toLowerCase() !== target) continue;
    if (Array.isArray(value)) return String(value[0] ?? '');
    return value === undefined || value === null ? '' : String(value);
  }
  return '';
}

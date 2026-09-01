/** Generates a sortable UUIDv7 for client-side entities and idempotency keys. */
export function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  const millis = BigInt(Date.now());
  for (let index = 5; index >= 0; index -= 1) {
    bytes[5 - index] = Number((millis >> BigInt(index * 8)) & 255n);
  }
  bytes[6] = (bytes[6] & 15) | 112;
  bytes[8] = (bytes[8] & 63) | 128;
  const raw = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${raw.slice(0, 8)}-${raw.slice(8, 12)}-${raw.slice(12, 16)}-${raw.slice(16, 20)}-${raw.slice(20)}`;
}

/** Generates a non-predictable idempotency key for a retriable mutation. */
export function idempotencyKey(): string {
  return crypto.randomUUID();
}

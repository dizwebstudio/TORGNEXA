export const EVENT_TYPE_PATTERN = /^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$/;

export const commonEventOptions = [
  { name: 'Approval Required', value: 'governance.approval.requested.v1' },
  { name: 'Compliance Document Status Changed', value: 'compliance.document.status_changed.v1' },
  { name: 'Order Changed', value: 'commerce.orders.order_changed.v1' },
  { name: 'Order Created', value: 'commerce.orders.order_created.v1' },
  { name: 'Price Changed', value: 'commerce.pricing.price_changed.v1' },
  { name: 'Publication Status Changed', value: 'commerce.social.publication_status_changed.v1' },
  { name: 'Stock Changed', value: 'commerce.inventory.stock_changed.v1' },
  { name: 'Upload Quarantined', value: 'security.upload.quarantined.v1' },
];

export function normalizeEventTypes(selected: unknown, additional: unknown): string[] {
  const values: string[] = [];
  if (Array.isArray(selected)) {
    for (const value of selected) values.push(String(value).trim());
  }
  if (typeof additional === 'string' && additional.trim() !== '') {
    for (const value of additional.split(/[\n,]/)) values.push(value.trim());
  }
  const unique = [...new Set(values.filter(Boolean))];
  if (unique.length < 1 || unique.length > 128) throw new Error('Select between 1 and 128 TORGNEXA event types');
  for (const eventType of unique) {
    if (!EVENT_TYPE_PATTERN.test(eventType)) throw new Error(`Invalid TORGNEXA event type: ${eventType}`);
  }
  return unique.sort();
}

export function sameEventTypes(a: unknown, b: string[]): boolean {
  if (!Array.isArray(a)) return false;
  const normalized = [...new Set(a.map(String))].sort();
  return normalized.length === b.length && normalized.every((value, index) => value === b[index]);
}

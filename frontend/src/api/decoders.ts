export interface ProductPrice {minor_units: number; currency: string}
export interface ProductHit {id: string; code: string; title: string; description?: string; status: string; updated_at: string; image_url?: string; price?: ProductPrice}
export interface OrderHit {id: string; order_number: string; status: string; currency: string; grand_minor_units: number; placed_at: string; updated_at: string; product_title?: string; product_sku?: string; product_image_url?: string}
export interface NotificationHit {id: string; severity: string; title: string; body: string; occurrence_count: number; read_at?: string; updated_at: string}

export interface ProductPage {items: ProductHit[]; next_cursor?: string}
export interface OrderPage {items: OrderHit[]; next_cursor?: string}
export interface NotificationPage {items: NotificationHit[]}

function object(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}
function text(value: unknown): string | undefined { return typeof value === "string" ? value : undefined; }
function integer(value: unknown): number | undefined { return typeof value === "number" && Number.isSafeInteger(value) ? value : undefined; }

export function decodeProductPage(value: unknown): ProductPage {
  const root = object(value);
  if (!root || !Array.isArray(root.items)) throw new Error("invalid product page");
  const items = root.items.map((entry) => {
    const row = object(entry);
    const id = row && text(row.id), code = row && text(row.code), title = row && text(row.title), description = row && text(row.description), status = row && text(row.status), updated = row && text(row.updated_at), imageURL = row && text(row.image_url);
    if (!id || !code || !title || !status || !updated) throw new Error("invalid product hit");
    let price: ProductPrice | undefined;
    if (row && row.price !== undefined) {
      const candidate = object(row.price), minorUnits = candidate && integer(candidate.minor_units), currency = candidate && text(candidate.currency);
      if (!candidate || minorUnits === undefined || minorUnits < 0 || !currency || !/^[A-Z]{3}$/.test(currency)) throw new Error("invalid product price");
      price = {minor_units: minorUnits, currency};
    }
    return {id, code, title, status, updated_at: updated, ...(description !== undefined ? {description} : {}), ...(imageURL ? {image_url: imageURL} : {}), ...(price ? {price} : {})};
  });
  const cursor = text(root.next_cursor);
  return cursor ? {items, next_cursor: cursor} : {items};
}

export function decodeOrderPage(value: unknown): OrderPage {
  const root = object(value);
  if (!root || !Array.isArray(root.items)) throw new Error("invalid order page");
  const items = root.items.map((entry) => {
    const row = object(entry);
    const id = row && text(row.id), number = row && text(row.order_number), status = row && text(row.status), currency = row && text(row.currency), placed = row && text(row.placed_at), updated = row && text(row.updated_at), total = row && integer(row.grand_minor_units), productTitle = row && text(row.product_title), productSKU = row && text(row.product_sku), productImageURL = row && text(row.product_image_url);
    if (!id || !number || !status || !currency || !placed || !updated || total === undefined || total < 0) throw new Error("invalid order hit");
    return {id, order_number: number, status, currency, grand_minor_units: total, placed_at: placed, updated_at: updated, ...(productTitle ? {product_title: productTitle} : {}), ...(productSKU ? {product_sku: productSKU} : {}), ...(productImageURL ? {product_image_url: productImageURL} : {})};
  });
  const cursor = text(root.next_cursor);
  return cursor ? {items, next_cursor: cursor} : {items};
}

export function decodeNotificationPage(value: unknown): NotificationPage {
  const root = object(value);
  if (!root || !Array.isArray(root.items)) throw new Error("invalid notification page");
  const items = root.items.map((entry) => {
    const row = object(entry);
    const id = row && text(row.id), severity = row && text(row.severity), title = row && text(row.title), body = row && text(row.body), updated = row && text(row.updated_at), count = row && integer(row.occurrence_count), readAt = row && text(row.read_at);
    if (!id || !severity || !title || body === undefined || !updated || count === undefined || count < 1) throw new Error("invalid notification hit");
    return readAt ? {id, severity, title, body, occurrence_count: count, updated_at: updated, read_at: readAt} : {id, severity, title, body, occurrence_count: count, updated_at: updated};
  });
  return {items};
}

export interface ProductPrice {minor_units: number; currency: string}
export interface ProductHit {id: string; code: string; title: string; description?: string; status: string; updated_at: string; image_url?: string; price?: ProductPrice}
export interface OrderHit {id: string; order_number: string; status: string; currency: string; grand_minor_units: number; placed_at: string; updated_at: string; product_title?: string; product_sku?: string; product_image_url?: string}
export interface NotificationHit {id: string; severity: string; title: string; body: string; occurrence_count: number; read_at?: string; updated_at: string}

export interface ProductPage {items: ProductHit[]; next_cursor?: string}
export interface OrderPage {items: OrderHit[]; next_cursor?: string}
export interface NotificationPage {items: NotificationHit[]}
export interface ReturnQuantity {coefficient: number; scale: number; unit: string}
export interface ReturnSummary {
  id: string;
  order_id: string;
  status: string;
  reason_code: string;
  source: string;
  currency: string;
  requested_shipping_minor: number;
  requested_tax_minor: number;
  version: number;
  created_at: string;
  updated_at: string;
}
export interface ReturnItemHit {
  id: string;
  return_id: string;
  order_item_id: string;
  unit: string;
  disposition: string;
  requested: ReturnQuantity;
  received: ReturnQuantity;
  accepted: ReturnQuantity;
  version: number;
  created_at: string;
  updated_at: string;
}
export interface ReturnPage {items: ReturnSummary[]}
export interface ReturnDetails {return: ReturnSummary; items: ReturnItemHit[]}

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

function decodeQuantity(value: unknown): ReturnQuantity {
  const row = object(value);
  const coefficient = row && integer(row.coefficient), scale = row && integer(row.scale), unit = row && text(row.unit);
  if (coefficient === undefined || coefficient < 0 || scale === undefined || scale < 0 || scale > 9 || !unit || !/^[A-Z0-9_.-]{1,16}$/.test(unit)) throw new Error("invalid return quantity");
  return {coefficient, scale, unit};
}

function decodeReturn(value: unknown): ReturnSummary {
  const row = object(value);
  const id = row && text(row.id), orderID = row && text(row.order_id), status = row && text(row.status), reason = row && text(row.reason_code), source = row && text(row.source), currency = row && text(row.currency), shipping = row && integer(row.requested_shipping_minor), tax = row && integer(row.requested_tax_minor), version = row && integer(row.version), created = row && text(row.created_at), updated = row && text(row.updated_at);
  if (!id || !orderID || !status || !reason || !source || !currency || !/^[A-Z]{3}$/.test(currency) || shipping === undefined || shipping < 0 || tax === undefined || tax < 0 || version === undefined || version < 1 || !created || !updated) throw new Error("invalid return");
  return {id, order_id: orderID, status, reason_code: reason, source, currency, requested_shipping_minor: shipping, requested_tax_minor: tax, version, created_at: created, updated_at: updated};
}

function decodeReturnItem(value: unknown): ReturnItemHit {
  const row = object(value);
  const id = row && text(row.id), returnID = row && text(row.return_id), orderItemID = row && text(row.order_item_id), unit = row && text(row.unit), disposition = row && text(row.disposition), version = row && integer(row.version), created = row && text(row.created_at), updated = row && text(row.updated_at);
  if (!id || !returnID || !orderItemID || !unit || !/^[A-Z0-9_.-]{1,16}$/.test(unit) || !disposition || version === undefined || version < 1 || !created || !updated) throw new Error("invalid return item");
  return {id, return_id: returnID, order_item_id: orderItemID, unit, disposition, requested: decodeQuantity(row.requested), received: decodeQuantity(row.received), accepted: decodeQuantity(row.accepted), version, created_at: created, updated_at: updated};
}

export function decodeReturnPage(value: unknown): ReturnPage {
  const root = object(value);
  if (!root || !Array.isArray(root.items)) throw new Error("invalid return page");
  return {items: root.items.map(decodeReturn)};
}

export function decodeReturnDetails(value: unknown): ReturnDetails {
  const root = object(value);
  if (!root || !Array.isArray(root.items)) throw new Error("invalid return details");
  return {return: decodeReturn(root.return), items: root.items.map(decodeReturnItem)};
}

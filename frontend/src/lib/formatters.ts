export type FixedQuantity = {coefficient: number; scale: number; unit: string};

/** Formats minor currency units with the Russian locale and currency symbol. */
export function formatMoneyMinor(minorUnits: number, currency = "RUB", maximumFractionDigits?: number): string {
  return new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency,
    ...(maximumFractionDigits === undefined ? {} : {maximumFractionDigits}),
  }).format(minorUnits / 100);
}

/** Formats a money object returned by the API. */
export function formatMoneyValue(value: {minor_units: number; currency: string}): string {
  return formatMoneyMinor(value.minor_units, value.currency);
}

/** Formats optional report money values while keeping the report placeholder. */
export function formatReportMoney(value: string, currency = "RUB"): string {
  if (value === "—" || value === "") return "—";
  return formatMoneyMinor(Number(value), currency, 2);
}

/** Formats a quantity encoded as coefficient plus decimal scale. */
export function formatQuantity(value: FixedQuantity, labels: Readonly<Record<string, string>> = {}): string {
  const amount = value.coefficient / (10 ** value.scale);
  return `${amount} ${labels[value.unit] ?? value.unit}`;
}

/** Formats a minor-unit amount for views where the currency code is shown separately. */
export function formatAmountWithCurrency(minorUnits: number | undefined, currency: string | undefined): string {
  return `${((minorUnits ?? 0) / 100).toLocaleString("ru-RU", {minimumFractionDigits: 2})} ${currency ?? ""}`.trim();
}

/** Pretty-prints diagnostic JSON consistently across catalog workspaces. */
export function formatJson(value: unknown): string {
  return JSON.stringify(value, null, 2);
}

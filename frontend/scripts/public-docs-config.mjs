export const defaultPublicURL = "http://127.0.0.1:5173";

export function publicURL() {
  const value = (process.env.TORGNEXA_PUBLIC_URL ?? defaultPublicURL).trim().replace(/\/+$/, "");
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`TORGNEXA_PUBLIC_URL must be an absolute http(s) URL: ${value}`);
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error(`TORGNEXA_PUBLIC_URL must use http or https: ${value}`);
  }
  return value;
}

export function docsURL() {
  return `${publicURL()}/docs`;
}

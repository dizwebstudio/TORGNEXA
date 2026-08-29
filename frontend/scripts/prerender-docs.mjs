import {mkdir, readFile, writeFile} from "node:fs/promises";
import {fileURLToPath, pathToFileURL} from "node:url";
import {resolve} from "node:path";
import {docsURL, publicURL} from "./public-docs-config.mjs";

const frontendRoot = fileURLToPath(new URL("..", import.meta.url));
const distRoot = resolve(frontendRoot, "dist");
const ssrRoot = resolve(frontendRoot, ".prerender");
const output = resolve(distRoot, "docs", "index.html");

function escapeAttribute(value) {
  return value.replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;");
}

function jsonForScript(value) {
  return JSON.stringify(value).replaceAll("<", "\\u003c");
}

const clientIndex = await readFile(resolve(distRoot, "index.html"), "utf8");
const stylesheetTags = [...clientIndex.matchAll(/<link[^>]+rel="stylesheet"[^>]*>/g)].map(match => match[0]).join("\n    ");
if (!stylesheetTags) throw new Error("client build has no stylesheet link");

const ssrModulePath = resolve(ssrRoot, "docs-entry.js");
const ssrModule = await import(pathToFileURL(ssrModulePath).href);
if (typeof ssrModule.renderDocumentation !== "function") throw new Error("SSR docs entry does not export renderDocumentation");
const markup = ssrModule.renderDocumentation();
if (!markup.includes("Документация TORGNEXA") || !markup.includes("Пошаговое подключение")) throw new Error("prerendered docs content is incomplete");

const siteURL = publicURL();
const canonical = docsURL();
const structuredData = {
  "@context": "https://schema.org",
  "@type": "TechArticle",
  headline: "Документация TORGNEXA — интеграции, каталог и синхронизация",
  description: "Официальная документация TORGNEXA: подключение маркетплейсов, интернет-магазинов, платежей и CRM, управление каталогом, заказами и синхронизацией.",
  inLanguage: "ru-RU",
  url: canonical,
  mainEntityOfPage: canonical,
  publisher: {"@type": "Organization", name: "TORGNEXA"},
};
const html = `<!doctype html>
<html lang="ru">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <meta name="color-scheme" content="light" />
    <meta name="theme-color" content="#f5f7fb" />
    <meta name="referrer" content="same-origin" />
    <meta name="description" content="${escapeAttribute(structuredData.description)}" />
    <meta property="og:title" content="${escapeAttribute(structuredData.headline)}" />
    <meta property="og:description" content="${escapeAttribute(structuredData.description)}" />
    <meta property="og:type" content="article" />
    <meta property="og:locale" content="ru_RU" />
    <meta property="og:url" content="${escapeAttribute(canonical)}" />
    <meta property="og:site_name" content="TORGNEXA" />
    <meta name="twitter:card" content="summary" />
    <link rel="canonical" href="${escapeAttribute(canonical)}" />
    <title>${escapeAttribute(structuredData.headline)}</title>
    ${stylesheetTags}
    <script type="application/ld+json">${jsonForScript(structuredData)}</script>
  </head>
  <body>
    <div id="root">${markup}</div>
  </body>
</html>
`;
await mkdir(resolve(distRoot, "docs"), {recursive: true});
await writeFile(output, html);

const robots = `User-agent: *
Allow: /docs
Disallow: /api/
Disallow: /catalog
Disallow: /orders
Disallow: /inventory
Disallow: /incidents
Disallow: /integrations
Disallow: /social
Disallow: /sync
Disallow: /counterparties
Disallow: /finance
Disallow: /approvals
Disallow: /compliance
Disallow: /notifications
Disallow: /reports
Disallow: /audit
Disallow: /settings
Disallow: /mcp
Disallow: /oidc/
Sitemap: ${siteURL}/sitemap.xml
`;
await writeFile(resolve(distRoot, "robots.txt"), robots);

const lastmod = process.env.TORGNEXA_DOCS_LASTMOD?.trim();
const lastmodTag = lastmod ? `\n    <lastmod>${escapeAttribute(lastmod)}</lastmod>` : "";
const sitemap = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>${escapeAttribute(canonical)}</loc>${lastmodTag}
    <changefreq>monthly</changefreq>
    <priority>0.8</priority>
  </url>
</urlset>
`;
await writeFile(resolve(distRoot, "sitemap.xml"), sitemap);
console.log(`Public docs prerendered: ${output}`);

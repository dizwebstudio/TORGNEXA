import {mkdir, readFile, writeFile} from "node:fs/promises";
import {fileURLToPath, pathToFileURL} from "node:url";
import {resolve} from "node:path";
import {docsURL, publicURL} from "./public-docs-config.mjs";

const frontendRoot = fileURLToPath(new URL("..", import.meta.url));
const distRoot = resolve(frontendRoot, "dist");
const ssrRoot = resolve(frontendRoot, ".prerender");

function escapeMarkup(value) {
  return value.replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("'", "&#39;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

function jsonForScript(value) {
  return JSON.stringify(value).replaceAll("<", "\\u003c");
}

function outputForPath(pathname) {
  const relative = pathname.replace(/^\/+/, "");
  if (!relative.startsWith("docs")) throw new Error(`documentation path must be under /docs: ${pathname}`);
  return resolve(distRoot, relative, "index.html");
}

const clientIndex = await readFile(resolve(distRoot, "index.html"), "utf8");
const stylesheetTags = [...clientIndex.matchAll(/<link[^>]+rel="stylesheet"[^>]*>/g)].map(match => match[0]).join("\n    ");
if (!stylesheetTags) throw new Error("client build has no stylesheet link");

const ssrModulePath = resolve(ssrRoot, "docs-entry.js");
const ssrModule = await import(pathToFileURL(ssrModulePath).href);
if (typeof ssrModule.renderDocumentation !== "function") throw new Error("SSR docs entry does not export renderDocumentation");
if (!Array.isArray(ssrModule.documentationPages) || ssrModule.documentationPages.length < 10) throw new Error("SSR docs entry has too few documentation pages");
if (!Array.isArray(ssrModule.troubleshootingFaq) || ssrModule.troubleshootingFaq.length < 3) throw new Error("SSR docs entry has no troubleshooting FAQ");

const siteURL = publicURL();
const rootPage = {
  path: "/docs",
  heading: "Документация TORGNEXA",
  title: "Документация TORGNEXA — интеграции, WMS и автоматизация",
  description: "Официальная документация TORGNEXA: подключение маркетплейсов, интернет-магазинов, платежей и CRM, работа с каталогом, WMS, маркировкой, возвратами и автоматизацией.",
};
const pages = [rootPage, ...ssrModule.documentationPages];
const canonicalFor = page => `${siteURL}${page.path}`;

function structuredDataFor(page, canonical) {
  const breadcrumbs = [
    {"@type": "ListItem", position: 1, name: "TORGNEXA", item: siteURL},
    {"@type": "ListItem", position: 2, name: "Документация", item: docsURL()},
  ];
  if (page.path !== "/docs") breadcrumbs.push({"@type": "ListItem", position: 3, name: page.heading, item: canonical});
  const graph = [
    {
      "@type": "TechArticle",
      "@id": `${canonical}#article`,
      headline: page.title,
      description: page.description,
      inLanguage: "ru-RU",
      url: canonical,
      mainEntityOfPage: canonical,
      isPartOf: {"@type": "TechArticle", "@id": `${docsURL()}#article`, url: docsURL(), name: rootPage.title},
      publisher: {"@type": "Organization", name: "TORGNEXA"},
    },
    {
      "@type": "BreadcrumbList",
      "@id": `${canonical}#breadcrumb`,
      itemListElement: breadcrumbs,
    },
  ];
  if (page.path === "/docs/troubleshooting") {
    graph.push({
      "@type": "FAQPage",
      "@id": `${canonical}#faq`,
      mainEntity: ssrModule.troubleshootingFaq.map(({question, answer}) => ({
        "@type": "Question",
        name: question,
        acceptedAnswer: {"@type": "Answer", text: answer},
      })),
    });
  }
  return {
    "@context": "https://schema.org",
    "@graph": graph,
  };
}

async function writeDocumentationPage(page, markup) {
  if (!markup.includes(page.path === "/docs" ? "Документация TORGNEXA" : page.heading)) throw new Error(`prerendered docs content is incomplete: ${page.path}`);
  const canonical = canonicalFor(page);
  const structuredData = structuredDataFor(page, canonical);
  const html = `<!doctype html>
<html lang="ru">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <meta name="color-scheme" content="light" />
    <meta name="theme-color" content="#f5f7fb" />
    <meta name="referrer" content="same-origin" />
    <meta name="description" content="${escapeMarkup(page.description)}" />
    <meta property="og:title" content="${escapeMarkup(page.title)}" />
    <meta property="og:description" content="${escapeMarkup(page.description)}" />
    <meta property="og:type" content="article" />
    <meta property="og:locale" content="ru_RU" />
    <meta property="og:url" content="${escapeMarkup(canonical)}" />
    <meta property="og:site_name" content="TORGNEXA" />
    <meta name="twitter:card" content="summary" />
    <link rel="canonical" href="${escapeMarkup(canonical)}" />
    <title>${escapeMarkup(page.title)}</title>
    ${stylesheetTags}
    <script type="application/ld+json">${jsonForScript(structuredData)}</script>
  </head>
  <body>
    <div id="root">${markup}</div>
  </body>
</html>
`;
  const output = outputForPath(page.path);
  await mkdir(resolve(output, ".."), {recursive: true});
  await writeFile(output, html);
}

for (const page of pages) {
  const markup = page.id ? ssrModule.renderDocumentation(page.id) : ssrModule.renderDocumentation();
  await writeDocumentationPage(page, markup);
}

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
if (lastmod && !/^\d{4}-\d{2}-\d{2}$/.test(lastmod)) throw new Error("TORGNEXA_DOCS_LASTMOD must use YYYY-MM-DD");
const lastmodTag = lastmod ? `\n    <lastmod>${escapeMarkup(lastmod)}</lastmod>` : "";
const sitemap = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${pages.map(page => `  <url>
    <loc>${escapeMarkup(canonicalFor(page))}</loc>${lastmodTag}
    <changefreq>monthly</changefreq>
    <priority>${page.path === "/docs" ? "0.9" : "0.7"}</priority>
  </url>`).join("\n")}
</urlset>
`;
await writeFile(resolve(distRoot, "sitemap.xml"), sitemap);
console.log(`Public docs prerendered: ${pages.length} pages under ${distRoot}/docs`);

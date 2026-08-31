import {readFile} from "node:fs/promises";
import {resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {docsURL} from "./public-docs-config.mjs";

const frontendRoot = fileURLToPath(new URL("..", import.meta.url));
const distRoot = resolve(frontendRoot, "dist");
const files = {
  robots: resolve(distRoot, "robots.txt"),
  sitemap: resolve(distRoot, "sitemap.xml"),
};
const [robots, sitemap] = await Promise.all(Object.values(files).map(file => readFile(file, "utf8")));
const canonical = docsURL();
const siteURL = canonical.replace(/\/docs$/, "");
const urls = [...sitemap.matchAll(/<loc>([^<]+)<\/loc>/g)].map(match => match[1]);
const uniqueUrls = new Set(urls);
const checks = [
  [urls.length === 21, "sitemap contains the documentation hub and 20 topical pages"],
  [uniqueUrls.size === urls.length, "sitemap URLs are unique"],
  [urls.includes(canonical), "sitemap contains the documentation hub"],
  [robots.includes("Allow: /docs") && robots.includes("Disallow: /api/"), "robots policy protects private routes"],
  [robots.includes(`Sitemap: ${siteURL}/sitemap.xml`), "robots points to sitemap"],
  [sitemap.includes("<urlset") && !sitemap.includes("#"), "sitemap is a clean XML URL list"],
];

const pages = await Promise.all(urls.map(async url => {
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    return {url, html: "", error: "invalid sitemap URL"};
  }
  if (parsed.origin !== new URL(canonical).origin || (parsed.pathname !== "/docs" && !parsed.pathname.startsWith("/docs/"))) {
    return {url, html: "", error: "sitemap URL is outside the public docs tree"};
  }
  const pathname = parsed.pathname.replace(/\/+$/, "") || "/";
  try {
    const html = await readFile(resolve(distRoot, pathname.replace(/^\/+/, ""), "index.html"), "utf8");
    return {url, html, error: ""};
  } catch {
    return {url, html: "", error: "static HTML file is missing"};
  }
}));

const titles = pages.map(({html}) => html.match(/<title>([^<]+)<\/title>/)?.[1] ?? "");
checks.push(
  [pages.every(page => !page.error), "every sitemap URL has a static HTML file"],
  [pages.every(({html}) => (html.match(/<h1\b/g) ?? []).length === 1), "every topic page has exactly one H1"],
  [new Set(titles).size === titles.length && titles.every(Boolean), "every page has a unique title"],
  [pages.every(({url, html}) => html.includes(`<link rel="canonical" href="${url}" />`)), "every page has a self-referencing canonical URL"],
  [pages.every(({html}) => html.includes('type="application/ld+json"') && html.includes("BreadcrumbList")), "every page has article and breadcrumb JSON-LD"],
  [pages.every(({html}) => !html.includes("/src/main.tsx") && !html.includes('type="module"')), "public HTML does not require the SPA module"],
  [pages.filter(({url}) => url !== canonical).every(({html}) => html.includes('class="docs-page-guide"')), "every topical page has a short reader orientation"],
  [pages.some(({url, html}) => url.endsWith("/troubleshooting") && html.includes("FAQPage") && html.includes("Question")), "troubleshooting page has FAQ JSON-LD"],
  [pages.every(({html}) => [...html.matchAll(/<img\b[^>]*>/g)].every(([tag]) => tag.includes("alt=") && tag.includes("width=") && tag.includes("height=") && tag.includes('loading="lazy"') && tag.includes('decoding="async"'))), "every documentation screenshot has accessible dimensions and lazy loading"],
  [pages.some(({url, html}) => url.endsWith("/integrations") && html.includes("Пошаговое подключение") && html.includes("Долями")), "integration page has current connection guidance"],
  [pages.some(({url, html}) => url.endsWith("/marking") && html.includes("gtin_mismatch") && html.includes("Безопасность кодов")), "marking page has operator guidance and safety boundaries"],
  [pages.some(({url, html}) => url.endsWith("/integration-status") && html.includes("health history") && html.includes("unknown")), "integration status page explains snapshot states"],
  [pages.some(({url, html}) => url.endsWith("/ai-assistant") && html.includes("grounding_state") && html.includes("typed preview")), "AI assistant page explains evidence and previews"],
);

const failed = checks.filter(([passed]) => !passed).map(([, message]) => message);
if (failed.length) throw new Error(`Public docs check failed:\n- ${failed.join("\n- ")}`);
console.log(`Public docs check: PASS (${checks.length} assertions, ${pages.length} pages)`);

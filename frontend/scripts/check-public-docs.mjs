import {readFile} from "node:fs/promises";
import {resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {docsURL} from "./public-docs-config.mjs";

const frontendRoot = fileURLToPath(new URL("..", import.meta.url));
const distRoot = resolve(frontendRoot, "dist");
const files = {
  html: resolve(distRoot, "docs", "index.html"),
  robots: resolve(distRoot, "robots.txt"),
  sitemap: resolve(distRoot, "sitemap.xml"),
};
const [html, robots, sitemap] = await Promise.all(Object.values(files).map(file => readFile(file, "utf8")));
const canonical = docsURL();
const checks = [
  [html.includes("<h1>Документация TORGNEXA</h1>"), "prerendered HTML has the canonical H1"],
  [html.includes("<title>Документация TORGNEXA — интеграции, каталог и синхронизация</title>"), "prerendered HTML has the title"],
  [html.includes('name="description"'), "prerendered HTML has the description"],
  [html.includes(`<link rel="canonical" href="${canonical}" />`), "prerendered HTML has the canonical URL"],
  [html.includes('type="application/ld+json"'), "prerendered HTML has JSON-LD"],
  [html.includes("Пошаговое подключение") && html.includes("Долями"), "prerendered HTML has current integration content"],
  [!html.includes("/src/main.tsx") && !html.includes('type="module"'), "docs HTML does not require the SPA bundle"],
  [robots.includes("Allow: /docs") && robots.includes("Disallow: /api/"), "robots policy protects private routes"],
  [robots.includes(`Sitemap: ${canonical.replace(/\/docs$/, "")}/sitemap.xml`), "robots points to sitemap"],
  [sitemap.includes(`<loc>${canonical}</loc>`) && !sitemap.includes("#"), "sitemap contains canonical docs URL only"],
];
const failed = checks.filter(([passed]) => !passed).map(([, message]) => message);
if (failed.length) throw new Error(`Public docs check failed:\n- ${failed.join("\n- ")}`);
console.log(`Public docs check: PASS (${checks.length} assertions)`);

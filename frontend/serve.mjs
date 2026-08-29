import { createServer } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { dirname, extname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(process.env.FRONTEND_ROOT ?? resolve(dirname(fileURLToPath(import.meta.url)), "dist"));
const port = Number.parseInt(process.env.PORT ?? "5173", 10);
const mimeTypes = new Map([
  [".css", "text/css; charset=utf-8"],
  [".gif", "image/gif"],
  [".html", "text/html; charset=utf-8"],
  [".ico", "image/x-icon"],
  [".jpeg", "image/jpeg"],
  [".jpg", "image/jpeg"],
  [".js", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".png", "image/png"],
  [".svg", "image/svg+xml"],
  [".webp", "image/webp"],
  [".woff", "font/woff"],
  [".woff2", "font/woff2"],
]);

function headers(pathname, contentType, fileExists) {
  const immutable = fileExists && pathname.startsWith("/assets/");
  return {
    "Cache-Control": immutable ? "public, max-age=31536000, immutable" : "no-store",
    "Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' https:; img-src 'self' data: https:; font-src 'self'; base-uri 'self'; frame-ancestors 'none'; form-action 'self' https:",
    "Content-Type": contentType,
    "Referrer-Policy": "same-origin",
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
  };
}

function safePath(pathname) {
  let decoded;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    return null;
  }
  if (!decoded.startsWith("/") || decoded.includes("\\") || decoded.split("/").includes("..")) {
    return null;
  }
  const candidate = resolve(root, `.${decoded}`);
  return candidate === root || candidate.startsWith(`${root}${sep}`) ? candidate : null;
}

const server = createServer(async (request, response) => {
  if (request.method !== "GET" && request.method !== "HEAD") {
    response.writeHead(405, { Allow: "GET, HEAD" });
    response.end();
    return;
  }
  let pathname;
  try {
    pathname = new URL(request.url ?? "/", "http://frontend").pathname;
  } catch {
    response.writeHead(400);
    response.end();
    return;
  }

  let file = safePath(pathname);
  let fileExists = true;
  try {
    if (!file || !(await stat(file)).isFile()) throw new Error("not a file");
  } catch {
    file = resolve(root, "index.html");
    fileExists = false;
  }

  try {
    const body = await readFile(file);
    const contentType = mimeTypes.get(extname(file).toLowerCase()) ?? "application/octet-stream";
    response.writeHead(200, headers(pathname, contentType, fileExists));
    if (request.method === "HEAD") response.end();
    else response.end(body);
  } catch {
    response.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
    response.end("Not Found\n");
  }
});

server.listen(port, "0.0.0.0");

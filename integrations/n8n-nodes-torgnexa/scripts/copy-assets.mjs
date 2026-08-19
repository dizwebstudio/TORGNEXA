import { cp, mkdir, readdir } from 'node:fs/promises';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const nodes = join(root, 'nodes');
const dist = join(root, 'dist');

async function walk(dir) {
  const out = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...await walk(path));
    else out.push(path);
  }
  return out;
}

for (const src of await walk(nodes)) {
  if (!(src.endsWith('.node.json') || src.endsWith('.svg'))) continue;
  const dst = join(dist, 'nodes', relative(nodes, src));
  await mkdir(dirname(dst), { recursive: true });
  await cp(src, dst);
}

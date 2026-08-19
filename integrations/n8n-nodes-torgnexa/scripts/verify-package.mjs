import { access, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const root = resolve(new URL('..', import.meta.url).pathname);
const pkg = JSON.parse(await readFile(resolve(root, 'package.json'), 'utf8'));
if (pkg.devDependencies?.['@n8n/node-cli']) throw new Error('@n8n/node-cli must not be a build dependency');
if (pkg.peerDependencies?.['n8n-workflow'] !== '2.16.0') throw new Error('n8n-workflow must be exact');
for (const path of pkg.n8n.credentials.concat(pkg.n8n.nodes)) await access(resolve(root, path));
console.log('n8n package verification: PASS');

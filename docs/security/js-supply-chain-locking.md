# JavaScript supply-chain locking

Status: unsafe JavaScript publication is fail-closed.

TORGNEXA has three JavaScript/TypeScript surfaces. Their release policy is declared in
`supply-chain/js-artifacts.json` and enforced by `scripts/check-js-supply-chain.sh`.

## n8n community node

The publishable n8n package no longer depends on the large `@n8n/node-cli` build graph.
Its release build uses only exact `typescript@5.9.3`; the committed npm lockfile records the
registry URL and SHA-512 integrity and is intentionally limited to the root plus that one
build package. `n8n-workflow@2.16.0` is an exact optional peer supplied by n8n at runtime and
is not bundled into the package.

CI installs the graph with `npm ci --ignore-scripts`, builds with a pinned Node 22.16.0
runtime, runs the offline security tests, verifies package contents, and dry-runs `npm pack`.
No `npm install`, `*`, `latest`, semver ranges, Git dependencies or arbitrary tarball URLs are
permitted on this release path.

## Frontend

The browser frontend now has a committed `frontend/package-lock.json`; Community
Compose uses `npm ci --ignore-scripts` to build the loopback-only local frontend
container. Production publication remains disabled in
`supply-chain/js-artifacts.json` until the release policy explicitly admits the
artifact. This is deliberate fail-closed behavior: a local self-hosted preview
does not silently become a published TORGNEXA production artifact.

## Generated TypeScript SDK

The generated SDK has no external npm dependencies and is consumed through a
local `file:` reference by the frontend. It is not an independently published
JavaScript artifact in the current release inventory.

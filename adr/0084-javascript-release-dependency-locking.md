# ADR 0084: JavaScript production artifacts require exact lockable graphs

## Status
Accepted as a security-hardening supplement to Task 065.

## Context
The n8n package previously used wildcard build dependencies and no npm lockfile. The frontend
also had no transitive lockfile. Exact top-level versions alone are insufficient because npm
can resolve different transitive bytes later.

## Decision
Every JavaScript surface is registered in `supply-chain/js-artifacts.json`. A release-enabled
artifact must have exact dependency specifications and a committed npm v3 lockfile whose root
manifest matches package.json and whose remote packages use registry HTTPS plus SHA-512
integrity. CI uses `npm ci --ignore-scripts`; `npm install` is not a release operation.

The n8n package build graph is deliberately reduced to exact `typescript@5.9.3`; the large
`@n8n/node-cli` dev graph is not needed to compile TORGNEXA's type-only node sources. The exact
`n8n-workflow@2.16.0` relationship remains an optional runtime peer and is not bundled.

The frontend remains source-only until a reviewed frontend lockfile is committed. An unlocked
frontend can therefore not become a production artifact by accident.

## Alternatives considered
Keeping wildcard CLI dependencies was rejected because a stable source commit could resolve a
different graph later. Pinning only the direct CLI version was rejected because its large
transitive graph would still require a reviewed lock and add unnecessary install-script and
package-maintainer surface. Publishing the frontend with exact direct versions but no lock was
rejected because transitive dependencies would remain mutable.

## Compatibility impact
The n8n runtime node and credential metadata are unchanged. Development commands that depended
on `@n8n/node-cli` are replaced with compiler/package verification commands. The frontend is
unchanged as source but remains non-publishable until its transitive graph is locked.

## Migration and data impact
No database or customer-data migration is involved. The change affects package manifests,
build metadata, CI policy and generated release bytes only.

## Operational impact
Network-enabled CI installs the release-enabled n8n graph with `npm ci --ignore-scripts` using
a pinned Node version. Frontend publication is a deliberate future enablement requiring a
reviewed lockfile; repository source validation remains available without downloaded modules.

## Security and privacy impact
Floating dependency specs, arbitrary resolved URLs and missing integrity values fail policy.
The n8n build graph contains one integrity-pinned compiler package, lifecycle scripts are not
run during dependency qualification, and no unlocked frontend graph can enter production.

## Consequences
The publishable n8n graph is small, reviewable and integrity-pinned. Frontend enablement cannot
occur until its transitive graph becomes reproducible. The CI Node bootstrap action is itself
pinned by full commit SHA and registered in the Action pin manifest.

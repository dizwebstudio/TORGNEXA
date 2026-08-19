# Task 110: Public Application Documentation

## Status
`implemented`

## Objective
Publish a complete Russian-language guide to operating TORGNEXA, available without authentication and illustrated with screenshots from the running application.

## Subtasks
- [x] 110a public route and responsive documentation layout;
- [x] 110b user, administrator, integrations, security and troubleshooting content;
- [x] 110c reproducible screenshots from the local Docker deployment;
- [x] 110d accessibility, unauthenticated route, build and Docker smoke checks.

## Acceptance
- `/docs` renders without an OIDC session;
- content covers first login, navigation, workspace, users, integrations, credentials, sync, monitoring, security and troubleshooting;
- screenshots are local repository assets with captions and alt text;
- no secrets, tokens or private tenant data appear in content or screenshots;
- frontend tests and production build pass.

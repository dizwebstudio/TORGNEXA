# Migration 000024 — Current user profiles

This expand migration adds `user_profiles`, a tenant-scoped editable projection
for the authenticated user. The identity provider remains authoritative for
the username and email; the database stores only the one-way `subject_ref`,
bounded profile fields, optimistic version and mutation idempotency evidence.

The optional `picture_upload_id` references the existing upload quarantine
pipeline. The profile API accepts only a released upload whose latest security
evidence detected an image. Removing a picture clears the profile reference;
the immutable upload and scan evidence remain retained according to the upload
and privacy policies.

Row-level security, immutable identity fields, optimistic version checks and
append-only audit records protect cross-workspace access and profile changes.

The durable privacy workflow also recognizes `user_profile` subjects. Export
creates an encrypted artifact containing the profile projection, restriction
disables the workspace membership, and deletion/anonymization clears the
profile fields, detaches the avatar and disables the membership. Provider
identity fields may be cleared only inside the retention worker's explicit
`app.privacy_execution` transaction flag; ordinary application updates cannot
change them.

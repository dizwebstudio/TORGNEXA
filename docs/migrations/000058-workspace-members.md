# Workspace members migration

Migration 000058 adds the tenant-scoped application membership mapping used by Task 100. Authentication and password ownership remain with OIDC; `workspace_members` stores only a minimized email/display name, optional reviewed OIDC subject mapping, application role, status and concurrency metadata.

Forced RLS requires both organization and workspace scope. Member identity, email, invitation key and creation time are immutable. Role/status changes increment the version, are authorized by `settings.members.write`, and append audit evidence. Application code rejects any transition that would remove the final active workspace administrator.

Email is classified as contact PII. It is not emitted to events or audit summaries; tenant deletion and the privacy retention workflow own removal from PostgreSQL and derived backups according to deployment policy.

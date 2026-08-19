# Notification preferences UI migration

Migration 000059 extends the existing tenant- and recipient-scoped notification preference rows with email/SMS channels, a bounded category allow-list and timezone-aware quiet hours. Existing web UI/webhook readers and writers remain compatible through safe defaults.

Preferences contain no provider credentials, addresses or delivery payloads. Email and SMS delivery remain provider-controlled capabilities; enabling a channel cannot reveal or configure its secret material. Critical notifications bypass quiet hours, while all other messages are evaluated against channel enablement, severity, category and local time.

# Task 102: Notification Settings UI

## Status
`implemented`

## Objective
Connect the Settings screen to the existing tenant-scoped notification preference contracts and API.

## Dependencies
098, 056

## Acceptance
- load and update current-recipient channel preferences;
- fail closed for unknown channels and preference fields;
- expose loading, validation, conflict and retry states;
- do not expose remote provider credentials or delivery payload PII;
- frontend contract decoders and deterministic tests cover success and failures.

## Implementation

- production routes expose current-recipient preferences for web UI, webhook, email and SMS;
- channel enablement, minimum severity, category allow-list and timezone-aware quiet hours are persisted in PostgreSQL;
- delivery suppresses disabled, below-threshold, excluded-category and quiet-hour notifications while allowing critical alerts through DND;
- Settings loads all channels without exposing provider credentials and provides validation/retry states.
- The notification editor is isolated in the dedicated `Каналы и важность` Settings tab; switching tabs preserves an unsaved in-memory draft without persisting it in browser storage.

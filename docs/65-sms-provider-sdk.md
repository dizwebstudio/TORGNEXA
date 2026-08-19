# SMS Provider SDK

Notification Center gains a provider-neutral SMS channel for transactional operational notifications. Marketing communication must be separately permissioned and comply with consent/policy requirements.

## Port

`SMSProvider` supports send, delivery-status lookup/webhook, sender capability metadata, health and provider message IDs. Optional capabilities include templates, concatenation metadata and alphanumeric sender where available.

## Safety/operations

- phone numbers are PII and follow privacy/retention/redaction rules;
- rate/tenant quotas and abuse controls are mandatory;
- do not put secrets or highly sensitive business data in SMS bodies;
- delivery receipts are idempotent;
- fallback chains (SMS -> email/TG/MAX etc.) are policy-driven and loop-safe;
- marketing SMS requires explicit campaign/consent policy distinct from transactional alerts.

# Customer Service / CRM-lite

TORGNEXA provides a unified operational inbox, not a full general-purpose CRM.

## Sources

Marketplace questions/reviews/messages, social comments/messages, classified leads, PUDO incidents, support webhooks and manually created cases.

## Entities

Conversation, Message, Participant, Case, SLA, Assignment, Tag, Template, CustomerProfileReference.

## Requirements

- deduplicate the same remote thread/event;
- preserve remote IDs and channel context;
- respect connector capabilities for replies;
- assignment/queue/SLA/escalation;
- AI can draft/summarize/classify, but write actions follow permission and approval rules;
- PII minimized in analytics and prompt payloads.

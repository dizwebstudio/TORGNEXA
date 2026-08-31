# Telegram Connector

Task 041 adds the Telegram Bot API adapter on top of Task-020 Social Core.

The admitted baseline publishes text, one photo, 2–10 photo albums and one MP4 video to one configured channel; supports HTTPS URL buttons for text, single-photo and single-video messages. Task 192 composes the approval-bound single-message edit route, Task 193 composes the approval-bound single-message delete route, Task 194 composes verified channel-post webhooks through the host Inbox/outbox boundary and Task 195 composes webhook subscription lifecycle through the official Bot API. Provider scheduling, callback buttons, comments and analytics are outside this application admission.

Task 020 remains the owner of Content, immutable ContentVariant, Publication state, scheduling, audit and outbox evidence. Task 174 composes text, photo/album and MP4 video publication through the worker's released-upload bridge. The connector owns only Telegram protocol adaptation and bounded remote IDs.

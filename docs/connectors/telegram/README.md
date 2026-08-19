# Telegram Connector

Task 041 adds the Telegram Bot API adapter on top of Task-020 Social Core.

The admitted baseline publishes text, one photo, 2–10 photo albums and one MP4 video to one configured channel; supports HTTPS URL buttons, single-message edit and bounded deletion. Provider scheduling, inbound bot updates, callback buttons, comments and analytics are outside this admission.

Task 020 remains the owner of Content, immutable ContentVariant, Publication state, scheduling, audit and outbox evidence. The connector owns only Telegram protocol adaptation and bounded remote IDs.

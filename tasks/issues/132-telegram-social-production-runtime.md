# Task 132 — Telegram Social production runtime

## Problem

Task 041 qualified a Telegram SDK connector, while Task 020 supplied canonical
Social Core persistence. Neither was connected to the production API, worker
or frontend, so the catalog correctly kept Telegram planned.

## Acceptance criteria

- Telegram account creation, encrypted bot-token enrollment, strict negative
  `chat_id` configuration and connector health run through the normal account
  boundary.
- The production runtime admits only `social.post.text`; media, buttons,
  edit/delete and inbound updates remain unavailable until their host workflows
  are independently composed.
- Authenticated tenant API can create/list channels and create/list immediate or
  scheduled text publications through canonical Social Core state.
- A leased worker moves `scheduled -> ready -> publishing` and calls the
  provider-neutral `SocialPublisher` port.
- Successful remote identity is stored as append-only operational evidence.
  Recovery finalizes a publication with a receipt and never repeats an
  ambiguous Telegram write; missing receipt after a crashed publishing lease
  becomes `write_outcome_unknown`.
- `/social` exposes channel setup, text composition, scheduling and status
  history; the integration card routes to that working surface.
- OpenAPI, generated SDKs, catalog, documentation, migrations, tests and
  architecture review are updated. Runtime inventory becomes 11 generic
  integrations, eight working separate-surface providers and 19 planned.

## Explicit exclusions

- image/gallery/video uploads;
- URL buttons, edit and delete;
- inbound Telegram updates and comments;
- automatic retry after an ambiguous write;
- provider-owned scheduling.

# Migration 000023 — offline demo product images

This additive expand migration allows product image references to use the
frontend-bundled `/demo-images/demo-N.svg` assets. External image references
remain HTTPS-only and released upload content paths remain restricted to the
server's exact upload route. Existing image rows are unchanged.

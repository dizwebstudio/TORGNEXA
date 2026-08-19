# MoySklad Conformance Plan

Task-064 suite v1 is mandatory before admission. The provider adapter uses only synthetic credentials and the Task-029 sandbox fixture. Required evidence is the canonical 13-check report at `docs/connectors/moysklad/conformance-report.json`.

Provider-specific tests additionally cover product/order pagination, gzip intent, exact inventory decimals, mid-row inventory cursor resume, host/type-bound meta href parsing, duplicate rows, signed stock preservation, exponent/malformed stock, malformed envelopes/cursors, credential validation, normalized rate/auth/transport failures, and raw remote-body non-disclosure.

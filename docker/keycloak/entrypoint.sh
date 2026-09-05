#!/bin/sh
set -eu

# Keep Temurin's opt-in system-CA integration while retaining Keycloak's
# command-line contract: Compose passes `start`/`start-dev` as arguments.
exec /__cacert_entrypoint.sh /opt/keycloak/bin/kc.sh "$@"

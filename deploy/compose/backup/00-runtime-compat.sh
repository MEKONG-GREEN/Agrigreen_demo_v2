#!/bin/sh
# Invoked only by the upstream first-init hooks, after initdb created PG_VERSION.
set -eu
test "$(cat "$PGDATA/PG_VERSION")" = 17
printf '%s\n' postgres17-trixie-postgis3.6-v1 > "$PGDATA/.agrigreen-runtime-compat"
chmod 0600 "$PGDATA/.agrigreen-runtime-compat"

#!/bin/sh
# Refuse physical reuse across OS/glibc/PostGIS changes: logical restore into a new volume.
set -eu
case "${1:-}" in
    postgres|-*)
        data=${PGDATA:-/var/lib/postgresql/data}
        if [ -s "$data/PG_VERSION" ]; then
            marker="$data/.agrigreen-runtime-compat"
            expected=postgres17-trixie-postgis3.6-v1
            if [ ! -r "$marker" ] || [ "$(cat "$marker")" != "$expected" ]; then
                echo 'INCOMPATIBLE PGDATA: refuse old/unmarked physical volume. Use pg_dump/pg_restore into a NEW volume; see docs/dev/p0-postgres17-upgrade.md.' >&2
                exit 78
            fi
        fi
        ;;
esac
exec /usr/local/bin/docker-entrypoint.sh "$@"

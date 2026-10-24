#!/bin/sh
# Actual Linux/Docker checks. No host ports; trust auth is confined to --network none.
# Credentials for the encrypted backup stay in a temporary env file, never stdout.
set -eu
variant=${1:?postgres or backup}
case "$variant" in postgres|backup) ;; *) echo "unknown image variant" >&2; exit 2 ;; esac
suffix="$(date +%s)-$$"
image=${P0_IMAGE_TAG:-"agrigreen/p0-sec-ci-${variant}:${suffix}"}
name="ag-p0132-${variant}-${suffix}"
data="${name}-data"
repo="${name}-repo"
restored="${name}-restored"
restore_name="${name}-restore"
tmp=$(mktemp -d)
cleanup() {
    docker rm -f "$name" "$restore_name" >/dev/null 2>&1 || true
    docker volume rm "$data" "$repo" "$restored" >/dev/null 2>&1 || true
    rm -rf "$tmp"
}
trap cleanup EXIT HUP INT TERM
umask 077
printf 'PGBACKREST_REPO1_CIPHER_PASS=%s\n' "$(openssl rand -hex 32)" > "$tmp/backup.env"
docker build --pull -t "$image" "deploy/compose/$variant"
user=$(docker image inspect -f '{{.Config.User}}' "$image")
case "$user" in ''|root|root:*|0|0:*) echo "image defaults to root" >&2; exit 1 ;; esac
test "$(docker run --rm --network none --entrypoint id "$image" -u)" -ne 0
docker run --rm --network none --entrypoint sh "$image" -ec 'test ! -e /usr/local/bin/gosu'
set +e
docker run --rm --network none --user root "$image" postgres --version > "$tmp/root.log" 2>&1
root_rc=$?
set -e
test "$root_rc" = 78
grep -q 'Refuse root' "$tmp/root.log"
test "$(docker image inspect -f '{{json .Config.Healthcheck.Test}}' "$image")" != null
docker volume create "$data" >/dev/null
if [ "$variant" = backup ]; then
    docker volume create "$repo" >/dev/null
    docker run -d --name "$name" --network none --env-file "$tmp/backup.env" \
        -e POSTGRES_HOST_AUTH_METHOD=trust -v "$data:/var/lib/postgresql/data" \
        -v "$repo:/var/lib/pgbackrest" "$image" postgres \
        -c archive_mode=on -c wal_level=replica \
        -c 'archive_command=pgbackrest --stanza=agrigreen archive-push %p' >/dev/null
else
    docker run -d --name "$name" --network none -e POSTGRES_HOST_AUTH_METHOD=trust \
        -v "$data:/var/lib/postgresql/data" "$image" >/dev/null
fi
wait_healthy() {
    for _ in $(seq 1 90); do
        state=$(docker inspect -f '{{.State.Health.Status}}' "$1")
        if [ "$state" = healthy ]; then return 0; fi
        if [ "$(docker inspect -f '{{.State.Running}}' "$1")" != true ]; then break; fi
        sleep 2
    done
    echo "image did not become healthy: $1" >&2
    # Do not dump docker inspect/env or application logs containing credentials.
    return 1
}
assert_nonroot() {
    docker exec "$1" sh -ec 'test "$(id -u)" -ne 0; test "$(stat -c %u /proc/1)" -ne 0'
}
wait_healthy "$name"
assert_nonroot "$name"
docker exec "$name" psql -U postgres -v ON_ERROR_STOP=1 -d postgres -q -c \
    'CREATE EXTENSION IF NOT EXISTS postgis; CREATE EXTENSION IF NOT EXISTS pgrouting; CREATE TABLE p0132_probe (id integer PRIMARY KEY); INSERT INTO p0132_probe VALUES (132);'
test "$(docker exec "$name" psql -U postgres -d postgres -Atc 'SELECT id FROM p0132_probe')" = 132
docker restart "$name" >/dev/null
wait_healthy "$name"
assert_nonroot "$name"
test "$(docker exec "$name" psql -U postgres -d postgres -Atc 'SELECT id FROM p0132_probe')" = 132
echo "--- PASS: $variant nonroot process, HEALTHCHECK, fresh volume/extensions, restart persistence"
if [ "$variant" = backup ]; then
    docker exec "$name" pgbackrest --stanza=agrigreen stanza-create
    docker exec "$name" pgbackrest --stanza=agrigreen check
    docker exec "$name" pgbackrest --stanza=agrigreen --type=full backup
    docker exec "$name" pgbackrest --stanza=agrigreen info --output=json > "$tmp/info.json"
    # Require at least one completed full backup; a passing version check is insufficient.
    jq -e '.[0].backup | any(.type == "full")' "$tmp/info.json" >/dev/null
    docker volume create "$restored" >/dev/null
    docker run --rm --network none --env-file "$tmp/backup.env" \
        -v "$restored:/var/lib/postgresql/data" -v "$repo:/var/lib/pgbackrest:ro" \
        --entrypoint pgbackrest "$image" --stanza=agrigreen --type=default restore
    docker run -d --name "$restore_name" --network none --env-file "$tmp/backup.env" \
        -e POSTGRES_HOST_AUTH_METHOD=trust -v "$restored:/var/lib/postgresql/data" \
        -v "$repo:/var/lib/pgbackrest:ro" "$image" postgres -c archive_mode=off >/dev/null
    wait_healthy "$restore_name"
    assert_nonroot "$restore_name"
    test "$(docker exec "$restore_name" psql -U postgres -d postgres -Atc 'SELECT id FROM p0132_probe')" = 132
    echo '--- PASS: encrypted pgBackRest full backup and restore into clean nonroot volume'
fi
# A preexisting volume cannot be silently accepted after losing/mismatching its marker.
docker exec "$name" rm /var/lib/postgresql/data/.agrigreen-runtime-compat
docker restart "$name" >/dev/null
for _ in $(seq 1 20); do
    [ "$(docker inspect -f '{{.State.Running}}' "$name")" = false ] && break
    sleep 1
done
test "$(docker inspect -f '{{.State.Running}}' "$name")" = false
test "$(docker inspect -f '{{.State.ExitCode}}' "$name")" = 78
echo "--- PASS: $variant rejects preexisting physical PGDATA without compatibility marker"

#!/bin/sh
# Actual compatibility rehearsal: old PostGIS3.5 -> logical dump -> clean trixie/3.6.
# No host ports/production data, no existing volumes reused or deleted.
set -eu
suffix="$(date +%s)-$$"
image="agrigreen/p0132-logical:${suffix}"
old="ag-p0132-old-${suffix}"
new="ag-p0132-new-${suffix}"
oldvol="${old}-data"
newvol="${new}-data"
tmp=$(mktemp -d)
cleanup() {
    docker rm -f "$old" "$new" >/dev/null 2>&1 || true
    docker volume rm "$oldvol" "$newvol" >/dev/null 2>&1 || true
    rm -rf "$tmp"
}
trap cleanup EXIT HUP INT TERM
umask 077
docker build --pull -t "$image" deploy/compose/postgres
docker volume create "$oldvol" >/dev/null
docker volume create "$newvol" >/dev/null
docker run -d --name "$old" --network none --user postgres \
    -e POSTGRES_HOST_AUTH_METHOD=trust -v "$oldvol:/var/lib/postgresql/data" postgis/postgis:17-3.5 >/dev/null
wait_pg() {
    for _ in $(seq 1 90); do
        if docker exec "$1" pg_isready -q -h 127.0.0.1 -U postgres; then return 0; fi
        if [ "$(docker inspect -f '{{.State.Running}}' "$1")" != true ]; then break; fi
        sleep 2
    done
    echo "PostgreSQL rehearsal did not become ready" >&2
    return 1
}
wait_pg "$old"
docker exec "$old" createdb -U postgres migration_probe
docker exec -i "$old" psql -U postgres -d migration_probe -v ON_ERROR_STOP=1 -q <<'SQL'
CREATE EXTENSION postgis;
CREATE ROLE p0132_reader NOLOGIN;
CREATE COLLATION p0132_vi (provider=icu, locale='vi-VN');
CREATE TABLE p0132_probe (id integer PRIMARY KEY, tenant integer NOT NULL, label text COLLATE p0132_vi NOT NULL, geom geometry(Point,4326));
CREATE INDEX p0132_label_idx ON p0132_probe (label);
CREATE INDEX p0132_geom_idx ON p0132_probe USING gist (geom);
INSERT INTO p0132_probe VALUES (1,1,'An Giang',ST_SetSRID(ST_Point(105,10),4326)),(2,2,'Đồng Tháp',ST_SetSRID(ST_Point(106,11),4326));
ALTER TABLE p0132_probe ENABLE ROW LEVEL SECURITY;
CREATE POLICY p0132_tenant ON p0132_probe TO p0132_reader USING (tenant = current_setting('p0132.tenant')::integer);
GRANT USAGE ON SCHEMA public TO p0132_reader;
GRANT SELECT ON p0132_probe TO p0132_reader;
SQL
test "$(docker exec "$old" psql -U postgres -d migration_probe -qAtc "SELECT extversion LIKE '3.5%' FROM pg_extension WHERE extname='postgis'")" = t
# Read-only old PGDATA mount must be refused before any server startup/change.
set +e
docker run --rm --network none -v "$oldvol:/var/lib/postgresql/data:ro" "$image" > "$tmp/guard.log" 2>&1
rc=$?
set -e
test "$rc" = 78
grep -q 'INCOMPATIBLE PGDATA' "$tmp/guard.log"
docker exec "$old" pg_dump -U postgres --format=custom migration_probe > "$tmp/probe.dump"
test -s "$tmp/probe.dump"
docker run -d --name "$new" --network none -e POSTGRES_HOST_AUTH_METHOD=trust \
    -v "$newvol:/var/lib/postgresql/data" "$image" >/dev/null
wait_pg "$new"
# Client-supplied roles from the source are created before restore, preserving grants/RLS.
docker exec "$new" psql -U postgres -v ON_ERROR_STOP=1 -q -c 'CREATE ROLE p0132_reader NOLOGIN;'
docker exec "$new" createdb -U postgres migration_probe
docker exec -i "$new" pg_restore -U postgres --exit-on-error -d migration_probe < "$tmp/probe.dump"
test "$(docker exec "$new" psql -U postgres -d migration_probe -qAtc "SELECT extversion LIKE '3.6%' FROM pg_extension WHERE extname='postgis'")" = t
test "$(docker exec "$new" psql -U postgres -d migration_probe -qAtc 'SELECT count(*) FROM p0132_probe')" = 2
test "$(docker exec "$new" psql -U postgres -d migration_probe -qAtc "SELECT ST_AsText(geom) FROM p0132_probe WHERE id=1")" = 'POINT(105 10)'
test "$(docker exec "$new" psql -U postgres -d migration_probe -qAtc "SET ROLE p0132_reader; SET p0132.tenant='1'; SELECT string_agg(label,',') FROM p0132_probe")" = 'An Giang'
# The text/spatial indexes are rebuilt by logical restore on the new ICU/glibc.
docker exec "$new" psql -U postgres -d migration_probe -qAtc \
    "SET enable_seqscan=off; EXPLAIN SELECT label FROM p0132_probe WHERE label='Đồng Tháp'" | grep -q p0132_label_idx
test "$(docker exec "$new" psql -U postgres -d migration_probe -qAtc "SELECT count(*) FROM pg_collation WHERE collname='p0132_vi' AND collversion IS DISTINCT FROM pg_collation_actual_version(oid)")" = 0
test "$(docker exec "$new" cat /var/lib/postgresql/data/.agrigreen-runtime-compat)" = postgres17-trixie-postgis3.6-v1
echo '--- PASS: old physical volume refused; logical PostGIS3.5->3.6 restore into NEW trixie volume preserves rows, geometry, grants/RLS and rebuilt text indexes'

# PostgreSQL 17 (same major) + PostGIS 3.6 on maintained Debian 13 trixie.
# Upstream froze 17-3.5/bullseye in October 2026. OS/glibc change requires logical
# dump/restore into a NEW volume; the entrypoint refuses old/unmarked physical data.
FROM postgis/postgis:17-3.6
RUN apt-get update \
 && apt-get upgrade -y --no-install-recommends \
 && apt-get install -y --no-install-recommends postgresql-17-pgrouting \
 && rm -rf /var/lib/apt/lists/* \
 && rm -f /usr/local/bin/gosu \
 && test -f /usr/share/postgresql/17/extension/pgrouting.control \
 && install -d -o postgres -g postgres -m 0700 /var/lib/postgresql/data \
 && install -d -o postgres -g postgres -m 3775 /var/run/postgresql
COPY --chown=postgres:postgres agrigreen-entrypoint.sh /usr/local/bin/agrigreen-entrypoint.sh
COPY --chown=postgres:postgres 00-runtime-compat.sh /docker-entrypoint-initdb.d/00-runtime-compat.sh
RUN chmod 0755 /usr/local/bin/agrigreen-entrypoint.sh
# gosu chỉ phục vụ entrypoint chạy root; wrapper từ chối root, không cần helper cũ.
USER postgres:postgres
ENTRYPOINT ["/usr/local/bin/agrigreen-entrypoint.sh"]
CMD ["postgres"]
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=6 \
  CMD pg_isready -q -h 127.0.0.1 -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-${POSTGRES_USER:-postgres}}" || exit 1

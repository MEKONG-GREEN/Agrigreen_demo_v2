# PostgreSQL 17 + PostGIS + pgBackRest (MIT) cho sao lưu Local/Dev/CI (mục 22).
# Cùng image nền với dịch vụ postgres của docker-compose.yml; kho pgdg đã có sẵn trong image.
FROM postgis/postgis:17-3.5
RUN apt-get update \
 && apt-get install -y --no-install-recommends pgbackrest \
 && rm -rf /var/lib/apt/lists/* \
 && install -d -o postgres -g postgres -m 0750 /var/lib/pgbackrest /var/log/pgbackrest /var/spool/pgbackrest /tmp/pgbackrest
COPY pgbackrest.conf /etc/pgbackrest/pgbackrest.conf

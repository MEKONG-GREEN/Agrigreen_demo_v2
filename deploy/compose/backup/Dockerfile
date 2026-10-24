# PostgreSQL 17 + PostGIS + pgRouting + pgBackRest (MIT) cho sao lưu Local/Dev/CI (mục 22).
# Cùng image nền với dịch vụ postgres của docker-compose.yml; kho pgdg đã có sẵn trong image.
FROM postgis/postgis:17-3.5

# 12/10/2026 — PGDG đã rút kho bullseye khỏi apt.postgresql.org. Chú thích đầy đủ
# ở deploy/compose/postgres/Dockerfile. Hai Dockerfile này phải sửa CÙNG NHAU: file
# kia làm đỏ job "Kiểm thử" và "Compose Local/Dev"; file này làm đỏ restore-drill.yml
# (chạy 02:00 thứ Hai hằng tuần, dựng stack với --build).
#
# Chỉ kho pgdg mất; deb.debian.org/debian bullseye vẫn còn, nên pgbackrest
# (gói của Debian) không bị ảnh hưởng — chỉ postgresql-17-pgrouting cần kho pgdg.
RUN sed -i -E 's#(https?://)apt\.postgresql\.org#\1apt-archive.postgresql.org#g' \
      /etc/apt/sources.list.d/pgdg.list \
 && apt-get update -o Acquire::Check-Valid-Until=false \
 && apt-get upgrade -y --no-install-recommends \
 && apt-get install -y --no-install-recommends pgbackrest postgresql-17-pgrouting \
 && rm -rf /var/lib/apt/lists/* \
 && test -f /usr/share/postgresql/17/extension/pgrouting.control \
 && install -d -o postgres -g postgres -m 0750 /var/lib/pgbackrest /var/log/pgbackrest /var/spool/pgbackrest /tmp/pgbackrest
COPY pgbackrest.conf /etc/pgbackrest/pgbackrest.conf

# Named volumes mới kế thừa ownership này; volume cũ phải đã thuộc postgres.
# Entrypoint chính thức vẫn khởi tạo DB nhưng không cần quyền root/chown lúc chạy.
RUN install -d -o postgres -g postgres -m 0700 /var/lib/postgresql/data \
 && install -d -o postgres -g postgres -m 3775 /var/run/postgresql
USER postgres:postgres
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=6 \
  CMD pg_isready -q -h 127.0.0.1 -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-${POSTGRES_USER:-postgres}}" || exit 1

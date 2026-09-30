# Ảnh operand CloudNativePG cho AgriGreen (G4): PostgreSQL 17 + PostGIS 3.5 (ảnh postgis chính thức
# của dự án CNPG) + pgRouting (pf/waterway, dbadmin.Extensions). Cùng phiên bản với deploy/compose/postgres.
# Sao lưu pgBackRest chạy trong sidecar của plugin CNPG-I (Dalibo) — không cài vào ảnh này.
#   docker build -t registry.example.vn/agrigreen/cnpg-postgis-pgrouting:17-3.5 deploy/k8s/images/postgres-cnpg
# Ghim digest khi phát hành; ký ảnh (mục 20).
FROM ghcr.io/cloudnative-pg/postgis:17-3.5
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends postgresql-17-pgrouting \
 && rm -rf /var/lib/apt/lists/*
# uid 26 = postgres trong ảnh CNPG.
USER 26

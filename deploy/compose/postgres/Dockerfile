# PostgreSQL 17 + PostGIS + pgRouting (GPL-2.0, dùng như extension của DB — không liên kết vào mã ứng dụng)
# cho pf/waterway (G2, mục 16). Kho pgdg đã có sẵn trong image postgis. CI (job test) và Compose dùng chung.
# Chỉ dùng pgr_dijkstra và các hàm truy vấn đường đi có ở cả pgRouting 3.x và 4.x; dựng topology bằng Go.
FROM postgis/postgis:17-3.5
RUN apt-get update \
 && apt-get install -y --no-install-recommends postgresql-17-pgrouting \
 && rm -rf /var/lib/apt/lists/*

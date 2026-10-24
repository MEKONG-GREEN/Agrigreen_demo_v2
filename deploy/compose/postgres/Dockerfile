# PostgreSQL 17 + PostGIS + pgRouting (GPL-2.0, dùng như extension của DB — không liên kết vào mã ứng dụng)
# cho pf/waterway (G2, mục 16). Kho pgdg đã có sẵn trong image postgis. CI (job test) và Compose dùng chung.
# Chỉ dùng pgr_dijkstra và các hàm truy vấn đường đi có ở cả pgRouting 3.x và 4.x; dựng topology bằng Go.
FROM postgis/postgis:17-3.5

# -----------------------------------------------------------------------------
# 12/10/2026 — PGDG đã rút kho bullseye khỏi apt.postgresql.org (Debian 11 hết
# hỗ trợ), nên "apt-get update" báo:
#
#   E: The repository 'http://apt.postgresql.org/pub/repos/apt bullseye-pgdg
#      Release' does not have a Release file.
#
# postgis/postgis:17-3.5 là image Debian 11 (bullseye) — bảng tag của
# postgis/docker-postgis ghi rõ như vậy, và không có biến thể bookworm cho
# 17-3.5. Biến thể Debian mới duy nhất là 18-3.6 (trixie), tức PostgreSQL 18:
# ĐỔI SANG ĐÓ LÀ NÂNG MAJOR, phải pg_upgrade cả cơ sở dữ liệu demo. Không làm.
#
# Các gói vẫn còn nguyên ở bản lưu trữ apt-archive.postgresql.org. Dưới đây chỉ
# ĐỔI TÊN MÁY CHỦ trong pgdg.list bằng sed, giữ nguyên "signed-by=" và danh sách
# thành phần mà image gốc đã khai — an toàn hơn là ghi lại cả dòng.
#
# Acquire::Check-Valid-Until=false: tệp Release trong kho lưu trữ đã đóng băng
# nên có thể quá hạn Valid-Until; không tắt kiểm tra chữ ký, chỉ tắt kiểm tra
# ngày.
#
# Khi đội nâng PostgreSQL lên 18 thì xoá cả khối sed này.
# -----------------------------------------------------------------------------
RUN sed -i -E 's#(https?://)apt\.postgresql\.org#\1apt-archive.postgresql.org#g' \
      /etc/apt/sources.list.d/pgdg.list \
 && apt-get update -o Acquire::Check-Valid-Until=false \
 && apt-get install -y --no-install-recommends postgresql-17-pgrouting \
 && rm -rf /var/lib/apt/lists/* \
 && test -f /usr/share/postgresql/17/extension/pgrouting.control

# Named volumes mới kế thừa ownership này; volume cũ phải đã thuộc postgres.
# Entrypoint chính thức vẫn khởi tạo DB nhưng không cần quyền root/chown lúc chạy.
RUN install -d -o postgres -g postgres -m 0700 /var/lib/postgresql/data \
 && install -d -o postgres -g postgres -m 3775 /var/run/postgresql
USER postgres:postgres
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=6 \
  CMD pg_isready -q -h 127.0.0.1 -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-${POSTGRES_USER:-postgres}}" || exit 1

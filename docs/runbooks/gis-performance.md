# GIS: bản đồ chuẩn hóa cho TMS và đo thời gian tải lớp

Áp dụng cho GIS US-INT-02 (FN-17, "≤ 500 ms") và GIS US-BASE-01 AC-2 ("tối đa 1.000 polygon/viewport, ≤ 1,5 s").

## 1. Bản đồ chuẩn hóa cho TMS

`GET /v1/geo/standard-map?bbox=minLon,minLat,maxLon,maxLat&zoom=&layers=&min_load_tons=&admin_level=&limit=` (Application API, chỉ đọc):

- MỘT lần gọi trả GeoJSON `FeatureCollection` gồm mọi lớp đã chuẩn hóa, mỗi đối tượng có `properties.layer`:
  `admin_boundaries` (tỉnh khi zoom < 10, xã khi ≥ 10, hoặc `admin_level`), `cooperatives` (ranh giới/marker HTX), `facilities` (kho/Hub kèm `capacity` — widget dung lượng), `roads` (lọc theo `min_load_tons`), `waterways` (tuyến của phiên bản mạng đang phát hành, `passable` theo `min_load_tons`).
- `layers` tóm tắt số đối tượng / cắt ngưỡng của từng lớp; `capacity_thresholds` là ngưỡng % widget; `basemaps` là bản đồ nền đã cấu hình (tham số chuỗi `gis.basemap_url:street|satellite|terrain`). Nhà cung cấp tile trong lãnh thổ chưa chọn (RB-7) → `basemaps` rỗng và `basemap_notice` giải thích.
- Không có thửa hay dữ liệu cá nhân. Mỗi lớp tối đa `limit` (≤ 1.000) đối tượng.
- Định dạng có phiên bản `schema = agrigreen.standard_map/v1`; đổi không tương thích → `v2` song song.

**Quyền** — TMS thuộc vùng mkg (mkg/logistics), chỉ gọi API công khai này, không nối DB pf (R-03):

1. Realm có client `agrigreen-tms` (client credentials, tài khoản dịch vụ `service-account-agrigreen-tms`, vai `map_consumer`). Lấy bí mật ở trang quản trị Keycloak (Clients → agrigreen-tms → Credentials) và cấp cho TMS qua kho bí mật trong lãnh thổ.
2. Đặt thuộc tính người dùng `tenant_id` của `service-account-agrigreen-tms` = tenant của triển khai (claim `tenant_id` qua scope `agrigreen-claims`; thiếu thì API trả 401).
3. TMS lấy token: `POST /realms/agrigreen/protocol/openid-connect/token` với `grant_type=client_credentials`, rồi gọi API với `Authorization: Bearer`.

Vai `map_consumer` đọc toàn tenant (lớp nền dùng chung). Người dùng bản đồ khác (`plot_viewer`, `plot_editor`, `coop_manager`, `geo_admin`, `platform_admin`) cũng gọi được, theo phạm vi của mình.

## 2. Cách đo và kết quả

Kiểm thử `TestStandardMapForTMSLatency_GIS_US_INT_02_US_BASE_01` (`internal/platform/geo/handler/standardmap_integration_test.go`) dựng bộ dữ liệu tổng hợp của một xã trên PostgreSQL 17 + PostGIS thật:

| Lớp | Số đối tượng |
|---|---|
| Thửa 60 × 60 m (nhập GeoJSON, 5 lần × 200) | 1.000 |
| Ranh giới HTX | 60 |
| Kho/Hub | 200 |
| Tuyến đường bộ (3 điểm) | 100 |
| Đoạn đường thủy (cổng `pf/waterway` giả; truy vấn khung nhìn thật được kiểm ở `TestLayerPortForStandardMap_GIS_US_INT_02`) | 300 |
| Ranh giới xã + tỉnh | 2 |

Đo qua HTTP (httptest, đủ middleware xác thực), 1 lần khởi động rồi 30 lần gọi, lấy p95:

- `GET /v1/geo/standard-map?zoom=13` (661 đối tượng, 5 lớp, vai `map_consumer`) — **ngưỡng kiểm thử p95 < 500 ms**;
- `GET /v1/geo/layers?layers=plots&zoom=15` (1.000 polygon thửa trong khung nhìn) — **ngưỡng p95 ≤ 1,5 s** (US-BASE-01 AC-2, phần máy chủ).

Chạy:

```powershell
. .\scripts\dev-env.ps1
go test -count=1 -v -run TestStandardMapForTMSLatency ./internal/platform/geo/handler/
```

Kết quả ghi nhận trên máy dev (Windows 11, PostgreSQL 17.x + PostGIS local, 30/09/2026):

| Phép đo | p95 |
|---|---|
| Bản đồ chuẩn hóa (661 đối tượng) | 45,7 ms |
| Lớp thửa zoom 15 (1.000 polygon) | 86,8 ms |

Giới hạn của phép đo: dữ liệu tổng hợp một xã, cùng máy (không có mạng giữa TMS và API, không TLS, không PgBouncer); thời gian vẽ phía trình duyệt của 1.000 polygon không nằm trong số đo (MapLibre vẽ lớp GeoJSON cục bộ). Đo lại trên hạ tầng thật khi có (UAT): cùng truy vấn, cùng khung nhìn, qua gateway.

Mọi lớp lọc khung nhìn bằng toán tử `&&` trên cột hình học có chỉ mục GiST (vd `admin_boundary_geom` của `pf_geo.admin_boundary`).

## 3. Còn chờ

- Ranh giới **địa chính (VN-2000)** chờ khách cấp dữ liệu (US-BASE-01); nhập qua `/v1/geo/plots/import` (shapefile VN-2000 → WGS84) đã có.
- Nhà cung cấp tile vệ tinh/đường phố/địa hình trong lãnh thổ (RB-7) — khi chọn, đặt tham số `gis.basemap_url:<loại>` (có phê duyệt).
- Phạm vi dữ liệu TMS cần (lớp, tần suất) và SLA độ trễ trên hạ tầng thật: chờ khách chốt.

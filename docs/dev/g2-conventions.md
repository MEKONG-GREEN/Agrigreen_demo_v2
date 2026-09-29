# Quy ước phát triển G2

Đọc cùng [g1-conventions.md](g1-conventions.md). Mọi quy ước G1 vẫn áp dụng (cấu trúc module, RLS, sổ chỉ ghi thêm, PII chỉ ở vault, kiểm thử bắt buộc, lệnh kiểm tra trước khi báo xong, tệp dùng chung). Tài liệu này chỉ ghi phần **thêm** cho G2.

## 1. Phạm vi và quyết định đã chốt (29/09/2026)

| Chủ đề | Quyết định |
|---|---|
| Phạm vi | App HTX (`platform/cropcycle`, liên thông KN ↔ HTX) **và** GIS nền, `pf/waterway`, `pf/simulation` + `optimizer/`, thiết kế `mkg/*` + `mkg/hub` + van công bố |
| Kênh nông dân | **App Flutter riêng** `apps/htx-app`, dùng chung lõi di động với `apps/field-app` qua package `packages/agrigreen_mobile` (tách ở đợt 1). Zalo Mini App để sau (ADR-0010) |
| Tài khoản nông dân | Tên đăng nhập là SĐT (chuẩn hóa `0xxxxxxxxx`); kích hoạt bằng **QR / liên kết dùng một lần** (Keycloak action token) do quản lý HTX phát. Không gửi mật khẩu, không SMS ở G2 |
| mkg | Tài liệu thiết kế chi tiết mkg theo RB-1..7, module `mkg/hub`, hợp đồng van công bố `hub_facility` v1 và `hub_capacity` v1, siết `contractcheck` |
| Mã story | Mã story trùng giữa các bộ backlog → **luôn ghi tiền tố**: `HTX US-LOG-05`, `KN US-HTX-05`, `GIS US-WATER-01`, `CGH FN-07` (trong tên kiểm thử, chú thích, tài liệu) |

Giả định tạm (chờ khách, ghi trong báo cáo nếu bám vào):
- ĐTK 1 một tenant (ADR-0009); dữ liệu HTX thuộc tenant chương trình.
- HTX US-LOG-03 "một bản ghi duy nhất" = một **đầu lô** + một dòng nhật ký cho mỗi thửa (giữ minh chứng từng thửa cho MRV).
- Người ghi nhật ký không được tự duyệt (tinh thần R-56).
- Cổng thu hoạch theo bước thu hoạch của SOP đã ghim (offset − cửa sổ), không cứng 60 ngày.
- Sản lượng nhập theo **kg** (`shared/mass`), hiển thị t/ha (decimal); âm → từ chối; bất thường → cảnh báo cần xác nhận.
- Ngưỡng khớp GPS 50 m là tham số `cropcycle.gps_match_tolerance_m`.
- Hub của MKG đi qua van công bố; kho bên thứ ba là `pf_geo.facility` thuộc tenant.
- Ranh giới HTX nằm ở **pf/geo** (không phải `mkg/hub`).

## 2. Số migration và vai DB

Vai đã khai trong `internal/shared/dbadmin/dbadmin.go` và userlist PgBouncer (CI + `deploy/compose/pgbouncer`). Không sửa các tệp đó; cần vai mới thì ghi trong báo cáo.

| Cụm | Số | Module | Schema | Vai DB |
|---|---|---|---|---|
| pf | 000020 | cropcycle: SOP, gắn giống ↔ SOP | `pf_cropcycle` | `cropcycle_app` |
| pf | 000021 | cropcycle: vụ, nhật ký, duyệt, thu hoạch, chiếu | `pf_cropcycle` | `cropcycle_app` |
| pf | 000022 | geo: ranh giới HTX, cơ sở/kho, sức chứa, đường bộ, log GPS | `pf_geo` | `geo_app` |
| pf | 000023 | waterway: đoạn, công trình, phiên bản mạng, đồ thị | `pf_waterway` | `waterway_app` |
| pf | 000024 | simulation: hub ứng viên, kịch bản, ảnh chụp đầu vào, kết quả | `pf_simulation` | `simulation_app` |
| pf | 000025 | accounts: liên kết tài khoản ↔ nông hộ, lượt kích hoạt QR | `pf_accounts` | `accounts_app` |
| pf | 000026 | extension: yêu cầu hỗ trợ (HTX US-SUPPORT-01 → KN US-TASK-01) | `pf_extension` | `extension_app` |
| pf | 000027 | masterdata: tham số và danh mục G2 (cropcycle, GIS, waterway, simulation) | `pf_masterdata` | `masterdata_app` |
| pf | 000028 | notify / sync: bổ sung G2 (người nhận theo thửa, lọc nhóm dữ liệu) | theo module | theo module |
| pf | 000029–000034 | dự phòng đợt 2 — xin số từ người tích hợp | | |
| mkg | 000002 | mkg/hub: hub, kho, lô, dịch chuyển tồn | `mkg_hub` | `hub_app` |
| mkg | 000003 | mkg/hub: bảng xuất van công bố (outbox của van) | `mkg_hub` | `hub_app` |

## 3. Hạ tầng mới

- **pgRouting** có sẵn ở cụm pf (`dbadmin.Extensions`): máy dev Windows 4.0.1; CI và Compose dùng image `deploy/compose/postgres` (PostGIS 17-3.5 + `postgresql-17-pgrouting` từ pgdg). **Chỉ dùng `pgr_dijkstra` / `pgr_ksp`** (có ở cả 3.x và 4.x). Dựng topology bằng Go (không `pgr_createTopology`, đã bỏ ở 4.0).
- **Python 3.12** cho `optimizer/`: gói riêng có `pyproject.toml`, kiểm thử `pytest`, giấy phép phụ thuộc thuộc `deploy/policies/allowed-licenses.txt` (OR-Tools Apache-2.0, HiGHS MIT). Chỉ nhận dữ liệu không định danh (RB-7).
- **Flutter**: `packages/agrigreen_mobile` (lõi chung), `apps/field-app`, `apps/htx-app`. SDK 3.47.5.

## 4. Sự kiện G2 (tên đã chốt)

| Sự kiện | Phát | Nhận |
|---|---|---|
| `pf.cropcycle.sop.published` | cropcycle | sync |
| `pf.cropcycle.season.opened` / `.completed` / `.corrected` | cropcycle | notify, GIS lớp giai đoạn |
| `pf.cropcycle.farm_log.recorded` / `.reviewed` | cropcycle | notify; MRV (G3) |
| `pf.cropcycle.harvest.declared` | cropcycle | dashboard |
| `pf.cropcycle.coop_season_area.changed` | cropcycle | mechanization → `AreaFeed.ApplySyncedArea` (CGH FN-07) |
| `pf.party.household.registered` / `.membership_changed` | party | notify (KN US-HTX-05) |
| `pf.extension.support_request.submitted`, `pf.extension.task.created` | extension | extension (tự sinh nhiệm vụ ≤ 1 phút, KN US-TASK-01), notify |
| `pf.geo.cooperative_boundary.recorded`, `pf.geo.facility.recorded` | geo | GIS |
| `pf.waterway.network.published` | waterway | simulation |
| `pf.simulation.job.requested` / `.completed` | simulation | optimizer / notify |
| `mkg.hub.facility.published`, `mkg.hub.capacity.published` (van `hub_facility`, `hub_capacity`) | mkg/hub | pf/geo (mô hình đọc hub) |

Tên dạng `pf.<module>.<thực thể>.<động từ quá khứ>`; schema ở `contracts/events/<tên>.v1.json`; struct có `//ag:event <tên> v1` (và `//ag:publication <hợp đồng>` nếu đi qua van).

## 5. Vai mới

`farmer` (nông dân, phạm vi = thửa của hộ mình qua liên kết tài khoản ↔ nông hộ), `coop_manager` (đã có), `sop_admin` (soạn SOP; phát hành cần `param_approver` khác người soạn), `waterway_editor` (vẽ/sửa mạng), `waterway_publisher` (phát hành phiên bản mạng, khác người dựng), `simulation_analyst`, `hub_operator` (mkg). Khai hằng ở `port/` của module; thêm vào realm `deploy/compose/keycloak/agrigreen-realm.json` (kiểm thử `realm_test.go` đòi đủ mọi hằng `Role*`).

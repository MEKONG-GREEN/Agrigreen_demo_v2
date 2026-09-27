# Lộ trình theo cổng

Lộ trình theo Phụ lục A của tài liệu kiến trúc v3.0. Kế hoạch lập ngày 27/09/2026. Sprint dài 2 tuần.

## G0: nền móng (mốc gốc 30/09/2026, đề xuất 10/10/2026)

- [x] Monorepo theo mục 6, cấu trúc module `port/domain/app/store/handler/module.go`
- [x] `shared/*`: tenant, ledger, outbox, inbox, idempotency, money, mass, clock, authz, problem, telemetry, ids
- [x] Mẫu bảng sổ gồm 3 lớp bảo vệ, chuỗi băm, verify, neo hằng ngày, phân vùng
- [x] `WithTenantTx` + kiểm thử rò rỉ tenant R-32 (chạy thẳng DB trên máy dev; qua PgBouncer trong CI)
- [x] Module mẫu `pf/party` (đồng ý theo mục đích) chạy end-to-end qua Application API
- [x] Công cụ cổng: archlint (R-02/03/04/10/12/14), migratecheck (R-23/25), contractcheck (R-64, van công bố)
- [x] CI: lint → test (PostgreSQL + PgBouncer) → govulncheck, go-licenses, gitleaks, trivy → build, SBOM, quét image, ký cosign
- [x] Docker Compose ĐTK 1, realm Keycloak, cấu hình PgBouncer
- [x] ADR 0001–0013
- [x] CI xanh toàn bộ trên GitHub, kể cả R-32 qua PgBouncer và build/ký 6 image (27/09/2026)
- [ ] Chạy thử Compose trên máy có Docker
- [ ] MKG ký các ADR đề xuất: 0006, 0007, 0009, 0010, 0011

## G1: ĐTK 1, ngày 15/12/2026

Người dùng là cán bộ khuyến nông, TTKNQG và Cục Kinh tế hợp tác; một tenant.

| Sprint | Thời gian | Nội dung |
|---|---|---|
| S1 | 12–25/10 | `pf/masterdata`: đơn vị hành chính có hiệu lực theo sáp nhập 2025, giống lúa, tham số có phiên bản với `param_setter`/`param_approver`. `vault/identity`: API mục 10.3, khóa X25519 theo hộ, blind index. Bề mặt Admin với MFA |
| S2 | 26/10–8/11 | `pf/geo`: thửa PostGIS, chồng lấn, diện tích geodesic. Cây phân cấp KN. HTX và nông hộ (định danh cá nhân ở vault). `apps/web-ops` Next.js. Chốt engine đồng bộ (23.1) |
| S3 | 9–22/11 | `apps/field-app` Flutter: đăng nhập offline 72 giờ, SQLCipher, hàng đợi UUIDv7 + idem_key, vẽ ranh giới offline, mã hóa ảnh ngay khi chụp |
| S4 | 23/11–6/12 | `platform/extension` (nhiệm vụ SLA 24 giờ), thư viện quy trình, dashboard 4 KPI. `platform/mechanization` (CGH: cân đối QT-01/02). Danh mục trường MRV (C-1). OTLP (ADR-0013) |
| S5 | 7–15/12 | Cứng hóa, bộ sinh dữ liệu tổng hợp (R-95), UAT theo ca kiểm thử trích từ prototype `mekong-green`, diễn tập sao lưu và phục hồi |

### Trạng thái G1 (28/09/2026)

Mã đã có đủ các phần của kế hoạch S1–S5. Đối chiếu yêu cầu → kiểm thử nằm ở [docs/uat/g1-acceptance.md](uat/g1-acceptance.md), trong đó 35 ô ghi "chưa có" hoặc "một phần".

**Đã có:**
- Backend 8 module: masterdata, party, geo, content, extension, mechanization (CGH), mrv, sync.
- Kho định danh `identity-vault`.
- Web `apps/web-ops` nối API thật.
- App Flutter `apps/field-app`: ngoại tuyến, SQLCipher, mã hóa ảnh, tương thích vector với Go.
- Bộ sinh dữ liệu tổng hợp `cmd/seed-synthetic`.
- Kịch bản E2E 14 bước; Compose đầy đủ; CI 8 job.

**Còn thiếu trước khi nghiệm thu G1:**
- Xuất báo cáo theo mẫu Bộ và email định kỳ (US-DASH-02/03, CGH FN-10).
- Nhập/xuất Excel (hiện chỉ có CSV).
- Kênh đẩy thông báo (push/Zalo) cho chỉ đạo khẩn và tin mới.
- Kiểm thử giao diện đầu–cuối trên trình duyệt; chạy app Flutter trên thiết bị Android thật.
- Diễn tập sao lưu và phục hồi.
- **Dữ liệu thật:** mã và danh mục xã ĐBSCL chính thức sau sáp nhập (seed hiện tại là dữ liệu minh họa); danh mục trường MRV (C-1).
- **Khoảng trống API** cho web: [docs/dev/web-backend-gaps.md](dev/web-backend-gaps.md), G-02..G-10.
## G2: 31/03/2027
- App HTX:
  - `platform/cropcycle`: nhật ký canh tác dạng sổ, sản lượng decimal.
  - Liên thông KN ↔ HTX bằng sự kiện.
- `pf/waterway` (pgRouting, có phiên bản), `pf/simulation`, `optimizer/`.
- Thiết kế chi tiết `mkg/*` và các hợp đồng van công bố đầu tiên.

## G3–G6
Theo Phụ lục A của tài liệu kiến trúc:
- **G3 (Q3/2027):** metrics, MRV, measurement, edge-agent. Mẫu cân C-6 phải chốt trước Q1/2027.
- **G4 (Q2/2028):** Kubernetes trong lãnh thổ.
- **G5 (Q4/2028):** Public API và metering.
- **G6 (Q1/2029):** cấu hình Phase 1, không sửa mã.

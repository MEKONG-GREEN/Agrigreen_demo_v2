# ADR-0050: `dom/hrm` = module nghiệp vụ `pf/hrm` — danh mục nhân sự ở module của chủ, không ở `cap/masterdata`

**Trạng thái:** Đề xuất — khung + danh mục nhân sự hiện thực ở W11.21 (12/10/2026, pf 000147); dữ liệu thật chờ Phòng HCTH (Q-070) · **Ngày:** 12/10/2026 (gói W11.21) · **Yêu cầu:** kiến trúc v4.1 §5 (bốn tầng), §7.1 (R-MOD-05, R-MOD-09), R-CODE-01, RB-3 (PII qua vault) · **Nguồn:** BRD Hành chính – Tổng hợp v3.0 (07/10/2026) FN-19, UR-ADM-08; đối chiếu v4.3/MKG v1.4/BRD KN Phụ lục C mục H1, H2, §2.5 · **Thành phần:** `internal/platform/hrm`, `cmd/api-platform/assembly/hrm.go`, `migrations/pf/000147_pf_hrm_catalog.up.sql`, `tools/archlint/nofloat.go` · **Liên quan:** [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0049](0049-cap-price-pf-price.md)

## Bối cảnh

- BRD HCTH v3.0 là nghiệp vụ nhân sự mới hoàn toàn (21 FN): hồ sơ tối giản, nâng bậc, đánh giá, báo cáo Sở NS01–NS06. Kế hoạch đặt module `pf/hrm` (`dom/hrm`, schema `pf_hrm`, vai `r_hrm`) ở W11.22 (H2); H1 (danh mục, FN-19) ghi tạm "`pf_masterdata` / `cap/masterdata`".
- Danh mục FN-19 (chức vụ có cờ lãnh đạo và hệ số PCCV, ngạch/CDNN có nhóm tương đương và bảng hệ số theo bậc có ngày hiệu lực, trình độ, chuyên ngành gắn nhóm theo trình độ cho NS05, danh sách khai báo sẵn) và tham số nâng bậc/quy đổi thành tích **chỉ được đọc bởi nghiệp vụ nhân sự** (hồ sơ W11.22, nâng bậc W11.25, đánh giá W11.26, báo cáo Sở W11.28). Chủ dữ liệu là Phòng HCTH; Admin sửa (BRD §VI.4).
- `pf/masterdata` (`cap/masterdata`) là năng lực dùng chung: danh mục enum Go (`Catalogs`) + tham số có phiên bản duyệt bốn mắt. Danh mục FN-19 cần thuộc tính riêng theo loại, tham chiếu chéo (chuyên ngành → nhóm của từng trình độ), bảng hệ số theo bậc kiểu số thập phân có ngày hiệu lực — không phải "mã → nhãn".
- ADR-0027 mục 5–6: phụ thuộc chỉ đi xuống; năng lực `cap/*` chỉ tách khi có ≥ 3 bên dùng (R-MOD-09); phòng ban KN không phải module — nhưng HRM không phải cấu hình phòng ban mà là một miền dữ liệu riêng có PII, vai DB riêng và vòng đời riêng (bản đối chiếu Phụ lục C: "dom/hrm mới").

## Quyết định

### 1. Dựng `pf/hrm` ngay ở W11.21

`pf/hrm` (`internal/platform/hrm`, `kind: domain`, `aka: [dom/hrm]`, `zones: [pf]`, `enable: per_tenant`) sở hữu schema `pf_hrm`; vai nhóm `r_hrm` (ADR-0028; `app_platform` là thành viên). W11.21 dựng khung (module.yaml, port, app, store, handler, gốc lắp ráp, `app/KN`, modgen, kiểm thử vai) cùng với danh mục; W11.22 thêm hồ sơ vào cùng module, cùng schema (migration riêng). Không schema `_pub` ở khung (báo cáo Sở W11.28 đọc trong module; thêm `pf_hrm_pub` khi `cap/analytics` cần).

Lý do không đặt danh mục ở `cap/masterdata`: một bên dùng (chưa đạt quy tắc ba); thuộc tính và quy tắc riêng của miền; nếu đặt ở tầng capability thì `pf/hrm` phải đọc ngược qua port mà không thêm gì cho bên khác; chủ dữ liệu (HCTH) khác chủ của tham số chung.

### 2. Dữ liệu: ba sổ chỉ ghi thêm, decimal, không PII

- `pf_hrm.catalog_item` — mọi danh mục mã → tên của FN-19, mỗi dòng một phiên bản của `(kind, code)`; phiên bản mới nhất là hiện hành; ngừng sử dụng = phiên bản `retired` (BR-01, không xóa). Thuộc tính riêng của loại ở `attrs` (jsonb) — domain kiểm chặt (trường không thuộc loại bị từ chối), số là chuỗi thập phân.
- `pf_hrm.grade_scale` — bảng hệ số của một ngạch: `numeric(5,2)[]` theo bậc, thời gian giữ bậc, `valid_from`. Tra as-of (`valid_from` lớn nhất ≤ ngày, cùng ngày lấy bản ghi sau). BR-02/AC-02: đổi hệ số không đổi quyết định đã ban hành — W11.25 tra bảng tại ngày hưởng và chụp hệ số vào sổ quyết định.
- `pf_hrm.rule_param` — tham số có phiên bản, `valid_from`: `promotion` (quy tắc nâng bậc), `achievement_conversion` (bảng quy đổi thành tích). Payload JSON kiểm chặt theo khóa (`domain.PromotionRules`, `AchievementConversion`); tham chiếu mã kỷ luật/khen thưởng phải đang dùng.
- Cột `placeholder`: dữ liệu mẫu cài khi triển khai chưa được Phòng HCTH xác nhận (Q-070); mọi lần ghi của người dùng là `false`. Màn hình gắn nhãn "Mẫu".
- Không dữ liệu cá nhân ở gói này. Hồ sơ (họ tên, ngày sinh, SĐT, dân tộc, tôn giáo …) ở W11.22 qua `vault/identity` (RB-3).
- Cấm float trong toàn module (agnofloat `floatFreeZones`).

### 3. Quyền theo cấu hình bên dùng

`port.Access` (phòng ban chủ, vai ghi, vai xem) ở gốc lắp ráp (`assembly.KNHRMAccess`: Admin ghi; Cấp Tỉnh ở phòng `hanh_chinh_tong_hop` xem). Không luồng duyệt bốn mắt (BRD: "Hệ thống không có luồng trình ký, phê duyệt cho nghiệp vụ Phòng HCTH"); mọi lần ghi là phiên bản có người ghi (sổ). Ma trận quyền hồ sơ (FN-20) ở W11.22.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Danh mục ở `pf/masterdata` (catalog + param có phiên bản), `pf/hrm` dựng ở W11.22 | Một bên dùng; thuộc tính và tham chiếu chéo riêng; thêm cạnh phụ thuộc và một vòng chuyển dữ liệu khi W11.22 cần sổ quyết định/hệ số. Bản đối chiếu H1 ghi "có (`pf_masterdata`)" — ADR này thay |
| Mỗi danh mục một bảng | 16 loại, phần lớn chỉ mã → tên; một sổ chung + `attrs` kiểm ở domain gọn hơn, vẫn giữ bảng riêng cho hai thứ có cấu trúc số (bảng hệ số, tham số) |
| Tham số nâng bậc qua `cap/workflow` duyệt bốn mắt | BRD không có luồng duyệt cho HCTH; Admin là người duy nhất ghi. Bật lại được nếu PO yêu cầu (tham số đã có phiên bản) |

## Hệ quả

- Thêm một schema, một vai nhóm (`r_hrm`) — `cmd/migrate bootstrap` phải chạy trước pf 000147.
- KN có route `/v1/hrm/*`; menu "Danh mục nhân sự" của HCTH (Cấp Tỉnh) và mục Admin trong phòng ban HCTH bấm được; các mục HCTH khác vẫn "Sắp có".
- Các gói W11.22–W11.28 thêm bảng vào `pf_hrm` (mỗi gói một migration), đọc danh mục qua `app` của module (hoặc `hrm/port.Catalog` cho module khác).

## Hiện thực

### W11.21 khung + danh mục nhân sự (12/10/2026, pf 000147)

- `internal/platform/hrm`: `module.yaml` (sinh: `ports: [Catalog]`, `owns_schema: [pf_hrm]`), `port` (Access, Catalog, lỗi `hrm.*`), `domain` (16 loại danh mục, chuẩn hóa mã ngạch — AC-03, thuộc tính theo loại, bảng hệ số, tham số, dữ liệu mặc định), `store`, `app`, `handler`.
- pf 000147: schema `pf_hrm`; sổ `catalog_item`, `grade_scale`, `rule_param` (apply_ledger, RLS); `r_hrm` SELECT/INSERT + `k_ref`.
- HTTP (`contracts/openapi/application.yaml`, mục pf/hrm): `GET /v1/hrm/status`, `POST /v1/hrm/defaults`, `GET|POST /v1/hrm/catalogs/{kind}`, `GET /v1/hrm/catalogs/{kind}/{code}/history`, `POST /v1/hrm/catalogs/{kind}/{code}/revise`, `GET /v1/hrm/grade-scales?at=`, `POST /v1/hrm/grade-scales/{grade}`, `GET /v1/hrm/grade-scales/{grade}/history`, `GET|POST /v1/hrm/rules/{key}`.
- Dữ liệu mặc định: theo BRD (mẫu ngạch có tên, nhóm tương đương, trình độ + nhóm chuyên ngành NS05, LLCT, QLNN, ngoại ngữ, tin học, Kinh/"Không"); còn lại là mẫu (chức vụ, chuyên ngành, khen thưởng, kỷ luật, biến động, lý do nghỉ, bảng hệ số NĐ 204/2004, hai tham số) — `placeholder = true`.
- Web-ops: màn `/kn/nhan-su/danh-muc` (Cấp Tỉnh – HCTH) và `/kn/phong-ban/hanh_chinh_tong_hop/danh-muc-nhan-su` (Admin); backend giả lập `src/api/mock/kn-hrm.ts`.
- Giả định: [gia-dinh-brd-2026-10.md](../kn/gia-dinh-brd-2026-10.md) mục W11.21.

### W11.22 hồ sơ nhân sự (13/10/2026, pf 000150, vault 000004)

- Sổ `pf_hrm.staff`, `staff_event`, `record_access`, `perm_rule` (apply_ledger, RLS, `r_hrm` SELECT/INSERT). Không PII: `subject_ref` trỏ kho định danh; tên trường đã đổi thay cho giá trị.
- Kho định danh mở rộng (không đổi `client.Client`): `client.Profiles` — `Amend` (họ tên/SĐT/địa chỉ, chỉ mục mù SĐT), `PutSection`/`GetSection` cho bảng `vault.section` (khóa riêng mỗi ngăn, mục đích gắn cứng: `hrm.profile`, `hrm.sensitive`); `vault.access_log.op` thêm `amend`, `section_write`, `section_read`.
- Ma trận quyền FN-20 ở `domain/perm.go`, chồng lên `KNContext` (nhóm vai suy ở server, đơn vị nội bộ W11.07); bậc = phép chiếu của sự kiện ngạch/bậc (`domain.Project`), truy vấn as-of.
- Bật dữ liệu thật sau DPIA (Q-005): cờ `AG_HRM_RECORDS`, mặc định tắt ở production.
- Giả định: [gia-dinh-brd-2026-10.md](../kn/gia-dinh-brd-2026-10.md) mục W11.22.

### W11.23 Contact (14/10/2026)

- Port `hrm/port.Directory` (`StaffDirectory`) cho `pf/contact` ([ADR-0054](0054-cap-directory-pf-contact.md)): hồ sơ chiếu tại hôm nay, chỉ mã tham chiếu và mã danh mục (không PII); áp cổng `AG_HRM_RECORDS`, không áp ma trận FN-20. Kho định danh chiếu năm sinh/email của ngăn `hrm.profile` vào thẻ danh bạ (mục đích `contact.directory`) — `pf/contact` không đọc ngăn hồ sơ.
- Giả định: [W11.23](../kn/gia-dinh/W11.23.md).

### W11.28 báo cáo Sở NS01–NS06 + dashboard nhân sự (14/10/2026, pf 20261013230821)

- `domain/reports.go`: dựng 6 biểu thuần từ phép chiếu as-of + trường kho; quy tắc V.2.0 (R1, R2 chặn; R3, R6, TK cảnh báo); dashboard FN-06.
- Sổ `pf_hrm.report_log` (apply_ledger, RLS, `r_hrm` SELECT/INSERT): lịch sử xuất/tải + nhật ký trường nhạy cảm; KHÔNG lưu tệp — tải lại dựng lại theo thời điểm biết (bitemporal: sự kiện/danh mục/tham số ghi sau `known_at` bị bỏ qua) và so SHA-256.
- Kho định danh: phần mở rộng tùy chọn `client.ProfileBatch` (`POST /v1/subjects/batch-read`, ≤ 200 chủ thể, một lượt hạn mức) — `module.yaml` của vault/identity cung cấp thêm port `ProfileBatch`.
- `UseEgress`: tải tệp qua điểm kiểm soát xuất pf/metering (ADR-0024, kênh report_export, nội bộ).
- HTTP: `/v1/hrm/reports*`, `/v1/hrm/report-exports/{id}/file`, `/v1/hrm/dashboard*`; web-ops "Dashboard nhân sự", "Biểu mẫu báo cáo Sở". Giả định: [W11.28](../kn/gia-dinh/W11.28.md).

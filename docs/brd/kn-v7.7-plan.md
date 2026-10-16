# Kế hoạch triển khai KN Phòng Trồng trọt v7.7

> Căn cứ: [kn-trong-trot-v7.7.md](kn-trong-trot-v7.7.md) (BRD), [kn-v7.7-gap.md](kn-v7.7-gap.md) (khoảng cách). Quy ước mã: `docs/dev/g1-conventions.md` (module `port/domain/app/store/handler/module.go`, migration chỉ tiến, RLS, sổ `apply_ledger`, MFA cho Admin API, `problem.*`, cursor, sự kiện `pf.<module>.<thực thể>.<động từ>`).
> Mỗi gói làm trong worktree riêng, nhánh riêng từ `sub/kn-v77`; không sửa `internal/shared/dbadmin/dbadmin.go`, `deploy/**`, `ci.yml`, migration của gói khác. Thay đổi realm Keycloak (`deploy/compose/keycloak/agrigreen-realm.json`) thuộc `deploy/**` → **gói P1 soạn bản vá và giao người tích hợp áp dụng**.

## 1. Nguyên tắc thiết kế chung

1. **Không tạo schema/vai DB mới** nếu tránh được (vai DB do người tích hợp giữ). Đặt theo module sẵn có:
   - gán vai trò, ngữ cảnh phiên, nhật ký hỗ trợ → `pf_accounts` (`accounts_app`);
   - phòng ban, khu vực → `pf_party` (`party_app`);
   - danh mục nông nghiệp mới → `pf_masterdata` (`masterdata_app`);
   - mẫu báo cáo (engine, phiên bản, gán loại), gửi Chi cục → `pf_reports` (`reports_app`);
   - phát hành, công việc, bài nộp, rà soát, kỳ, tổng hợp, lịch công tác, vật tư → `pf_extension` (`extension_app`);
   - máy móc HTX → `pf_mechanization`; thửa → `pf_geo`; kiểu thông báo mới → `pf_notify`.
   Nếu người tích hợp muốn tách module riêng (vd `pf/kntt`), đổi vị trí bảng nhưng giữ số migration đã cấp.
2. **Mọi bản ghi nghiệp vụ KN mới có `department_code`** và lọc theo phòng ban của ngữ cảnh phiên.
3. **RB-1:** mọi thao tác “sửa/mở lại/thu hồi” là sự kiện bù (xem gap §4.1).
4. **RB-3:** tên, SĐT ở vault; Log hiển thị tên qua tra `actor_ref` lúc đọc.
5. **API Log chuẩn cho màn Detail:** mỗi thực thể có `GET …/{id}/log` trả `[{event, actor_ref, actor_display, at, changes:[{field, old, new}], reason}]` (cursor). P6 dùng một component Log chung.
6. **Kiểm thử bắt buộc** (g1-conventions §5) + tên kiểm thử mang mã FN v7.7, vd `TestFN_TI_02_04_ReopenRequiresReason`.

## 2. Cấp số migration (`migrations/pf`)

Trên main số cao nhất là 000046; 000047 đã dành (roadmap); `sub/g5` dùng 000047–000050 — khi hợp nhất vào `main` (03/10/2026) đánh số lại thành **000080–000083**. **KN v7.7 dùng dải 000060–000079**; số tự do tiếp theo 000084.

| Số | Gói | Schema | Nội dung |
|---|---|---|---|
| 000060 | P1 | `pf_accounts` | `role_assignment_event` (sổ: granted, changed, revoked; vai trò, `org_ref` Nút, `department_codes[]`, `mgmt_level`, `commune_codes[]`) + `role_assignment_current`; `session_context` (context_id, user_ref, assignment_id, department_code, created_at, expires_at) |
| 000061 | P1 | `pf_accounts` | `support_session_event` + `support_action_log` (sổ: Admin, thay cho ai, chức năng, nội dung, lý do – FN-AD-04.07); `account_profile_change` (sổ log chỉnh sửa trường tài khoản: trường, cũ → mới dạng tham chiếu, không chứa PII thô) |
| 000062 | P2 | `pf_party` | `department_event` + `department` (theo đơn vị: mã, tên, mô tả, thứ tự, trạng thái active/not_open/suspended); `region` (khu vực theo tỉnh) + `org_region_event` (gán khu vực cho Nút xã, sổ) |
| 000063 | P2 | `pf_masterdata` | mở rộng tập catalog: `crop` (phân cấp `parent_code`), `material` (ĐVT), `production_model`, `report_type` (loại báo cáo: tuần, tháng, hàng vụ, năm, đột xuất, gửi Chi cục); thuộc tính `pest.affected_crops`, `pest.severity_thresholds` (tùy chọn) |
| 000064 | P2 | — | dự phòng |
| 000065 | P3 | `pf_reports` | `form_template` (mã, tên, phòng ban, cấp nhập liệu, loại báo cáo), `form_template_version` (sổ: định nghĩa lưới jsonb, trạng thái draft/published/retired, áp dụng từ kỳ, `supersedes_id`), `form_assignment_event` (gán mẫu × phiên bản vào loại báo cáo của phòng ban × cấp) |
| 000066 | P3 | `pf_reports` | `form_import` (nạp từ Excel: tệp, kết quả phân tích) |
| 000067 | P3 | — | dự phòng |
| 000068 | P4 | `pf_extension` | `report_batch_event` (đợt phát hành: mẫu[], phạm vi xã, loại kỳ, khoảng ngày, tên vụ, quy tắc hạn, số ngày chốt sau hạn, ghi chú; issued/withdrawn/deadline_changed), `report_period` (kỳ đã chia: nhãn, từ–đến, hạn, giờ chốt tự động), `report_task_event` + `report_task_current` (công việc xã × biểu × kỳ; trạng thái), `provincial_value` (giá trị cột Tỉnh giao theo xã, sổ) |
| 000069 | P4 | `pf_extension` | `submission_version` (sổ: task, phiên bản mẫu, `cells` jsonb theo chữ cái cột, kind draft/submitted, `on_behalf_of`, `supersedes_id`), `review_event` (adjustment_requested{lý do}, confirmed), `reminder_event` (thủ công/tự động, nội dung, người nhận) |
| 000070 | P4 | `pf_extension` | `period_event` (closed, auto_closed, reopened{phạm vi xã/biểu, lý do, hạn mở}, reclosed) |
| 000071 | P4 | `pf_extension` | `consolidation_run` (sổ: kỳ, biểu, phạm vi, người/“Hệ thống”, số xã có dữ liệu, số xã/ô thay đổi, ảnh chụp cấu trúc khu vực), `consolidation_cell_change` (xã, biểu, cột Excel, cũ → mới, người sửa, lúc xã gửi, loại: new/resubmit/adjusted/computed), `consolidation_snapshot` (giá trị tổng hợp theo mẫu, nguồn cho dashboard/TW/Chi cục) |
| 000072 | P4 | `pf_reports` | `dispatch_recipient` (danh bạ Chi cục: `contact_ref` vault hoặc hộp thư đơn vị), `dispatch_event` (xuất file, gửi, gửi lỗi, gửi lại: người nhận, CC, kỳ, tệp/link, trạng thái), `dispatch_file` (tệp tạo ra, liên kết kho đối tượng), `gsheet_link` (tệp Google tạo ra, quyền chia sẻ) |
| 000073 | P4 | `pf_notify` | kiểu thông báo mới: `report_task_assigned`, `report_task_due`, `report_reminder`, `report_adjustment_requested`, `report_period_reopened`, `work_schedule_reviewed`, `work_schedule_due` |
| 000074 | P4 | — | dự phòng |
| 000075 | P5 | `pf_extension` | `work_schedule_event` (tuần × cán bộ: draft_saved, submitted, approved, change_requested{lý do}, amended, locked), `work_schedule_item` (sổ: ngày, buổi/giờ, nội dung, địa điểm xã/HTX, thành phần phối hợp, ghi chú, `supersedes_id`) |
| 000076 | P5 | — | dự phòng |
| 000077 | P7 | `pf_extension` | `material_record` (sổ: xã, thời điểm ghi nhận, vật tư, số lượng numeric(14,4), ĐVT, ghi chú, `department_code`) |
| 000078 | P7 | `pf_mechanization` | nếu cần: `coop_machine_count_event` (HTX × loại máy × số lượng, lịch sử) khi bảng `machine` hiện có không đủ |
| 000079 | P7 | `pf_geo` | thuộc tính thửa v7.7: `farmer_ref` (vault), tên lô, ghi chú, diện tích khai báo, HTX, `department_code` |
| 000080–000083 | — | — | **đã dùng bởi G5** (đánh số lại khi hợp nhất: apiclient/metering foundation, apiclient, metering, notify egress_anomaly) |
| 000084–000089 | — | — | dự phòng KN (sửa sai, chỉ mục hiệu năng P8) — lấy số tự do tiếp theo khi cần |

`migratecheck` đã chấp nhận khoảng trống (000030, 000031, 000033, 000039, 000044 hiện trống).

## 3. Các gói công việc

Thứ tự phụ thuộc:

```
P2 ──► P1 ──► P4 ──► P8
 │      │      ▲      ▲
 └────► P3 ────┘      │
        P1 ──► P5 ────┤
P6 (khung: chạy ngay; màn: theo hợp đồng P1–P5, dùng mock) ──┤
P7 (sau hợp đồng P1, P3/P4, P5) ─────────────────────────────┘
```

Đợt 1 (song song): **P2**, **P6-khung**, **P3-engine** (với `department_code` dạng chuỗi), **P8-khung UAT**. Đợt 2: **P1**, **P3-designer**, **P5**. Đợt 3: **P4**, **P6-màn**, **P7**. Đợt 4: **P8-E2E**. Hợp đồng OpenAPI của từng gói chốt **trước** khi gói phụ thuộc bắt đầu (nộp `contracts/openapi/*.yaml` sớm, mock trong `apps/web-ops/src/api/mock`).

---

### P1 — Danh tính, phiên, nhiều vai trò + phòng ban
- **FN:** FN-CH-00.01, FN-AD-04.04, FN-AD-04.07; NFR bảo mật, phiên.
- **Phạm vi:**
  1. Gán vai trò (vai trò × Nút × phòng ban[] × cấp quản lý × xã phụ trách[]) — sổ + chiếu; kiểm: Nút hợp lệ theo vai trò (Cấp Quốc gia → `ttkn_national`; Cấp Tỉnh, Cấp Xã → `ttkn_province`; xã phụ trách ⊂ con của Nút; phòng ban thuộc đơn vị và đang áp dụng); Admin không phòng ban.
  2. Ngữ cảnh phiên (gap §4.5 phương án A): `GET /v1/me/contexts`, `POST /v1/me/context` → `context_id`; middleware `X-AG-Context` dựng `Principal` mới `{ActorRef, Role(kn_tw|kn_tinh|kn_xa|admin), Department, OrgRef, Communes, MFA}` và `Scope` = Nút xuống dưới ∩ phòng ban (Cấp Xã: danh sách xã). Hàm tiện ích `authz.RequireKN(ctx, role…)`, `scope.AllowsCommune`. Giữ đường cũ (`org_ref`) cho module khác.
  3. Trang mặc định theo vai trò trả trong context (`default_route`).
  4. Tài khoản: Detail + Log chỉnh sửa (`GET /v1/accounts/users/:id/log`), Phiên đang hoạt động, Lịch sử đăng nhập (Keycloak Admin REST sessions/events), khóa → logout mọi phiên, chặn tạo khi chưa có Nút/phòng ban, trùng SĐT/email → `accounts.duplicate_identity`, cảnh báo Cấp Xã chưa gán xã; nạp Excel nhiều vai trò.
  5. Chế độ hỗ trợ (FN-AD-04.07): `POST /v1/accounts/support-sessions` {vai trò, tài khoản/xã} → context đặc biệt `on_behalf_of`; mọi ghi trong context này bắt buộc `reason`; nhật ký `GET /v1/accounts/support-log`; yêu cầu `amr` có `otp`.
  6. **Bản vá realm** (giao người tích hợp): `permanentLockout:false`, `waitIncrementSeconds:900`, `maxFailureWaitSeconds:900`, `maxTemporaryLockouts` theo Q-21; client `agrigreen-web-ops` idle 14400 s; client mobile session max 86400 s; `username` = SĐT chuẩn hóa (luồng import); theme đăng nhập `agrigreen` theo Design Sample (Hình 3a) có banner lỗi, “Quên mật khẩu?” → trang liên hệ Admin; vai realm mới `kn_user` (cổng vào bề mặt KN) — vai nghiệp vụ nằm trong `role_assignment`.
- **Tệp/module:** `internal/platform/accounts/{port,domain,app,store,handler}`, `internal/shared/authz` (bổ sung nhỏ, tương thích ngược: `Principal.Context`, middleware context — ghi trong báo cáo), `cmd/api-platform/assembly/modules.go`, `migrations/pf/000060–000061`, `docs/errors.md` (mục accounts), `contracts/openapi/application.yaml` (`/v1/me/contexts`, `/v1/me/context`), `admin.yaml` (`/v1/accounts/users/:id/assignments`, `/log`, `/sessions`, `/login-history`, `/support-sessions`, `/support-log`), `contracts/events/pf.accounts.assignment.*.v1.json`; bản vá realm dạng tệp diff trong `docs/brd/` hoặc PR riêng cho người tích hợp.
- **Phụ thuộc:** P2 (bảng phòng ban; có thể bắt đầu với interface `DepartmentReader` giả).
- **Kiểm thử:** cách ly tenant; một người 3 vai trò (Hình 25b) → 3 context; Cấp Xã 2 xã chỉ thấy 2 xã; hai Cấp Xã khác phòng ban cùng xã không thấy dữ liệu nhau; phòng ban chuyển “Đang áp dụng” → context mới xuất hiện ngay; khóa tài khoản → context hết hiệu lực; Admin hỗ trợ bắt buộc lý do; idempotency.
- **Câu hỏi:** Q-03, Q-05, Q-21, Q-22.

### P2 — Cây tổ chức 3 cấp, phòng ban, khu vực, danh mục, menu theo cấu hình
- **FN:** FN-AD-04.01 (giao diện), FN-AD-04.02, FN-AD-04.03, FN-AD-04.05; UR-KT-02.
- **Phạm vi:**
  1. Phòng ban theo đơn vị (CRUD trạng thái, chặn xóa khi có tài khoản/dữ liệu, cảnh báo khi ngưng); seed 6 phòng ban An Giang (Q-02), Trồng trọt “Đang áp dụng”.
  2. Khu vực: danh mục theo tỉnh + gán cho Nút xã (sổ); trả kèm trong `/v1/orgs` và cây.
  3. Cây 3 cấp: bảo đảm mỗi xã An Giang (102 xã/phường/đặc khu) có Nút `extension_team` gắn mã ĐVHC; chặn di chuyển Nút cấp 1, 2; bảng “Ảnh hưởng” (`GET /v1/orgs/:ref/impact`: tài khoản, HTX, thửa, kỳ chưa chốt — gọi port của accounts/party/geo/extension).
  4. Danh mục: `crop` (nhóm › cây), `material` (ĐVT), `production_model`, `report_type`; `pest` gắn cây trồng; `machine_type` = 14 loại PL3 CGH (seed khi có danh sách, Q-01); cột “biểu đang dùng” (đếm tham chiếu từ P3 qua port).
  5. **Mô tả module/menu:** `GET /v1/departments/{code}/modules` trả cây menu của phòng ban theo vai trò (khóa menu, nhãn, đường dẫn, số đếm `badge_source`) — nguồn cho sidebar P6, để bật phòng ban mới không cần phát hành app. Menu Trồng trọt định nghĩa trong mã (bảng tĩnh có phiên bản), phòng ban khác “Chưa mở”.
- **Tệp:** `internal/platform/party/*`, `internal/platform/masterdata/*` (+ `seeddata/` phòng ban, khu vực An Giang, danh mục), `migrations/pf/000062–000063`, OpenAPI `admin.yaml` (`/v1/departments*`, `/v1/regions*`, `/v1/orgs/:ref/region`, `/impact`), `application.yaml` (`GET /v1/departments`, `/modules`).
- **Phụ thuộc:** không.
- **Câu hỏi:** Q-02 (tên 6 phòng ban), Q-04 (danh sách khu vực và xã thuộc từng khu vực), Q-01 (danh mục cây trồng, sâu bệnh, giống, loại máy theo mẫu).

### P3 — Engine mẫu báo cáo + 14 mẫu + trình thiết kế cho Admin
- **FN:** FN-AD-04.06; nền cho FN-XA-03.02, FN-TI-02.05, FN-TW-01.02; NFR tương thích Excel.
- **Phạm vi:**
  1. **Mô hình lưới** (jsonb có lược đồ `contracts/schemas/form-template.v1.json`): danh sách cột theo chữ cái Excel (`letter`, `title`, `kind` ∈ fixed|provincial|input|per_period|formula, `dtype`, `unit`, `required`, `catalog`, `formula`, `period_block` cho mẫu nhiều kỳ trên một dòng: khối n cột lặp theo kỳ), các dòng tiêu đề nhiều tầng với vùng gộp (`merges`), dòng chỉ tiêu (cho mẫu dạng danh sách đối tượng × mức, vd T05/T06), dòng tổng (Khu vực, Toàn tỉnh: hàm tổng, năng suất = sản lượng/diện tích), “Yêu cầu biểu mẫu” (văn bản nguyên văn), ràng buộc của mẫu (vd T03: Σ trà = DT xuống giống PL 01 — tham chiếu liên biểu cùng xã cùng kỳ), cột cố định định danh (vd A–J), nhóm cột cho “Đi tới”.
  2. **Bộ tính công thức** (Go, `internal/platform/reports/formula`): tập hàm con (cộng, trừ, nhân, chia, SUM theo dải cột cùng dòng, IF, ROUND, MIN/MAX, tham chiếu ô Tỉnh giao), chạy giống nhau ở server, web (bản TypeScript sinh từ cùng bộ ca thử) và mobile (Dart) — hoặc server là nguồn đúng, client chỉ hiển thị kết quả tạm; **bộ ca thử vàng** lấy từ tệp Excel gốc (so khớp kết quả với `excelize.CalcCellValue`).
  3. **Phiên bản:** nháp → phát hành; “áp dụng từ kỳ”; kỳ đã có số giữ phiên bản cũ; khôi phục phiên bản cũ thành nháp; nhân bản; nạp từ Excel (phân tích tiêu đề gộp, chữ cái cột → nháp để Admin chỉnh).
  4. **Gán loại báo cáo:** mẫu × phiên bản → (phòng ban, cấp, loại báo cáo); một loại nhiều mẫu; “Báo cáo đột xuất” khi có mẫu → cờ menu cho P2/P6.
  5. **Render:** dựng HTML-ready JSON cho màn nhập/xem (P6/P7), Excel đúng bố cục (`render/xlsx.go` mở rộng: tiêu đề gộp, chữ cái, dòng xã, dòng tổng tô xanh), PDF khổ ngang (ADR-0014, Roboto); “Tải mẫu Excel” chỉ dòng của xã; “Nhập từ Excel” map theo chữ cái cột.
  6. **Seed 14 mẫu** từ cấu trúc đã biết (§4) — T01, T04 dựng được ngay; các mẫu khác chờ tệp Excel (Q-01).
  7. **Trình thiết kế Admin (API):** thao tác lưới (thêm/xóa cột trước/sau, dòng tiêu đề/chỉ tiêu, gộp/tách, đặt công thức), kiểm công thức tham chiếu ô không tồn tại → `reports.formula_invalid_ref`, mẫu chưa gán loại → không phát hành được.
- **Tệp:** `internal/platform/reports/{domain/form*,formula,render,app,store,handler}`, `migrations/pf/000065–000066`, `contracts/schemas/form-template.v1.json`, OpenAPI `admin.yaml` (`/v1/reports/forms*`, `/forms/:id/versions*`, `/forms/:id/publish`, `/forms/import`, `/form-assignments*`), `application.yaml` (`GET /v1/reports/forms/available?department&level&report_type`, `GET /v1/reports/forms/:id/versions/:v/layout`), `testdata/forms/*.xlsx` (khi có tệp gốc).
- **Phụ thuộc:** P2 (danh mục, phòng ban — dùng mã chuỗi để không chặn).
- **Câu hỏi:** Q-01, Q-08, Q-17.

### P4 — Phát hành, công việc, nộp, rà soát, chốt/mở kỳ, tổng hợp, Chi cục
- **FN:** FN-TI-02.01…02.06, FN-TW-01.01/01.02, FN-XA-03.01/03.02 (phía server)/03.07.
- **Phạm vi:**
  1. **Phát hành** (`POST /v1/extension/report-batches`, `/preview`, `/:id/withdraw`, `/:id/deadline`): tự chia kỳ (tuần theo quy tắc Q-17, tháng, vụ, năm), tiêu đề cột theo kỳ, sinh `report_task` = mẫu × xã × kỳ cho cán bộ phụ trách cùng phòng ban, cảnh báo xã chưa có cán bộ, giá trị Tỉnh giao (nhập, Excel, lấy đợt trước); việc > 2 s → 202 + job (8.670 công việc).
  2. **Công việc cần làm** (`GET /v1/extension/report-tasks?mine`), trạng thái Chưa nhập/Lưu nháp/Đã nộp/Trễ hạn/Yêu cầu điều chỉnh/Đã xác nhận/Đã chốt; số đếm menu.
  3. **Bài nộp** (`POST /v1/extension/report-tasks/:id/submissions` kind draft|submitted, `GET …/history`): kiểm ô theo mẫu (chỉ cột kỳ đang mở, ràng buộc của mẫu), tính công thức, từ chối sau chốt (`extension.period_closed`), offline-safe (UUIDv7 + idem_key, đồng bộ qua `sync` entity mới `report_task`, `form_layout`).
  4. **Trạng thái nộp + nhắc:** `GET /v1/extension/report-batches/:id/status`, `POST /reminders` (chọn xã/tất cả), job tự nhắc trước hạn/khi trễ theo tham số `extension.report_reminder_*` (masterdata params); nhắc tới Cán bộ xã < 1 phút (notify + FCM data-only).
  5. **Rà soát:** `POST /v1/extension/submissions/:id/adjustment-request` (lý do bắt buộc), `/confirm`; chặn Admin tự xác nhận bản mình nhập thay.
  6. **Kỳ:** `POST /v1/extension/report-periods/:id/close`, job tự chốt (hạn + n ngày), `POST /reopen` {xã/biểu, lý do, hạn mở} → tự chốt lại khi hết hạn; cảnh báo “số liệu đã thay đổi” nếu kỳ đã gửi Chi cục.
  7. **Tổng hợp:** `GET /v1/extension/consolidations?period&form&region` (lần gần nhất), `GET /pending-changes`, `POST /v1/extension/consolidations` (Cập nhật; “Dữ liệu đã mới nhất” nếu không đổi), `GET /:id/changes`, xuất Excel theo mẫu/PDF/nhật ký; tự chạy một lần lúc chốt (“Hệ thống”); `consolidation_snapshot` là nguồn duy nhất cho dashboard Tỉnh/TW, báo cáo TW, Chi cục.
  8. **Dashboard** (`GET /v1/extension/kn-dashboard?province|region&season&period`): đọc snapshot + trạng thái nộp; < 5 s/500 người (đọc bảng tóm tắt, cache theo `consolidation_run`).
  9. **Báo cáo TW/Tỉnh/Xã:** xem bảng theo mẫu từ snapshot, so sánh cùng kỳ năm trước, nhãn Đã chốt/Tạm tính, Báo cáo của xã + thống kê của xã.
  10. **Chi cục** (`pf_reports`): danh bạ người nhận (mặc định lần trước), `POST /v1/reports/dispatches` (xuất tệp: Excel theo mẫu, PDF, link Google), `POST /:id/send` (SMTP công vụ, **đính kèm** — mở rộng `mail/smtp.go`), giới hạn dung lượng → chỉ link, gửi lại, log; bộ chuyển `reports/gsheet` **tắt mặc định** theo ADR mới (gap §4.3) — viết ADR-0022 trước khi bật.
- **Tệp:** `internal/kn/extension/*` (gói con `reportcycle`), `internal/platform/reports/{dispatch,gsheet,mail}`, `internal/platform/notify` (kiểu mới), `internal/platform/sync` (entity pull mới — ghi chú cho người tích hợp nếu đụng `sync-contract.md`), `migrations/pf/000068–000073`, OpenAPI cả hai tệp, `contracts/events/pf.extension.report_task.*`, `pf.extension.submission.*`, `pf.extension.period.*`, `pf.extension.consolidation.completed`, `docs/errors.md`, `docs/adr/0022-xuat-google-sheets.md` (Đề xuất).
- **Phụ thuộc:** P1 (context, scope), P3 (mẫu, engine), P2 (khu vực).
- **Câu hỏi:** Q-07, Q-08, Q-09, Q-14, Q-15, Q-17, Q-18, Q-20.

### P5 — Lịch công tác tuần
- **FN:** FN-XA-03.08, FN-TI-02.07.
- **Phạm vi:** lịch tuần mỗi cán bộ (mặc định tuần kế tiếp), mục theo ngày + buổi/giờ, sao chép tuần trước, cảnh báo trùng giờ, Nháp → Chờ duyệt → Đã duyệt / Yêu cầu chỉnh sửa (lý do), điều chỉnh lịch đã duyệt → phần điều chỉnh chờ duyệt lại, khóa tuần đã kết thúc, “Đăng ký muộn”, tab Chưa đăng ký, duyệt hàng loạt, nhắc người duyệt quá hạn, xuất Excel/PDF; người duyệt = Cấp Tỉnh cùng phòng ban, cùng tỉnh, cấp quản lý “Trưởng phòng” (Q-05); hạn đăng ký, hạn duyệt là tham số theo tỉnh; cán bộ tỉnh đăng ký lịch của mình (ai duyệt? Q-16).
- **Tệp:** `internal/kn/extension` (gói con `schedule`), `migrations/pf/000075`, OpenAPI `application.yaml` (`/v1/extension/work-schedules*`, `/approve`, `/request-changes`, `/bulk-approve`, `/unregistered`), sự kiện `pf.extension.work_schedule.*`, notify kiểu `work_schedule_*`.
- **Phụ thuộc:** P1.
- **Câu hỏi:** Q-05, Q-16.

### P6 — Giao diện web-ops theo Design Sample + mọi màn v7.7
- **Phạm vi:**
  1. **Khung (chạy ngay):** thay token trong `apps/web-ops/src/app/globals.css`: sidebar gradient `#123B24 → #1D5A38`, rộng 248 px (thu gọn 72 px, flyout trắng), mục ≥ 36 px, mục con 34 px, icon 18 px nét 2; active nền `#E6F6EA`, chữ `#123B24`, vạch trái 3 px `#00A82D`; nút chính `#00A82D`; nền trang `#F7FAFB`; font **Noto Serif** (h1–h3, tên màn, khối, thẻ) + **Roboto** (còn lại) — tự host tệp font (RB-7, không gọi Google Fonts lúc chạy), giấy phép OFL/Apache thuộc allow-list; footer cuối sidebar; topbar breadcrumb + chuông + menu tài khoản (Đang dùng, Đổi vai trò/phòng ban, Đổi mật khẩu, Đăng xuất).
  2. **Thành phần chung:** `ListPage` (bộ lọc dạng thẻ + Đặt lại, bảng STT, nhãn trạng thái, xem/sửa/xóa, phân trang số trang), `DetailPage` (quay lại, thanh nút, khối Nhãn–Giá trị, tab, **khung Log**), `Dashboard` (Chỉ số nóng, Chỉ số so sánh, Cảnh báo), `FormGrid` (lưới mẫu báo cáo: chữ cái cột, tiêu đề gộp, màu ô theo loại, cố định cột, cuộn ngang, “Đi tới”, ô thay đổi viền vàng ● + tooltip cũ/người/thời điểm, dòng tổng xanh), `RoleContextPicker`.
  3. **Menu từ API** (P2 `/modules`) thay cho `src/nav/nav.ts` cứng ở bề mặt Trồng trọt; menu cũ giữ sau cờ `NEXT_PUBLIC_SURFACE=legacy` cho module khác (không xóa trang).
  4. **Màn:** Chọn vai trò & phòng ban (3b), Tổng quan hệ thống Admin (3c), Dashboard TW/Tỉnh (3d/3e), Báo cáo tuần/tháng/vụ/năm TW (5), Phát hành biểu (7), Trạng thái nộp (8), Rà soát list + detail (9), Tổng hợp các xã + nhật ký (10), Chi cục (11), Duyệt lịch (12), Công việc cần làm (13), Nhập biểu (14, 15), HTX list/detail (16a/b), Máy móc (17), Vẽ thửa (18), Vật tư (19), Báo cáo của xã (20), Đăng ký lịch (21), ĐVHC (22), Cơ cấu tổ chức (23), Phòng ban (24), Người dùng list/detail (25a/b), Danh mục nông nghiệp (26), Mẫu báo cáo + trình thiết kế (27), Menu các cấp + dải hỗ trợ (28). Cấp Quốc gia: ẩn mọi nút thêm/sửa/xóa.
  5. Mock backend `src/api/mock/*` cho mọi hợp đồng mới để làm song song.
- **Tệp:** `apps/web-ops/src/{app,components,nav,auth,api}`, `apps/web-ops/public/fonts/`, Vitest + Playwright `tests/e2e/kn-*.spec.ts`.
- **Phụ thuộc:** khung: không; màn: hợp đồng OpenAPI P1–P5.
- **Câu hỏi:** Q-19 (nhận diện TTKNQG: tên sản phẩm, logo, ảnh nền, footer).

### P7 — Field-app (Cán bộ xã)
- **Phạm vi:** đăng nhập email/SĐT + PIN 6 số + offline 72 giờ + phiên 24 giờ (đã có lõi, thêm chọn context, lưu context offline); trang mặc định **Công việc cần làm** (thay `home_screen` KN); nhập biểu theo mẫu **mỗi xã một tab**, offline (tải layout phiên bản mẫu + công việc qua sync pull; bài nộp qua outbox; tự lưu nháp khi chuyển tab; báo lỗi tại ô), giao diện lưới tối ưu Android rẻ (cuộn ngang, cố định cột định danh, “Đi tới” nhóm cột); thông báo đẩy công việc mới/sắp hạn/nhắc; lịch công tác tuần (đăng ký, gửi duyệt, nhận kết quả); thu thập dữ liệu: HTX & nông hộ (thêm/sửa, PII qua vault), máy móc HTX, vẽ thửa (thêm hình tròn/vuông, tên nông dân, HTX, diện tích khai báo; chặn chồng lấn), vật tư. Ẩn màn KN cũ (chỉ đạo, thư viện, danh bạ, MRV) khỏi bề mặt Trồng trọt (giữ mã).
- **Server phần P7:** API thu thập dữ liệu (HTX theo xã phụ trách cho Cấp Xã — mở route Application API cho thêm/sửa HTX, hiện chỉ Admin import), máy móc theo HTX, vật tư, thuộc tính thửa; migration 000077–000079.
- **Tệp:** `apps/field-app/lib/{ui/screens,application}`, `packages/agrigreen_mobile/lib/data/{auth,sync}`, `internal/platform/{party,mechanization,geo,extension}`, kiểm thử Dart.
- **Phụ thuộc:** P1 (context), P3/P4 (layout, công việc, bài nộp), P5 (lịch).
- **Câu hỏi:** Q-23 (bản đồ nền offline: nguồn tile trong lãnh thổ), Q-06.

### P8 — E2E, hiệu năng, UAT
- **Phạm vi:** `docs/uat/kn-v7.7-acceptance.md` (khung ngay ở đợt 1: một dòng cho mỗi FN × AC, cột “Kiểm thử”, “Kết quả”); kịch bản E2E Go `tests/e2e/kn77_scenario_test.go` (`TestKN77AnGiangTuan36`): Admin khởi tạo → Tỉnh phát hành T01/T03/T04/T05/T06 → hai Cán bộ xã nộp (một offline) → nhắc → yêu cầu điều chỉnh → gửi lại → Cập nhật tổng hợp (kiểm ô thay đổi, nhật ký) → chốt tự động → mở lại → gửi Chi cục (SMTP giả, Google tắt) → TW xem; Playwright theo vai trò; kiểm tải dashboard 500 người dùng < 5 s; đối chiếu Excel xuất với tệp mẫu gốc (chữ cái cột, công thức).
- **Phụ thuộc:** tất cả; khung UAT không phụ thuộc.

## 4. Mẫu báo cáo T01–V06: đã biết và phải xin

Tệp Excel gốc (3 Google Sheet: “Báo cáo cây lúa, rau màu hằng tuần”, “Báo cáo cây ăn trái hằng tháng”, “Báo cáo áp dụng TBKT, CGH, CCG hằng vụ”) **không có trong đầu vào**. BRD v7.0 đã “bỏ phần chi tiết yêu cầu nhập liệu theo từng báo cáo”.

| Mẫu | Đã biết từ BRD/wireframe | Đủ để dựng? | Phải xin TTKN An Giang |
|---|---|---|---|
| **T01** PL 01 | Đủ cột A–O (STT, Khu vực, Xã, KH tỉnh giao, KHXG địa phương, Tuần 1…n tăng thêm, Lũy kế = tổng tuần, Ngày kết thúc XG, Dự kiến ngày thu hoạch), tiêu đề gộp, yêu cầu biểu mẫu nguyên văn, tên sheet | Có (bản nháp) | Tệp gốc để đối chiếu; số cột tuần tối đa; kiểu dữ liệu cột N, O (ngày); công thức Lũy kế chính xác; tên sheet theo vụ |
| **T02** PL 01B | “Số tăng thêm; bảng phụ phân trà”; xã có lúa mùa | Không | Toàn bộ cột, bảng phụ phân trà (các trà nào), danh sách xã có lúa mùa |
| **T03** PL 02 | Σ các trà = DT xuống giống PL 01; cột cùng kỳ M–O do xã nhập; tiêu đề ngày theo kỳ | Không | Danh sách trà (vd mạ, đẻ nhánh, đòng, trổ, chín…), bố cục cột A–L, ý nghĩa M–O |
| **T04** PL 03 | A–J (KH XG, DT XG, % KHXG, DT thu hoạch, năng suất, sản lượng, % DT thu hoạch), K…HR = 54 cây × 4 chỉ tiêu (DT XG, DT thu hoạch, năng suất, sản lượng), tiêu đề gộp 2 tầng nhóm cây; số lũy kế; yêu cầu biểu mẫu nguyên văn | Một phần | **Danh sách 54 cây + nhóm cây** đúng thứ tự cột; công thức E–J (tổng các cây? năng suất = sản lượng/DT) |
| **T05** PL 04 | 20 đối tượng × 3 mức; bắt buộc hằng tuần | Không | Danh sách 20 đối tượng; 3 mức (nhẹ/trung bình/nặng? đơn vị ha); có cột tổng, “trong đó nặng”, diện tích phòng trị? |
| **T06** PL 05 | 70 cặp cây – sâu bệnh × 3 mức | Không | Danh sách 70 cặp; bố cục dòng/cột |
| **T07** PL 07 | Số tăng thêm, khối 4 cột mỗi tuần; thêm công thức khối “Lũy kế thu hoạch” | Không | 4 cột của khối (DT thu hoạch, năng suất, sản lượng, …?), cột cố định đầu |
| **M01** | 30 cây × 6 cột; có cột tự tính; sheet “Tháng 7”; sửa công thức lệch cột; trước ngày 10 | Không | Danh sách 30 cây, 6 cột (DT hiện có, trồng mới, cho trái, năng suất, sản lượng…?), công thức đúng |
| **V01** PL1 3G3T | “Theo cột mẫu”; cột “Mật độ sạ ≤ 150 kg/ha” nhập số thực tế | Không | Toàn bộ cột |
| **V02** PL2 1P5G | “Theo cột mẫu”; dùng khối A–P đầu tiên | Không | Toàn bộ cột A–P |
| **V03** PL3 CGH | 14 loại máy; diện tích áp dụng | Không | Danh sách 14 loại máy (cũng là danh mục loại máy), cột số lượng/diện tích |
| **V04** PL4 CCG | 40 giống; DTXG = tổng các giống | Không | Danh sách 40 giống (đối chiếu danh mục giống lúa), nhóm giống |
| **V05** PL5 | 4 nhóm: ≤ 80; > 80 – < 100; 100–150; > 150 kg/ha | Một phần | Cột diện tích/tỷ lệ cho từng nhóm, cột tổng |
| **V06** PL6 | “Theo cột mẫu” | Không | Toàn bộ cột, danh sách mô hình (danh mục mô hình sản xuất) |
| **CC01** (Hình 27) | Mẫu cấp Tỉnh “Báo cáo gửi Chi cục”, bản nháp | Không | Có mẫu Chi cục riêng hay chỉ gửi T01–T07? Nếu có: bố cục |

**Yêu cầu gửi TTKN An Giang:** tệp Excel gốc (.xlsx, giữ công thức, không chỉ PDF) của 3 bộ Google Sheet, bản đang dùng vụ Thu Đông 2026, kèm sheet “Yêu cầu biểu mẫu”/hướng dẫn nhập liệu hiện hành; danh sách xã theo Khu vực; danh mục cây trồng, sâu bệnh, giống, loại máy, mô hình dùng trong các sheet.

## 5. Câu hỏi cho khách hàng

| Mã | Câu hỏi | Gói |
|---|---|---|
| Q-01 | Gửi tệp Excel gốc của 14 mẫu (và CC01 nếu có) cùng danh mục dùng trong mẫu (§4) | P2, P3 |
| Q-02 | Tên, mã, thứ tự 6 phòng ban của TTKN An Giang (wireframe mới có Trồng trọt; Chăn nuôi - Thủy sản; Kế hoạch - Tài chính; Thông tin - Truyền thông; 2 phòng còn lại?) | P2 |
| Q-03 | Cán bộ không có email: kích hoạt tài khoản thế nào? Chấp nhận liên kết đặt mật khẩu thay “mật khẩu tạm” gửi đi? Có cần SMS (nhà cung cấp trong nước)? | P1 |
| Q-04 | Danh sách Khu vực (huyện cũ) và xã thuộc từng Khu vực của An Giang sau sắp xếp 2025 | P2 |
| Q-05 | Danh sách giá trị “Cấp quản lý” (Chuyên viên, Phó trưởng phòng, Trưởng phòng…?); người duyệt lịch = Trưởng phòng — có ủy quyền Phó phòng? | P1, P5 |
| Q-06 | Cấp Quốc gia có quyền xem HTX, máy, thửa (ma trận ghi R) — xem ở đâu khi menu TW không có nhóm Dữ liệu? Qua drill-down dashboard? | P6, P7 |
| Q-07 | “Báo cáo năm” của Cấp Quốc gia: cộng dồn các kỳ vụ/tháng hay mẫu năm riêng? | P4 |
| Q-08 | Báo cáo Chi cục dùng mẫu riêng (CC01) hay gửi đúng T01–T07/M01/V01–V06? | P3, P4 |
| Q-09 | Email Chi cục và CC là hộp thư đơn vị hay cá nhân? (quyết định lưu ở vault) | P4 |
| Q-10 | Xuất **Word** (NFR Tích hợp) áp dụng cho báo cáo nào? | P4 |
| Q-11 | Chỉ đạo khẩn, nhiệm vụ hỗ trợ, danh bạ trực đã triển khai G1: tắt hẳn ở An Giang hay giữ ở module dùng chung? | P6 |
| Q-12 | Báo cáo Đề án 1 triệu ha (đã có) còn cần không, có chuyển thành “Báo cáo đột xuất”? | P3 |
| Q-13 | Công cụ quản trị nền tảng (tham số, xung đột đồng bộ, giám sát thiết bị) đặt trong nhóm Quản trị hệ thống của Admin? | P6 |
| Q-14 | “Email công vụ TTKN An Giang” là máy chủ thư công vụ tỉnh (SMTP trong nước) hay Google Workspace/Gmail? Văn bản chấp thuận dùng Google Drive/Sheets (máy chủ ngoài lãnh thổ) cho số liệu tổng hợp không định danh; chia sẻ link theo email người nhận hay “ai có liên kết”? | P4 |
| Q-15 | Cấu hình tự nhắc: bao nhiêu giờ/ngày trước hạn, nhắc lại khi trễ bao lâu một lần, ai nhận bản sao? | P4 |
| Q-16 | Lịch của cán bộ Cấp Tỉnh (“Lịch của tôi”) do ai duyệt? Hạn đăng ký và hạn duyệt mặc định? | P5 |
| Q-17 | Quy tắc chia kỳ: tuần ISO (Tuần 36) hay tuần thứ n của vụ (“Tuần 3 của vụ”) — wireframe dùng cả hai; tuần bắt đầu thứ Hai? tháng nộp trước ngày 10 tháng sau? | P3, P4 |
| Q-18 | Danh mục vụ (Đông Xuân, Hè Thu, Thu Đông, lúa mùa) và khoảng ngày mặc định ở An Giang | P2, P4 |
| Q-19 | Bộ nhận diện TTKNQG: tên sản phẩm, logo, ảnh nền đăng nhập, dòng footer | P6 |
| Q-20 | Có cần nạp số liệu lịch sử từ Google Sheet (các kỳ trước) để so sánh cùng kỳ năm trước? | P4 |
| Q-21 | Khóa tạm 15 phút: sau bao nhiêu lần khóa tạm thì khóa hẳn (ATTT cấp 3)? — **Đã trả lời 03/10/2026 (A3):** khóa hẳn sau 3 lần khóa tạm trong 24 giờ (`permanentLockout: true`, `maxTemporaryLockouts: 3`, `maxDeltaTimeSeconds: 86400`) | P1 |
| Q-22 | MFA cho Admin: TOTP (đã có) có đủ? Admin Mekong Green có cần tài khoản riêng cho từng người? | P1 |
| Q-23 | Bản đồ nền vệ tinh cho vẽ thửa offline: nguồn tile nào (trong lãnh thổ hoặc qua proxy ADR-0007)? | P7 |

## 6. Rủi ro chính

| Rủi ro | Giảm thiểu |
|---|---|
| Chưa có tệp Excel → 12/14 mẫu không dựng được | Engine + T01/T04 trước; trình thiết kế cho Admin nhập phần còn lại; xin Q-01 ngay |
| Công thức Excel khác kết quả | Ca thử vàng từ tệp gốc, so với `excelize.CalcCellValue` |
| Đổi mô hình phân quyền ảnh hưởng mọi module | Context là lớp bổ sung; `org_ref` cũ giữ cho module khác; chạy lại E2E G1–G4 |
| Google Sheets/RB-7 | ADR-0022, tắt mặc định, dự phòng đính kèm + link kho trong lãnh thổ |
| 8.670 công việc mỗi đợt, tổng hợp 102 xã × 54 cây × 4 cột | Sinh qua job (R-52), snapshot tổng hợp, chỉ mục theo (kỳ, biểu, xã) |

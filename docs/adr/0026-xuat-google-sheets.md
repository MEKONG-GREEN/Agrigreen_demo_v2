# ADR-0026: Xuất bản báo cáo tổng hợp sang Google Sheets (Báo cáo Chi cục, KN v7.7)

**Trạng thái:** Đề xuất · **Ngày:** 02/10/2026 · **Thành phần:** `internal/platform/reports/dispatch` (cờ, van), `internal/platform/reports/app/dispatch.go`, `pf_reports.gsheet_link` (pf 000072)

> Kế hoạch KN v7.7 (`docs/brd/kn-v7.7-plan.md` P4 mục 10) gọi tạm là "ADR-0022"; số 0022–0025 đã dùng ở nhánh G5 nên ADR này mang số **0026** (quy ước KN, `kn-common.md`).

## Bối cảnh

- BRD v7.7 FN-TI-02.06: Cấp Tỉnh xuất file toàn tỉnh (Excel theo mẫu, PDF **và/hoặc Google Sheet**) từ lần tổng hợp gần nhất và gửi email cho Chi cục ngay từ hệ thống; "link Google Sheet chia sẻ quyền xem cho người nhận"; "file vượt giới hạn đính kèm → chỉ gửi link Google Sheet". Giả định A3: TTKN An Giang cung cấp email công vụ và **tài khoản Google**.
- RB-7: mọi thứ lưu dữ liệu ở trong lãnh thổ. ADR-0007 chỉ cho dùng nguồn/đích ngoài lãnh thổ khi dữ liệu **không định danh**, qua allowlist và một ADR riêng. Google Drive/Sheets là máy chủ ngoài lãnh thổ (gap §4.3).
- Dữ liệu cần đưa lên là **số tổng hợp theo xã/khu vực/tỉnh theo mẫu** (diện tích, sản lượng, năng suất…) và tên đơn vị hành chính — không có tên cán bộ, SĐT, email (gap §4.2). Nhưng "chia sẻ quyền xem cho người nhận" sẽ gửi **địa chỉ email người nhận** cho Google — là dữ liệu cá nhân/công vụ ra ngoài lãnh thổ.
- Khách chưa trả lời Q-14 (email công vụ là SMTP trong nước hay Google Workspace; văn bản chấp thuận dùng Google; chia sẻ theo email hay "ai có liên kết").

## Quyết định (đề xuất)

1. **Bộ chuyển Google Sheet TẮT mặc định.** Chỉ bật khi đủ cả ba: `AG_REPORTS_GSHEET=on`, `AG_EXTDATA_GSHEET_ADR=ADR-0026` (ADR này đã được **chấp nhận**) và `AG_NONPII_ALLOWLIST` chứa `reports.gsheet` (cơ chế ADR-0007). Thiếu bất kỳ điều kiện nào → `POST /v1/reports/dispatches` với `formats: [gsheet]` trả `409 reports.gsheet_disabled`. Admin xem trạng thái ở `GET /v1/reports/dispatch-settings` (Admin API).
2. **Van mặc định chặn** (`dispatch.Valve`) trước khi gọi Google: chỉ cột số (decimal) và cột nhãn tổ chức (STT, Khu vực, Xã, nhãn chỉ tiêu) được đi; mọi cột chữ tự do, ngày, danh mục khác → lỗi `reports.gsheet_valve_blocked`, không tự bỏ qua. Kiểm thử `TestFN_TI_02_06_GSheetDisabledByDefaultAndValve`.
3. **Thông tin xác thực Google** (OAuth refresh token hoặc service account của TTKN tỉnh) lưu trong kho bí mật/KMS trong lãnh thổ, chỉ Admin kết nối; hiện thực `dispatch.GSheetPublisher` **chưa có trong repo** — chỉ viết khi ADR được chấp nhận. Mỗi lần tạo/chia sẻ tệp ghi `pf_reports.gsheet_link` + sự kiện sổ `dispatch_event` + điểm kiểm soát DLP (`export_audit` kind `dispatch`).
4. **Chia sẻ:** mặc định `anyone_with_link` (không gửi email người nhận cho Google; rủi ro lộ link — link chỉ đi trong email công vụ). `recipients` (chia sẻ theo email) chỉ khi khách có văn bản chấp thuận đưa địa chỉ ra ngoài lãnh thổ (`AG_REPORTS_GSHEET_SHARE=recipients`).
5. **Đường dự phòng mặc định (đang dùng):** đính kèm Excel/PDF trong email qua SMTP công vụ; vượt giới hạn `reports.dispatch_attachment_limit_mb` (tạm 10 MB) mà Google tắt → gửi **liên kết tải có hạn** do api-platform phục vụ trong lãnh thổ (`GET /v1/public/report-links/{token}`, hạn `reports.dispatch_link_ttl_hours` mặc định 168 giờ = 7 ngày, ghi sổ truy cập, qua điểm kiểm soát xuất — quyết định A4 03/10/2026; trước đó URL ký sẵn của kho đối tượng hạn 15 phút).

## Hệ quả

- Không có dữ liệu nào ra ngoài lãnh thổ cho tới khi ADR được chấp nhận và cấu hình bật — UAT FN-TI-02.06 phần "link Google Sheet" ghi **một phần (chờ khách Q-14)**.
- ~~Liên kết tải dự phòng hạn 15 phút là quá ngắn cho người nhận ngoài hệ thống~~ — **đã chốt A4 (03/10/2026):** liên kết 7 ngày do api-platform phục vụ, mã thông báo ngẫu nhiên chỉ lưu băm, sổ `pf_reports.dispatch_link_access`, Admin thu hồi được (`POST /v1/reports/dispatches/{id}/revoke-links`), hết hạn/thu hồi → 410 (pf 000085).
- Nếu "email công vụ" là Google Workspace (Q-14), chính đường SMTP cũng ra ngoài lãnh thổ: nội dung email chỉ chứa số tổng hợp; áp cùng ADR này.

## Phương án đã xét

- Bật Google mặc định như BRD: vi phạm RB-7 khi chưa có văn bản chấp thuận — loại.
- Thay Google bằng bảng tính trực tuyến trong lãnh thổ (OnlyOffice/Collabora tự host): đáp ứng RB-7 nhưng là thành phần mới (giấy phép AGPL của OnlyOffice không thuộc allow-list) — để ngỏ.

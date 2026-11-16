## PA1-HTX-SUPPORT-GPSLOG — Yêu cầu hỗ trợ App HTX và log GPS về module chủ HTX (#313)

- **Căn cứ:** kế hoạch PA1 đã duyệt 16/11/2026 (`kn-handoff/PLAN-KN-PA1-20261116.md`, bước 2), quyết định Q7 của
  `kn-handoff/PLAN-KN-STANDALONE-20261107.md` ("chuyển về chủ HTX, không xóa"), báo cáo
  `kn-handoff/reports/app-kn-v1-tuong-thich-20261115.md` mục D dòng 5–6. Không có FN BRD 2.0 mới: chức năng là HTX
  US-SUPPORT-01 (App HTX) đã có, chỉ đổi module chủ. Lựa chọn kiến trúc ghi trên issue #313 trước khi code.
- **Module chủ:** `pf/cooperative`, phần nông dân (`season`): đã có liên kết tài khoản nông dân ↔ nông hộ, quản lý HTX
  theo `OrgRef` và consumer log GPS `pf.cropcycle.gps_log`.
- **Dữ liệu (migration pf):**
  - `20261116020729_pf_extension_support_request_export`: view chỉ-để-chép `pf_extension_pub.v1_support_request_export`
    (hợp đồng `contracts/pubview`, chỉ cấp `r_extension_pub`) kèm trạng thái và `sync_seq` nhiệm vụ KN lúc đọc; thu quyền
    INSERT của `r_extension`, `extension_app` trên `pf_extension.support_request` — bảng cũ giữ chỉ đọc, không DROP.
  - `20261116020730_pf_cooperative_support_request`: bảng `pf_coop.support_request` (chỉ ghi thêm, RLS tenant,
    `sync_seq` riêng, `legacy_task_id`/`legacy_task_status` cho dòng chép), quyền `r_cooperative`.
  - `20261116020731_pf_cooperative_support_request_copy`: chép một lần theo tenant (`shared.chain_head`), giữ id, khóa
    idem, toạ độ, ảnh; dòng chép lấy `sync_seq` nhiệm vụ cũ và chuỗi mới đặt lớn hơn để con trỏ đồng bộ của máy nông dân
    không bỏ sót. Kịch bản đếm trước/sau: `scripts/pa1-313-support-copy/count.sql`.
- **Giả định:**
  - Yêu cầu mới **không còn tự sinh nhiệm vụ khuyến nông** (`pf.extension.task.created`): KN chạy độc lập (quyết định
    07/11) và HTX phải chạy khi `pf/extension` tắt. Consumer thông báo nhiệm vụ của KN giữ nguyên cho sự kiện đã có.
  - HTX/xã của yêu cầu lấy theo xã viên hiện tại của nông hộ (trước đây theo HTX lúc liên kết tài khoản); nông hộ chưa
    là xã viên HTX đang hoạt động → `cropcycle.household_no_cooperative`.
  - Danh sách cho quản lý HTX chuyển sang `GET /v1/cropcycle/support-requests` (chỉ `coop_manager` HTX mình,
    `platform_admin`); đường cũ `/v1/extension/support-requests` thuộc bề mặt KN nên gỡ, web đổi trong cùng gói. App HTX
    không gọi đường này. `POST/GET /v1/support-requests` và entity đồng bộ `support_request` giữ nguyên.
  - Sự kiện `pf.cooperative.support_request.submitted` v1 thay `pf.extension.support_request.submitted` (schema cũ giữ
    trong `contracts/events` làm hồ sơ). Khóa chống trùng log GPS giữ tiền tố `extension:support_request:<id>`.
  - Ghi log GPS qua pf/geo chạy ngoài giao dịch inbox (`txdeps`, P0-POOL-SHARED-2 #143) cho mọi sự kiện của consumer
    `pf.cropcycle.gps_log`, như consumer cũ của yêu cầu hỗ trợ.
  - Toạ độ và độ chính xác nhận số thường (≤ 10 chữ số trước/sau dấu chấm, không mũ) — chặn `1e100000`, `1e-100000`.

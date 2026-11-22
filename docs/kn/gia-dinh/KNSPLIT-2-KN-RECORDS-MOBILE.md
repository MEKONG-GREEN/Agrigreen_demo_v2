## KNSPLIT-2-KN-RECORDS-MOBILE — App hiện trường chuyển HTX / nông hộ sang hồ sơ KN riêng (#161)

- **Nguồn:** #161 (con của #67 KNSPLIT-2-KN-RECORDS). Nguyên tắc: KN không ghi dữ liệu HTX nền tảng.
- **Trước:** `KnCollectionService` (field-app) và `ApiActionKind` (lõi di động) tạo/sửa/vô hiệu HTX và thêm/sửa nông hộ qua `POST/PUT /v1/kn/cooperatives…`, `PUT /v1/kn/households/{ref}`.
- **Sau:**
  - Lệnh mới (đều POST, Idempotency-Key cố định `act:<id>`, phát lại an toàn): `kn_record_create` → `POST /v1/kn/producer-records`; `kn_record_update` → `POST /v1/kn/producer-records/{ref}` (kèm `base_version`); `kn_record_deactivate|kn_record_reactivate` → `POST …/{ref}/deactivate|reactivate {reason, base_version}`.
  - Sửa gửi **đủ** trường nhập được của bản đang xem (máy chủ thay toàn bộ, trường vắng = xóa), người đại diện/chủ hộ chỉ gửi khi nhập mới (vắng = giữ). Sửa nông hộ chỉ đổi diện tích, giữ HTX/THT cha.
  - Danh sách: `GET /v1/kn/producer-records?kind=cooperative|coop_group` (ô "Loại" HTX / Tổ hợp tác), nông hộ của một hồ sơ: `kind=household&parent_ref=…` (không lọc tên — tên chủ hộ ở kho định danh). Trang đầu đệm để xem ngoại tuyến như cũ.
  - Thêm HTX: chọn tùy chọn "Liên kết HTX đã công bố" (`/v1/kn/producer-htx-candidates`, cần mạng; lỗi → ẩn ô chọn). Tab Máy móc / Thửa ruộng của chi tiết HTX đọc theo `htx_ref`; chưa liên kết → thông báo, không đọc theo mã hồ sơ KN.
  - Kiểm trên máy trước khi xếp hàng: diện tích là số thập phân thường (dấu phẩy → chấm, ≤ 16 ký tự, không số mũ/dấu, ≤ 4 chữ số lẻ, ≤ 1.000.000 — kiểm chuỗi trước khi đọc số), người đại diện/chủ hộ bắt buộc khi thêm, lý do 10–1000 ký tự (bằng `knrecords/domain`; trước đây app chặn ≥ 20 theo sổ `pf_party.org_event`).
  - Thông điệp tiếng Việt cho `extension.kn_record_name_taken|household_exists|version_conflict|state_conflict|not_found`.
- **Hàng đợi cũ trên máy đã cài bản trước:** mã lệnh `kn_cooperative_*`, `kn_household_*` giữ ở `RetiredActionKind` chỉ để đọc được dòng cũ; không xếp hàng mới, không phát lại (`ActionQueue.pending` bỏ qua). Migration SQLite cục bộ v4 đánh dấu dòng còn chờ là `failed` với `last_error = 'retired'` (dọn sau 48 giờ). Giả định: không tự chuyển lệnh cũ sang hồ sơ KN (dữ liệu HTX cũ vẫn là HTX nền tảng, như #67); cán bộ nhập lại nếu cần.
- **Ngoại tuyến:** sửa hai lần liên tiếp khi mất mạng dùng cùng `base_version` → lệnh thứ hai bị 409 `version_conflict` khi phát lại và bị bỏ (báo "tải lại rồi sửa"). Chấp nhận cho G1 (giống web: khóa lạc quan).
- **Còn đọc legacy (chỉ GET, không ghi):** bộ chọn HTX của màn Thửa (`GET /v1/kn/cooperatives`) — chuyển ở #162 cùng màn web.
- **Sửa kèm:** hộp thoại nhập lý do (`knAskReason`) giữ bộ điều khiển trong State của hộp thoại (trước đây hủy ngay khi đóng, lỗi "TextEditingController used after being disposed" trong hiệu ứng đóng).
- **Câu hỏi PO:** (1) Lệnh ghi HTX cũ còn chờ trên máy cán bộ khi cập nhật app: bỏ (hiện tại) hay cần màn báo "lệnh cũ không gửi được"? (2) App có cần màn riêng cho Nông hộ không thuộc HTX/THT nào không? (hiện chỉ thêm nông hộ từ chi tiết HTX/THT, như bản cũ).

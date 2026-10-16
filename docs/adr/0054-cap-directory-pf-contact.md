# ADR-0054: `cap/directory` = module năng lực `pf/contact` — danh bạ dùng chung, một bản ghi/người ở kho định danh, lọc trường ở API

**Trạng thái:** Đề xuất — hiện thực ở W11.23 (14/10/2026, pf 20261013230912, vault 20261013165158); Q-071, Q-072 theo đề xuất của dev, chờ chủ sản phẩm · **Ngày:** 14/10/2026 (gói W11.23) · **Yêu cầu:** kiến trúc v4.1 §5, §7.1 (R-MOD-05, R-MOD-09), RB-3, R-04, R-14, R-41, R-42 · **Nguồn:** BRD Hành chính – Tổng hợp v3.0 FN-13 (UR-GEN-01, UR-GEN-02, UR-ADM-10, UR-HC-27, UR-QG-02, UR-XA-21); đối chiếu v4.3/MKG v1.4/BRD KN Phụ lục C mục H3, Q-071, Q-072 · **Thành phần:** `internal/kn/contact`, `internal/vault/identity` (thẻ danh bạ), `internal/kn/hrm` (port `Directory`), `cmd/api-platform/assembly/contact.go`, realm `vault_directory` · **Liên quan:** [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0050](0050-dom-hrm-pf-hrm.md)

> **Bị thay thế một phần bởi [ADR-0055](0055-tach-he-thong-khuyen-nong.md)** (trả lời PO Q-KNS-05, 14/10/2026): Contact không còn là năng lực dùng chung `cap/directory`. FN-13 của BRD Hành chính - Tổng hợp v3.0 (UR-GEN-01, UR-GEN-02, UR-QG-02, UR-ADM-10) là chức năng của KN (Phòng HCTH), nên `pf/contact` thuộc hệ KN. HTX hay MKG cần danh bạ thì dựng bản riêng. Phần còn lại của ADR này (một bản ghi mỗi người ở kho định danh, lọc trường ở API) giữ nguyên.

## Bối cảnh

- FN-13: menu "Contact" (nhóm Dùng chung) cho MỌI vai; Cấp TW, Cấp Xã chỉ thấy họ tên, năm sinh, SĐT — "hệ thống không trả về các trường khác" (BR-03); Cấp Tỉnh, Admin thêm chức danh, đơn vị, cấp, email. Một người một bản ghi; hồ sơ nhân sự, người liên hệ của đơn vị, thành viên Hội đồng tham chiếu bản ghi đó (BR-01); các module khác không tạo dữ liệu liên hệ riêng (UR-GEN-01).
- Liên hệ là dữ liệu cá nhân. Hồ sơ nhân sự (W11.22) đã đặt họ tên, SĐT ở `vault.identity` và ngày sinh, email ở ngăn `hrm.profile` (mục đích `hrm.profile`, vai realm `vault_registrar`) — Cấp TW, Cấp Xã không có và không được có vai đó.
- G1 đã có "danh bạ trực" `pf_extension.staff` với cột `display_name` dạng rõ (Q-072 hỏi có thay không). Bên dùng dự kiến: KN (mọi phòng ban), HCTH (hồ sơ, Hội đồng), về sau HTX/MKG (Q-072) — đạt quy tắc ba của R-MOD-09.

## Quyết định

1. **`pf/contact` (`aka: [cap/directory]`, `kind: capability`, `enable: always`)** sở hữu `pf_contact` (vai `r_contact`). Không phụ thuộc module nghiệp vụ: nguồn cán bộ (`port.Staff`), nhãn đơn vị (`port.Labels`) và quyền (`port.Access`) do gốc lắp ráp nối — KN nối `pf/hrm` qua port mới `hrm/port.Directory`.
2. **Một bản ghi = một chủ thể kho** (`subject_ref`). Cán bộ thuộc tỉnh không chép sang `pf_contact` — chiếu từ hồ sơ nhân sự (sửa tại FN-07), theo cổng `AG_HRM_RECORDS`. Liên hệ ngoài tỉnh: chủ thể kho mới (mục đích `contact.directory`) + sổ `pf_contact.person` (cấp, cơ quan, chức danh, ẩn, TÊN trường đã đổi). Thẻ Đơn vị: sổ `pf_contact.unit` (liên hệ công vụ của tổ chức). Không PII trong `pf_contact`.
3. **Thẻ danh bạ ở kho (`OpCard`, `POST /v1/subjects/cards`).** Đọc theo lô ≤ 200 chủ thể, CHỈ trường được hỏi trong {họ tên, SĐT, năm sinh, email}. Năm sinh, email lấy ngăn `contact.card` (liên hệ ngoài tỉnh) hoặc kho tự CHIẾU từ ngăn `hrm.profile` (chỉ năm của ngày sinh, email) — người gọi không bao giờ đọc được ngăn hồ sơ, định danh đầy đủ, CCCD, địa chỉ. Mục đích duy nhất `contact.directory`; vai `vault_registrar`, `vault_display` hoặc vai mới `vault_directory` (realm: vai tổng hợp `kn_user` chứa nó). Mỗi chủ thể đọc được ghi một dòng sổ truy cập op `card`.
4. **Lọc trường ở API theo vai.** Bộ `basic` có DTO riêng (id + 3 trường), kho không được hỏi email, bộ lọc trường ẩn bị bỏ qua, từ khóa chỉ so họ tên/SĐT, không chi tiết, không ghi. Bộ `full` cho Cấp Tỉnh, Admin. Cập nhật: Admin và Cấp Tỉnh ở phòng HCTH.
5. **Danh bạ trực (Q-072):** tên cán bộ của `pf/extension` đọc từ Contact trước; `pf_extension.staff.display_name` chỉ còn là nguồn dự phòng. Bảng `pf_extension.staff` còn giữ phần định tuyến trực/phân công (cán bộ ↔ địa bàn) — bỏ hẳn là gói sau.

## Hệ quả

- Mọi cán bộ KN đọc được họ tên, năm sinh, SĐT của cán bộ khác qua kho (có sổ truy cập theo mục đích) — đúng BRD nhưng cần căn cứ pháp lý (Q-071, Luật BVDLCN 91/2025, NĐ 356/2025) và thông báo chủ thể; ghi vào DPIA. Chưa có kênh thông báo chủ thể.
- Tìm theo họ tên đọc thẻ của mọi ứng viên (họ tên chỉ ở kho) — chi phí tuyến tính theo cỡ danh bạ, sổ truy cập kho tăng theo; giới hạn 5.000 liên hệ. Chỉ mục mù theo tên là việc sau (chung với W11.22).
- Realm đã nhập ở môi trường thật phải thêm vai `vault_directory` và composite của `kn_user` bằng tay.
- Ứng dụng khác dùng Contact: thêm cấu hình `port.Access` ở gốc lắp ráp của ứng dụng và cấp `vault_directory` cho vai của ứng dụng.

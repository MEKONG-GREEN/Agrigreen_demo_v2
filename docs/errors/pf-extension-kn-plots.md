### pf/extension (`dom/extension` — thửa ruộng KN, Trồng trọt FN-XA-03.05, KN-STANDALONE-5-TT-DATA #271)

Thửa ruộng chuyển từ module HTX về kho KN (`/v1/kn/plots*` do `internal/kn/extension/knplot` phục vụ). **Không có mã mới:**
giữ nguyên các mã `geo.*` của FN-XA-03.05 đã công bố ở `docs/errors.md` (mục pf/geo — chồng lấn, phiên bản cũ, hình sai,
toạ độ, diện tích, địa giới xã, chỉ xem, ngoài phạm vi, không tìm thấy, chế độ hỗ trợ, phụ thuộc) để web và field-app không đổi.
Mã thửa đã có (`plot_ref` trùng) trả mã trạng thái thửa đã công bố. Dùng chung `validation_failed` (tên lô, ghi chú có SĐT/CCCD,
cách vẽ, diện tích khai báo — kiểm cú pháp thập phân thường trước khi parse, lý do, `cooperative_ref` không phải hồ sơ KN loại HTX
đang hoạt động trong phạm vi), `forbidden` (Cấp Tỉnh ghi), `idempotency_required`.

Lịch công tác (FN-XA-03.08, FN-TI-02.07): ô HTX không phải hồ sơ KN loại HTX đang hoạt động của đúng xã → mã
`extension.cooperative_invalid` đã có.

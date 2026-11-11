### pf/extension (`dom/extension` — máy móc HTX của KN, Trồng trọt FN-XA-03.04, KN-STANDALONE-5-TT-DATA-MAY #306)

Số máy theo loại của HTX chuyển từ module CGH về kho KN (`/v1/kn/machine-counts*` do `internal/kn/extension/knmachine`
phục vụ, HTX là hồ sơ KN loại HTX). **Không có mã mới:** giữ nguyên các mã `mechanization.*` của FN-XA-03.04 đã công bố ở
`docs/errors.md` (mục pf/mechanization — `read_only`, `support_reason_required`, `cooperative_not_found`,
`cooperative_invalid`, `machine_type_unknown`, `machine_type_duplicate`, `machine_quantity_invalid`, `out_of_scope`,
`dependency_unavailable`) để web và field-app không đổi. `mechanization.cooperative_not_found` nay nghĩa là mã không phải
hồ sơ KN loại HTX trong phạm vi (mã HTX nền tảng cũng trả 404). `mechanization.machine_quantity_invalid`: số lượng phải là
số JSON nguyên 1–1.000.000 viết bằng chữ số thường — kiểm cú pháp trước khi parse (`1e3`, `2.0`, `1e100000` bị chặn).
Dùng chung `validation_failed` (thiếu `lines`, thiếu loại máy, ghi chú quá 500 ký tự, quá 100 dòng, phòng ban sai).

Hồ sơ KN (FN-XA-03.03): gửi `htx_ref` khi tạo, hoặc đổi `htx_ref` khi sửa → `validation_failed` trường `htx_ref` (liên kết
HTX đã công bố hoãn tới pha kết nối). Năm sản xuất (`/v1/kn/machine-profiles/{ref}`): mã `extension.kn_attrs_*` đã có,
`extension.kn_attrs_cooperative_not_found` theo cùng khóa hồ sơ KN.

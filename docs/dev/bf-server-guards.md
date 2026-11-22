# Guard server nền TT-10 / TT-11

Gói #113 BF-SRV-GUARD; PRD kn-bugfix-brd-alignment ngày 23/10/2026. Codex làm phần guard nền (commit ae9ac85c); Claude nhận lại gói từ 26/10/2026 (Codex hết lượt), rebase lên dev và bổ sung phần dưới.

- Dòng khai báo máy phải có số lượng nguyên trong khoảng 1–1.000.000. Handler JSON và use case đều kiểm; số 0 không còn là khai báo hợp lệ. Để bỏ một loại, bỏ dòng đó khỏi mảng thay thế; sự kiện removed vẫn giữ Quantity=0 và số cũ để dựng lịch sử. Không sửa dữ liệu lịch sử cũ.
- Chủ hộ khi tạo thửa hoặc khi gửi thay đổi chủ hộ phải có tên không rỗng. Trả 422 validation_failed, trường farmer_name, thông báo tiếng Việt trước khi gọi kho định danh. Nhãn trả về vẫn được che.
- Thửa KN (R-A1): `declared_area_ha` âm hoặc 0 bị 422 theo trường khi tạo và khi sửa. Khi tạo, số này được kiểm **trước** khi ghi tên chủ hộ vào kho định danh, nên lần tạo bị từ chối không để lại bản ghi định danh. Tên thửa và xã bắt buộc (đã có); regression `TestTT11_NumericAndRequiredGuards` kiểm cả không ghi thửa, không ghi sổ, thửa hiện có giữ nguyên.
- Diện tích trả về lấy từ geometry; declared_area_ha là số khai báo phụ đã có, không thay diện tích hình. Guard chồng lấn KN online và cách xác nhận/xung đột riêng của API HTX giữ nguyên.
- Không đổi schema, route, tenant, node, vai hay quyền; không triển khai luồng KN ghi dữ liệu HTX.

## Năm sản xuất máy và cây trồng thửa: hồ sơ KN riêng (#119)

Năm máy (TT-10) và cây trồng thửa (TT-11) không thuộc mô hình nền: khai báo máy là loại × số lượng theo HTX, `KNAttrs` của thửa không có cây trồng. Hai thuộc tính này nằm trong hồ sơ KN riêng của gói [#119 BF-SRV-KN-ATTRS](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/119) (sổ `pf_extension.kn_machine_year`, `pf_extension.kn_plot_crop`; route `/v1/kn/machine-profiles/{cooperative_ref}`, `/v1/kn/plot-profiles[/{plot_ref}]`). Năm sản xuất bắt buộc, nguyên, 1900 – năm hiện tại giờ Việt Nam; cây trồng bắt buộc, thuộc danh mục đang dùng. Chi tiết: `docs/kn/gia-dinh/BF-SRV-KN-ATTRS.md` (có sau khi #119 gộp).

Module nền không nhận thêm hai trường này, để không nhận trường rồi bỏ đi và không mở đường KN ghi dữ liệu HTX. Thứ tự gộp: #119 trước, rồi #113. Màn web #103 (máy) và #104 (thửa) dùng cả hai: số lượng/hình qua module nền, năm/cây trồng qua hồ sơ KN.

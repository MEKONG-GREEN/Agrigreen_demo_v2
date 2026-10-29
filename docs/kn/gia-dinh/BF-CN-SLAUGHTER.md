## BF-CN-SLAUGHTER — Danh mục cơ sở giết mổ: chọn xã từ ĐVHC, cột riêng từng trường, CSGM mẫu (#169)

Bug log CNTS CN-10; BRD CNTS v3.7 FN-06 (BR-02, BR-03), FN-01 AC-02. Đối chiếu: `kn-handoff/reports/cn-09-10-11-14-audit-20261028.md`. Làn KN.

| Mục | Quyết định / giả định | Bằng chứng |
|---|---|---|
| Chọn xã | Ô "Xã/phường/đặc khu" thành ô chọn: chọn tỉnh (mặc định tỉnh của xã đang lưu, hoặc tỉnh duy nhất), rồi chọn xã cấp `commune` có hiệu lực tại **ngày áp dụng** của form (`GET /v1/masterdata/admin-units?parent=&at=`), hiện "Tên (mã)". Giá trị cũ không còn trong danh sách vẫn hiện để không mất | `CommuneField.tsx`, `cnts.test.tsx` |
| Máy chủ kiểm | Đã có từ W11.41/KNSPLIT: ràng buộc `AdminUnit` của danh mục (`module_masterdata_constraints.go`) tra `k_ref.admin_unit` của tenant tại `valid_from`, sai cấp hoặc không có → 422 `masterdata.cnts_commune_invalid` trên `attrs.commune_code`. Áp cho tạo, sửa, dùng lại, nạp tệp (dòng bị loại kèm lý do) và dữ liệu nền. Gói này chỉ thêm kiểm thử (mã lạ, mã tỉnh, xã hết hiệu lực, nạp tệp) | `TestW1141_CNTSCatalogs` |
| Phạm vi người gọi | Danh mục CNTS chỉ Admin (`masterdata_admin`/`platform_admin`) sửa, phạm vi là cả tenant; "trong phạm vi" = có trong ĐVHC của tenant (RLS `k_ref`). Không có vai cấp xã/tỉnh nào được ghi danh mục nên không thêm lọc theo tỉnh | — |
| Danh sách | CSGM có cột riêng: Xã (tên), Mã xã, Loại, Mộc dấu GS, Mộc dấu GC, Loài, Địa chỉ, Hiệu lực từ, Trạng thái, thay cột tóm tắt. Lọc theo xã (các xã có trong danh sách đang xem). Danh mục khác giữ cột tóm tắt | `cnts.test.tsx` |
| Chi tiết | Trường xã hiện "Tên (mã)"; bảng giá trị theo ngày hiện tên xã nếu tra được | `cnts.test.tsx` |
| Dữ liệu nền | Thêm 4 CSGM **mẫu** (`CSGM_MAU_01…04`) vào `masterdata_catalogs.json` để danh mục và lưới lịch trực (BF-CN-DUTY) không trống khi demo. Không có tệp "Lịch trực ksgm" gốc trong kho nên tên, địa chỉ, mộc dấu (`91.900.*`) là giá trị giả lập ghi rõ "(dữ liệu mẫu)"; mã xã là 4 xã An Giang thật còn hiệu lực (30316, 30742, 30628, 30463), không dùng 30292 để không đổi các kiểm thử lọc theo xã. `facility_ref` cố định. Dữ liệu nền chạy cho mọi tenant (như các danh mục CNTS khác). Golden `kn-masterdata-catalogs.json` (64 mục) và `kn-masterdata-lookups.json` (danh sách CSGM theo ngày) cập nhật theo | `TestOwnedCatalogsMatchLegacyContract`, `TestCatalogsRequireRegistration`, `seededFacilities` |
| Mock | 3 CSGM giả lập thêm ở xã giả lập 91101–91103; mock từ chối mã xã không có trong ĐVHC giả lập (giữ 30292 của mục mẫu cũ) | `cnts.test.tsx` |
| BR-03 | Không đổi: đổi xã/mộc dấu tạo giá trị mới theo ngày hiệu lực; kiểm thử AC-02 (GS đến 30/09, thêm GC từ 01/10) vẫn chạy | `TestW1141_CNTSCatalogs` |

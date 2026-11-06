# Nghiệm thu CNTS v4.0 (BRD 2.0)

Đối chiếu theo BRD CNTS v4.0 (CR-CNTS-001). Mỗi gói thêm mục của FN mình chạm; kiểm thử tích hợp chạy trên PostgreSQL thật
(pg-ephemeral), kho định danh, danh mục, đơn vị hành chính là bản giả trừ khi ghi khác.

## FN-10 — Danh sách hộ chăn nuôi (CNTS-V4-HOUSEHOLD-LIST #251)

Nguồn và giới hạn: [giả định](../kn/gia-dinh/CNTS-V4-HOUSEHOLD-LIST.md), [ADR-0056](../adr/0056-ho-chan-nuoi-cnts-v4.md).

| Tiêu chí BRD | Bằng chứng tự động | Phạm vi |
|---|---|---|
| H1–H3: thêm hộ HCN-02…HCN-06 + check-in tại nhà hộ → Đang dùng; mã HCN-01 tự cấp `HCN`+7 số, không dùng lại, theo tenant | `TestLivestockHouseholds_251`, `TestCodeNameChanged` | G1 |
| AC-05: thêm hộ chưa check-in → không lưu (422 `checkin_required`) | `TestLivestockHouseholds_251`; vitest `livestock.test.ts` | G1 |
| AC-06: Cấp Xã chỉ thấy, chỉ thêm hộ xã mình | `TestLivestockHouseholds_251` | G1 |
| Ma trận quyền: cán bộ tổng hợp Cấp Tỉnh CRU + vô hiệu hóa; viên chức Cấp Tỉnh chỉ xem; Cấp TW không có quyền | `TestLivestockHouseholds_251` | G1 |
| HCN-02/03 dữ liệu cá nhân: họ tên, SĐT ở kho định danh (không ở sổ KN); danh sách che bớt, SĐT che một phần (Mục I.8.1) | `TestLivestockHouseholds_251`, `TestIdentityRevisions_251` | G1 |
| HCN-05: vật nuôi theo danh mục FN-06 đang dùng, số nguyên ≥ 0, nhiều vật nuôi; số rất lớn (1e100000) bị chặn | `TestLivestockHouseholds_251`, `TestNormalizeFields`, `TestAccuracyAndCheckin` | G1 |
| Ngoại lệ: trùng họ tên, ấp, xã với hộ Đang dùng → cảnh báo (409 kèm mã hộ trùng); xác nhận hộ khác → lưu | `TestLivestockHouseholds_251` | G1 |
| Ngoại lệ: sai số GPS > 50 m → cảnh báo, vẫn lưu; không bắt được GPS → không check-in, báo thử lại | `TestLivestockHouseholds_251`; vitest `livestock.test.ts` | G1 |
| BR-03: vị trí chỉ từ check-in GPS, không nhập tay; sửa thông tin không đổi vị trí | `TestLivestockHouseholds_251` | G1 |
| H4: đổi vị trí phải check-in lại và nhập lý do | `TestLivestockHouseholds_251` | G1 |
| BR-05, AC-08: sửa loại chăn nuôi → lịch sử người sửa, thời điểm, cũ → mới; họ tên/SĐT cũ → mới dạng che bớt | `TestLivestockHouseholds_251`, `TestIdentityRevisions_251`, `TestHTTPClientEndToEnd` | G1 |
| BR-04: không xóa, chỉ vô hiệu hóa (cán bộ tổng hợp Cấp Tỉnh, có lý do); không sửa/kích hoạt lại sau vô hiệu hóa | `TestLivestockHouseholds_251` | G1 |
| BR-08, AC-09 (phần danh sách): hộ Chờ bổ sung vị trí → cán bộ xã check-in → Đang dùng, lịch sử ghi chuyển trạng thái | `TestLivestockHouseholds_251` (hộ chờ ghi như BM-10 sẽ ghi) | G1; tạo hộ từ BM-10: #252 |
| BR-07, AC-07 (phần danh sách): hộ vô hiệu hóa không còn trong danh sách Đang dùng | `TestLivestockHouseholds_251` (lọc `status=active`) | G1; ô chọn DB-04: #252 |
| Xóa định danh xóa luôn bản sửa họ tên/SĐT (RB-3) | `TestIdentityRevisions_251` | G1 |
| Menu Tỉnh "Số liệu cấp xã › Danh sách hộ chăn nuôi", Xã "Dữ liệu › Danh sách hộ chăn nuôi" | `TestWebMenuFixture`, `TestBuilderMatchesWebGoldenFixture` | G1 |

Chưa nghiệm thu ở gói này: đếm hộ ở báo cáo định kỳ (FN-16 BR-07, CN-5), lớp trang trại bản đồ (FN-18, CN-6), ô chọn DB-04 và tạo
hộ từ BM-10 (FN-11, #252).

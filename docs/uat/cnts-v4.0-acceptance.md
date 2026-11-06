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
hộ từ BM-10 (FN-11, #252 — xem mục FN-11 dưới).

## FN-11 — Báo cáo dịch bệnh BM-10 nhiều hộ (CNTS-V4-OUTBREAK-MULTI #252)

Nguồn và giới hạn: [giả định](../kn/gia-dinh/CNTS-V4-OUTBREAK-MULTI.md), [ADR-0056](../adr/0056-ho-chan-nuoi-cnts-v4.md) mục 7–8.

| Tiêu chí BRD | Bằng chứng tự động | Phạm vi |
|---|---|---|
| BR-03, DB-04: một ổ dịch chọn được nhiều hộ trong danh sách hộ chăn nuôi; hộ ngoài danh sách, xã khác, lặp → chặn | `TestFN11_MultiHousehold_252`, `TestCheckHouseholds`; vitest `outbreak-ui.test.tsx` | G1 |
| UR-XA-10: DB-04 phải có ít nhất 1 hộ (DB-15 ≥ 1) | `TestFN11_OutbreakVersions`, `TestCheckHouseholds`; vitest `outbreak.test.ts` | G1 |
| AC-04: chọn 3 hộ → DB-15 = 3, không sửa tay (giá trị gửi lên bị bỏ qua) | `TestFN11_MultiHousehold_252`; vitest `outbreak-ui.test.tsx` | G1 |
| DB-14: loại chăn nuôi lấy theo từng hộ (HCN-06), hộ nhập mới thì cán bộ chọn; chụp lúc ghi — hộ đổi loại sau đó không đổi báo cáo cũ (FN-10 BR-06) | `TestFN11_MultiHousehold_252` | G1 |
| DB-04 "nhập hộ chưa có" → hộ "Chờ bổ sung vị trí" trong danh sách, gắn ổ dịch tạo ra nó, dùng được ở báo cáo dịch bệnh khác (FN-10 BR-08, AC-09) | `TestFN11_MultiHousehold_252`; vitest `outbreak.test.ts` | G1 |
| FN-10 BR-07, AC-07: hộ vô hiệu hóa không chọn được ở DB-04 của báo cáo mới; vẫn hiện ở báo cáo cũ | `TestFN11_MultiHousehold_252` | G1 |
| AC-05, DB-16: bệnh nhóm thủy sản (loài của bệnh có nhóm `thuy_san`, FN-06) để trống DB-16 → không gửi được; ghi riêng nuôi trồng, khai thác; ≥ 0, ≤ 2 số lẻ; `1e100000` bị chặn trước khi parse | `TestFN11_MultiHousehold_252`, `TestCheckAqua`, `TestParseHectares`; vitest `outbreak.test.ts`, `outbreak-ui.test.tsx` | G1 |
| BR-02: mỗi lần cập nhật một phiên bản, mỗi phiên bản giữ tập hộ của nó; kết thúc/mở lại chụp lại tập hộ hiện hành | `TestFN11_MultiHousehold_252`, `TestFN11_OutbreakVersions` | G1 |
| BR-03 "tổng hợp vùng dịch lấy số của lần cập nhật mới nhất": DB-15, DB-16 lưu theo phiên bản, bản hiện hành là số mới nhất | `TestFN11_MultiHousehold_252` | G1; vùng dịch FN-18: CN-6 #254 |
| Ổ dịch cũ một hộ: giữ nguyên, API trả `legacy = true`, DB-15 hiện 1, DB-14/DB-16 trống; cập nhật diễn biến chọn hộ theo quy tắc mới | `TestFN11_LegacyOutbreak_252` | G1 |
| Không chuyển dữ liệu: số ổ dịch, số phiên bản, số phiên bản v1, tổng quy mô đàn, số con chết trước/sau migration bằng nhau; chuỗi băm sạch | `TestOutbreakMultiUpgradeKeepsLegacy` (pg-ephemeral) | G1 |
| Luồng phụ "xuất Word theo mẫu": Word BM-10 có số hộ (DB-15), bảng hộ (mã, chủ hộ che bớt, ấp, loại), DB-16 khi có | `TestWordDataHouseholds` | G1 |
| AC-01…AC-03, BR-01 (giữ nguyên từ W11.49): cảnh báo Khẩn, chặn thiếu vị trí, tiêu hủy > quy mô đàn | `TestFN11_OutbreakVersions`, `TestFN11_SuspectConfirmDismiss` | G1 |

Chưa nghiệm thu ở gói này: vùng dịch hiện số hộ, diện tích thủy sản bị ảnh hưởng và lớp trang trại trên bản đồ (FN-18 BR-02, CN-6
#254 — dùng `affected_households`, `aqua_*` của phiên bản mới nhất).

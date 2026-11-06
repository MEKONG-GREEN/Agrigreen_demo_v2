# Nghiệm thu CNTS v4.0 (BRD 2.0, CR-CNTS-001)

Căn cứ duy nhất: BRD CNTS v4.0. Mỗi gói CNTS v4.0 thêm phần FN mình chạm. Kiểm thử tích hợp chạy trên PostgreSQL thật
(pg-ephemeral); camera, GPS của Mobile là nguồn giả trong kiểm thử.

## FN-12 — Lịch công tác: check-in, check-out kèm ảnh (CNTS-V4-CHECKOUT-PHOTO #249)

Giả định và mô hình dữ liệu: [CNTS-V4-CHECKOUT-PHOTO](../kn/gia-dinh/CNTS-V4-CHECKOUT-PHOTO.md).

| Tiêu chí BRD v4.0 | Bằng chứng tự động | Trạng thái |
|---|---|---|
| CV-07, luồng ngoại lệ "Check-in, check-out không có ảnh → chặn": check-in thiếu ảnh, ảnh hỏng, không phải ảnh, > 500 KB bị từ chối 422 trường `photo` | `TestWorkScheduleCheckoutPhoto`, `TestVisitPhotoType`; field-app `hủy chụp ảnh → không check-in` | Đạt |
| B4b: Check-out chỉ sau check-in; nút Check-out chỉ hiện sau check-in | `TestWorkScheduleCheckoutPhoto` (409 `extension.checkout_no_checkin`); field-app `CV-08: nút Check-out chỉ sau check-in` | Đạt |
| AC-09: bấm check-out chưa chụp ảnh thì không lưu được | `TestWorkScheduleCheckoutPhoto` | Đạt |
| BR-06: check-in, check-out lưu ảnh, GPS, thời điểm, độ chính xác; không sửa được | `TestWorkScheduleCheckoutPhoto` (check-out lần hai 409 `extension.checkout_exists`; ảnh tải lại đúng nội dung), `TestWorkScheduleCheckin` | Đạt |
| Check-out cách địa điểm CV-06 > 500 m → cảnh báo | `TestWorkScheduleCheckoutPhoto` (`checkout_far` ở phản hồi và chi tiết lịch) | Đạt |
| AC-11: check-out mất mạng 11:30, có mạng 14:00 → ghi 11:30 | `TestWorkScheduleCheckoutPhoto` (giờ thiết bị giữ nguyên khi gửi muộn); field-app lệnh chờ giữ `captured_at` lúc bấm | Đạt |
| AC-12: check-in 08:00, check-out 11:30 → thời gian ở địa điểm 3 giờ 30 phút | `TestWorkScheduleCheckoutPhoto` (`stay_minutes` 210), `TestCheckoutRules`; web `visits.test.tsx`; field-app `máy chủ trả check-out` | Đạt |
| AC-13: check-out offline 16:30 ngày 06/10, đồng bộ 07:10 ngày 07/10 → ghi 16:30 kèm "Đồng bộ trễ" | `TestWorkScheduleCheckoutPhoto` (`synced_late`, `synced_at`), `TestCheckoutRules`; web hiện "Đồng bộ trễ · thời điểm đồng bộ" | Đạt phần ghi nhận; gỡ nhãn, tự đóng cảnh báo: #250 |
| UR-TINH-18: người theo dõi xem check-in, check-out, thời gian ở địa điểm và ảnh | `TestWorkScheduleCheckoutPhoto` (người duyệt tải ảnh; cán bộ xã khác không); web chi tiết lịch khối "Check-in, check-out tại địa điểm" | Đạt ở chi tiết lịch; màn theo dõi Hình 49: #250 |
| Dữ liệu cũ: check-in trước v4.0 không ảnh giữ nguyên, đánh dấu, không bắt nhập bù; đếm trước/sau, quay lui | `TestWorkScheduleCheckinLegacy` (số dòng, băm dòng cũ không đổi; `verify_chain` đúng; câu INSERT của bản trước vẫn chạy) | Đạt |
| BR-07/AC-10: nhãn "Chưa check-out" lúc 23:59, cảnh báo người theo dõi | — | Gói CN-4 #250 |

Cách chạy:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v -run 'WorkSchedule' ./internal/kn/extension/cntscal/handler/
go test ./internal/kn/extension/domain/ -run 'VisitPhoto|CheckoutRules'
cd apps/web-ops; npx vitest run src/kn/schedule/
cd apps/field-app; flutter test test/work_schedule_checkin_test.dart
```

## FN-12/FN-13 — Theo dõi check-in, check-out; cảnh báo "Chưa check-out" (CNTS-V4-CHECKIN-MONITOR #250)

Giả định: [CNTS-V4-CHECKIN-MONITOR](../kn/gia-dinh/CNTS-V4-CHECKIN-MONITOR.md). Giờ giả lập (đồng hồ cố định của kiểm thử), mốc 23:59
tính theo giờ Việt Nam.

| Tiêu chí BRD v4.0 | Bằng chứng tự động | Trạng thái |
|---|---|---|
| FN-12 BR-07: mục đã check-in, tới 23:59 cùng ngày chưa check-out → nhãn "Chưa check-out" ở màn cán bộ xã và màn theo dõi | `TestNoCheckout` (23:58:59 chưa, 23:59 có, mốc 16:59 UTC); `TestWorkScheduleNoCheckout` (`no_checkout` ở chi tiết lịch, `mine` của cán bộ xã, danh sách theo dõi `no_checkout_count`, cảnh báo `checkout_missing`); web `visits.test.tsx`; field-app `CNTS v4.0 BR-07 (#250)` | Đạt |
| FN-12 AC-10: check-in 08:00, chưa check-out đến 23:59 → sáng hôm sau người theo dõi thấy nhãn | `TestWorkScheduleNoCheckout` (người duyệt đọc lịch lúc 23:59 và sau đó) | Đạt |
| Mục III.5 dòng 13 / FN-13 BR-04: cảnh báo "Chưa check-out", mức Cần chú ý, gửi người theo dõi lịch cán bộ xã; không tạo trùng | `TestWorkScheduleNoCheckout` (job `extension_cnts_alert` sinh 2 cảnh báo, người nhận = người duyệt được lịch; chạy lại không sinh thêm), `TestTransition` (bảng loại dòng 13) | Đạt |
| FN-12 BR-08 / AC-13: check-out offline 16:30, đồng bộ 07:10 hôm sau → gỡ nhãn, cảnh báo tự đóng, lịch ghi 16:30 kèm "Đồng bộ trễ" | `TestWorkScheduleNoCheckout` (nhãn gỡ ngay khi đọc; job tự đóng cảnh báo, `auto_closed`, kết quả ghi giờ check-out và giờ đồng bộ) | Đạt (tự đóng trong ≤ 5 phút — chu kỳ job) |
| UR-TINH-18, Hình 49: màn theo dõi check-in, check-out, thời gian ở địa điểm, ảnh của cán bộ xã; mục chưa check-out hiện nhãn | web `visits.test.tsx` (`<VisitMonitor>`: cột, Đồng bộ trễ, lọc "Chỉ mục Chưa check-out", xem ảnh), `schedule.test.tsx` (tab "Check-in, check-out") | Đạt |
| Job tìm tenant có check-in bằng vai `ag_ops_jobs` (chỉ cột `tenant_id`, `day`) | `TestWorkScheduleNoCheckout` (`CNTSAlertTenants`) | Đạt |
| Dữ liệu cũ: check-in trước v4.0 (không ảnh) không gắn nhãn, không sinh cảnh báo — không bắt nhập bù | `TestWorkScheduleNoCheckout` | Đạt |

Cách chạy:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v -run 'WorkScheduleNoCheckout' ./internal/kn/extension/cntscal/handler/
go test ./internal/kn/extension/domain/ ./internal/kn/extension/cntsalert/domain/
cd apps/web-ops; npx vitest run src/kn/schedule/
cd apps/field-app; flutter test test/work_schedule_checkin_test.dart
```
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

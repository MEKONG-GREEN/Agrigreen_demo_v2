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

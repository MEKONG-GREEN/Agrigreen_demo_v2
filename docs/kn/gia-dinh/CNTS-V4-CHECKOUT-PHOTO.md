## CNTS-V4-CHECKOUT-PHOTO — Check-in bắt buộc ảnh, check-out kèm ảnh (#249, pf 20261106191624)

Nguồn: BRD CNTS v4.0 (CR-CNTS-001) FN-12 (B4, B4b, luồng ngoại lệ, BR-06, BR-07, BR-08, AC-09, AC-11, AC-12, AC-13),
UR-XA-05, UR-XA-09 CV-07/CV-08, UR-TINH-18, Mục VIII "Định vị, ảnh", "Làm việc offline"; PLAN-BRD2 mục 5 (check-in cũ
không ảnh giữ nguyên, đánh dấu dữ liệu cũ, không bắt nhập bù).

| Mục | Phương án đã chọn | Ở đâu |
|---|---|---|
| Mô hình dữ liệu | Check-in giữ sổ `work_schedule_checkin` (thêm 4 cột ảnh NULL được, đăng ký `register_ledger_columns` — ADR-0012). Check-out là sổ chỉ ghi thêm riêng `work_schedule_checkout` (chuỗi `wcheckout:<item_id>`, một mục một check-out, `checkin_id` trỏ check-in của mục). Như check-in: sổ không có tọa độ (QĐ-8); tọa độ check-out vào bảng có hạn lưu `work_schedule_checkin_point` với `checkin_id` = id check-out, job cũ xóa khi quá hạn | Migration `20261106191624_pf_extension_cnts_checkout_photo`, `store/schedule_checkin.go` |
| Check-in cũ không ảnh | Dòng có `photo_key IS NULL` = dữ liệu trước v4.0: giữ nguyên (không UPDATE, không nhập bù), API trả `legacy_no_photo: true`, web ghi "Dữ liệu cũ — không ảnh", app ghi "dữ liệu cũ, không ảnh". Check-in cũ vẫn check-out được (kèm ảnh) | `TestWorkScheduleCheckinLegacy` |
| Kịch bản đếm trước/sau và quay lui | Migration chỉ thêm (cột NULL được + sổ mới), không chuyển dữ liệu: số dòng check-in và băm dòng cũ không đổi, `verify_chain` đúng cho chuỗi cũ và mới. Quay lui = chạy lại bản ứng dụng trước #249: câu INSERT cũ (không cột ảnh) vẫn chạy trên lược đồ mới và tạo dòng "dữ liệu cũ"; bảng/cột mới để nguyên (không DROP — CONTEXT §4). Kiểm chứng trên PostgreSQL thật (pg-ephemeral) | `TestWorkScheduleCheckinLegacy`; truy vấn đếm dưới bảng |
| Lưu ảnh | Dùng lại kho đối tượng của module (`shared/objstore`, cùng kho của ảnh thực địa KNQG, tệp KHTC — `Deps.Store`/`UseObjectStore`), khóa `cnts-visit-photo/<tenant>/<item>/<checkin|checkout>/<sha256>`; sổ chỉ giữ khóa, kiểu, cỡ, SHA-256. Ảnh là hồ sơ công tác của cán bộ (BR-06 "lưu ảnh… không sửa được"): không mã hóa phong bì theo hộ, không có hạn xóa như tọa độ. Ghi ảnh trước giao dịch DB (không I/O ngoài trong giao dịch); yêu cầu bị từ chối sau khi ghi ảnh để lại tệp mồ côi khóa theo băm (như KNQG) | `app.putVisitPhoto` |
| Kích thước, kiểu ảnh | ≤ 500 KB (BRD Mục VIII), JPEG/PNG/WebP đoán từ nội dung. Gửi trong thân JSON (`photo`, base64) để lệnh ngoại tuyến của Mobile là một request một Idempotency-Key; dưới trần thân 1 MiB của API | `domain.VisitPhotoType`, `handler.visitRequest` |
| Xem ảnh | `GET …/items/{item_id}/checkin/photo`, `…/checkout/photo`: người đăng ký lịch và người duyệt được lịch (như quyền xem tọa độ, UR-TINH-18); người khác 404/403 | `app.VisitPhoto` |
| Check-out | Chỉ sau check-in của mục (409 `extension.checkout_no_checkin`); thời điểm thiết bị thuộc ngày công tác, không trước check-in, không ở tương lai, gửi muộn ≤ 7 ngày (như check-in); cách địa điểm CV-06 > ngưỡng (500 m) → cảnh báo `checkout_far`; GPS yếu, vị trí giả lập → cảnh báo | `app.Checkout`, `domain.CheckCheckoutAfter` |
| Thời gian ở địa điểm, Đồng bộ trễ | `stay_minutes` = check-out − check-in (phút, BR-07/AC-12); `synced_late` khi máy chủ nhận (đồng hồ dịch vụ, `synced_at`) sau ngày công tác giờ Việt Nam (BR-08/AC-13). Suy ra khi đọc, không lưu cờ thừa | `domain.StayMinutes`, `domain.SyncedLate` |
| Ngoài phạm vi gói | Nhãn "Chưa check-out" lúc 23:59, cảnh báo cho người theo dõi, tự đóng cảnh báo khi check-out đồng bộ (BR-07, BR-08 phần cảnh báo, AC-10, Mục III.5 dòng 13) và màn Hình 49 thuộc CN-4 #250, dựa trên sổ `work_schedule_checkout` (`tenant_id, schedule_id`/`item_id`) và cột ảnh của gói này | #250 |

Truy vấn đếm (chạy trước và sau khi áp migration, kết quả phải bằng nhau ở cột `cu`):

```sql
SELECT count(*) AS tong, count(*) FILTER (WHERE photo_key IS NULL) AS cu FROM pf_extension.work_schedule_checkin;
SELECT count(*) AS checkout FROM pf_extension.work_schedule_checkout;
```

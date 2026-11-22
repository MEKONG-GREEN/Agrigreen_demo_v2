## CNTS-V4-CHECKIN-MONITOR — Theo dõi check-in, check-out; cảnh báo "Chưa check-out" 23:59 (#250, pf 20261106210511)

Nguồn: BRD CNTS v4.0 (CR-CNTS-001) FN-12 (BR-07, BR-08, AC-10, AC-13), FN-13 (BR-04, Mục III.5 dòng 13), UR-TINH-18
(Hình 49). Dựng trên sổ `work_schedule_checkin` (cột ảnh) và `work_schedule_checkout` của #249.

| Mục | Phương án đã chọn | Ở đâu |
|---|---|---|
| Nhãn "Chưa check-out" | Suy ra khi đọc, không lưu: mục đã check-in (có ảnh), chưa có check-out và đồng hồ dịch vụ ≥ 23:59 ngày công tác giờ Việt Nam (`clock.Zone()`). Check-out đồng bộ trễ (BR-08) làm nhãn tự gỡ. API: `no_checkout` trên check-in, `no_checkout_count` trên lịch, cảnh báo `checkout_missing` | `domain.NoCheckout`, `app.applyCheckins` |
| Màn cán bộ xã | Web (chi tiết lịch) và field-app (dòng mục công tác) hiện nhãn từ máy chủ | `Visits.tsx`, `work_schedule.dart` |
| Màn theo dõi (Hình 49) | Tab "Check-in, check-out" của màn Lịch công tác Cấp Tỉnh: mọi mục của lịch Cấp Xã trong tuần (lọc xã, tìm cán bộ), check-in, check-out, thời gian ở địa điểm, Đồng bộ trễ, ảnh, nhãn; lọc nhanh "Chỉ mục Chưa check-out". Dùng lại API danh sách lịch (đã có mục và check-in), không thêm endpoint | `VisitMonitor`, `ApproveSchedules.tsx` |
| Cảnh báo dòng 13 | Loại `no_checkout` của sổ cảnh báo CNTS (FN-13), mức Cần chú ý, chống trùng theo mục (`no_checkout:<xã>:item:<item_id>`), `subject_kind` = `commune` (xã của mục). Người dùng đóng tay thì không mở lại | `cntsalert/app/no_checkout.go` |
| Người nhận "người theo dõi lịch cán bộ xã" | Người duyệt được lịch của cán bộ đó theo quy tắc của phòng ban (FN-12 BR-04: người theo dõi được phân công hoặc Lãnh đạo phòng; hoặc cấp quản lý theo cấu hình) — cùng hàm với người nhận thông báo duyệt | `app.ScheduleFollowers` (nối ở `UseCNTSAlerts`) |
| Mốc 23:59 | Dùng job sẵn có `extension_cnts_alert` của worker (mỗi 5 phút, canh mốc tròn UTC = mốc tròn giờ Việt Nam) — lượt 00:00 sinh cảnh báo ngay sau 23:59; nhãn trên màn chính xác tới phút vì tính khi đọc. Không thêm job, không gắn cứng ngày | `JobTenant`, `TestWorkScheduleNoCheckout` (đồng hồ giả lập) |
| Tự đóng (BR-08) | Cùng job: cảnh báo đang mở của mục đã có check-out → tự đóng (ghi giờ check-out, giờ đồng bộ nếu trễ). Độ trễ tối đa một chu kỳ job (5 phút) | `closeCheckedOut` |
| Cửa sổ quét | Ngày công tác trong 8 ngày gần nhất (check-out gửi muộn tối đa 7 ngày — #249), đọc hết theo trang; tối đa 500 cảnh báo mới mỗi lần chạy mỗi tenant | `store.UnclosedCheckins` |
| Phạm vi | Chỉ lịch Cấp Xã của phòng CNTS (cảnh báo "người theo dõi lịch cán bộ xã"); nhãn hiện cho mọi check-in có ảnh | — |
| Dữ liệu cũ | Check-in trước v4.0 (không ảnh, khi đó chưa có check-out) không gắn nhãn, không sinh cảnh báo — không bắt nhập bù (PLAN mục 5) | `TestWorkScheduleNoCheckout` |
| Migration | Chỉ GRANT `SELECT (tenant_id, day)` sổ check-in cho `ag_ops_jobs` (tìm tenant) và chỉ mục `(tenant_id, day)`; không đổi dữ liệu | `20261106210511_pf_extension_cnts_no_checkout` |

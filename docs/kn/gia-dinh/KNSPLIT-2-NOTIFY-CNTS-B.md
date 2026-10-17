## KNSPLIT-2-NOTIFY-CNTS-B — Xóa bộ dựng CNTS dùng chung

Nền1eb927b0. Ca âm27832 actualexit1 trên A chứng minh cả Renderers nil và empty đều gọi fallback CNTS cho ba sự kiện dù không có owner renderer. B xóa ba case khỏi HandleEvent, xóa domain/cnts.go và generator/test legacy. Cấu hình có provider vẫn chạy đúng ba callback của KN; cấu hình thiếu provider không dựng CNTS ngầm. Registry generic, authority inbox/transaction/IDs/source_event, Normalize/Redact, RLS và kênh gửi giữ nguyên.

Chuyển nguyên năm KindCNTS* sang extension/port/notification_cnts.go, không alias ngược; owner provider và hai tệp kiểm tra composition đổi qualifier. Giá trị kind, JSON, URL, role, email, thời gian, SQL và dữ liệu lưu giữ nguyên, không thêm migration. Modgen chỉ cập nhật metadata đã bị gỡ. Không đổi native MRV (HTX/MKG), không thay các builder KN khác còn lại trong core.

Giữ byte Git của ba artifact: kn-notify-consumers.json (16 nhóm/41 binding), kn-notify-cnts-inputs.json (46 input) và kn-notify-cnts.json (46 kết quả OLD). Owner test chỉ đọc expected; bỏ log/generator legacy. Kiểm thử W1143 cũ được thay bằng 46 kết quả đầy đủ (kind/severity/body/email/topic/dedup/nil/lỗi/giờ/rune) cùng PG registeredAlertReminder/Duty và generic delivery guard (normalization/redaction/rollback/invalid-link/source provenance), không xuất API Normalize chỉ để giữ test hay thêm cap→KN import.

Impact: tìm tất cả năm kind, CNTSEvents/CNTSNotifications/links và nhánh HandleEvent; full notify/... + KN extension/price roots + API assembly + worker + tests/notifyconsumers + tests/notify, tổng9package có test. Đổi quyền sở hữu kiểu và xóa runtime fallback nên chạy toàn phạm vi trên PostgreSQL riêng. Chỉ prune baseline thực đã xóa (dự kiến343→330), không sửa rule/canonical alias. Pure5, full static, PG9 thật không FAIL/SKIP và helper9migration/remote trước dev. Không OpenAPI/UI/mobile thay đổi; main/worktree giữ. G1 còn notify khác, authz/accounts/party, data products/routes/tenant/client và KHTC reminder.


Pure61559 actualexit0 đủ5package, RED27832actual1→GREEN gồm nil/empty×3event khôngfallback, registered3event giữdecodeerror; 46owner outputs/errors khớp frozen. Git blob cả3golden nguyên1eb927b0. Modgen/prune343→330 đúng13legacyentries, rule/alias không đổi. Git ghi rename5declaration100% từportcap vềKNport; không alias. Production3d910ce6; chưa static/PG/helper/dev.

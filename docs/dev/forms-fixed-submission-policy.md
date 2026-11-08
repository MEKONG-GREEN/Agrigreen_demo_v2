# Forms: fixed submission policy và receipt gửi

Ứng dụng đăng ký `Profile.FixedMaxSubmissions` và `AutoLockAtLimit` bằng cấu hình khởi động thật. Zero giữ profile có giới hạn cấu hình theo kỳ. Rule đã lưu trong `pf_forms.profile_submission_policy` không đổi/xóa; khởi động với cùng tên mà khác rule hoặc bỏ fixed rule sẽ lỗi. Bỏ hẳn profile khỏi ứng dụng làm capability profile đó không tồn tại. Thiếu migration không được fallback.

Đây là registry tĩnh của deployment, không có tenant/PII; không có route cấu hình và không dùng biến ngữ cảnh GUC để chọn rule. Native đọc đặc tả hiệu lực theo profile của **kỳ canonical**; `period_spec` nguyên bản vẫn dùng cho receipt publication lịch sử. Trigger submission và review cũng đọc rule này. SQL hoặc caller giả profile không thay được cap. Không sửa dữ liệu lịch sử.

Publish/PublishBatch từ chối override khác fixed cap. Submit/Review dùng khóa kỳ hiện hữu. Auto-lock policy khóa bài tại cap, cấm cả accepted và returned ở cap; profile không bật giữ hành vi cũ. Owner projection `ResolvedActor.ReadOnly` chặn ghi trước cấp quyền ngoại lệ và trước receipt replay.

Với profile đăng ký fixed policy và có `Idempotency-Key`, Submit ghi `pf_forms.submission_receipt` bằng `shared.apply_ledger` cùng giao dịch submission/held, meta, workflow và outbox. Request v1 gắn nội dung và nil-vs-zero base, offline ID/thời gian, ghi chú; DB kiểm receipt có đúng tuple gốc của thao tác. Danh tính receipt bị khóa bởi stream gắn key với seq=1, kể cả sau TTL của ledger_idem; mất receipt không được suy từ latest. Replay xác thực lại actor/profile/template/current target trước, trả đúng bài nộp và trạng thái ACK gốc, không tiêu tốn lần gửi. Đổi nội dung/actor/target dùng cùng khóa trả conflict. Receipt không đặt thêm trần byte thấp hơn hợp đồng số liệu native; kiểm biểu bất biến trước encode, giữ đủ Unicode/JSON escape. Profile không fixed không ghi receipt mới và giữ hành vi sync ID, timestamp, payload cũ; fixed policy không cho dùng ID tồn tại làm receipt của một khóa khác.

Thứ tự triển khai: chạy migration tiến bằng `go run ./cmd/migrate up` (môi trường triển khai đã cấu hình, chọn cụm pf), sau đó khởi động API/worker với owner Profile. Không cần backfill. Nếu registry xung đột, đối soát cấu hình và migration; không xóa/chỉnh rule để vượt cổng.

Kiểm lại (từ repo, đặt toolchain/Go temp theo hướng dẫn dự án):

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/platform/forms/... ./internal/kn/extension/khtcperiod/... ./internal/kn/extension/khtcform/... ./internal/kn/extension/adhocintegration ./internal/kn/extension ./cmd/api-platform/assembly ./migrations/...
```

Các test policy/receipt mang hậu tố217; consumer HRM/CNTS/reportcycle và assembly cần nằm trong membership hồi quy. `go build ./...`, `go vet ./...`, cổng tĩnh, full web và whole Linux race/CI vẫn áp dụng.

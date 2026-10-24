## P0-WORKER-REPORTS — Worker gửi báo cáo: hết panic, không gửi trùng, tự kết nối lại (#128)

Nguồn: báo cáo Ecombox (commit 5f7751f2, §6), đối chiếu lại 24/10/2026. Ngoại lệ làn shared (Claude làm theo chỉ đạo "Mở
ticket P0 và chia việc chạy song song cuối tuần"), Codex review chéo trước khi gộp.

### Nguyên nhân

- `cmd/worker/reports.go` dựng `reports.New(nil, …)`: handler gửi Chi cục gửi email xong rồi ghi dấu "đã gửi người nhận"
  qua `Service.tx` trên DB nil → panic → worker sập → RabbitMQ giao lại → gửi lại người đã nhận (6 email một lần bấm),
  lần gửi kẹt "Đang gửi" và "Gửi lại" bị chặn (409 `reports.dispatch_sending`) vĩnh viễn.
- Kênh RabbitMQ đóng → `inbox.ConsumeWith` trả lỗi → `run` trả lỗi → tiến trình thoát (20 lần khởi động lại).

### Đã làm

| Mục | Cách làm | Ở đâu |
|---|---|---|
| Pool DB thật + kiểm khi khởi động | pf/reports của worker dựng trên pool `app_worker` (như các module nguồn); `Module.RequireWorkerDeps` (DB, Clock, Store, Params) chạy ở bước `prepare` của consumer TRƯỚC khi nhận thông điệp — thiếu → worker không khởi động. `Service.tx` thiếu DB → 503 thay vì panic; handler panic → lỗi thông điệp (NACK/DLQ), không sập worker | `cmd/worker/reports.go`, `cmd/worker/main.go`, `internal/platform/reports/module.go` |
| Nhận việc nguyên tử | `UPDATE pf_reports.dispatch SET claim_token, claim_until WHERE id AND status='sending' AND attempts=$n AND (claim_until IS NULL OR claim_until <= now)` ở giao dịch riêng, commit trước khi gửi. Giao dịch inbox chỉ đọc (bỏ khóa `FOR NO KEY UPDATE` — khóa đó sẽ chặn câu nhận việc) | `store.ClaimDispatch`, `app.HandleDispatchRequested` |
| Không gửi lại người đã nhận | Trước mỗi người nhận: gia hạn việc (mất quyền giữ → dừng) + đọc mới dấu `recipient_mailed` (idem_key `dispatch-mailed:<lần gửi>:r<vòng>:<người nhận>`, sổ chỉ ghi thêm; vòng = số dòng `sent` của lần gửi); gửi; ghi dấu ngay ở giao dịch riêng với ctx không bị hủy | `store.RenewDispatchClaim`, `store.HasDispatchEvent` |
| Ghi kết quả | sent/failed + dòng sổ ghi có điều kiện `claim_token` còn đúng, rồi nhả việc — giao dịch inbox hủy sau đó, giao lại thấy "sent" → không gửi | `store.FinishDispatchClaim` |
| "Đang gửi" quá hạn | Hạn `AG_REPORT_DISPATCH_LEASE` (thời lượng Go 3m–24h, mặc định 15m; sai → lỗi khởi động api-platform/worker; worker còn kiểm hạn ≥ hạn xử lý thông điệp inbox 2m + biên). Quá hạn (worker chết, sự kiện vào DLQ) → "Gửi lại" được phép và **giữ nguyên lượt** (không tăng `attempts`), sổ `send_requested` ghi `resumed: true` → worker mới chỉ gửi người chưa có dấu. Chưa ai nhận việc: tính hạn từ `updated_at` lúc xếp gửi | `app.SendDispatch`, `sendingStale` |
| Gửi lại sau "Gửi lỗi" một phần (review #128) | Lượt mới (`attempts`+1) nhưng **cùng vòng gửi**: dấu người nhận khóa theo vòng (số lần đã `sent`), không theo lượt → người đã nhận ở lượt lỗi không nhận lần hai; sổ `send_requested` ghi `continues_round: true`. Vòng mới (gửi lại mọi người) chỉ khi bấm "Gửi lại" sau "Đã gửi" — chủ động. Dấu dạng cũ theo lượt vẫn được đọc cho lượt dở dang lúc nâng cấp | `app.SendDispatch`, `mailedKey`, `store.CountDispatchEvents` |
| SMTP không sống quá hạn nhận việc (review #128) | Mỗi lời gọi `Mailer.Send` chạy với ctx có deadline = lúc vừa gia hạn + hạn − biên (¼ hạn, tối đa 30s) → SMTP treo bị cắt ("Gửi lỗi", gửi lại được) trước khi worker khác được nhận lại. Khởi động: `ValidateSendLease` từ chối hạn < `inbox.DefaultHandlerTimeout` + biên. Worker đến muộn (thư viện SMTP bỏ qua deadline) ghi dấu gặp 23505 → coi là đã có dấu, log `reports.dispatch.duplicate_send`, không lỗi handler | `app.HandleDispatchRequested`, `dispatch.SendMargin`, `dispatch.ValidateSendLease`, `cmd/worker/reports.go` |
| RabbitMQ | Vòng giám sát: phiên (kết nối + mỗi consumer một kênh + khai báo lại hàng đợi/DLQ/bind) đứt → hủy mọi consumer của phiên, chờ dừng hẳn, lùi 1s·2^(n-1) (trần 30s), quay số lại tới khi được, đăng ký lại consumer. Lần quay số đầu lỗi → không khởi động (cấu hình sai lộ ngay). SIGTERM → dừng êm như cũ. `/readyz` theo kết nối hiện hành | `cmd/worker/amqp_reconnect.go` |

### Giả định / hạn chế (hỏi lại khi cần)

| Câu hỏi | Lựa chọn đã dùng | Đổi thế nào |
|---|---|---|
| Worker giữ việc chết, thông điệp được giao lại khi việc còn hạn | Bỏ qua (không gửi song song, inbox ghi đã xử lý); người dùng "Gửi lại" sau hạn (mặc định 15 phút). Không có job tự quét lần gửi quá hạn | Thêm job `reports_dispatch_reclaim` xếp lại sự kiện cho lần gửi quá hạn nếu PO muốn tự động |
| Cửa sổ trùng còn lại | Email đã đi nhưng chưa ghi được dấu (DB lỗi đúng lúc đó) → lần nhận lại có thể gửi trùng **một** người nhận — "đúng một lần trong thực tế", SMTP không có giao dịch hai pha | — |
| Hạn nhận việc so với thời gian gửi | Việc được gia hạn trước mỗi người nhận; một lời gọi SMTP bị cắt ở hạn − biên, nên email lớn/SMTP chậm hơn mức đó ghi "Gửi lỗi" | Tăng `AG_REPORT_DISPATCH_LEASE` |
| Hiển thị | Màn web không đổi: "Đang gửi" quá hạn bấm "Gửi lại" như "Gửi lỗi" | — |
| Migration | `pf 20261024021637_pf_reports_dispatch_claim`: thêm `claim_token`, `claim_until` vào BẢNG CHIẾU `pf_reports.dispatch` (không phải sổ); sổ `dispatch_event` giữ nguyên chỉ ghi thêm | — |
| LogMailer của pf/notify (#127) | Không đụng | — |

### Kiểm thử

- `cmd/worker`: `TestReportsWorkerStartupWiresDB` (PostgreSQL thật — pool app_worker, kiểm phụ thuộc, handler chạy giao dịch; consumer có `prepare` lỗi khi thiếu cấu hình), `TestReportsWorkerNilDBFailsFast`, `TestAMQPLoopReconnects`, `TestAMQPLoopStopsDuringBackoff`, `TestAMQPBackoff` (phiên giả, không cần broker).
- `tests/kn77`: `TestP0WorkerReports_ConcurrentWorkersSendOnce` (4 worker cùng sự kiện → mỗi người nhận đúng 1 email), `TestP0WorkerReports_StaleSendingResumes` (worker chết sau người 1 → còn hạn: 409 + giao lại không gửi; quá hạn: "Gửi lại" cùng lượt chỉ gửi người 2), `TestA4_ChiCucDownloadLink7Days` (giao lại sau khi inbox hủy không gửi trùng).
- Review #128 (PostgreSQL thật, trước sửa FAIL / sau sửa PASS): `TestP0WorkerReports_PartialFailureRetryKeepsMailed` (người 2 bị SMTP từ chối → "Gửi lại" không gửi lại người 1; sau "sent" vòng mới gửi mọi người), `TestP0WorkerReports_LeaseExpiryDuringSMTPSendsOnce` (hạn 2s, A treo trong SMTP → bị cắt trước hạn; B nhận lại → đúng một lần SMTP thành công tới người 1), `TestP0WorkerReports_LateFinisherMarkTolerated` (SMTP bỏ qua deadline → dấu muộn gặp 23505 được dung thứ). `cmd/worker` `TestReportsWorkerNilDBFailsFast` kiểm `1m` bị từ chối và `ValidateSendLease`.

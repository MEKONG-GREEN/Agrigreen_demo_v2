---
paths:
  - "internal/**/app/**/*.go"
  - "internal/**/store/**/*.go"
  - "internal/**/*worker*.go"
  - "internal/shared/**/*.go"
  - "cmd/worker*/**/*.go"
---
# Go: transaction, I/O, worker

Nguồn: [transactions-and-io.md](../../docs/conventions/transactions-and-io.md), [fail-closed-config.md](../../docs/conventions/fail-closed-config.md), AGENTS.md §9 mục 1, 2, 4. Khi lệch, nguồn thắng.

## Trong thân `func(tx pgx.Tx) error` (`tenant.WithTenantTx`, `db.WithContextTx`, `db.WithModuleTenantTx`, `db.WithAggTx`)

- **Chỉ được:** SQL trên schema của module, `outbox.Enqueue(ctx, tx, …)`, hàm thuần.
- **Không được:** gọi `port` module khác (`Forms.FormVersion`, `Scope.Scope`, `Units.Get`, `Params.At`, `Orgs.Get`, …); gửi email/SMS/FCM; S3/kho tệp; KMS/`vault/identity`; HTTP; publish RabbitMQ.
- **Đọc trước, ghi sau:** lấy dữ liệu module khác ngoài tx; trong tx kiểm lại bằng điều kiện SQL (phiên bản/trạng thái), 0 hàng → lỗi stale → thử lại có giới hạn (mẫu `internal/kn/extension/reportcycle/app/deps.go`: tối đa 5 lần rồi 503).
- **Gửi sau commit hoặc qua outbox**; việc gửi chịu được chạy lại.
- Lấy kết nối phải có thời hạn (context có deadline).

```go
// SAI: port module khác trong tx -> cần kết nối thứ hai -> cạn pool ở 20 CCU
err := tenant.WithTenantTx(ctx, s.pool, tid, func(tx pgx.Tx) error {
    v, err := s.forms.FormVersion(ctx, formID)
    if err != nil { return err }
    return s.store.OpenTask(ctx, tx, taskID, v)
})

// ĐÚNG: đọc ngoài tx, kiểm lại phiên bản trong tx
v, err := s.forms.FormVersion(ctx, formID)
if err != nil { return err }
err = tenant.WithTenantTx(ctx, s.pool, tid, func(tx pgx.Tx) error {
    return s.store.OpenTask(ctx, tx, taskID, v) // UPDATE … WHERE form_version = $v
})
```

## Worker và kênh ngoài (fail-closed)

- Kiểm phụ thuộc lúc khởi động (pool DB, broker, kênh gửi khác nil và kết nối được); thiếu → thoát với lỗi nêu rõ, không panic giữa chừng. Mất broker → tự kết nối lại.
- Nhận việc nguyên tử: `UPDATE … SET status='sending', claimed_at=now() WHERE id=$1 AND status='pending' RETURNING …`; việc `sending` quá hạn được trả về `pending`.
- Thiếu cấu hình kênh ngoài → kênh tắt, bản gửi `dropped` kèm `channel_disabled:<lý do>`; cấu hình một phần/sai → **không khởi động**, lỗi nêu tên biến. Chỉ ghi `sent` khi kênh thật đã nhận. Bộ giả (`AG_MAILER=log`) chỉ trên máy phát triển.
- `AG_MFA_DISABLED` chỉ hợp lệ khi `AG_DEPLOY_ENV=demo`.

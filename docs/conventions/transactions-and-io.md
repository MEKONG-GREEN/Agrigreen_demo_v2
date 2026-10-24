# Transaction và I/O: không gọi port module khác, không I/O ngoài trong DB transaction

Bắt buộc từ 24/10/2026 ([AGENTS.md §9 mục 1](../../AGENTS.md#9-lỗi-bắt-buộc-tránh)). Ticket gốc: [#125](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/125), [#126](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/126), [#139](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/139).

## Quy tắc

Bên trong hàm truyền cho `tenant.WithTenantTx`, `db.WithContextTx`, `db.WithModuleTenantTx`, `db.WithAggTx` (hoặc bất kỳ chỗ nào đang giữ `pgx.Tx`):

1. **Không gọi port của module khác** mà port đó có thể tự mở transaction/lấy kết nối mới (`Forms.FormVersion`, `Scope.Scope`, `Units.Get`, `Params.At`, `Orgs.Get`, …).
2. **Không I/O ra ngoài DB**: gửi email/SMS/FCM, ghi/đọc kho tệp (S3), gọi KMS/`vault/identity`, HTTP tới dịch vụ khác, publish RabbitMQ.
3. Không chờ vô hạn: lấy kết nối từ pool phải có thời hạn (context có deadline).

Được phép trong tx: SQL trên chính schema của module, ghi outbox (`shared/outbox`) để việc gửi xảy ra sau commit, gọi hàm thuần.

## Vì sao

Mỗi transaction giữ một kết nối của pool (mặc định `DefaultUnitMaxConns = 20` cho cả đơn vị triển khai — mọi module dùng chung một pool). Nếu trong tx lại gọi port mở tx thứ hai, mỗi request cần **hai** kết nối. Với N request đồng thời ≥ kích thước pool, mọi request đều đang giữ một kết nối và chờ kết nối thứ hai → **deadlock pool**, toàn hệ treo (báo cáo Ecombox: treo ở 20 CCU). I/O mạng trong tx kéo dài thời gian giữ kết nối và khóa hàng theo độ trễ của dịch vụ ngoài; dịch vụ ngoài chậm → cạn pool.

## Cách làm đúng

**Đọc trước, ghi sau.** Lấy dữ liệu của module khác ngoài tx, rồi mở tx chỉ cho phần ghi của module mình. Nếu dữ liệu đọc ngoài tx có thể đổi trước khi tx chạy, kiểm lại trong tx bằng điều kiện SQL (phiên bản/trạng thái) và **thử lại có giới hạn** (mẫu `deps.go` trong `internal/kn/extension/reportcycle/app` do #125 đưa vào: đọc phụ thuộc ngoài tx, chạy lại thân tx tối đa 5 lần, sau đó trả 503).

```go
// SAI: port module khác bên trong tx → cần kết nối thứ hai
err := tenant.WithTenantTx(ctx, s.pool, tid, func(tx pgx.Tx) error {
    v, err := s.forms.FormVersion(ctx, formID) // mở WithTenantTx riêng
    if err != nil { return err }
    return s.store.OpenTask(ctx, tx, taskID, v)
})

// ĐÚNG: đọc ngoài tx, kiểm lại phiên bản trong tx
v, err := s.forms.FormVersion(ctx, formID)
if err != nil { return err }
err = tenant.WithTenantTx(ctx, s.pool, tid, func(tx pgx.Tx) error {
    return s.store.OpenTask(ctx, tx, taskID, v) // UPDATE … WHERE form_version = $v; 0 hàng → errStale → thử lại
})
```

**Gửi sau commit hoặc qua outbox.**

```go
// SAI: gửi email trong tx
err := tenant.WithTenantTx(ctx, s.pool, tid, func(tx pgx.Tx) error {
    if err := s.store.MarkDigest(ctx, tx, id); err != nil { return err }
    return s.mailer.Send(ctx, to, subject, body)
})

// ĐÚNG: ghi ý định trong tx (outbox/hàng đợi gửi), worker gửi sau commit với claim nguyên tử
err := tenant.WithTenantTx(ctx, s.pool, tid, func(tx pgx.Tx) error {
    if err := s.store.MarkDigest(ctx, tx, id); err != nil { return err }
    return outbox.Enqueue(ctx, tx, outbox.Event{ /* loại sự kiện + payload digest */ })
})
```

Giữ idempotency khi chuyển I/O ra sau commit: việc gửi phải chịu được chạy lại ([idempotency.md](idempotency.md)) và nhận việc nguyên tử (`UPDATE … SET status='sending' WHERE id=$1 AND status='pending' RETURNING …`).

## Mẫu kiểm thử: pool nhỏ, gọi song song

Test tái hiện phải **đỏ trên dev trước khi sửa** và xanh sau sửa. Mở pool riêng kích thước nhỏ (pool của `pgtest.Get(t).Pool` có 16 kết nối — quá lớn), gọi song song nhiều hơn kích thước pool, đặt hạn chờ:

```go
func TestOpenTask_NoPoolDeadlock(t *testing.T) {
    env := pgtest.Get(t)
    dsn, err := dbadmin.RoleURL(env.PoolURL, migrations.PF, env.Suffix, "<vai app>")
    if err != nil { t.Fatal(err) }
    const size = 2
    p, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: true, MaxConns: size, AppName: "pool-deadlock-test"})
    if err != nil { t.Fatal(err) }
    t.Cleanup(p.Close)
    svc := newServiceWithPool(t, p) // mọi module dùng CHUNG pool nhỏ, như production

    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()
    var g errgroup.Group
    for i := 0; i < 4*size; i++ {
        g.Go(func() error { _, err := svc.GetTask(ctx, actor, taskID); return err })
    }
    if err := g.Wait(); err != nil {
        t.Fatalf("gọi song song với pool %d kết nối: %v (deadline = treo pool)", size, err)
    }
}
```

Điểm mấu chốt: các module tham gia (bên gọi và port được gọi) phải dùng **cùng** pool nhỏ; hết hạn context = lỗi deadlock. Chạy với `-v` và xác nhận `--- PASS` ([AGENTS.md §8](../../AGENTS.md#8-kiểm-thử-và-cổng)).

## Review

Reviewer tìm trong diff mọi hàm `func(tx pgx.Tx) error` mới/sửa và kiểm từng lời gọi bên trong: chỉ `tx.*`, store của module, outbox, hàm thuần. Lời gọi `s.<port>.…`, `mailer`, `storage`, `vault`, `http` trong thân tx → yêu cầu sửa.

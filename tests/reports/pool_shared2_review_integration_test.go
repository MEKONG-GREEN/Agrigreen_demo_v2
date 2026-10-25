package reportstest

// P0-POOL-SHARED-2 — review #143 C2: email lịch báo cáo.
//   - Hạn SMTP tính từ TRƯỚC giao dịch nhận việc (cùng mốc với delivery_claim_until), không từ lúc giao dịch nhận commit:
//     giao dịch nhận chờ kết nối lâu (pool cạn) vẫn không đẩy SMTP quá hạn giữ.
//   - MarkDelivered chỉ ghi khi nhận việc còn là của mình: worker mất nhận việc không ghi đè kết quả, không xóa nhận việc
//     của worker khác.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestP0PoolShared2_DeliverySMTPDeadlineFromClaimStart(t *testing.T) {
	modPool := pgtest.Get(t).SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	f := newFixtureWith(t, true, modPool)
	ev, _ := scheduledCompleted(t, f)
	const lease = 4 * time.Second // biên dispatch.SendMargin = 1 s → hạn gửi = mốc trước giao dịch nhận + 3 s
	f.mod.UseDeliveryLease(lease)
	var stopped atomic.Int64
	start := time.Now()
	f.mailer.withCtx = func(ctx context.Context) error {
		select {
		case <-ctx.Done():
		case <-time.After(3 * lease):
		}
		stopped.Store(int64(time.Since(start)))
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("fixture: SMTP treo")
	}
	// Giữ kết nối DUY NHẤT của pool module 2 s: giao dịch nhận việc (ngoài giao dịch inbox) phải chờ.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := modPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(2 * time.Second); conn.Release() }()
	if _, err := inbox.Process(ctx, f.pool, port.DeliveryConsumer, ev, f.mod.DeliveryHandler()); err == nil {
		t.Fatal("SMTP quá hạn phải trả lỗi (giao lại)")
	}
	if d := time.Duration(stopped.Load()); d == 0 || d >= lease {
		t.Fatalf("P0-POOL-SHARED-2 C2: SMTP dừng sau %s, muốn trước hạn nhận việc %s (giao dịch nhận chờ ~2 s)", d, lease)
	}
	if st, _, claimed := deliveryState(t, f); st != nil || claimed {
		t.Fatalf("SMTP quá hạn: status=%v claimed=%v", st, claimed)
	}
}

func TestP0PoolShared2_DeliveryLostClaimNotOverwritten(t *testing.T) {
	f := newFixture(t, true)
	ev, _ := scheduledCompleted(t, f)
	g := newGate(t)
	f.mailer.before = func(port.Message) error { return g.wait() }
	done := processAsync(f.pool, port.DeliveryConsumer, ev, f.mod.DeliveryHandler())
	g.waitEntered(t, "Mailer.Send")
	// Nhận việc của worker 1 hết hạn trong lúc gửi, worker 2 nhận lại (giả lập).
	other := ids.New()
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE pf_reports.schedule_run SET delivery_claim_id = $1, delivery_claim_until = $2
		  WHERE job_id IS NOT NULL AND delivery_status IS NULL`, other, f.clk.Now().Add(time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	g.open()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var holder *string
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT delivery_claim_id::text FROM pf_reports.schedule_run WHERE job_id IS NOT NULL
		  ORDER BY slot_at DESC LIMIT 1`).Scan(&holder)
	}); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := deliveryState(t, f); st != nil || holder == nil || *holder != other.String() {
		t.Fatalf("P0-POOL-SHARED-2 C2: worker mất nhận việc ghi đè kết quả/xóa nhận của worker khác: status=%v holder=%v", st, holder)
	}
}

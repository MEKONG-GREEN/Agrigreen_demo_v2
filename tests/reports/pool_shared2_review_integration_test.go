package reportstest

// P0-POOL-SHARED-2 — review #143 C2: email lịch báo cáo.
//   - Hạn SMTP tính từ TRƯỚC giao dịch nhận việc (cùng mốc với delivery_claim_until), không từ lúc giao dịch nhận commit:
//     giao dịch nhận chờ kết nối lâu (pool cạn) vẫn không đẩy SMTP quá hạn giữ.
//   - MarkDelivered chỉ ghi khi nhận việc còn là của mình: worker mất nhận việc không ghi đè kết quả, không xóa nhận việc
//     của worker khác.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// TEST-143-DEFLAKE (#153): không đo đồng hồ từ một mốc phía test (máy tải → độ trễ trước giao dịch nhận và độ trễ fake
// nhận ra ctx hủy cộng vào số đo, đỏ giả). Kiểm trực tiếp: giao dịch nhận việc phải chờ kết nối DUY NHẤT mà test giữ tới
// mốc released; hạn neo TRƯỚC giao dịch nhận (đúng) → hạn ctx SMTP < released + (lease − biên); hạn neo SAU khi giao dịch
// nhận commit (lỗi C2) → hạn ≥ released + (lease − biên). So bằng đồng hồ đơn điệu, không cần dung sai.
func TestP0PoolShared2_DeliverySMTPDeadlineFromClaimStart(t *testing.T) {
	modPool := pgtest.Get(t).SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	f := newFixtureWith(t, true, modPool)
	ev, _ := scheduledCompleted(t, f)
	const lease = 4 * time.Second // biên reports.SendMargin = 1 s → hạn gửi = mốc trước giao dịch nhận + 3 s
	const hold = 2 * time.Second  // giữ kết nối: giao dịch nhận chờ
	sendWindow := lease - reports.SendMargin(lease)
	f.mod.UseDeliveryLease(lease)
	var mu sync.Mutex
	var (
		deadline time.Time
		hasDL    bool
		stopErr  error
		calls    int
	)
	f.mailer.withCtx = func(ctx context.Context) error {
		dl, ok := ctx.Deadline()
		select {
		case <-ctx.Done():
		case <-time.After(3 * lease):
		}
		mu.Lock()
		if calls == 0 {
			deadline, hasDL, stopErr = dl, ok, ctx.Err()
		}
		calls++
		mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("fixture: SMTP treo")
	}
	// Giữ kết nối DUY NHẤT của pool module: giao dịch nhận việc (ngoài giao dịch inbox) phải chờ tới released.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := modPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitBefore := modPool.Stat().EmptyAcquireWaitTime()
	releasedCh := make(chan time.Time, 1)
	go func() {
		time.Sleep(hold)
		at := time.Now()
		conn.Release()
		releasedCh <- at
	}()
	if _, err := inbox.Process(ctx, f.pool, port.DeliveryConsumer, ev, f.mod.DeliveryHandler()); err == nil {
		t.Fatal("SMTP quá hạn phải trả lỗi (giao lại)")
	}
	released := <-releasedCh
	// Giao dịch nhận việc phải thật sự chờ kết nối bị giữ — không thì test không phân biệt được hai mốc neo hạn.
	if waited := modPool.Stat().EmptyAcquireWaitTime() - waitBefore; waited < hold/4 {
		t.Fatalf("giao dịch nhận việc không chờ kết nối bị giữ (chờ %s) — kịch bản không kiểm được C2", waited)
	}
	mu.Lock()
	dl, ok, serr, n := deadline, hasDL, stopErr, calls
	mu.Unlock()
	if n == 0 {
		t.Fatal("Mailer.Send không được gọi")
	}
	if limit := released.Add(sendWindow); !ok || !dl.Before(limit) {
		t.Fatalf("P0-POOL-SHARED-2 C2: hạn SMTP (có hạn=%v) muộn %s so với mốc trả kết nối + %s — hạn phải neo TRƯỚC giao dịch nhận việc",
			ok, dl.Sub(limit), sendWindow)
	}
	if !errors.Is(serr, context.DeadlineExceeded) {
		t.Fatalf("P0-POOL-SHARED-2 C2: SMTP phải dừng vì hạn của chính nó, ctx.Err()=%v", serr)
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

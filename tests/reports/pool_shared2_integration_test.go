package reportstest

// P0-POOL-SHARED-2 (#143): consumer pf/reports KHÔNG gọi ra ngoài (kho đối tượng, danh bạ email, SMTP) khi đang giữ giao
// dịch inbox. Pool inbox CỠ 1 phải rảnh trong lúc ghi kho / gửi email treo; lỗi ngoài → không ghi kết quả, giao lại làm
// lại; giao lặp (cùng hoặc khác event_id, kể cả đồng thời) → một tệp, một email mỗi người nhận.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// storeHook bọc kho đối tượng của module: before chạy trước mỗi Put.
type storeHook struct {
	objstore.Store
	mu     sync.Mutex
	puts   atomic.Int32
	before func() error
}

func (h *storeHook) set(fn func() error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.before = fn
}

func (h *storeHook) Put(ctx context.Context, key string, r io.Reader, size int64, sum, ct string) (objstore.Info, error) {
	h.puts.Add(1)
	h.mu.Lock()
	before := h.before
	h.mu.Unlock()
	if before != nil {
		if err := before(); err != nil {
			return objstore.Info{}, err
		}
	}
	return h.Store.Put(ctx, key, r, size, sum, ct)
}

// gate chặn lời gọi ra ngoài tới khi test mở; entered báo mỗi lần vào.
type gate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGate(t *testing.T) *gate {
	g := &gate{entered: make(chan struct{}, 16), release: make(chan struct{})}
	t.Cleanup(g.open)
	return g
}

func (g *gate) wait() error {
	g.entered <- struct{}{}
	<-g.release
	return nil
}

func (g *gate) open() { g.once.Do(func() { close(g.release) }) }

func (g *gate) waitEntered(t *testing.T, what string) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(30 * time.Second):
		t.Fatalf("%s không được gọi", what)
	}
}

// assertPoolIdle: lời gọi ngoài đang treo — pool inbox cỡ 1 không có kết nối nào bị mượn và phục vụ được giao dịch khác.
func assertPoolIdle(t *testing.T, sized *pgxpool.Pool, what string) {
	t.Helper()
	if n := sized.Stat().AcquiredConns(); n != 0 {
		t.Fatalf("P0-POOL-SHARED-2: %s trong lúc giữ %d kết nối pool inbox (giao dịch inbox mở)", what, n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pgxBegin(ctx, sized); err != nil {
		t.Fatalf("pool cỡ 1 bị giữ trong lúc %s: %v", what, err)
	}
}

func pgxBegin(ctx context.Context, p *pgxpool.Pool) error {
	tx, err := p.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	_, err = tx.Exec(ctx, "SELECT 1")
	return err
}

func processAsync(sized *pgxpool.Pool, consumer string, env outbox.Envelope, h inbox.Handler) chan error {
	done := make(chan error, 1)
	go func() {
		_, err := inbox.Process(context.Background(), sized, consumer, env, h)
		done <- err
	}()
	return done
}

func TestP0PoolShared2_ExportPutOutsideInboxTx(t *testing.T) {
	f := newFixture(t, false)
	sized := f.env.SizedPool(t, migrations.PF, reports.DBRole, 1)
	admAG := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	r := admAG.must(admAG.post("/v1/reports/exports", map[string]any{
		"template_key": "kn.sowing_progress", "format": "xlsx", "params": map[string]any{"period": "2026-09"},
	}), http.StatusAccepted, "yêu cầu xuất")
	id := str(r.body, "id")
	ev := f.events(port.EventExportRequested)[0]

	// Ghi kho lỗi → lỗi (giao lại), việc xuất vẫn queued, không sự kiện hoàn tất.
	f.hook.set(func() error { return errors.New("fixture: kho đối tượng lỗi") })
	if _, err := inbox.Process(context.Background(), sized, port.ExportConsumer, ev, f.mod.ExportHandler()); err == nil {
		t.Fatal("lỗi kho đối tượng bị nuốt")
	}
	if job := admAG.must(admAG.get("/v1/reports/exports/"+id), http.StatusOK, "trạng thái").body; job["status"] != "queued" {
		t.Fatalf("ghi kho lỗi vẫn đổi việc xuất: %v", job)
	}

	g := newGate(t)
	f.hook.set(g.wait)
	done := processAsync(sized, port.ExportConsumer, ev, f.mod.ExportHandler())
	g.waitEntered(t, "Store.Put")
	assertPoolIdle(t, sized, "ghi kho đối tượng")
	g.open()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	job := admAG.must(admAG.get("/v1/reports/exports/"+id), http.StatusOK, "trạng thái").body
	if job["status"] != "completed" || job["row_count"].(float64) != 2 {
		t.Fatalf("việc xuất sau khi ghi kho: %v", job)
	}
	if n := len(f.events(port.EventExportCompleted)); n != 1 {
		t.Fatalf("một sự kiện hoàn tất: %d", n)
	}
	// Giao lặp (cùng event_id và event_id khác) → không ghi kho thêm.
	f.hook.set(nil)
	puts := f.hook.puts.Load()
	f.deliver(port.ExportConsumer, ev, f.mod.ExportHandler())
	dup := ev
	dup.EventID = ids.New()
	f.deliver(port.ExportConsumer, dup, f.mod.ExportHandler())
	if f.hook.puts.Load() != puts || len(f.objects()) != 1 {
		t.Fatalf("giao lặp ghi kho thêm: puts %d→%d, objects %v", puts, f.hook.puts.Load(), f.objects())
	}
}

// scheduledCompleted lập lịch tuần một người nhận có email, chạy một mốc, dựng tệp; trả sự kiện hoàn tất.
func scheduledCompleted(t *testing.T, f *fixture) (outbox.Envelope, string) {
	t.Helper()
	mgr := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	r1 := ids.New()
	f.dir.emails[r1] = "can.bo@ttkn-angiang.test"
	f.clk.Advance(-time.Hour) // 09:00 thứ Hai: lịch 10:00 — mốc gần nhất là tuần trước, không gửi bù
	sch := mgr.must(mgr.adminPost("/v1/reports/schedules", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx",
		"frequency": "weekly", "weekday": 1, "hour": 10, "recipients": []string{r1.String()}}), http.StatusCreated, "lập lịch").body
	f.clk.Advance(2 * time.Hour)
	if err := f.mod.Jobs(func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }, nil)[0].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runWorker()
	evs := f.events(port.EventExportCompleted)
	if len(evs) != 1 {
		t.Fatalf("một việc xuất theo lịch hoàn tất: %d", len(evs))
	}
	return evs[0], str(sch, "id")
}

func deliveryState(t *testing.T, f *fixture) (status *string, sent *int, claimed bool) {
	t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT delivery_status, sent_count, delivery_claim_id IS NOT NULL FROM pf_reports.schedule_run
		  WHERE job_id IS NOT NULL ORDER BY slot_at DESC LIMIT 1`).Scan(&status, &sent, &claimed)
	}); err != nil {
		t.Fatal(err)
	}
	return status, sent, claimed
}

func TestP0PoolShared2_DeliverySMTPOutsideInboxTx(t *testing.T) {
	f := newFixture(t, true)
	ev, _ := scheduledCompleted(t, f)
	sized := f.env.SizedPool(t, migrations.PF, reports.DBRole, 1)

	g := newGate(t)
	f.mailer.before = func(port.Message) error { return g.wait() }
	done := processAsync(sized, port.DeliveryConsumer, ev, f.mod.DeliveryHandler())
	g.waitEntered(t, "Mailer.Send")
	assertPoolIdle(t, sized, "gửi SMTP")
	if st, _, claimed := deliveryState(t, f); st != nil || !claimed {
		t.Fatalf("trong lúc gửi: lần chạy phải đang được nhận, chưa có kết quả (status=%v claimed=%v)", st, claimed)
	}
	g.open()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if st, sent, claimed := deliveryState(t, f); st == nil || *st != "sent" || sent == nil || *sent != 1 || claimed {
		t.Fatalf("sau khi gửi: status=%v sent=%v claimed=%v", st, sent, claimed)
	}
	// Giao lặp: không gửi lại.
	f.mailer.before = nil
	dup := ev
	dup.EventID = ids.New()
	f.deliver(port.DeliveryConsumer, ev, f.mod.DeliveryHandler())
	f.deliver(port.DeliveryConsumer, dup, f.mod.DeliveryHandler())
	if n := len(f.mailer.messages()); n != 1 {
		t.Fatalf("email: %d, muốn 1", n)
	}
}

// SMTP lỗi → lỗi (giao lại), chưa ghi kết quả gửi, đã trả nhận việc; giao lại gửi được.
func TestP0PoolShared2_DeliverySMTPErrorReleasesClaim(t *testing.T) {
	f := newFixture(t, true)
	ev, _ := scheduledCompleted(t, f)
	f.mailer.before = func(port.Message) error { return errors.New("fixture: SMTP lỗi") }
	if _, err := inbox.Process(context.Background(), f.pool, port.DeliveryConsumer, ev, f.mod.DeliveryHandler()); err == nil {
		t.Fatal("lỗi SMTP bị nuốt")
	}
	if st, _, claimed := deliveryState(t, f); st != nil || claimed {
		t.Fatalf("SMTP lỗi: delivery_status=%v claimed=%v", st, claimed)
	}
	f.mailer.before = nil
	f.deliver(port.DeliveryConsumer, ev, f.mod.DeliveryHandler())
	if st, sent, claimed := deliveryState(t, f); st == nil || *st != "sent" || sent == nil || *sent != 1 || claimed || len(f.mailer.messages()) != 1 {
		t.Fatalf("giao lại: status=%v sent=%v claimed=%v email=%d", st, sent, claimed, len(f.mailer.messages()))
	}
}

// Giao lặp ĐỒNG THỜI (hai worker, hai event_id của cùng việc xuất) trong lúc SMTP chậm: chỉ một worker nhận việc gửi,
// worker kia chờ rồi thấy đã gửi — đúng một email.
func TestP0PoolShared2_DeliveryConcurrentRedeliverySendsOnce(t *testing.T) {
	f := newFixture(t, true)
	ev, _ := scheduledCompleted(t, f)
	g := newGate(t)
	f.mailer.before = func(port.Message) error { return g.wait() }
	dup := ev
	dup.EventID = ids.New()
	d1 := processAsync(f.pool, port.DeliveryConsumer, ev, f.mod.DeliveryHandler())
	g.waitEntered(t, "Mailer.Send")
	d2 := processAsync(f.pool, port.DeliveryConsumer, dup, f.mod.DeliveryHandler())
	time.Sleep(1500 * time.Millisecond) // worker 2 đang chờ việc gửi do worker 1 giữ
	select {
	case <-g.entered:
		t.Fatal("worker thứ hai gửi song song")
	default:
	}
	g.open()
	for _, d := range []chan error{d1, d2} {
		if err := <-d; err != nil {
			t.Fatal(err)
		}
	}
	if n := len(f.mailer.messages()); n != 1 {
		t.Fatalf("email: %d, muốn 1", n)
	}
	if st, sent, claimed := deliveryState(t, f); st == nil || *st != "sent" || sent == nil || *sent != 1 || claimed {
		t.Fatalf("kết quả gửi: status=%v sent=%v claimed=%v", st, sent, claimed)
	}
}

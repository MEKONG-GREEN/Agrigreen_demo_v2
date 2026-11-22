package public_test

// P0-POOL-SHARED-2 (#143): consumer xuất Public API (pf.apiclient.export) KHÔNG đọc nguồn (port module khác) và KHÔNG ghi
// kho đối tượng khi đang giữ giao dịch inbox — pool CỠ 1 rảnh trong lúc ghi kho treo; ghi kho lỗi → không ghi gì, giao
// lại dựng bình thường; giao lặp → không ghi thêm.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/public"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// putHook bọc kho đối tượng: before chạy trước mỗi Put (chặn, tiêm lỗi).
type putHook struct {
	objstore.Store
	puts   atomic.Int32
	before func() error
}

func (h *putHook) Put(ctx context.Context, key string, r io.Reader, size int64, sum, ct string) (objstore.Info, error) {
	h.puts.Add(1)
	if h.before != nil {
		if err := h.before(); err != nil {
			return objstore.Info{}, err
		}
	}
	return h.Store.Put(ctx, key, r, size, sum, ct)
}

// probeCollection ghi số kết nối đang mượn của pool inbox mỗi lần đọc nguồn (port module khác).
type probeCollection struct {
	*fakeCollection
	pool *pgxpool.Pool
	mu   sync.Mutex
	held []int32
}

func (p *probeCollection) CollectionCells(ctx context.Context, q port.CollectionQuery) (port.CollectionPage, error) {
	p.mu.Lock()
	p.held = append(p.held, p.pool.Stat().AcquiredConns())
	p.mu.Unlock()
	return p.fakeCollection.CollectionCells(ctx, q)
}

func TestExportOutsideInboxTxOnSingleConnPool_P0PoolShared2(t *testing.T) {
	pool := pgtest.Get(t).Pool(t, migrations.PF, port.DBRole)
	sized := pgtest.Get(t).SizedPool(t, migrations.PF, port.DBRole, 1)
	local, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hook := &putHook{Store: local}
	var svc *app.Service
	e := newEnv(t, func(d *public.Deps) {
		svc = &app.Service{DB: pool, Clock: d.Clock, OperatorTenant: d.OperatorTenant, Store: hook}
		d.Svc = svc
	})
	probe := &probeCollection{fakeCollection: e.coll, pool: sized}
	svc.Collection = probe
	op := e.client.OperatorTenant
	r := e.do(http.MethodPost, "/v1/data/collection-volumes/exports", e.key, []byte(`{"format":"csv","season_code":"2026-HT"}`), "Idempotency-Key", "exp-pool-0001")
	if r.status != http.StatusAccepted {
		t.Fatalf("yêu cầu xuất: %d %s", r.status, r.raw)
	}
	jobID := r.body["id"].(string)
	ctx := context.Background()
	var env outbox.Envelope
	if err := tenant.WithTenantTx(ctx, pool, op, func(tx pgx.Tx) error {
		var payload []byte
		if err := tx.QueryRow(ctx, `SELECT id, payload::text FROM shared.outbox WHERE tenant_id = $1::uuid AND event_type = $2::text`, op,
			port.EventExportRequested).Scan(&env.EventID, &payload); err != nil {
			return err
		}
		env.EventType, env.TenantID, env.Payload = port.EventExportRequested, op, payload
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Ghi kho lỗi → handler lỗi (giao lại), job vẫn queued, không tệp.
	hook.before = func() error { return errors.New("fixture: kho đối tượng lỗi") }
	if _, err := inbox.Process(ctx, sized, port.ExportConsumer, env, svc.HandleExportRequested); err == nil {
		t.Fatal("lỗi kho đối tượng bị nuốt")
	}
	if jr := e.do(http.MethodGet, "/v1/jobs/"+jobID, e.key, nil); jr.body["status"] != "queued" {
		t.Fatalf("ghi kho lỗi vẫn hoàn tất job: %s", jr.raw)
	}

	// 2. Ghi kho treo: kết nối DUY NHẤT của pool inbox phải rảnh (không giao dịch nào mở trong lúc ghi kho).
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	hook.before = func() error {
		close(entered)
		<-release
		return nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := inbox.Process(ctx, sized, port.ExportConsumer, env, svc.HandleExportRequested)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("không ghi kho đối tượng")
	}
	if n := sized.Stat().AcquiredConns(); n != 0 {
		t.Fatalf("P0-POOL-SHARED-2: ghi kho đối tượng trong lúc giữ %d kết nối (giao dịch inbox mở)", n)
	}
	hold, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := tenant.WithTenantTx(hold, sized, op, func(tx pgx.Tx) error { _, err := tx.Exec(hold, "SELECT 1"); return err }); err != nil {
		t.Fatalf("pool cỡ 1 bị giữ trong lúc ghi kho: %v", err)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	probe.mu.Lock()
	for i, n := range probe.held {
		if n != 0 {
			t.Errorf("đọc nguồn lần %d trong lúc giữ %d kết nối pool inbox", i+1, n)
		}
	}
	probe.mu.Unlock()
	jr := e.do(http.MethodGet, "/v1/jobs/"+jobID, e.key, nil)
	if jr.body["status"] != "completed" || jr.body["rows"] != float64(2) {
		t.Fatalf("job sau khi ghi kho: %s", jr.raw)
	}
	// 3. Giao lặp (cùng event_id, rồi event_id khác) → không ghi kho thêm.
	puts := hook.puts.Load()
	hook.before = nil
	if processed, err := inbox.Process(ctx, sized, port.ExportConsumer, env, svc.HandleExportRequested); err != nil || processed {
		t.Fatalf("giao lặp: processed=%v err=%v", processed, err)
	}
	if hook.puts.Load() != puts {
		t.Fatal("giao lặp ghi kho thêm")
	}
}

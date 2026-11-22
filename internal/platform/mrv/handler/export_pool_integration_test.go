package handler_test

// P0-POOL-SHARED-2 (#143): consumer xuất gói MRV (pf.mrv.export.requested) ghi gói vào kho đối tượng NGOÀI giao dịch inbox
// — pool inbox CỠ 1 rảnh trong lúc ghi kho treo; ghi kho lỗi → không gói, không sự kiện; giao lại → đúng một gói.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// mrvPutHook bọc kho đối tượng: before chạy trước mỗi Put.
type mrvPutHook struct {
	objstore.Store
	mu     sync.Mutex
	before func() error
}

func (h *mrvPutHook) Put(ctx context.Context, key string, r io.Reader, size int64, sum, ct string) (objstore.Info, error) {
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

func (h *mrvPutHook) set(fn func() error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.before = fn
}

func TestExportPutOutsideInboxTx_P0PoolShared2(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col := f.as(f.tenant, f.p89, port.RoleCollector)
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", awd("30001")), 201, "")
	exp := f.as(f.tenant, f.p89, port.RoleExporter)
	j := exp.do(f.app.URL, http.MethodPost, "/v1/mrv/exports", "k-"+ids.New().String(), map[string]any{"filters": map[string]any{"evidence_type": "awd_log"}})
	expect(t, j, 202, "")
	id := j.str("id")
	env := f.requestedEvent(f.tenant, id)

	hook := &mrvPutHook{Store: f.store}
	mod := mrv.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, mrv.Deps{Scope: f.scopes, Consent: f.consent, Store: hook, Params: f.params})
	sized := pgtest.Get(t).SizedPool(t, migrations.PF, mrv.DBRole, 1)
	counts := func() (packages, completed int) {
		_ = tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_mrv.export_package WHERE export_id = $1", uuid.MustParse(id)).Scan(&packages); err != nil {
				return err
			}
			return tx.QueryRow(ctx, "SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND payload->>'export_id' = $2",
				port.EventExportCompleted, id).Scan(&completed)
		})
		return
	}

	hook.set(func() error { return errors.New("fixture: kho đối tượng lỗi") })
	if _, err := inbox.Process(ctx, sized, port.ExportConsumer, env, mod.ExportHandler()); err == nil {
		t.Fatal("lỗi kho đối tượng bị nuốt")
	}
	if p, c := counts(); p != 0 || c != 0 {
		t.Fatalf("ghi kho lỗi vẫn ghi sổ: gói %d, sự kiện %d", p, c)
	}

	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(open)
	hook.set(func() error {
		close(entered)
		<-release
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := inbox.Process(ctx, sized, port.ExportConsumer, env, mod.ExportHandler())
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("không ghi kho đối tượng")
	}
	if n := sized.Stat().AcquiredConns(); n != 0 {
		t.Fatalf("P0-POOL-SHARED-2: ghi gói MRV vào kho trong lúc giữ %d kết nối pool inbox", n)
	}
	open()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	hook.set(nil)
	env2 := env
	env2.EventID = ids.New()
	if _, err := inbox.Process(ctx, sized, port.ExportConsumer, env2, mod.ExportHandler()); err != nil {
		t.Fatal(err)
	}
	if p, c := counts(); p != 1 || c != 1 {
		t.Fatalf("mong đúng 1 gói và 1 sự kiện completed, có %d và %d", p, c)
	}
}

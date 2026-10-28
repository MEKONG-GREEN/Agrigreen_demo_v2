package handler_test

// P0-POOL-SHARED-2 (#143, bàn giao từ #139): consumer log GPS yêu cầu hỗ trợ (pf.extension.gps_log) ghi log qua pf/geo
// (giao dịch riêng trên cùng pool) NGOÀI giao dịch inbox; giao lặp → khóa chống trùng của geo, không log thứ hai.

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// probeGpsLog ghi số kết nối đang mượn của pool inbox mỗi lần ghi log (port pf/geo).
type probeGpsLog struct {
	*fakeGpsLog
	pool *pgxpool.Pool
	mu   sync.Mutex
	held []int32
}

func (p *probeGpsLog) RecordFix(ctx context.Context, fix geoport.GpsFix) (geoport.GpsFixReceipt, error) {
	p.mu.Lock()
	p.held = append(p.held, p.pool.Stat().AcquiredConns())
	p.mu.Unlock()
	return p.fakeGpsLog.RecordFix(ctx, fix)
}

func TestSupportRequestGpsLogOutsideInboxTx_P0PoolShared2(t *testing.T) {
	f := newFixture(t)
	f.seedPeople()
	farmers := &fakeFarmers{links: map[uuid.UUID][]accport.HouseholdLink{}}
	f.mod.UseFarmers(farmers)
	farmer, _ := f.farmerIn(farmers, f.tenant, f.org.c1, "30001")
	farmer.must(farmer.post("/v1/support-requests", map[string]any{"category": "dich_hai", "description": "Rầy nâu xuất hiện dày ở lô 3, lúa đang trổ",
		"location": map[string]any{"lat": "10.3861", "lon": "105.4352"}, "gps_accuracy_m": 8}), http.StatusCreated, "gửi có GPS")
	var ev outbox.Envelope
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		ev = outbox.Envelope{TenantID: f.tenant, SchemaVersion: 1}
		return tx.QueryRow(context.Background(), `SELECT id, event_type, payload FROM shared.outbox WHERE event_type = $1 ORDER BY id LIMIT 1`,
			port.EventSupportRequestSubmitted).Scan(&ev.EventID, &ev.EventType, &ev.Payload)
	}); err != nil {
		t.Fatal(err)
	}
	sized := f.env.SizedPool(t, migrations.PF, extension.DBRole, 1)
	gl := &probeGpsLog{fakeGpsLog: &fakeGpsLog{fixes: map[string]geoport.GpsFix{}}, pool: sized}
	f.mod.UseGpsLog(gl)
	h := f.mod.GpsLogHandler()
	dup := ev
	dup.EventID = uuid.New()
	for _, e := range []outbox.Envelope{ev, ev, dup} {
		if _, err := inbox.Process(context.Background(), sized, port.GpsLogConsumer, e, h); err != nil {
			t.Fatal(err)
		}
	}
	gl.mu.Lock()
	defer gl.mu.Unlock()
	if len(gl.held) == 0 || len(gl.fixes) != 1 {
		t.Fatalf("log GPS: %d lời gọi, %d log", len(gl.held), len(gl.fixes))
	}
	for i, n := range gl.held {
		if n != 0 {
			t.Errorf("P0-POOL-SHARED-2: ghi log GPS lần %d trong lúc giữ %d kết nối pool inbox", i+1, n)
		}
	}
}

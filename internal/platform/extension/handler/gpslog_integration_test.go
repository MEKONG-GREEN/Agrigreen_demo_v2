package handler_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type fakeGpsLog struct {
	mu    sync.Mutex
	fixes map[string]geoport.GpsFix
}

func (f *fakeGpsLog) RecordFix(ctx context.Context, fix geoport.GpsFix) (geoport.GpsFixReceipt, error) {
	if _, err := tenant.FromContext(ctx); err != nil {
		return geoport.GpsFixReceipt{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, dup := f.fixes[fix.IdemKey]
	f.fixes[fix.IdemKey] = fix
	return geoport.GpsFixReceipt{Duplicate: dup}, nil
}

// HTX US-GPS-03 AC-3 (mục đích support): consumer pf.extension.gps_log ghi log GPS của
// yêu cầu hỗ trợ SAU khi đã commit, một log mỗi yêu cầu (giao lặp an toàn); yêu cầu
// "Thiếu vị trí GPS" không có log.
func TestSupportRequestGpsLogConsumer_HTX_US_GPS_03(t *testing.T) {
	f := newFixture(t)
	f.seedPeople()
	farmers := &fakeFarmers{links: map[uuid.UUID][]accport.HouseholdLink{}}
	f.mod.UseFarmers(farmers)
	farmer, _ := f.farmerIn(farmers, f.tenant, f.org.c1, "30001")
	withGPS := map[string]any{"category": "dich_hai", "description": "Rầy nâu xuất hiện dày ở lô 3, lúa đang trổ",
		"location": map[string]any{"lat": "10.3861", "lon": "105.4352"}, "gps_accuracy_m": 8}
	noGPS := map[string]any{"category": "ky_thuat", "description": "Cần hướng dẫn bón phân giai đoạn đòng"}
	id := str(farmer.must(farmer.post("/v1/support-requests", withGPS), http.StatusCreated, "gửi có GPS").body, "support_request", "id")
	farmer.must(farmer.post("/v1/support-requests", noGPS), http.StatusCreated, "gửi thiếu GPS")

	var envs []outbox.Envelope
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT id, event_type, payload FROM shared.outbox WHERE event_type = $1 ORDER BY id`, port.EventSupportRequestSubmitted)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e := outbox.Envelope{TenantID: f.tenant, SchemaVersion: 1}
			if err := rows.Scan(&e.EventID, &e.EventType, &e.Payload); err != nil {
				return err
			}
			envs = append(envs, e)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	h := f.mod.GpsLogHandler()
	if _, err := inbox.Process(context.Background(), f.pool, port.GpsLogConsumer, envs[0], h); err == nil {
		t.Fatal("chưa nối GpsLog phải lỗi để giao lại")
	}
	gl := &fakeGpsLog{fixes: map[string]geoport.GpsFix{}}
	f.mod.UseGpsLog(gl)
	for _, e := range append(envs, envs...) {
		if _, err := inbox.Process(context.Background(), f.pool, port.GpsLogConsumer, e, h); err != nil {
			t.Fatal(err)
		}
	}
	fx, ok := gl.fixes["extension:support_request:"+id]
	if len(gl.fixes) != 1 || !ok || fx.Purpose != geoport.GpsPurposeSupport || fx.ActorRef != farmer.actor || fx.Lat.String() != "10.3861" ||
		fx.CommuneCode != "30001" || fx.Source != geoport.GpsSourceDevice {
		t.Fatalf("log GPS yêu cầu hỗ trợ: %+v", gl.fixes)
	}
}

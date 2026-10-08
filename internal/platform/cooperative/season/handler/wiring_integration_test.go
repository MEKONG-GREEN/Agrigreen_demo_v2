package handler_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// fakeGpsLog ghi nhận RecordFix theo IdemKey (giống pf/geo: gọi lặp → Duplicate).
type fakeGpsLog struct {
	mu    sync.Mutex
	fixes map[string]geoport.GpsFix
	fail  int
}

func (f *fakeGpsLog) RecordFix(ctx context.Context, fix geoport.GpsFix) (geoport.GpsFixReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := tenant.FromContext(ctx); err != nil {
		return geoport.GpsFixReceipt{}, err
	}
	if f.fail > 0 {
		f.fail--
		return geoport.GpsFixReceipt{}, errors.New("geo tạm thời không phản hồi")
	}
	if _, ok := f.fixes[fix.IdemKey]; ok {
		return geoport.GpsFixReceipt{Duplicate: true}, nil
	}
	f.fixes[fix.IdemKey] = fix
	return geoport.GpsFixReceipt{}, nil
}

// envelopes đọc phong bì outbox của các loại sự kiện (như worker nhận).
func (f *fixture) envelopes(types ...string) []outbox.Envelope {
	f.t.Helper()
	var out []outbox.Envelope
	err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT id, event_type, payload FROM shared.outbox WHERE event_type = ANY($1::text[]) ORDER BY id`, types)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e := outbox.Envelope{TenantID: f.tenant, SchemaVersion: 1}
			if err := rows.Scan(&e.EventID, &e.EventType, &e.Payload); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// HTX US-GPS-03 AC-3: log GPS ghi SAU khi nghiệp vụ commit, qua consumer của outbox —
// mở vụ / nhật ký / khai sản lượng có toạ độ → một log mỗi bản ghi (khóa theo bản ghi,
// giao lặp và thử lại an toàn); GPS bị từ chối → không log; lỗi geo tạm thời → giao lại
// (nghiệp vụ đã lưu không bị chặn).
func TestGpsLogConsumerAfterCommit_HTX_US_GPS_03(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	fa := f.farmer(f.h1)
	fa.mustOpen(f.p1)
	expect(t, fa.log(f.p1, nil), http.StatusCreated, "ghi nhật ký")
	expect(t, f.managerA().log(f.p1, map[string]any{"gps": map[string]any{"source": "denied"}, "activity_code": "kiem_tra", "sop_step_seq": nil}),
		http.StatusCreated, "ghi hộ không GPS")

	gl := &fakeGpsLog{fixes: map[string]geoport.GpsFix{}, fail: 1}
	h := f.mod.GpsLogHandler()
	envs := f.envelopes(port.GpsLogEvents...)
	if len(envs) != 3 {
		t.Fatalf("sự kiện: %d", len(envs))
	}
	// Chưa nối pf/geo → lỗi (giao lại), không mất sự kiện.
	if _, err := inbox.Process(context.Background(), f.db, port.GpsLogConsumer, envs[0], h); err == nil {
		t.Fatal("chưa nối GpsLog phải lỗi")
	}
	f.mod.UseGpsLog(gl)
	for _, e := range envs {
		ok, err := inbox.Process(context.Background(), f.db, port.GpsLogConsumer, e, h)
		if err != nil { // lỗi tạm thời lần đầu → giao lại
			ok, err = inbox.Process(context.Background(), f.db, port.GpsLogConsumer, e, h)
		}
		if err != nil || !ok {
			t.Fatalf("%s: %v %v", e.EventType, ok, err)
		}
		if ok, _ := inbox.Process(context.Background(), f.db, port.GpsLogConsumer, e, h); ok {
			t.Fatal("giao lặp phải bị inbox bỏ qua")
		}
	}
	// Gọi thẳng lần nữa (inbox khác) → geo nhận cùng IdemKey, không trùng.
	for _, e := range envs {
		if _, err := inbox.Process(context.Background(), f.db, port.GpsLogConsumer+".again", e, h); err != nil {
			t.Fatal(err)
		}
	}
	if len(gl.fixes) != 2 {
		t.Fatalf("mở vụ + nhật ký có GPS → 2 log, ghi hộ không GPS → không: %v", gl.fixes)
	}
	var purposes []string
	for _, fx := range gl.fixes {
		purposes = append(purposes, fx.Purpose)
		if fx.ActorRef != fa.actor || fx.PlotRef != f.p1 || fx.Source != geoport.GpsSourceDevice || fx.Lon == nil || fx.Lon.String() != "105.6" ||
			fx.CapturedAt.IsZero() || fx.TargetRef == uuid.Nil {
			t.Fatalf("log GPS: %+v", fx)
		}
	}
	if !contains(purposes, geoport.GpsPurposeSeasonOpen) || !contains(purposes, geoport.GpsPurposeFarmLog) {
		t.Fatalf("mục đích: %v", purposes)
	}
	// Nguồn cung cho mô phỏng (#4, #5): vụ đang mở tính theo sản lượng dự kiến.
	sup, err := f.mod.SupplyStats().SeasonSupply(tenant.WithID(context.Background(), f.tenant), 2026)
	if err != nil || len(sup) != 1 || sup[0].CooperativeRef != f.coopA || sup[0].SeasonCode != "DX" || sup[0].AreaHa.String() != "1.5" ||
		sup[0].PaddyKg.IsNegative() || !sup[0].HarvestedAreaHa.IsZero() || sup[0].Seasons != 1 {
		t.Fatalf("SeasonSupply: %+v %v", sup, err)
	}
	if sup, _ := f.mod.SupplyStats().SeasonSupply(tenant.WithID(context.Background(), f.tenant), 2025); len(sup) != 0 {
		t.Fatalf("năm khác: %+v", sup)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

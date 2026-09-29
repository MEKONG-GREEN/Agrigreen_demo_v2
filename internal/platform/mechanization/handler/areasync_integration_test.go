package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

func areaChanged(t *testing.T, tid, coop uuid.UUID, season string, year int, ha string, at time.Time) outbox.Envelope {
	t.Helper()
	b, err := json.Marshal(ccport.CoopSeasonAreaChangedV1{CooperativeRef: coop, SeasonCode: season, SeasonYear: year, AreaHa: ha,
		Seasons: 3, OpenSeasons: 2, CompletedSeasons: 1, SourceAt: at.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	return outbox.Envelope{EventID: ids.New(), TenantID: tid, EventType: ccport.EventCoopSeasonAreaChanged, SchemaVersion: 1,
		OccurredAt: at, Producer: "pf/cropcycle", Payload: b}
}

// CGH FN-07 (QT-03): consumer pf.mechanization.area_sync áp diện tích vụ từ App HTX
// (pf.cropcycle.coop_season_area.changed), idempotent theo event_id, không ghi đè bản
// Admin đã khóa, bỏ qua sự kiện cũ hơn, cách ly tenant.
func TestCGHFN07AreaSyncConsumerIdempotentAndRespectsLock(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	h := f.mod.AreaSyncHandler()
	ctx := context.Background()
	run := func(env outbox.Envelope) bool {
		t.Helper()
		ok, err := inbox.Process(ctx, f.pool, port.AreaSyncConsumer, env, h)
		if err != nil {
			t.Fatalf("consumer: %v", err)
		}
		return ok
	}
	areas := func(season string) []map[string]any {
		return list(adm.get("/season-areas?season_code="+season+"&season_year=2026").body, "items")
	}
	t0 := defaultNow.Add(-time.Hour)

	// Chưa có bản ghi → áp, nhãn nguồn App HTX.
	env := areaChanged(t, f.tenant, f.c1, "HT", 2026, "42.5000", t0)
	if !run(env) {
		t.Fatal("lần đầu phải xử lý")
	}
	cur := areas("HT")
	if len(cur) != 1 || str(cur[0], "area_ha") != "42.5" || cur[0]["source"] != "app_htx" || cur[0]["source_label"] != "App HTX" {
		t.Fatalf("diện tích App HTX: %v", cur)
	}
	// Giao lặp cùng event_id → inbox bỏ qua; gọi thẳng handler lần nữa cũng không ghi sổ thêm (khóa event_id).
	if run(env) {
		t.Fatal("giao lặp phải bị inbox bỏ qua")
	}
	if _, err := inbox.Process(ctx, f.pool, port.AreaSyncConsumer+".replay", env, h); err != nil {
		t.Fatal(err)
	}
	id := cur[0]["id"].(string)
	hist := list(adm.do(f.admin, http.MethodGet, "/v1/mechanization/season-areas/"+id+"/events", nil).body, "items")
	if len(hist) != 1 || hist[0]["action"] != "synced" {
		t.Fatalf("một dòng sổ cho một event_id: %v", hist)
	}
	// Sự kiện cũ hơn (giao không theo thứ tự) → bỏ qua.
	run(areaChanged(t, f.tenant, f.c1, "HT", 2026, "10", t0.Add(-time.Minute)))
	if cur := areas("HT"); str(cur[0], "area_ha") != "42.5" {
		t.Fatalf("bản cũ hơn không được ghi đè: %v", cur)
	}
	// Bản mới hơn → áp.
	run(areaChanged(t, f.tenant, f.c1, "HT", 2026, "50", t0.Add(time.Minute)))
	if cur := areas("HT"); str(cur[0], "area_ha") != "50" {
		t.Fatalf("bản mới hơn: %v", cur)
	}

	// QT-03: Admin nhập tay + khóa → App HTX không ghi đè, không có dòng sổ mới.
	a := adm.area(f.c2, "DX", 2026, "120.5", true)
	run(areaChanged(t, f.tenant, f.c2, "DX", 2026, "300", t0.Add(2*time.Minute)))
	dx := list(adm.get("/season-areas?season_code=DX&season_year=2026").body, "items")
	for _, r := range dx {
		if r["id"] == a["id"] && (str(r, "area_ha") != "120.5" || r["source"] != "manual") {
			t.Fatalf("bản khóa bị ghi đè: %v", r)
		}
	}
	hist = list(adm.do(f.admin, http.MethodGet, "/v1/mechanization/season-areas/"+a["id"].(string)+"/events", nil).body, "items")
	if len(hist) != 1 {
		t.Fatalf("bản khóa không có dòng sổ đồng bộ: %v", hist)
	}

	// Cách ly tenant: sự kiện của tenant khác không chạm tenant này.
	other := ids.New()
	if _, err := inbox.Process(ctx, f.pool, port.AreaSyncConsumer, areaChanged(t, other, f.c1, "HT", 2027, "7", t0), h); err != nil {
		t.Fatal(err)
	}
	if td := list(adm.get("/season-areas?season_code=HT&season_year=2027").body, "items"); len(td) != 0 {
		t.Fatalf("tenant khác lọt vào: %v", td)
	}
}

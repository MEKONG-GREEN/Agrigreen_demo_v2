package handler_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	wwport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func envOf(t *testing.T, tid uuid.UUID, typ string, payload any) outbox.Envelope {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return outbox.Envelope{EventID: ids.New(), EventType: typ, SchemaVersion: 1, TenantID: tid, OccurredAt: time.Now().UTC(),
		RecordedAt: time.Now().UTC(), Producer: "pf/test", Payload: b}
}

func (f *fixture) deliver(t *testing.T, env outbox.Envelope) bool {
	t.Helper()
	ok, err := inbox.Process(context.Background(), f.pool, port.LayerLogConsumer, env, f.mod.LayerLogHandler())
	if err != nil {
		t.Fatalf("%s: %v", env.EventType, err)
	}
	return ok
}

func (f *fixture) countLayer(t *testing.T, tid uuid.UUID) int {
	t.Helper()
	var n int
	if err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_metrics.layer_event`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func sameFeatures(t *testing.T, what string, a, b port.LayerSnapshot) {
	t.Helper()
	ab, _ := json.Marshal(domain.Normalize(a.Features))
	bb, _ := json.Marshal(domain.Normalize(b.Features))
	if string(ab) != string(bb) || a.Watermark != b.Watermark {
		t.Fatalf("%s: phát lại ≠ dựng thẳng từ nhật ký\n%s wm=%v\n%s wm=%v", what, ab, a.Watermark, bb, b.Watermark)
	}
}

func attr(t *testing.T, fs []port.LayerFeature, ref uuid.UUID, k string) (any, bool) {
	t.Helper()
	for _, f := range fs {
		if f.FeatureRef == ref {
			var m map[string]any
			_ = json.Unmarshal(f.Attributes, &m)
			return m[k], f.Retired
		}
	}
	t.Fatalf("không có đối tượng %s", ref)
	return nil, false
}

// GIS US-EVL-01 (nhật ký chỉ ghi thêm, trước/sau + nguồn, giao lặp không ghi thêm),
// US-SNAP-01 (ảnh chụp cuối ngày dựng từ nhật ký + watermark), US-REPLAY-01 (ảnh chụp +
// sự kiện sau watermark = dựng thẳng từ nhật ký), cách ly tenant và phạm vi.
func TestGISLayerLogSnapshotReplay(t *testing.T) {
	now := time.Now()
	f := newFixture(t, clock.NewFixed(now.Add(48*time.Hour))) // hôm nay (thật) đã "kết thúc"
	svc := f.mod.Service()
	ctx := context.Background()
	today := clock.BusinessDate(now)
	tid := f.tenant
	plot, coop, fac, season := ids.New(), ids.New(), ids.New(), ids.New()
	coopRef := ids.New()
	b1, b2, b3 := ids.New(), ids.New(), ids.New()

	created := envOf(t, tid, coopport.EventPlotCreated, coopport.PlotCreatedV1{PlotRef: plot, Code: "TH0000001", CommuneCode: "80001",
		CooperativeRef: &coopRef, Status: "active", BoundaryID: b1, AreaHa: "1.2500", Source: "kn_web", ActorRef: ids.New(), EventTime: now.UTC().Format(time.RFC3339)})
	if !f.deliver(t, created) || f.deliver(t, created) {
		t.Fatal("giao lần đầu phải xử lý, giao lặp phải bỏ qua (inbox)")
	}
	// Idempotent ở chính nhật ký (bỏ qua inbox): cùng source_event_id → không ghi thêm.
	if err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error { return svc.HandleLayerEvent(ctx, tx, created) }); err != nil {
		t.Fatal(err)
	}
	if n := f.countLayer(t, tid); n != 1 {
		t.Fatalf("giao lặp ghi thêm: %d dòng", n)
	}
	f.deliver(t, envOf(t, tid, coopport.EventPlotBoundaryRecorded, coopport.PlotBoundaryRecordedV1{PlotRef: plot, BoundaryID: b2, Version: 2,
		BecameCurrent: true, AreaHa: "1.3000", CommuneCode: "80001", Source: "kn_web", EventTime: now.UTC().Format(time.RFC3339)}))
	// Bản không thành hiện hành (xung đột) không đổi lớp.
	f.deliver(t, envOf(t, tid, coopport.EventPlotBoundaryRecorded, coopport.PlotBoundaryRecordedV1{PlotRef: plot, BoundaryID: ids.New(), Version: 9,
		BecameCurrent: false, AreaHa: "9.9", CommuneCode: "80001", Source: "kn_web", EventTime: now.UTC().Format(time.RFC3339)}))
	f.deliver(t, envOf(t, tid, coopport.EventCooperativeBoundaryRecorded, coopport.CooperativeBoundaryRecordedV1{CooperativeRef: coop, BoundaryID: ids.New(),
		Version: 1, Action: "recorded", Status: "active", AreaHa: "50.0000", CommuneCode: "86001", Source: "kn_web", EventTime: now.UTC().Format(time.RFC3339)}))
	commune := "80001"
	f.deliver(t, envOf(t, tid, geoport.EventFacilityRecorded, geoport.FacilityRecordedV1{FacilityRef: fac, EventID: ids.New(), Version: 1, Action: "created",
		Kind: "hub", Code: "HUB1", Name: "Hub 1", Status: "active", ProvinceCode: "80", CommuneCode: &commune, Lon: "105.7", Lat: "10.0", Capacity: "100.000", Unit: "t"}))
	f.deliver(t, envOf(t, tid, coopport.EventSeasonOpened, coopport.SeasonOpenedV1{SeasonRef: season, EventID: ids.New(), PlotRef: plot, CooperativeRef: coopRef,
		CommuneCode: "80001", SeasonCode: "DX", SeasonYear: 2026, VarietyCode: "OM18", SowingDate: "2026-09-01", ExpectedHarvestDate: "2026-12-01", AreaHa: "1.3"}))
	f.deliver(t, envOf(t, tid, wwport.EventNetworkPublished, wwport.NetworkPublishedV1{NetworkVersionID: ids.New().String(), VersionNo: 1, EffectiveFrom: now.UTC().Format(time.RFC3339)}))

	// Trước/sau của lần sửa ranh giới.
	var before, after map[string]any
	if err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error {
		var b, a []byte
		if err := tx.QueryRow(ctx, `SELECT before_attrs, after_attrs FROM pf_metrics.layer_event WHERE layer = 'plot' AND op = 'update'`).Scan(&b, &a); err != nil {
			return err
		}
		_ = json.Unmarshal(b, &before)
		_ = json.Unmarshal(a, &after)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if before["boundary_version"].(float64) != 1 || after["boundary_version"].(float64) != 2 || after["code"] != "TH0000001" {
		t.Fatalf("trước/sau: %v → %v", before, after)
	}

	n, err := svc.BuildSnapshots(ctx, tid, today)
	if err != nil || n != len(port.Layers)-1 { // cgh_balance chưa có sự kiện
		t.Fatalf("ảnh chụp: %d %v", n, err)
	}
	if n, _ := svc.BuildSnapshots(ctx, tid, today); n != 0 {
		t.Fatal("chụp lại cùng ngày không ghi thêm")
	}
	if _, err := svc.BuildSnapshots(ctx, tid, clock.BusinessDate(now.Add(72*time.Hour))); err == nil {
		t.Fatal("ngày chưa kết thúc không chụp")
	}

	// Thay đổi SAU ảnh chụp (cùng ngày, sau watermark): phải được áp khi phát lại.
	f.deliver(t, envOf(t, tid, coopport.EventPlotBoundaryRecorded, coopport.PlotBoundaryRecordedV1{PlotRef: plot, BoundaryID: b3, Version: 3,
		BecameCurrent: true, AreaHa: "1.4000", CommuneCode: "80001", Source: "kn_web", EventTime: now.UTC().Format(time.RFC3339)}))
	f.deliver(t, envOf(t, tid, geoport.EventFacilityRecorded, geoport.FacilityRecordedV1{FacilityRef: fac, EventID: ids.New(), Version: 2, Action: "retired",
		Kind: "hub", Code: "HUB1", Name: "Hub 1", Status: "retired", ProvinceCode: "80", CommuneCode: &commune, Lon: "105.7", Lat: "10.0", Capacity: "100.000", Unit: "t"}))

	for _, d := range []clock.Date{today, domain.AddDays(today, 1)} {
		for _, l := range port.Layers {
			r, err := svc.ReplayLayer(ctx, tid, l, d)
			if err != nil {
				t.Fatal(err)
			}
			full, err := svc.FullReplay(ctx, tid, l, d)
			if err != nil {
				t.Fatal(err)
			}
			sameFeatures(t, l+" "+d.String(), r, full)
		}
	}
	r, _ := svc.ReplayLayer(ctx, tid, port.LayerPlot, today)
	if v, _ := attr(t, r.Features, plot, "boundary_version"); v.(float64) != 3 {
		t.Fatalf("phát lại phải áp thay đổi sau watermark: %v", v)
	}
	r, _ = svc.ReplayLayer(ctx, tid, port.LayerFacility, today)
	if _, retired := attr(t, r.Features, fac, "code"); !retired {
		t.Fatal("cơ sở đã ngừng dùng")
	}
	// Ngày trước mọi sự kiện: rỗng.
	if r, _ := svc.ReplayLayer(ctx, tid, port.LayerPlot, domain.AddDays(today, -1)); len(r.Features) != 0 {
		t.Fatalf("hôm qua chưa có thửa: %v", r.Features)
	}
	// Cách ly tenant.
	if r, _ := svc.ReplayLayer(ctx, ids.New(), port.LayerPlot, today); len(r.Features) != 0 {
		t.Fatal("tenant khác thấy lớp")
	}

	// API.
	nat := f.as(tid, all, "geo_admin")
	res := nat.get("/v1/gis/events?layer=plot&from=" + today.String() + "&to=" + today.String())
	expect(t, res, 200, "")
	if it := res.items(); len(it) != 3 || it[0]["op"] != "create" || it[1]["before"] == nil || it[1]["source_type"] != coopport.EventPlotBoundaryRecorded {
		t.Fatalf("nhật ký thửa: %v", it)
	}
	page := nat.get("/v1/gis/events?limit=2&from=" + today.String())
	cur, _ := page.body["next_cursor"].(string)
	if len(page.items()) != 2 || cur == "" {
		t.Fatalf("trang 1: %v", page.body)
	}
	if p2 := nat.get("/v1/gis/events?limit=100&cursor=" + cur + "&from=" + today.String()); len(p2.items()) != f.countLayer(t, tid)-2 {
		t.Fatalf("trang 2: %d", len(p2.items()))
	}
	// Phạm vi xã 80001: thấy thửa, không thấy ranh giới HTX của xã 86001; lớp nền không địa bàn vẫn thấy.
	commUser := f.as(tid, authz.Scope{Units: []string{"80001"}}, "plot_viewer")
	res = commUser.get("/v1/gis/events?from=" + today.String())
	seen := map[string]bool{}
	for _, it := range res.items() {
		seen[it["layer"].(string)] = true
	}
	if !seen["plot"] || seen["coop_boundary"] || !seen["waterway"] {
		t.Fatalf("phạm vi xã: %v", seen)
	}
	expect(t, f.as(tid, all, "farmer").get("/v1/gis/events"), 403, "forbidden")
	expect(t, nat.get("/v1/gis/events?layer=weather"), 404, "metrics.unknown_layer")

	res = nat.get("/v1/gis/snapshots/" + today.String() + "?layers=plot,facility")
	expect(t, res, 200, "")
	if it := res.items(); len(it) != 2 || it[0]["watermark"] == nil {
		t.Fatalf("ảnh chụp: %v", it)
	}
	res = nat.get("/v1/gis/replay?date=" + today.String() + "&layers=plot&geometry=true")
	expect(t, res, 200, "")
	it := res.items()
	feats, _ := it[0]["features"].([]any)
	if len(it) != 1 || len(feats) != 1 || feats[0].(map[string]any)["geometry"] == nil {
		t.Fatalf("phát lại API: %v", it)
	}
	expect(t, nat.get("/v1/gis/replay?date="+domain.AddDays(clock.BusinessDate(now.Add(48*time.Hour)), 1).String()), 422, "metrics.future_date")
	res = f.as(tid, authz.Scope{Units: []string{"86001"}}, "plot_viewer").get("/v1/gis/replay?date=" + today.String() + "&layers=plot")
	if feats, _ := res.items()[0]["features"].([]any); len(feats) != 0 {
		t.Fatal("xã khác không thấy thửa")
	}
}

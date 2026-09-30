package handler_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type layerRow struct {
	op, module    string
	before, after map[string]any
}

func (f *fixture) layerRowFor(t *testing.T, tid uuid.UUID, sourceType string) []layerRow {
	t.Helper()
	var out []layerRow
	if err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT op, source_module, before_attrs, after_attrs FROM pf_metrics.layer_event
		  WHERE layer = 'plot' AND source_type = $1 ORDER BY recorded_at, id`, sourceType)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r layerRow
			var b, a []byte
			if err := rows.Scan(&r.op, &r.module, &b, &a); err != nil {
				return err
			}
			_ = json.Unmarshal(b, &r.before)
			_ = json.Unmarshal(a, &r.after)
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// GIS US-EVL-01 (ngừng dùng/khôi phục thửa vào nhật ký lớp `plot`, có trước/sau + nguồn),
// US-REPLAY-01 (phát lại D phản ánh thửa ngừng dùng) và R-61 (giao lặp = giao một lần).
func TestGISPlotRetireRestoreLayerLog(t *testing.T) {
	now := time.Now()
	f := newFixture(t, clock.NewFixed(now.Add(48*time.Hour)))
	svc := f.mod.Service()
	ctx := context.Background()
	today := clock.BusinessDate(now)
	tid := f.tenant
	plotA, plotB, coopRef, bA := ids.New(), ids.New(), ids.New(), ids.New()
	at := now.UTC().Format(time.RFC3339)
	codes := map[uuid.UUID]string{plotA: "TH0000001", plotB: "TH0000002"}

	for _, p := range []uuid.UUID{plotA, plotB} {
		b := bA
		if p == plotB {
			b = ids.New()
		}
		f.deliver(t, envOf(t, tid, geoport.EventPlotCreated, geoport.PlotCreatedV1{PlotRef: p, Code: codes[p], CommuneCode: "80001",
			CooperativeRef: &coopRef, Status: "active", BoundaryID: b, AreaHa: "1.2500", Source: "kn_web", ActorRef: ids.New(), EventTime: at}))
	}
	retA := envOf(t, tid, geoport.EventPlotRetired, geoport.PlotRetiredV1{PlotRef: plotA, PlotEventID: ids.New(), Code: "TH0000001",
		CommuneCode: "80001", CooperativeRef: &coopRef, StatusBefore: "active", StatusAfter: "retired",
		Reason: "Thửa đã chuyển đổi sang đất ở", ActorRef: ids.New(), EventTime: at})
	retA.Producer = "pf/geo"
	f.deliver(t, retA)
	f.deliver(t, envOf(t, tid, geoport.EventPlotRetired, geoport.PlotRetiredV1{PlotRef: plotB, PlotEventID: ids.New(), Code: "TH0000002",
		CommuneCode: "80001", CooperativeRef: &coopRef, StatusBefore: "active", StatusAfter: "retired",
		Reason: "Thửa trùng với thửa địa chính", ActorRef: ids.New(), EventTime: at}))

	// Phát lại D sau ngừng dùng: cả hai thửa ẩn (retired), thuộc tính ranh giới giữ nguyên.
	r, err := svc.ReplayLayer(ctx, tid, port.LayerPlot, today)
	if err != nil {
		t.Fatal(err)
	}
	if st, retired := attr(t, r.Features, plotA, "status"); !retired || st != "retired" {
		t.Fatalf("phát lại sau ngừng dùng: status=%v retired=%v", st, retired)
	}
	if v, _ := attr(t, r.Features, plotA, "boundary_id"); v != bA.String() {
		t.Fatalf("ngừng dùng không được làm mất ranh giới: %v", v)
	}

	// R-61: giao lặp (qua inbox và thẳng vào handler) không ghi thêm, phát lại không đổi.
	n0 := f.countLayer(t, tid)
	if f.deliver(t, retA) {
		t.Fatal("giao lặp phải bị inbox bỏ qua")
	}
	if err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error { return svc.HandleLayerEvent(ctx, tx, retA) }); err != nil {
		t.Fatal(err)
	}
	if n := f.countLayer(t, tid); n != n0 {
		t.Fatalf("giao lặp ghi thêm: %d → %d", n0, n)
	}
	r2, _ := svc.ReplayLayer(ctx, tid, port.LayerPlot, today)
	sameFeatures(t, "giao lặp", r, r2)

	// Ảnh chụp cuối ngày chứa trạng thái ngừng dùng; khôi phục A SAU ảnh chụp.
	if _, err := svc.BuildSnapshots(ctx, tid, today); err != nil {
		t.Fatal(err)
	}
	resA := envOf(t, tid, geoport.EventPlotRestored, geoport.PlotRestoredV1{PlotRef: plotA, PlotEventID: ids.New(), Code: "TH0000001",
		CommuneCode: "80001", CooperativeRef: &coopRef, StatusBefore: "retired", StatusAfter: "active",
		Reason: "Ngừng dùng nhầm, khôi phục lại", ActorRef: ids.New(), EventTime: at})
	resA.Producer = "pf/geo"
	f.deliver(t, resA)
	f.deliver(t, resA)

	// Trước/sau + nguồn.
	ret := f.layerRowFor(t, tid, geoport.EventPlotRetired)
	if len(ret) != 2 {
		t.Fatalf("số dòng retired = %d", len(ret))
	}
	var rowA layerRow
	for _, x := range ret {
		if x.after["plot_event_id"] != nil && x.after["code"] == "TH0000001" {
			rowA = x
		}
	}
	if rowA.op != "retire" || rowA.module != "pf/geo" || rowA.before["status"] != "active" || rowA.after["status"] != "retired" ||
		rowA.after["status_change_reason"] != "Thửa đã chuyển đổi sang đất ở" || rowA.after["area_ha"] != "1.2500" {
		t.Fatalf("dòng retire: %+v", rowA)
	}
	res := f.layerRowFor(t, tid, geoport.EventPlotRestored)
	if len(res) != 1 || res[0].op != "restore" || res[0].before["status"] != "retired" || res[0].after["status"] != "active" ||
		res[0].after["status_change_reason"] != "Ngừng dùng nhầm, khôi phục lại" {
		t.Fatalf("dòng restore: %+v", res)
	}

	// Phát lại = ảnh chụp + sự kiện sau watermark: A hiện lại, B vẫn ngừng dùng; bằng dựng thẳng.
	r, _ = svc.ReplayLayer(ctx, tid, port.LayerPlot, today)
	full, _ := svc.FullReplay(ctx, tid, port.LayerPlot, today)
	sameFeatures(t, "sau khôi phục", r, full)
	if st, retired := attr(t, r.Features, plotA, "status"); retired || st != "active" {
		t.Fatalf("A sau khôi phục: status=%v retired=%v", st, retired)
	}
	if st, retired := attr(t, r.Features, plotB, "status"); !retired || st != "retired" {
		t.Fatalf("B vẫn ngừng dùng: status=%v retired=%v", st, retired)
	}
}

package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// CGH FN-07 B5 / BR-04 / AC-02 / AC-03: nhật ký đồng bộ theo HTX × vụ (thời điểm, nguồn App
// HTX/Nhập tay, kết quả QT-03), chỉ báo độ tươi trên màn CGH khi lần nhận App HTX gần nhất cũ
// hơn cgh.area_feed_stale_hours (fallback nhập tay), và job báo Admin tối đa một lần mỗi ngày.
func TestCGHFN07AreaSyncLogFreshnessAndStaleAlert(t *testing.T) {
	f := newFixture(t, defaultNow)
	f.params.approve("cgh.area_feed_stale_hours", "24", "2025-01-01")
	adm, viewer := f.adminUser(), f.viewerUser()
	h := f.mod.AreaSyncHandler()
	ctx := context.Background()
	run := func(coop uuid.UUID, season, ha string, at time.Time) {
		t.Helper()
		if _, err := inbox.Process(ctx, f.pool, port.AreaSyncConsumer, areaChanged(t, f.tenant, coop, season, 2026, ha, at), h); err != nil {
			t.Fatal(err)
		}
	}
	t0 := defaultNow.Add(-time.Hour)
	run(f.c1, "HT", "42.5", t0)                   // áp
	run(f.c1, "HT", "10", t0.Add(-time.Minute))   // cũ hơn → bỏ qua
	adm.area(f.c2, "HT", 2026, "120.5", true)     // nhập tay + khóa
	run(f.c2, "HT", "300", t0.Add(2*time.Minute)) // giữ bản khóa

	st := viewer.get("/area-sync-status?season_code=HT&season_year=2026")
	if st.status != 200 || st.body["stale"] != false || st.body["last_app_htx_at"] == nil || str(st.body, "threshold_hours") != "24" {
		t.Fatalf("độ tươi: %d %v", st.status, st.body)
	}
	byCoop := map[string]map[string]any{}
	for _, it := range list(st.body, "items") {
		byCoop[it["cooperative_ref"].(string)] = it
	}
	c1, c2 := byCoop[f.c1.String()], byCoop[f.c2.String()]
	if c1["outcome"] != "stale_ignored" || c1["source_label"] != "App HTX" || num(c1, "sync_count") != 2 {
		t.Fatalf("HTX 1 (dòng mới nhất là bản cũ bị bỏ qua): %v", c1)
	}
	if c2["outcome"] != "kept_locked" || num(c2, "sync_count") != 2 {
		t.Fatalf("HTX 2 (bản khóa được giữ): %v", c2)
	}
	lg := adm.get("/area-sync-log?cooperative_ref=" + f.c2.String())
	rows := list(lg.body, "items")
	if lg.status != 200 || len(rows) != 2 || rows[0]["outcome"] != "kept_locked" || rows[1]["source"] != "manual" || rows[1]["source_label"] != "Nhập tay" {
		t.Fatalf("nhật ký HTX 2: %d %v", lg.status, rows)
	}
	if pg := adm.get("/area-sync-log?limit=1"); len(list(pg.body, "items")) != 1 || pg.body["next_cursor"] == nil {
		t.Fatalf("phân trang: %v", pg.body)
	}
	if r := viewer.get("/area-sync-log"); r.status != http.StatusForbidden {
		t.Fatalf("Cục KTHT không xem nhật ký chi tiết: %d", r.status)
	}

	// Quá ngưỡng 24 giờ → màn CGH báo đang dùng dữ liệu nhập tay dự phòng; job báo Admin một lần/ngày.
	f.clk.Advance(30 * time.Hour)
	st = viewer.get("/area-sync-status?season_code=HT&season_year=2026")
	if st.body["stale"] != true || st.body["fallback_manual"] != true || st.body["message"] == nil {
		t.Fatalf("quá hạn độ tươi: %v", st.body)
	}
	tenants := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant, ids.New()}, nil }
	if n, err := f.mod.RunAreaFeedCheck(ctx, tenants); err != nil || n != 1 {
		t.Fatalf("job cảnh báo: %d %v", n, err)
	}
	if n, err := f.mod.RunAreaFeedCheck(ctx, tenants); err != nil || n != 0 {
		t.Fatalf("chạy lại cùng ngày không báo lại: %d %v", n, err)
	}
	var cnt int
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM shared.outbox WHERE event_type = $1", port.EventAreaFeedStale).Scan(&cnt)
	}); err != nil || cnt != 1 {
		t.Fatalf("một sự kiện pf.mechanization.area_feed.stale: %d %v", cnt, err)
	}
	// Dữ liệu mới về → hết quá hạn.
	run(f.c1, "HT", "45", f.clk.Now())
	if st := viewer.get("/area-sync-status?season_code=HT&season_year=2026"); st.body["stale"] != false {
		t.Fatalf("có dữ liệu mới: %v", st.body)
	}
	// Cách ly tenant.
	other := f.as(ids.New(), f.dcrd, port.RoleCGHViewer)
	if o := other.get("/area-sync-status?season_code=HT&season_year=2026"); len(list(o.body, "items")) != 0 || o.body["last_app_htx_at"] != nil {
		t.Fatalf("rò tenant: %v", o.body)
	}
	// Chỉ ghi thêm.
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE pf_mechanization.area_sync_log SET outcome = 'applied'")
		return e
	}); err == nil {
		t.Fatal("nhật ký đồng bộ phải bất biến")
	}
}

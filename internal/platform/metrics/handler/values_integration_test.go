package handler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/defs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var all = authz.Scope{All: true}

func yesterday() clock.Date { return domain.AddDays(clock.BusinessDate(time.Now()), -1) }

// KN US-DASH-01/02: giá trị kỳ đã chốt được ghi sổ; chạy lại cùng đầu vào không ghi
// thêm (idempotent); đầu vào đổi (tính lại) → dòng mới supersedes dòng cũ kèm lý do,
// dòng cũ vẫn còn (không sửa, RB-1); API trả dòng hiện hành, history=true trả cả hai.
func TestKNDash02RecordIdempotentAndRecomputeSupersedes(t *testing.T) {
	f := newFixture(t, clock.NewFixed(time.Now()))
	svc := f.mod.Service()
	ctx := context.Background()
	d := yesterday()
	key := port.Key("kn.sown_area_ha")
	f.src.scopes = []port.Scope{{}, {Units: []string{"80"}}}
	f.src.set(key, "", "", "120.5")
	f.src.set(key, "80", "", "70.25")

	r, err := svc.RecordDay(ctx, f.tenant, d, []port.Key{key})
	if err != nil || r.Inserted != 2 {
		t.Fatalf("lần đầu: %+v %v", r, err)
	}
	r, err = svc.RecordDay(ctx, f.tenant, d, []port.Key{key})
	if err != nil || r.Inserted != 0 || r.Unchanged != 2 {
		t.Fatalf("chạy lại phải idempotent: %+v %v", r, err)
	}
	f.src.set(key, "", "", "121")
	r, err = svc.Backfill(ctx, f.tenant, d, d, []port.Key{key})
	if err != nil || r.Superseded != 1 || r.Unchanged != 1 {
		t.Fatalf("tính lại: %+v %v", r, err)
	}

	national := f.as(f.tenant, all, port.RoleMetricsViewer)
	res := national.get("/v1/metrics/kn.sown_area_ha/values?from=" + d.String() + "&to=" + d.String())
	expect(t, res, 200, "")
	it := res.items()
	if len(it) != 1 || it[0]["value"] != "121" || it[0]["supersedes_id"] == nil || it[0]["correction_reason"] == nil || it[0]["definition_version"].(float64) != 1 {
		t.Fatalf("hiện hành: %v", it)
	}
	res = national.get("/v1/metrics/kn.sown_area_ha/values?from=" + d.String() + "&to=" + d.String() + "&history=true")
	if it := res.items(); len(it) != 2 || it[0]["value"] != "120.5" || it[0]["current"] != false || it[1]["current"] != true {
		t.Fatalf("lịch sử: %v", it)
	}

	// Sổ chỉ ghi thêm: UPDATE bằng vai ứng dụng và owner đều bị chặn.
	for _, p := range []tenant.Beginner{f.pool, f.owner} {
		err := tenant.WithTenantTx(ctx, p, f.tenant, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `UPDATE pf_metrics.daily_value SET value = 0`)
			return e
		})
		if err == nil {
			t.Fatal("UPDATE daily_value phải bị chặn")
		}
	}
}

// Chưa hết ngày → không ghi (metrics.not_closed); AsOf hợp nhất cũng từ chối.
func TestRecordDayNotClosed(t *testing.T) {
	f := newFixture(t, clock.NewFixed(time.Now()))
	today := clock.BusinessDate(time.Now())
	if _, err := f.mod.Service().RecordDay(context.Background(), f.tenant, today, nil); err == nil || problem.From(err).Code != app.ErrNotClosed.Code {
		t.Fatalf("mong not_closed, nhận %v", err)
	}
	if _, err := f.mod.Source().AsOf(context.Background(), "kn.open_tasks", today, port.Scope{TenantID: f.tenant}); !errors.Is(err, port.ErrNotClosed) {
		t.Fatalf("AsOf hôm nay: %v", err)
	}
	if _, err := f.mod.Source().AsOf(context.Background(), "x.unknown", yesterday(), port.Scope{TenantID: f.tenant}); !errors.Is(err, port.ErrUnknownMetric) {
		t.Fatalf("AsOf key lạ: %v", err)
	}
}

// Phạm vi (ADR-0009) và cách ly tenant (RB-2): cán bộ tỉnh chỉ xem đơn vị trong phạm
// vi, không xem toàn tenant; tenant khác không thấy dòng nào; vai không liên quan bị chặn;
// vai KN xem chỉ số KN nhưng không xem chỉ số CGH.
func TestMetricsScopeTenantIsolationAndRoles(t *testing.T) {
	f := newFixture(t, clock.NewFixed(time.Now()))
	d := yesterday()
	key := port.Key("kn.open_tasks")
	f.src.scopes = []port.Scope{{}, {Units: []string{"80"}}, {Units: []string{"86"}}}
	f.src.set(key, "", "", "9")
	f.src.set(key, "80", "", "4")
	f.src.set(key, "86", "", "5")
	if _, err := f.mod.Service().RecordDay(context.Background(), f.tenant, d, []port.Key{key}); err != nil {
		t.Fatal(err)
	}
	q := "/v1/metrics/kn.open_tasks/values?from=" + d.String() + "&to=" + d.String()
	prov := f.as(f.tenant, authz.Scope{Units: []string{"80", "80001"}}, "kn_province_admin")
	expect(t, prov.get(q), 403, "metrics.out_of_scope")
	expect(t, prov.get(q+"&unit=86"), 403, "metrics.out_of_scope")
	res := prov.get(q + "&unit=80")
	expect(t, res, 200, "")
	if it := res.items(); len(it) != 1 || it[0]["value"] != "4" {
		t.Fatalf("tỉnh 80: %v", it)
	}
	expect(t, prov.get("/v1/metrics/cgh.coops_shortage/values?unit=80"), 403, "forbidden")
	expect(t, f.as(f.tenant, all, "farmer").get(q), 403, "forbidden")
	expect(t, f.as(f.tenant, all, port.RoleMetricsViewer).get("/v1/metrics/x.none/values"), 404, "metrics.unknown_metric")

	other := f.as(ids.New(), all, port.RoleMetricsViewer)
	res = other.get(q)
	expect(t, res, 200, "")
	if len(res.items()) != 0 {
		t.Fatalf("tenant khác thấy dữ liệu: %v", res.items())
	}

	// Danh mục định nghĩa theo vai: KN thấy 4 KPI KN (+ chỉ số khác có vai KN), không thấy CGH.
	res = prov.get("/v1/metrics/definitions")
	expect(t, res, 200, "")
	for _, it := range res.items() {
		if it["key"] == "cgh.coops_shortage" {
			t.Fatal("vai KN không thấy định nghĩa CGH")
		}
	}
	if n := len(f.as(f.tenant, all, port.RoleMetricsViewer).get("/v1/metrics/definitions").items()); n != 9 {
		t.Fatalf("metrics_viewer thấy %d định nghĩa", n)
	}
}

// CGH FN-10/11: chỉ số theo vụ lưu theo chiều vụ; thiếu dimension → danh sách vụ đã ghi.
func TestCGHFN10SeasonDimension(t *testing.T) {
	f := newFixture(t, clock.NewFixed(time.Now()))
	d := yesterday()
	key := port.Key("cgh.coop_sufficiency_pct")
	f.src.scopes = []port.Scope{{Dimension: "DX-2025"}, {Dimension: "DX-2026"}, {}}
	f.src.set(key, "", "DX-2025", "40")
	f.src.set(key, "", "DX-2026", "62.5")
	r, err := f.mod.Service().RecordDay(context.Background(), f.tenant, d, []port.Key{key})
	if err != nil || r.Inserted != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	c := f.as(f.tenant, all, "cgh_viewer")
	res := c.get("/v1/metrics/cgh.coop_sufficiency_pct/values")
	expect(t, res, 200, "")
	if dims, _ := res.body["dimensions"].([]any); len(dims) != 2 {
		t.Fatalf("dimensions: %v", res.body)
	}
	res = c.get("/v1/metrics/cgh.coop_sufficiency_pct/values?dimension=DX-2026&from=" + d.String())
	if it := res.items(); len(it) != 1 || it[0]["value"] != "62.5" {
		t.Fatalf("DX-2026: %v", it)
	}
}

// Tệp định nghĩa của phiên bản đã dùng bị sửa tại chỗ → từ chối (định nghĩa có phiên bản).
func TestDefinitionPinDetectsEditedFile(t *testing.T) {
	f := newFixture(t, clock.NewFixed(time.Now()))
	ctx := context.Background()
	c, err := domain.Load(defs.FS)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := c.Effective("kn.open_tasks", yesterday())
	pin := func(def domain.Def) error {
		return tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error { return store.PinDefinition(ctx, tx, f.tenant, def) })
	}
	if err := pin(d); err != nil {
		t.Fatal(err)
	}
	if err := pin(d); err != nil {
		t.Fatalf("ghim lại cùng băm: %v", err)
	}
	d.SHA256[0] ^= 0xff
	if err := pin(d); !errors.Is(err, store.ErrDefinitionEdited) {
		t.Fatalf("mong ErrDefinitionEdited, nhận %v", err)
	}
}

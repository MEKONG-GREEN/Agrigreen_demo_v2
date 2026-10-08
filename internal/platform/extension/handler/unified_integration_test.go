package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type fakeGeoStats map[string]int64 // xã → số thửa (diện tích = số thửa × 1,5 ha)

func (f fakeGeoStats) DigitizedByCommune(_ context.Context, communes []string) ([]partyport.PlotStat, error) {
	var out []partyport.PlotStat
	for c, n := range f {
		if communes == nil || slices.Contains(communes, c) {
			out = append(out, partyport.PlotStat{CommuneCode: c, Plots: n, AreaHa: decimal.NewFromInt(n).Mul(decimal.RequireFromString("1.5"))})
		}
	}
	return out, nil
}

func (f fakeGeoStats) DigitizedByCooperative(context.Context, []uuid.UUID) (map[uuid.UUID]partyport.PlotStat, error) {
	return map[uuid.UUID]partyport.PlotStat{}, nil
}

type fakeCoopStats map[string][2]int64

func (f fakeCoopStats) CooperativesByUnit(_ context.Context, units []string) ([]partyport.UnitCooperatives, error) {
	var out []partyport.UnitCooperatives
	for u, v := range f {
		if units == nil || slices.Contains(units, u) {
			out = append(out, partyport.UnitCooperatives{UnitCode: u, Cooperatives: v[0], Households: v[1]})
		}
	}
	return out, nil
}

// withStats dựng bản module nối pf/geo, pf/party giả (bảng điều hành gộp G-02) trên cùng DB.
func (f *fixture) withStats() *fixture {
	mod := extension.New(f.pool, f.clk, extension.Deps{Scope: f.org, Orgs: f.org, Units: newFakeUnits(), Params: f.params, Catalogs: fakeCatalogs{},
		Plots: fakeGeoStats{"30001": 4, "30002": 2, "31001": 3}, Coops: fakeCoopStats{"30001": {2, 10}, "31001": {1, 5}}})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), idempotency.Middleware(f.env.Pool(f.t, migrations.PF, "platform_api"))))
	srv := httptest.NewServer(eng)
	f.t.Cleanup(srv.Close)
	g := *f
	g.app, g.mod = srv, mod
	return &g
}

// G-02 (US-DASH-01 AC-2): GET /v1/dashboard gộp pf/extension + pf/geo + pf/party; toàn quốc = tổng các tỉnh; "tạm tính".
// G-09: tên cán bộ, HTX, xã trong danh sách nhiệm vụ.
func TestUnifiedDashboardG02AndTaskNamesG09(t *testing.T) {
	base := newFixture(t)
	f := base.withStats()
	p := f.seedPeople()
	f.params.set("extension.target_sown_area_ha:89", "4000")
	f.params.set("extension.target_sown_area_ha:92", "1000")
	p.off30001a.must(p.off30001a.post("/v1/extension/field-reports", sowing("30001", f.org.c1.String(), "10.5")), http.StatusCreated, "báo cáo")

	nat := p.national.must(p.national.get("/v1/dashboard?period=2026-09"), http.StatusOK, "toàn quốc").body
	if str(nat, "level") != "national" {
		t.Fatalf("level: %v", nat)
	}
	total := nat["total"].(map[string]any)
	if total["cooperatives"] != float64(3) || total["households"] != float64(15) || total["digitized_plots"] != float64(9) ||
		total["digitized_area_ha"] != "13.5000" || total["sown_area_ha"] != "10.5000" || total["target_area_ha"] != "5000.0000" {
		t.Fatalf("tổng toàn quốc: %v", total)
	}
	var sumPlots float64
	for _, c := range items(resp{body: nat}, "children") {
		sumPlots += c["digitized_plots"].(float64)
		if c["unit"] == "89" && (c["target_area_ha"] != "4000.0000" || c["name"] != "ĐV 89") {
			t.Fatalf("tỉnh 89: %v", c)
		}
	}
	if sumPlots != 9 {
		t.Fatalf("toàn quốc phải bằng tổng các tỉnh: %v", sumPlots)
	}
	// Tỉnh 89: con là xã; xã 30002 chưa báo cáo → tạm tính.
	prov := p.admin89.must(p.admin89.get("/v1/dashboard?period=2026-09"), http.StatusOK, "tỉnh").body
	pt := prov["total"].(map[string]any)
	if str(prov, "level") != "province" || pt["digitized_plots"] != float64(6) || pt["provisional"] != true ||
		pt["provisional_label"] != "Số liệu tạm tính" || pt["target_area_ha"] != "4000.0000" {
		t.Fatalf("tỉnh 89: %v", prov)
	}
	if r := p.admin92.get("/v1/dashboard?unit=89"); r.status != http.StatusForbidden {
		t.Fatalf("ngoài phạm vi: %d", r.status)
	}

	// G-09.
	task := p.off30001a.createTask("30001", map[string]any{"cooperative_ref": f.org.c1.String(), "assignee_ref": p.off30001b.actor.String()})
	if task["task_id"] == nil {
		t.Fatalf("tạo nhiệm vụ: %v", task)
	}
	its := items(p.admin89.must(p.admin89.get("/v1/extension/tasks"), http.StatusOK, "danh sách"), "items")
	if len(its) != 1 || its[0]["assignee_name"] != "Cán bộ 30001" || its[0]["commune_name"] != "ĐV 30001" || its[0]["cooperative_name"] == nil {
		t.Fatalf("tên hiển thị G-09: %v", its)
	}
}

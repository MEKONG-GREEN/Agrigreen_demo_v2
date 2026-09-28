package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// fakePlots là thống kê thửa giả của pf/geo.
type fakePlots struct{ byCoop map[uuid.UUID]int64 }

func (f fakePlots) DigitizedByCommune(context.Context, []string) ([]geoport.PlotStat, error) {
	return nil, nil
}
func (f fakePlots) DigitizedByCooperative(_ context.Context, coops []uuid.UUID) (map[uuid.UUID]geoport.PlotStat, error) {
	out := map[uuid.UUID]geoport.PlotStat{}
	for _, c := range coops {
		if n, ok := f.byCoop[c]; ok {
			out[c] = geoport.PlotStat{CooperativeRef: c, Plots: n, AreaHa: decimal.NewFromInt(n)}
		}
	}
	return out, nil
}

// G-03 (tên địa bàn, số hộ, số thửa, lọc đơn vị), G-04 (đồng ý trong danh sách hộ), G-08 (Admin GET /v1/orgs).
func TestReadModelG03G04G08(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	f.mod.UsePlotStats(fakePlots{byCoop: map[uuid.UUID]int64{tr.c1: 7}})
	officer := f.as(f.tenant, tr.t30001, port.RoleKNOfficer, port.RoleConsentWriter)
	r := officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907000001"))
	if r.status != http.StatusCreated {
		t.Fatalf("tạo hộ: %d %v", r.status, r.body)
	}
	hh := str(r.body, "household", "household_ref")

	// G-03: TW lọc theo tỉnh 89 → chỉ HTX c1, kèm tên xã/tỉnh, số hộ, số thửa.
	list := root.appDo(http.MethodGet, "/v1/cooperatives?admin_unit=89", nil)
	its := items(list)
	if len(its) != 1 || its[0]["ref"] != tr.c1.String() {
		t.Fatalf("lọc tỉnh 89: %v", list.body)
	}
	it := its[0]
	if it["commune_name"] != "Xã A" || it["province_name"] != "An Giang" || it["province_code"] != "89" ||
		it["member_households"] != float64(1) || it["digitized_plots"] != float64(7) {
		t.Fatalf("trường G-03: %v", it)
	}
	// Phạm vi vẫn áp: cán bộ xã 30001 lọc tỉnh 92 → rỗng.
	if n := len(items(officer.appDo(http.MethodGet, "/v1/cooperatives?admin_unit=92", nil))); n != 0 {
		t.Fatalf("ngoài phạm vi phải rỗng: %d", n)
	}
	if r := root.appDo(http.MethodGet, "/v1/cooperatives?admin_unit=XX", nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("mã đơn vị sai: %d", r.status)
	}
	if g := root.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String(), nil); g.body["member_households"] != float64(1) {
		t.Fatalf("chi tiết HTX: %v", g.body)
	}

	// G-04: đồng ý mrv.evidence xuất hiện trong danh sách hộ.
	cr := officer.appDo(http.MethodPost, "/v1/consents", map[string]any{
		"subject_ref": hh, "purpose": "mrv.evidence", "text_version": "2026-09", "status": "granted", "channel": "field_app",
		"event_time": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	})
	if cr.status != http.StatusCreated {
		t.Fatalf("ghi đồng ý: %d %v", cr.status, cr.body)
	}
	hl := items(officer.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil))
	cons, _ := hl[0]["consents"].([]any)
	if len(hl) != 1 || len(cons) != 1 || cons[0].(map[string]any)["status"] != "granted" || cons[0].(map[string]any)["purpose"] != "mrv.evidence" {
		t.Fatalf("G-04 consents: %v", hl)
	}

	// G-08: Admin API liệt kê cây, lọc theo cha; quản trị tỉnh chỉ thấy tỉnh mình.
	children := items(root.adminDo(http.MethodGet, "/v1/orgs?parent_ref="+tr.p89.String(), nil))
	if len(children) != 2 {
		t.Fatalf("con của TTKN 89: %v", children)
	}
	prov := f.as(f.tenant, tr.p92, port.RoleKNProvinceAdmin)
	for _, o := range items(prov.adminDo(http.MethodGet, "/v1/orgs", nil)) {
		if u, _ := o["admin_unit_code"].(string); u != "92" && u != "31001" {
			t.Fatalf("quản trị tỉnh 92 thấy tổ chức ngoài tỉnh: %v", o)
		}
	}
	if r := officer.adminDo(http.MethodGet, "/v1/orgs", nil); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã không có vai quản trị: %d", r.status)
	}

	// Cổng CooperativeStats (G-02) theo đơn vị.
	ctx := authz.WithPrincipal(tenant.WithID(context.Background(), f.tenant), authz.Principal{TenantID: f.tenant})
	stats, err := f.mod.CooperativeStats().CooperativesByUnit(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]int64{}
	for _, s := range stats {
		got[s.UnitCode] = [2]int64{s.Cooperatives, s.Households}
	}
	if got["30001"] != [2]int64{1, 1} || got["31001"] != [2]int64{1, 0} {
		t.Fatalf("thống kê HTX: %v", got)
	}
}

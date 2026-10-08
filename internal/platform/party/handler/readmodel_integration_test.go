package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// fakePlots là thống kê thửa giả của pf/geo.
type fakePlots struct{ byCoop map[uuid.UUID]int64 }

func (f fakePlots) DigitizedByCommune(context.Context, []string) ([]port.PlotStat, error) {
	return nil, nil
}
func (f fakePlots) DigitizedByCooperative(_ context.Context, coops []uuid.UUID) (map[uuid.UUID]port.PlotStat, error) {
	out := map[uuid.UUID]port.PlotStat{}
	for _, c := range coops {
		if n, ok := f.byCoop[c]; ok {
			out[c] = port.PlotStat{CooperativeRef: c, Plots: n, AreaHa: decimal.NewFromInt(n)}
		}
	}
	return out, nil
}

// G-03 (tên địa bàn, số hộ, số thửa, lọc đơn vị), G-08 (Admin GET /v1/orgs). G-04 (đồng ý trong danh sách hộ) và
// thống kê HTX theo đơn vị (G-02): cooperative/member/handler (W3.3).
func TestReadModelG03G08(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	f.mod.UsePlotStats(fakePlots{byCoop: map[uuid.UUID]int64{tr.c1: 7}})
	officer := f.as(f.tenant, tr.t30001, port.RoleKNOfficer, port.RoleConsentWriter)
	f.profiles.setHouseholds(tr.c1, 1) // một nông hộ đang là thành viên (pf/cooperative)

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
}

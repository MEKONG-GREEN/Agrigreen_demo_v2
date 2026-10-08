package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/porttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// HTX US-NEWS-03 (cổng cho pf/notify) và mô phỏng #3: nông hộ có thửa trong xã; tâm ranh giới HTX.
func TestPlotHouseholdsAndCoopCentroids(t *testing.T) {
	g := newG2(t)
	hh := ids.New()
	r := g.managerA().appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "cooperative_ref": g.coopA.String(),
		"household_ref": hh.String(), "geometry": rect(0, 0, 100, 100)})
	expect(t, r, http.StatusCreated, "tạo thửa")
	g.officerA().createCoop(g.coopA, rect(0, 0, 500, 500))
	ctx := hubCtx(g.fixture)
	got, err := g.mod.PlotHouseholds().HouseholdsInCommunes(ctx, []string{"31117"})
	if err != nil || len(got) != 1 || got[0] != hh {
		t.Fatalf("nông hộ xã 31117: %v %v", got, err)
	}
	if got, _ := g.mod.PlotHouseholds().HouseholdsInCommunes(ctx, []string{"31120"}); len(got) != 0 {
		t.Fatalf("xã khác: %v", got)
	}
	if got, _ := g.mod.PlotHouseholds().HouseholdsInCommunes(ctx, nil); len(got) != 1 {
		t.Fatalf("mọi xã: %v", got)
	}
	if got, _ := g.mod.PlotHouseholds().HouseholdsInCommunes(tenant.WithID(context.Background(), ids.New()), nil); len(got) != 0 {
		t.Fatalf("tenant khác: %v", got)
	}
	cs, err := g.mod.CoopCentroids().CoopCentroids(ctx, nil)
	c, ok := cs[g.coopA]
	dLat, dLon := degPerMetre(baseLat)
	if err != nil || !ok || len(cs) != 1 || c.Lat.InexactFloat64() < baseLat || c.Lat.InexactFloat64() > baseLat+500*dLat ||
		c.Lon.InexactFloat64() < baseLon || c.Lon.InexactFloat64() > baseLon+500*dLon {
		t.Fatalf("tâm ranh giới HTX: %v %v", cs, err)
	}
}

// ADR-0020 (RB-2): bề mặt đối tác /v1/partner/plots — tài khoản dịch vụ tenant MKG đọc thửa của tenant
// bên cấp qua cấp quyền plot_directory: hình học + diện tích + xã + HTX, KHÔNG nông hộ / tên thửa;
// mỗi lời gọi ghi sổ đọc; không cấp quyền / loại khác → 403; thu hồi → 403; vai khác → 403.
func TestPartnerPlotDirectory_ADR0020(t *testing.T) {
	g := newG2(t)
	mkg := ids.New()
	share := porttest.New()
	grant := share.Grant(g.tenant, mkg, partyport.KindPlotDirectory)
	g.mod.UseSharing(share)
	hh := ids.New()
	r := g.managerA().appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "cooperative_ref": g.coopA.String(),
		"household_ref": hh.String(), "name": "Ruộng nhà ông Ba", "geometry": rect(0, 0, 100, 100)})
	expect(t, r, http.StatusCreated, "tạo thửa")
	plot := str(r.body, "plot", "id")
	sa := g.as(mkg, uuid.Nil, partyport.RolePartnerSync)
	r = sa.appDo(http.MethodGet, "/v1/partner/plots/"+plot+"?grantor="+g.tenant.String(), nil)
	expect(t, r, http.StatusOK, "thửa qua bề mặt đối tác")
	if str(r.body, "ref") != plot || str(r.body, "cooperative_ref") != g.coopA.String() || str(r.body, "commune_code") != "31117" ||
		str(r.body, "area_ha") == "" || str(r.body, "geometry", "type") != "MultiPolygon" {
		t.Fatalf("danh bạ thửa: %s", r.raw)
	}
	for _, k := range []string{"household_ref", "name", "created_by"} {
		if _, ok := r.body[k]; ok {
			t.Fatalf("bề mặt đối tác không được mang %s: %s", k, r.raw)
		}
	}
	if strings.Contains(string(r.raw), hh.String()) || strings.Contains(string(r.raw), "ông Ba") {
		t.Fatalf("lộ nông hộ: %s", r.raw)
	}
	r = sa.appDo(http.MethodGet, "/v1/partner/plots?cooperative_ref="+g.coopA.String(), nil)
	if len(list(r.body, "items")) != 1 {
		t.Fatalf("danh sách thửa của HTX: %s", r.raw)
	}
	if n := share.ReadCount(); n != 2 {
		t.Fatalf("mỗi lời gọi một dòng sổ đọc: %d", n)
	}
	// Không cấp quyền (tenant khác), loại khác, vai khác → 403.
	expect(t, g.as(ids.New(), uuid.Nil, partyport.RolePartnerSync).appDo(http.MethodGet, "/v1/partner/plots/"+plot, nil), http.StatusForbidden, "không cấp quyền")
	third := ids.New()
	share.Grant(g.tenant, third, partyport.KindCooperativeDirectory)
	expect(t, g.as(third, uuid.Nil, partyport.RolePartnerSync).appDo(http.MethodGet, "/v1/partner/plots/"+plot, nil), http.StatusForbidden, "loại không được cấp")
	expect(t, g.as(mkg, uuid.Nil, coopport.RolePlotViewer).appDo(http.MethodGet, "/v1/partner/plots/"+plot, nil), http.StatusForbidden, "vai khác")
	// Thu hồi → 403.
	share.Revoke(grant.ID)
	expect(t, sa.appDo(http.MethodGet, "/v1/partner/plots/"+plot, nil), http.StatusForbidden, "đã thu hồi")
}

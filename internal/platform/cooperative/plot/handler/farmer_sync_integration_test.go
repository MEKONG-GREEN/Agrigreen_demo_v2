package handler_test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// App HTX (HTX US-SEASON-01): thiết bị nông dân nhận thửa của nông hộ liên kết qua
// FarmerChanges — không theo xã; thửa chuyển sang nông hộ khác hoặc ngừng dùng → tombstone;
// thửa của hộ khác cùng xã không bao giờ đi xuống; không có vai farmer → rỗng.
func TestFarmerPlotSyncByHousehold_HTX_US_SEASON_01(t *testing.T) {
	g := newG2(t)
	hh, otherHH := ids.New(), ids.New()
	mk := func(household uuid.UUID, east float64) uuid.UUID {
		r := g.managerA().appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "cooperative_ref": g.coopA.String(),
			"household_ref": household.String(), "geometry": rect(east, 0, 100, 100)})
		expect(t, r, http.StatusCreated, "tạo thửa")
		return uuid.MustParse(str(r.body, "plot", "id"))
	}
	own, moved, foreign := mk(hh, 0), mk(hh, 400), mk(otherHH, 1000)
	fs, ok := g.mod.SyncSource().(syncport.FarmerSource)
	if !ok || !slices.Equal(fs.FarmerEntities(), []string{"plot"}) {
		t.Fatalf("geo phải là FarmerSource cho plot: %v", ok)
	}
	farmer := syncCtx(g.fixture, uuid.Nil, port.RoleFarmer)
	scope := syncport.FarmerScope{Households: []uuid.UUID{hh}, Communes: []string{"31117"}}
	ch, last, err := fs.FarmerChanges(farmer, scope, "plot", 0, 100)
	if err != nil || len(ch) != 2 {
		t.Fatalf("thửa của hộ mình: %v %+v", err, ch)
	}
	for _, c := range ch {
		if c.ID == foreign || c.Op != "upsert" {
			t.Fatalf("thửa hộ khác cùng xã: %+v", c)
		}
	}
	// Chuyển thửa sang hộ khác, ngừng dùng thửa còn lại → tombstone cả hai; thửa hộ khác vẫn không xuống.
	expect(t, g.managerA().appDo(http.MethodPost, "/v1/plots/"+moved.String()+"/assign",
		map[string]any{"cooperative_ref": g.coopA.String(), "household_ref": otherHH.String()}), http.StatusOK, "chuyển nông hộ")
	expect(t, g.managerA().appDo(http.MethodPost, "/v1/plots/"+own.String()+"/retire", map[string]any{"reason": "Thửa đã chuyển đổi sang đất ở"}),
		http.StatusOK, "ngừng dùng")
	ch, _, err = fs.FarmerChanges(farmer, scope, "plot", last, 100)
	if err != nil || len(ch) != 2 {
		t.Fatalf("sau chuyển/ngừng: %v %+v", err, ch)
	}
	for _, c := range ch {
		if c.Op != "tombstone" || (c.ID != own && c.ID != moved) {
			t.Fatalf("tombstone: %+v", c)
		}
	}
	// Không có vai farmer, hoặc không có nông hộ → rỗng.
	if ch, _, _ := fs.FarmerChanges(syncCtx(g.fixture, uuid.Nil, coopport.RolePlotViewer), scope, "plot", 0, 100); len(ch) != 0 {
		t.Fatalf("không vai farmer: %+v", ch)
	}
	if ch, _, _ := fs.FarmerChanges(farmer, syncport.FarmerScope{}, "plot", 0, 100); len(ch) != 0 {
		t.Fatalf("không nông hộ: %+v", ch)
	}
	// Tenant khác cùng nông hộ: không thấy.
	p := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{port.RoleFarmer}}
	if ch, _, _ := fs.FarmerChanges(tenant.WithID(authz.WithPrincipal(context.Background(), p), p.TenantID), scope, "plot", 0, 100); len(ch) != 0 {
		t.Fatalf("tenant khác: %+v", ch)
	}
}

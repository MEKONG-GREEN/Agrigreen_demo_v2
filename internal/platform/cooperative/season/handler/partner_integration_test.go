package handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// ADR-0020: bề mặt đối tác season_harvest_dates (proto FM-01/02) — đọc vụ ở tenant bên cấp, trường
// tối thiểu (không nông hộ, không sản lượng), ghi sổ đọc; loại không được cấp → 403; thu hồi → 403;
// sổ đọc không ghi được → 503 và không trả dữ liệu.
func TestPartnerSeasonHarvestDates_ADR0020(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	season := f.managerA().mustOpen(f.p1)
	sa := f.as(f.mkg, uuid.Nil, partyport.RolePartnerSync)
	path := "/v1/partner/season-harvest-dates?plot_ref=" + f.p1.String()
	before := f.share.ReadCount()
	r := sa.get(path)
	expect(t, r, http.StatusOK, "ngày thu hoạch")
	items := list(r.body, "items")
	if len(items) != 1 || str(items[0], "season_ref") != season || str(items[0], "expected_harvest_date") == "" || str(items[0], "harvest_open_date") == "" {
		t.Fatalf("vụ của thửa: %s", r.raw)
	}
	for _, k := range []string{"household_ref", "opened_by", "mass_kg", "yield_t_per_ha", "expected_yield_t_per_ha"} {
		if _, ok := items[0].(map[string]any)[k]; ok {
			t.Fatalf("bề mặt đối tác không được mang %s: %s", k, r.raw)
		}
	}
	if f.share.ReadCount() != before+1 || f.share.Reads[len(f.share.Reads)-1].Kind != partyport.KindSeasonHarvestDates {
		t.Fatalf("sổ đọc: %+v", f.share.Reads)
	}
	// Vai khác (quản lý HTX) không đọc được bề mặt đối tác.
	expect(t, f.managerA().get(path), http.StatusForbidden, "quản lý HTX")
	// Tài khoản dịch vụ của tenant thứ ba có cấp quyền KHÁC loại → 403.
	third := ids.New()
	f.share.Grant(f.tenant, third, partyport.KindCooperativeDirectory)
	expectCode(t, f.as(third, uuid.Nil, partyport.RolePartnerSync).get(path), http.StatusForbidden, "party.share_grant_missing", "loại không được cấp")
	// Sổ đọc hỏng → 503, không trả dữ liệu.
	f.share.FailAudit = true
	r = sa.get(path)
	if r.status != http.StatusServiceUnavailable || len(list(r.body, "items")) != 0 {
		t.Fatalf("sổ đọc không ghi được phải 503 không dữ liệu: %d %s", r.status, r.raw)
	}
	f.share.FailAudit = false
}

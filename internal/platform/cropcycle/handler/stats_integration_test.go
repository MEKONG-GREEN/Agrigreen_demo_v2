package handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// Admin GET /v1/cropcycle/stats/cooperatives/{ref}: số vụ mở / nhật ký chờ duyệt
// hiện trong hộp thoại vô hiệu hóa HTX (HTX US-HTX-01, KN US-HTX-05). Chỉ
// registry_admin / platform_admin; cô lập tenant; HTX không hoạt động → 0 / 0.
func TestCoopStatsEndpoint(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	mgr := f.managerA()
	mgr.mustOpen(f.p1)
	mgr.mustOpen(f.p2)
	expect(t, mgr.log(f.p1, nil), http.StatusCreated, "ghi nhật ký")

	reg := f.as(f.tenant, uuid.Nil, partyport.RoleRegistryAdmin)
	r := reg.adminGet("/v1/cropcycle/stats/cooperatives/" + f.coopA.String())
	expect(t, r, http.StatusOK, "thống kê HTX A")
	if num(r.body, "open_seasons") != 2 || num(r.body, "pending_logs") != 1 || str(r.body, "cooperative_ref") != f.coopA.String() {
		t.Fatalf("thống kê HTX A sai: %s", r.raw)
	}

	r = f.platformAdmin().adminGet("/v1/cropcycle/stats/cooperatives/" + f.coopB.String())
	expect(t, r, http.StatusOK, "thống kê HTX B")
	if num(r.body, "open_seasons") != 0 || num(r.body, "pending_logs") != 0 {
		t.Fatalf("HTX B không có hoạt động: %s", r.raw)
	}

	// Cô lập tenant: quản trị tenant khác thấy 0 / 0 cho cùng HTX.
	other := f.as(ids.New(), uuid.Nil, partyport.RoleRegistryAdmin)
	r = other.adminGet("/v1/cropcycle/stats/cooperatives/" + f.coopA.String())
	expect(t, r, http.StatusOK, "tenant khác")
	if num(r.body, "open_seasons") != 0 || num(r.body, "pending_logs") != 0 {
		t.Fatalf("tenant khác thấy số liệu: %s", r.raw)
	}

	// Vai khác (quản lý HTX, cán bộ KN) không gọi được.
	expectCode(t, mgr.adminGet("/v1/cropcycle/stats/cooperatives/"+f.coopA.String()), http.StatusForbidden, "forbidden", "quản lý HTX")
	expectCode(t, f.officerA().adminGet("/v1/cropcycle/stats/cooperatives/"+f.coopA.String()), http.StatusForbidden, "forbidden", "cán bộ KN")
	expectCode(t, reg.adminGet("/v1/cropcycle/stats/cooperatives/khong-phai-uuid"), http.StatusNotFound, "not_found", "ref sai")
}

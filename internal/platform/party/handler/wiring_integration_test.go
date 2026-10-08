package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

type fakeActivity struct{ open, pending map[uuid.UUID]int64 }

func (f fakeActivity) CoopActivity(_ context.Context, coop uuid.UUID) (int64, int64, error) {
	return f.open[coop], f.pending[coop], nil
}

// HTX US-HTXSTATUS-01 AC-1: trước khi vô hiệu hóa, Admin thấy số nông hộ, vụ chưa xong
// và nhật ký chờ duyệt của HTX (vụ/nhật ký từ pf/cropcycle qua gốc lắp ráp); chỉ vai
// quản trị trong phạm vi; chưa nối cropcycle → số vụ/nhật ký null.
func TestDeactivationPreviewHTXUSHTXSTATUS01(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	mgr := f.as(f.tenant, tr.c1, port.RoleCoopManager)
	f.profiles.setHouseholds(tr.c1, 1) // một nông hộ đang là thành viên (pf/cooperative)
	path := "/v1/cooperatives/" + tr.c1.String() + "/deactivation-preview"
	r := root.adminDo(http.MethodGet, path, nil)
	if r.status != http.StatusOK || r.body["active_households"] != float64(1) || r.body["open_seasons"] != nil || r.body["season_activity_available"] != false {
		t.Fatalf("chưa nối cropcycle: %d %v", r.status, r.body)
	}
	f.mod.UseSeasonActivity(fakeActivity{open: map[uuid.UUID]int64{tr.c1: 3}, pending: map[uuid.UUID]int64{tr.c1: 7}})
	r = root.adminDo(http.MethodGet, path, nil)
	if r.status != http.StatusOK || r.body["open_seasons"] != float64(3) || r.body["pending_logs"] != float64(7) || r.body["active"] != true {
		t.Fatalf("xem trước: %d %v", r.status, r.body)
	}
	// Phạm vi: quản trị tỉnh 92 không thấy HTX của tỉnh 89; không có vai quản trị → 403.
	if r := f.as(f.tenant, tr.p92, port.RoleKNProvinceAdmin).adminDo(http.MethodGet, path, nil); r.status != http.StatusNotFound {
		t.Fatalf("tỉnh khác: %d", r.status)
	}
	if r := mgr.adminDo(http.MethodGet, path, nil); r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX: %d", r.status)
	}
	if r := root.adminDo(http.MethodGet, "/v1/cooperatives/"+tr.p89.String()+"/deactivation-preview", nil); r.status != http.StatusNotFound {
		t.Fatalf("không phải HTX: %d", r.status)
	}
}

package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
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
	if r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0904000009")); r.status != http.StatusCreated {
		t.Fatalf("tạo hộ: %v", r.body)
	}
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

// GIS US-BND-02: pf/party cài geo/port.CoopMarkers (điểm vị trí hồ sơ HTX trong khung nhìn)
// và CooperativeSites (vị trí + số hộ cho mô phỏng); cách ly tenant.
func TestCoopMarkersAndSites(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	mgr := f.as(f.tenant, tr.c1, port.RoleCoopManager)
	if r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0904000010")); r.status != http.StatusCreated {
		t.Fatalf("tạo hộ: %v", r.body)
	}
	ctx := tenant.WithID(context.Background(), f.tenant)
	ms, err := f.mod.CoopMarkers().MarkersInBBox(ctx, 105.0, 10.0, 106.0, 11.0, 100)
	if err != nil || len(ms) != 1 || ms[0].Ref != tr.c1 || ms[0].CommuneCode != "30001" || ms[0].Lat.String() != "10.3861" {
		t.Fatalf("marker HTX: %+v %v", ms, err)
	}
	if ms, _ := f.mod.CoopMarkers().MarkersInBBox(ctx, 100.0, 20.0, 101.0, 21.0, 100); len(ms) != 0 {
		t.Fatalf("ngoài khung nhìn: %+v", ms)
	}
	if ms, _ := f.mod.CoopMarkers().MarkersInBBox(tenant.WithID(context.Background(), ids.New()), 105, 10, 106, 11, 100); len(ms) != 0 {
		t.Fatal("tenant khác")
	}
	sites, err := f.mod.CooperativeSites().ActiveCooperatives(ctx)
	if err != nil || len(sites) != 2 {
		t.Fatalf("HTX đang hoạt động: %+v %v", sites, err)
	}
	for _, s := range sites {
		switch s.Ref {
		case tr.c1:
			if s.Members != 1 || s.Lon == nil || s.Lon.String() != "105.4352" {
				t.Fatalf("c1: %+v", s)
			}
		case tr.c2:
			if s.Members != 0 || s.Lon != nil {
				t.Fatalf("c2 chưa có toạ độ: %+v", s)
			}
		}
	}
}

package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// G-05: ranh giới hành chính theo cấp + đơn vị cha, đơn vị chưa có hình ở "missing".
func TestAdminBoundariesByParentG05(t *testing.T) {
	f := newFixture(t)
	adm := f.national()
	fc, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": []any{
		map[string]any{"type": "Feature", "properties": map[string]any{"code": "31117"}, "geometry": rect(-500, -500, 2000, 2000)},
	}})
	expect(t, adm.adminPost("/v1/geo/admin-boundaries/import?source=CloudMap%202026-09", "application/geo+json", fc), http.StatusOK, "nhập")
	r := f.officerA().appDo(http.MethodGet, "/v1/geo/admin-boundaries?level=commune&parent=92&zoom=10", nil)
	expect(t, r, http.StatusOK, "liệt kê xã của 92")
	feats := list(r.body, "features")
	if len(feats) != 1 || str(feats[0], "id") != "31117" || str(feats[0], "properties", "parent_code") != "92" ||
		str(feats[0], "properties", "name") != "Xã Thới Lai" {
		t.Fatalf("features: %s", r.raw)
	}
	if m := list(r.body, "missing"); len(m) != 1 || m[0] != "31120" {
		t.Fatalf("missing: %s", r.raw)
	}
	expect(t, adm.appDo(http.MethodGet, "/v1/geo/admin-boundaries?level=commune", nil), http.StatusUnprocessableEntity, "thiếu parent")
	expect(t, adm.appDo(http.MethodGet, "/v1/geo/admin-boundaries?level=district&parent=92", nil), http.StatusUnprocessableEntity, "cấp sai")
	expect(t, adm.appDo(http.MethodGet, "/v1/geo/admin-boundaries?level=commune&parent=XX", nil), http.StatusUnprocessableEntity, "cha sai")
}

// GIS FN-07 BR-26: nhập ranh giới xã (xem trước rồi áp dụng; nhập lại đóng hình cũ). Thửa phải nằm trong địa giới
// xã (BR-27) — kiểm ở pf/cooperative (cooperative/plot/handler, qua geoport.AdminGeometry) từ W3.3.
func TestAdminBoundaryImport(t *testing.T) {
	f := newFixture(t)
	adm := f.national()
	commune := rect(-500, -500, 2000, 2000)
	fc, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": []any{
		map[string]any{"type": "Feature", "properties": map[string]any{"code": "31117"}, "geometry": commune},
		map[string]any{"type": "Feature", "properties": map[string]any{"code": "00000"}, "geometry": commune},
	}})
	r := adm.adminPost("/v1/geo/admin-boundaries/import?dry_run=true&source=CloudMap%202026-09", "application/geo+json", fc)
	expect(t, r, http.StatusOK, "xem trước")
	if get(r.body, "dry_run") != true || get(r.body, "accepted") != 1.0 || get(r.body, "rejected") != 1.0 {
		t.Fatalf("xem trước: %s", r.raw)
	}
	expect(t, adm.appDo(http.MethodGet, "/v1/geo/admin-boundaries/31117", nil), http.StatusNotFound, "xem trước không áp dụng")
	r = adm.adminPost("/v1/geo/admin-boundaries/import?source=CloudMap%202026-09", "application/geo+json", fc)
	expect(t, r, http.StatusOK, "áp dụng")
	r = adm.appDo(http.MethodGet, "/v1/geo/admin-boundaries/31117", nil)
	if r.status != http.StatusOK || str(r.body, "properties", "name") != "Xã Thới Lai" {
		t.Fatalf("ranh giới xã: %d %s", r.status, r.raw)
	}
	// AdminGeometry (cho pf/cooperative): hình trong xã → 0 %, nửa ngoài → ~50 %.
	ag := f.mod.AdminGeometry()
	ctx := tenant.WithID(context.Background(), f.tenant)
	inside, _ := json.Marshal(rect(0, 0, 100, 100))
	half, _ := json.Marshal(rect(1450, 0, 100, 100))
	if p, err := ag.OutsideCommunePct(ctx, string(inside), "31117"); err != nil || p == nil || !p.IsZero() {
		t.Fatalf("trong xã: %v %v", p, err)
	}
	if p, err := ag.OutsideCommunePct(ctx, string(half), "31117"); err != nil || p == nil || p.LessThan(decimal.NewFromInt(40)) {
		t.Fatalf("nửa ngoài xã: %v %v", p, err)
	}
	if p, err := ag.OutsideCommunePct(ctx, string(inside), "31120"); err != nil || p != nil {
		t.Fatalf("xã chưa nhập ranh giới → nil: %v %v", p, err)
	}
	// Nhập lại: hình cũ được đóng (không xóa), đánh dấu replaced.
	r = adm.adminPost("/v1/geo/admin-boundaries/import?source=CloudMap%202026-10", "application/geo+json", fc)
	if fs := list(r.body, "features"); len(fs) != 2 || get(fs[0], "replaced") != true {
		t.Fatalf("nhập lại: %s", r.raw)
	}
	expect(t, adm.adminPost("/v1/geo/admin-boundaries/import", "application/geo+json", fc), http.StatusUnprocessableEntity, "thiếu nguồn")
}

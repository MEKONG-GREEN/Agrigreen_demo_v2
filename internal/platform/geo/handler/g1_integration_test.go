package handler_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
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

// US-SYNC-03: chi tiết xung đột ranh giới giữ cả hai phiên bản; PlotStats (G-02/G-03).
func TestConflictDetailBothVersions(t *testing.T) {
	f := newFixture(t)
	c := f.national()
	p1, _ := c.createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	r := c.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "cooperative_ref": f.coopA.String(),
		"geometry": rect(50, 0, 100, 100), "confirm_overlap": true})
	expect(t, r, http.StatusCreated, "cùng HTX đã xác nhận")
	p2 := str(r.body, "plot", "id")
	cid := str(list(r.body, "conflicts")[0], "id")

	d := f.officerA().appDo(http.MethodGet, "/v1/geo/conflicts/"+cid, nil)
	expect(t, d, http.StatusOK, "chi tiết xung đột")
	vs := list(d.body, "versions")
	if len(vs) != 2 || str(vs[0], "role") != "record" || str(vs[0], "plot_ref") != p2 || get(vs[0], "geometry") == nil ||
		str(vs[1], "role") != "overlapping" || str(vs[1], "plot_ref") != p1.String() || get(vs[1], "current") != true {
		t.Fatalf("versions: %s", d.raw)
	}
	expect(t, f.officerB().appDo(http.MethodGet, "/v1/geo/conflicts/"+cid, nil), http.StatusNotFound, "ngoài phạm vi")

	viewer, ok := f.mod.SyncSink().(syncport.ConflictViewer)
	if !ok {
		t.Fatal("SyncSink phải hiện thực ConflictViewer")
	}
	ctx := syncCtx(f, f.orgA)
	sv, err := viewer.ConflictVersions(ctx, authz.Scope{Units: []string{"31117"}}, uuid.MustParse(cid))
	if err != nil || len(sv) != 2 || sv[0].Role != syncport.VersionRoleRecord {
		t.Fatalf("ConflictVersions: %v %+v", err, sv)
	}
	if _, err := viewer.ConflictVersions(ctx, authz.Scope{Units: []string{"31120"}}, uuid.MustParse(cid)); !errors.Is(err, syncport.ErrConflictUnknown) {
		t.Fatalf("ngoài phạm vi: %v", err)
	}

	st, err := f.mod.PlotStats().DigitizedByCommune(ctx, []string{"31117"})
	if err != nil || len(st) != 1 || st[0].Plots != 2 {
		t.Fatalf("DigitizedByCommune: %v %+v", err, st)
	}
	bc, err := f.mod.PlotStats().DigitizedByCooperative(ctx, []uuid.UUID{f.coopA, f.coopB})
	if err != nil || bc[f.coopA].Plots != 2 || bc[f.coopB].Plots != 0 {
		t.Fatalf("DigitizedByCooperative: %v %+v", err, bc)
	}
}

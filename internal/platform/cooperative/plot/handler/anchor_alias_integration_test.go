package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// fakeAliases: neo HTX đã nhận quyền — Nút tạm prov là bí danh của Nút HTX thật cur (cooperative/anchor).
type fakeAliases struct{ prov, cur uuid.UUID }

func (a fakeAliases) CurrentOrg(_ context.Context, _, org uuid.UUID) (uuid.UUID, error) {
	if org == a.prov {
		return a.cur, nil
	}
	return org, nil
}

// D2 (ADR-0052): bản ghi ngoại tuyến tạo trên máy dưới Nút HTX TẠM trước khi HTX thật nhận quyền (sang Nút khác),
// đẩy lên SAU nhận quyền → vẫn accepted, ghi vào Nút hiện hành của neo; Nút không thuộc neo giữ nguyên.
func TestOfflineRecordUnderProvisionalSyncsAfterClaim_D2(t *testing.T) {
	prov := ids.New()
	f := newFixture(t)
	f.mod.svc.Anchors = app.OrgAliases(fakeAliases{prov: prov, cur: f.coopA})
	snk := f.mod.SyncSink()
	ctx := syncCtx(f, f.orgA, coopport.RolePlotEditor)
	sc := authz.Scope{Units: []string{"31117"}}
	dev := ids.New()

	plot := ids.New()
	id, pl, op, _ := boundaryRecord("create", plot, "31117", prov, rect(0, 0, 100, 100), nil)
	if res := snk.Accept(ctx, sc, syncRecord(id, "dev:"+dev.String()+":1", op, pl, nil, nil, dev)); res.Status != "accepted" {
		t.Fatalf("bản ghi ngoại tuyến dưới Nút tạm: %+v", res)
	}
	r := f.national().appDo(http.MethodGet, "/v1/plots/"+plot.String(), nil)
	expect(t, r, http.StatusOK, "thửa đồng bộ")
	if got := str(r.body, "plot", "properties", "cooperative_ref"); got != f.coopA.String() {
		t.Fatalf("thửa phải thuộc Nút hiện hành của neo %s, có %s", f.coopA, got)
	}
	// Gửi lặp → duplicate (không ghi thêm).
	if res := snk.Accept(ctx, sc, syncRecord(id, "dev:"+dev.String()+":1", op, pl, nil, nil, dev)); res.Status != "duplicate" {
		t.Fatalf("gửi lặp: %+v", res)
	}
	// Nút không thuộc neo giữ nguyên.
	p2 := ids.New()
	id2, pl2, op2, _ := boundaryRecord("create", p2, "31117", f.coopB, rect(300, 0, 100, 100), nil)
	if res := snk.Accept(ctx, sc, syncRecord(id2, "dev:"+dev.String()+":2", op2, pl2, nil, nil, dev)); res.Status != "accepted" {
		t.Fatalf("thửa HTX khác: %+v", res)
	}
	r = f.national().appDo(http.MethodGet, "/v1/plots/"+p2.String(), nil)
	if got := str(r.body, "plot", "properties", "cooperative_ref"); got != f.coopB.String() {
		t.Fatalf("Nút không thuộc neo đổi: %s", got)
	}
}

package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// HTX US-PLOT-01/02: Quản lý HTX đẩy ranh giới qua pf/sync (App HTX) KHÔNG cần vai plot_editor —
// chỉ thửa của HTX mình; thửa mới không ghi HTX nhận HTX của người vẽ; thửa HTX khác → rejected.
func TestCoopManagerSyncPush_HTX_US_PLOT_01_02(t *testing.T) {
	f := newFixture(t)
	snk := f.mod.SyncSink()
	ctx := syncCtx(f, f.coopA, port.RoleCoopManager) // chỉ vai coop_manager
	scA := authz.Scope{Units: []string{"31117"}}
	dev := ids.New()

	// Thửa mới bỏ trống cooperative_ref → thuộc HTX của quản lý.
	plot := ids.New()
	id, pl, op, _ := boundaryRecord("create", plot, "31117", uuid.Nil, rect(0, 0, 100, 100), nil)
	if res := snk.Accept(ctx, scA, syncRecord(id, "dev:"+dev.String()+":1", op, pl, nil, nil, dev)); res.Status != "accepted" {
		t.Fatalf("quản lý HTX vẽ thửa mới: %+v", res)
	}
	r := f.national().appDo(http.MethodGet, "/v1/plots/"+plot.String(), nil)
	expect(t, r, http.StatusOK, "thửa quản lý HTX vẽ")
	if str(r.body, "plot", "properties", "cooperative_ref") != f.coopA.String() {
		t.Fatalf("thửa mới phải thuộc HTX của người vẽ: %s", r.raw)
	}
	// Sửa ranh giới thửa của HTX mình → accepted (bản ghi bù, phiên bản mới).
	reason := "Đo lại ranh giới bằng GPS ngoài đồng"
	cid, cpl, cop, sup := boundaryRecord("correct", plot, "31117", uuid.Nil, rect(0, 0, 90, 100), &id)
	if res := snk.Accept(ctx, scA, syncRecord(cid, "dev:"+dev.String()+":2", cop, cpl, sup, &reason, dev)); res.Status != "accepted" {
		t.Fatalf("sửa thửa HTX mình: %+v", res)
	}
	// Thửa HTX khác: tạo ghi HTX khác → out_of_scope; sửa thửa của HTX khác → không thấy.
	xid, xpl, xop, _ := boundaryRecord("create", ids.New(), "31117", f.coopB, rect(400, 0, 50, 50), nil)
	if res := snk.Accept(ctx, scA, syncRecord(xid, "dev:"+dev.String()+":3", xop, xpl, nil, nil, dev)); res.Status != "rejected" || res.Code != "geo.out_of_scope" {
		t.Fatalf("vẽ thửa cho HTX khác: %+v", res)
	}
	other, _ := f.national().createPlot("31117", f.coopB, rect(600, 0, 50, 50))
	r = f.national().appDo(http.MethodGet, "/v1/plots/"+other.String(), nil)
	cur := uuid.MustParse(str(r.body, "plot", "properties", "boundary_id"))
	oid, opl, oop, osup := boundaryRecord("correct", other, "31117", uuid.Nil, rect(600, 0, 40, 50), &cur)
	if res := snk.Accept(ctx, scA, syncRecord(oid, "dev:"+dev.String()+":4", oop, opl, osup, &reason, dev)); res.Status != "rejected" {
		t.Fatalf("sửa thửa HTX khác phải bị từ chối: %+v", res)
	}
	// Gửi lặp → duplicate; vai khác (nông dân) vẫn bị chặn; tenant khác không sửa được.
	if res := snk.Accept(ctx, scA, syncRecord(id, "dev:"+dev.String()+":1", op, pl, nil, nil, dev)); res.Status != "duplicate" {
		t.Fatalf("gửi lặp: %+v", res)
	}
	fid, fpl, fop, _ := boundaryRecord("create", ids.New(), "31117", uuid.Nil, rect(800, 0, 50, 50), nil)
	if res := snk.Accept(syncCtx(f, f.coopA, "farmer"), scA, syncRecord(fid, "dev:"+dev.String()+":5", fop, fpl, nil, nil, dev)); res.Status != "rejected" || res.Code != "forbidden" {
		t.Fatalf("vai farmer: %+v", res)
	}
	other2 := ids.New()
	stranger := tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: other2, OrgRef: f.coopA, Roles: []string{port.RoleCoopManager}}), other2)
	sid, spl, sop, ssup := boundaryRecord("correct", plot, "31117", uuid.Nil, rect(0, 0, 80, 100), &cid)
	if res := snk.Accept(stranger, scA, syncRecord(sid, "dev:"+dev.String()+":6", sop, spl, ssup, &reason, dev)); res.Status != "rejected" {
		t.Fatalf("tenant khác sửa thửa: %+v", res)
	}
}

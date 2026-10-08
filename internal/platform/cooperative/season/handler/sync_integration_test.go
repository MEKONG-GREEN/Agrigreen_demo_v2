package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func (f *fixture) ctxFor(c caller, roles ...string) context.Context {
	ctx := tenant.WithID(context.Background(), c.tid)
	return authz.WithPrincipal(ctx, authz.Principal{ActorRef: c.actor, TenantID: c.tid, OrgRef: c.org, Roles: roles})
}

func rec(entity, op string, payload any, sup *uuid.UUID, reason string, at time.Time) syncport.Record {
	b, _ := json.Marshal(payload)
	r := syncport.Record{ID: ids.New(), IdemKey: "dev:" + ids.New().String() + ":1", Entity: entity, Op: op, EventTime: at,
		DeviceID: ids.New(), Payload: b, SupersedesID: sup}
	if reason != "" {
		r.CorrectionReason = &reason
	}
	return r
}

// HTX US-LOG-04 / US-YIELD-01 AC-3: bản ghi ngoại tuyến qua pf/sync — mở vụ, nhật ký,
// lô, sản lượng (chiều lên) và sop_version, season, farm_log kèm trạng thái duyệt (chiều xuống).
func TestSyncPushPull_HTX_US_LOG_04(t *testing.T) {
	f := newFixture(t)
	_, ver := f.publishedSOP("SOP OM5451", "OM5451")
	fa := f.farmer(f.h1, f.h2)
	ctx := f.ctxFor(fa, port.RoleFarmer)
	sink, src := f.mod.SyncSink(), f.mod.SyncSource()
	scope := authz.Scope{}
	now := f.clk.Now()

	open := rec(port.EntitySeasonOpen, syncport.OpCreate, map[string]any{"plot_ref": f.p1.String(), "season_code": "DX", "season_year": 2026,
		"variety_code": "OM5451", "sowing_date": today, "gps": map[string]any{"lon": "105.6", "lat": "10.05", "source": "device"}}, nil, "", now.Add(-time.Hour))
	if r := sink.Accept(ctx, scope, open); r.Status != syncport.StatusAccepted {
		t.Fatalf("mở vụ ngoại tuyến: %+v", r)
	}
	if r := sink.Accept(ctx, scope, open); r.Status != syncport.StatusDuplicate {
		t.Fatalf("gửi lại: %+v", r)
	}
	season := open.ID.String() // season_ref mặc định = id bản ghi
	lg := rec(port.EntityFarmLog, syncport.OpCreate, map[string]any{"season_ref": season, "activity_code": "gieo_sa", "sop_step_seq": 2,
		"machine_codes": []string{"MGS"}, "gps": map[string]any{"source": "manual_pick"}}, nil, "", now.Add(-30*time.Minute))
	if r := sink.Accept(ctx, scope, lg); r.Status != syncport.StatusAccepted {
		t.Fatalf("nhật ký ngoại tuyến: %+v", r)
	}
	// Hai máy cùng sửa một nhật ký: bản tới sau dựa trên phiên bản cũ → giữ cả hai, conflict.
	c1 := rec(port.EntityFarmLog, syncport.OpCorrect, map[string]any{"activity_code": "gieo_sa", "sop_step_seq": 2}, &lg.ID, "Sửa trên máy thứ nhất", now)
	c2 := rec(port.EntityFarmLog, syncport.OpCorrect, map[string]any{"activity_code": "gieo_sa"}, &lg.ID, "Sửa trên máy thứ hai", now)
	if r := sink.Accept(ctx, scope, c1); r.Status != syncport.StatusAccepted {
		t.Fatalf("sửa 1: %+v", r)
	}
	if r := sink.Accept(ctx, scope, c2); r.Status != syncport.StatusConflict {
		t.Fatalf("sửa 2 trên bản cũ phải conflict: %+v", r)
	}
	bad := rec(port.EntityFarmLog, syncport.OpCreate, map[string]any{"plot_ref": f.p3.String(), "activity_code": "gieo_sa"}, nil, "", now)
	if r := sink.Accept(ctx, scope, bad); r.Status != syncport.StatusRejected || r.Code != "cropcycle.season_not_open" {
		t.Fatalf("thửa chưa mở vụ: %+v", r)
	}
	f.managerA().mustOpen(f.p2)
	batch := rec(port.EntityLogBatch, syncport.OpCreate, map[string]any{"plot_refs": []string{f.p1.String(), f.p2.String(), f.p3.String()},
		"activity_code": "tuoi_nuoc"}, nil, "", now)
	if r := sink.Accept(ctx, scope, batch); r.Status != syncport.StatusAccepted {
		t.Fatalf("lô ngoại tuyến: %+v", r)
	}
	hv := rec(port.EntityHarvest, syncport.OpCreate, map[string]any{"season_ref": season, "mass_kg": "9750"}, nil, "", now)
	if r := sink.Accept(ctx, scope, hv); r.Status != syncport.StatusRejected || r.Code != "cropcycle.harvest_too_early" {
		t.Fatalf("thu hoạch sớm: %+v", r)
	}
	if r := sink.Accept(f.ctxFor(f.officerA(), "kn_officer"), scope, hv); r.Status != syncport.StatusRejected || r.Code != "forbidden" {
		t.Fatalf("KN không đẩy được: %+v", r)
	}

	// Chiều xuống cho nông dân.
	got := map[string][]syncport.Change{}
	for _, e := range src.Entities() {
		ch, _, err := src.Changes(ctx, scope, e, 0, 500)
		if err != nil {
			t.Fatal(err)
		}
		got[e] = ch
	}
	if len(got[port.EntitySOPVersion]) != 1 || got[port.EntitySOPVersion][0].ID.String() != ver {
		t.Fatalf("sop_version: %+v", got[port.EntitySOPVersion])
	}
	if len(got[port.EntitySeason]) != 2 {
		t.Fatalf("hai vụ của hai hộ: %d", len(got[port.EntitySeason]))
	}
	var tomb, pending int
	for _, c := range got[port.EntityFarmLog] {
		switch {
		case c.Op == syncport.OpTombstone:
			tomb++
		case strings.Contains(string(c.Data), `"status":"pending"`):
			pending++
		}
	}
	if tomb != 2 || pending != 3 {
		t.Fatalf("farm_log: %d tombstone, %d pending", tomb, pending)
	}
	// Người không có vai của module không nhận entity; quản lý HTX B không thấy vụ HTX A.
	none, _, err := src.Changes(f.ctxFor(f.as(f.tenant, uuid.Nil, "plot_viewer"), "plot_viewer"), scope, port.EntitySeason, 0, 100)
	if err != nil || len(none) != 0 {
		t.Fatalf("không vai: %v %v", none, err)
	}
	mb := f.managerB()
	other, _, err := src.Changes(f.ctxFor(mb, port.RoleCoopManager), scope, port.EntitySeason, 0, 100)
	if err != nil || len(other) != 0 {
		t.Fatalf("HTX B: %v %v", other, err)
	}

	// Đường nông dân của pf/sync (FarmerSource): phạm vi = nông hộ pf/sync truyền vào, không
	// tra lại liên kết — chỉ hộ h1 → một vụ; SOP hệ thống vẫn xuống; không vai farmer → rỗng.
	fs, ok := src.(syncport.FarmerSource)
	if !ok || len(fs.FarmerEntities()) != 3 {
		t.Fatal("cropcycle phải là FarmerSource cho sop_version, season, farm_log")
	}
	only := syncport.FarmerScope{Households: []uuid.UUID{f.h1}, Cooperatives: []uuid.UUID{f.coopA}}
	if ch, _, err := fs.FarmerChanges(ctx, only, port.EntitySeason, 0, 100); err != nil || len(ch) != 1 {
		t.Fatalf("FarmerChanges season của h1: %v %+v", err, ch)
	}
	if ch, _, err := fs.FarmerChanges(ctx, only, port.EntitySOPVersion, 0, 100); err != nil || len(ch) != 1 || ch[0].ID.String() != ver {
		t.Fatalf("FarmerChanges sop_version: %v %+v", err, ch)
	}
	if ch, _, _ := fs.FarmerChanges(ctx, syncport.FarmerScope{}, port.EntitySeason, 0, 100); len(ch) != 0 {
		t.Fatalf("không nông hộ: %+v", ch)
	}
	if ch, _, _ := fs.FarmerChanges(f.ctxFor(mb, port.RoleCoopManager), only, port.EntitySeason, 0, 100); len(ch) != 0 {
		t.Fatalf("không vai farmer: %+v", ch)
	}
}

// R-22: dựng lại bảng chiếu từ sổ cho kết quả giống hệt đường ghi trực tiếp.
func TestRebuildProjections_R22(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	fa, mgr := f.farmer(f.h1), f.managerA()
	s1 := fa.mustOpen(f.p1)
	s2 := mgr.mustOpen(f.p2)
	r := mgr.post("/v1/cropcycle/seasons/"+s2+"/cancel", map[string]any{"supersedes_id": str(mgr.get("/v1/cropcycle/seasons/"+s2).body, "current_event_id"),
		"reason": "Mở nhầm thửa khi mở hộ"})
	expect(t, r, http.StatusCreated, "hủy")
	l := fa.log(f.p1, nil)
	v2 := fa.post("/v1/cropcycle/farm-logs/"+str(l.body, "log", "id")+"/corrections", map[string]any{"activity_code": "bon_phan", "sop_step_seq": 3,
		"correction_reason": "Sửa lượng phân bón"})
	expect(t, mgr.post("/v1/cropcycle/farm-logs/"+str(v2.body, "log", "id")+"/reviews", map[string]any{"decision": "approved"}), http.StatusCreated, "duyệt")
	f.advanceDays(91)
	h := fa.post("/v1/cropcycle/seasons/"+s1+"/harvest", map[string]any{"mass_kg": "9750"})
	expect(t, h, http.StatusCreated, "khai")
	expect(t, fa.post("/v1/cropcycle/harvests/"+str(h.body, "harvest", "id")+"/corrections", map[string]any{"mass_kg": "9500",
		"correction_reason": "Cân lại tại kho"}), http.StatusCreated, "sửa sản lượng")

	snapshot := func() string {
		var out []string
		err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
			for _, q := range []string{
				`SELECT coalesce(string_agg((to_jsonb(s) - 'sync_seq' - 'updated_at')::text, '|' ORDER BY season_ref), '') FROM pf_coop.season_current s`,
				`SELECT coalesce(string_agg((to_jsonb(l) - 'sync_seq' - 'updated_at')::text, '|' ORDER BY id), '') FROM pf_coop.farm_log_current l`,
				`SELECT coalesce(string_agg((to_jsonb(a) - 'updated_at')::text, '|' ORDER BY cooperative_ref, season_code), '') FROM pf_coop.coop_season_area a`,
				`SELECT coalesce(string_agg((to_jsonb(y) - 'updated_at')::text, '|' ORDER BY season_ref), '') FROM pf_coop.yield_summary y`,
			} {
				var s string
				if err := tx.QueryRow(context.Background(), q).Scan(&s); err != nil {
					return err
				}
				out = append(out, s)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(out, "\n")
	}
	before := snapshot()
	expectCode(t, mgr.adminPost("/v1/cropcycle/projections/rebuild", nil), http.StatusForbidden, "forbidden", "quản lý HTX không dựng lại")
	r = f.platformAdmin().adminPost("/v1/cropcycle/projections/rebuild", nil)
	expect(t, r, http.StatusOK, "dựng lại")
	if num(r.body, "seasons") != 2 || num(r.body, "farm_logs") != 2 {
		t.Fatalf("số dòng dựng lại: %s", r.raw)
	}
	if after := snapshot(); after != before {
		t.Fatalf("bảng chiếu dựng lại khác:\n%s\n---\n%s", before, after)
	}
}

// Cổng port.Stats cho module khác (cảnh báo khi vô hiệu hóa HTX, diện tích vụ CGH FN-07).
func TestStatsPort(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	mgr := f.managerA()
	mgr.mustOpen(f.p1)
	mgr.mustOpen(f.p2)
	expect(t, mgr.log(f.p1, nil), http.StatusCreated, "ghi")
	ctx := tenant.WithID(context.Background(), f.tenant)
	act, err := f.mod.Stats().ActivityByCooperative(ctx, nil)
	if err != nil || act[f.coopA].OpenSeasons != 2 || act[f.coopA].PendingLogs != 1 || len(act) != 1 {
		t.Fatalf("hoạt động theo HTX: %+v %v", act, err)
	}
	if act, _ := f.mod.Stats().ActivityByCooperative(ctx, []uuid.UUID{f.coopB}); len(act) != 0 {
		t.Fatalf("HTX B không có hoạt động: %+v", act)
	}
	areas, err := f.mod.Stats().SeasonAreas(ctx, f.coopA, "DX", 2026)
	if err != nil || len(areas) != 1 || areas[0].AreaHa.String() != "3" || areas[0].OpenSeasons != 2 {
		t.Fatalf("diện tích vụ: %+v %v", areas, err)
	}
}

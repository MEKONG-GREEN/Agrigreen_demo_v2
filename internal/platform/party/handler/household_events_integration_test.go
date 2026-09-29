package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// householdEvents đọc sự kiện nông hộ của tenant trong shared.outbox (vai ag_outbox_publisher).
func (f *fixture) householdEvents(t *testing.T, typ, ref string) []map[string]string {
	t.Helper()
	pub := f.env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	rows, err := pub.Query(context.Background(), `SELECT coalesce(payload->>'source', ''), coalesce(payload->>'change', ''),
	    coalesce(payload->>'cooperative_ref', ''), coalesce(payload->>'prev_cooperative_ref', ''), coalesce(payload->>'commune_code', ''),
	    payload::text
	  FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2 AND payload->>'household_ref' = $3 ORDER BY occurred_at`, f.tenant, typ, ref)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var src, change, coop, prev, commune, raw string
		if err := rows.Scan(&src, &change, &coop, &prev, &commune, &raw); err != nil {
			t.Fatal(err)
		}
		out = append(out, map[string]string{"source": src, "change": change, "cooperative_ref": coop, "prev": prev, "commune": commune, "raw": raw})
	}
	return out
}

// KN US-HTX-05: nông hộ đăng ký từ App HTX phát pf.party.household.registered (mã
// định danh chung, không phát lại khi trùng); vào/chuyển/rời HTX phát
// pf.party.household.membership_changed cùng giao dịch. Không có PII trong sự kiện.
func TestHouseholdEventsKNUSHTX05(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	c1b := root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Lúa Vàng", "admin_unit_code": "30001", "parent_ref": tr.t30001.String()})
	mgr := f.as(f.tenant, tr.c1, port.RoleCoopManager)

	r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907000001"))
	if r.status != http.StatusCreated {
		t.Fatalf("tạo hộ: %d %v", r.status, r.body)
	}
	ref := str(r.body, "household", "household_ref")
	// Trùng chỉ mục mù → không phát lần hai.
	if r2 := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907000001")); r2.status != http.StatusOK {
		t.Fatalf("trùng: %d", r2.status)
	}
	reg := f.householdEvents(t, port.EventHouseholdRegistered, ref)
	if len(reg) != 1 || reg[0]["source"] != port.HouseholdSourceHTXApp || reg[0]["cooperative_ref"] != tr.c1.String() || reg[0]["commune"] != "30001" {
		t.Fatalf("registered: %v", reg)
	}
	for _, pii := range []string{"0907000001", "Nguyễn Văn An", "Ấp 1"} {
		if strings.Contains(reg[0]["raw"], pii) {
			t.Fatalf("sự kiện chứa dữ liệu cá nhân: %s", reg[0]["raw"])
		}
	}

	path := "/v1/households/" + ref + "/membership"
	// Quản lý HTX không chuyển hộ sang HTX khác.
	if r := mgr.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": c1b.String(), "reason": "Hộ xin chuyển sang HTX Lúa Vàng"}); r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX chuyển hộ: %d %v", r.status, r.body)
	}
	officer := f.as(f.tenant, tr.t30001, port.RoleKNOfficer)
	if r := officer.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": c1b.String(), "reason": "ngắn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu lý do: %d", r.status)
	}
	if r := officer.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": c1b.String(), "reason": "Hộ xin chuyển sang HTX Lúa Vàng"}); r.status != http.StatusOK || r.body["change"] != "transferred" || str(r.body, "household", "cooperative_ref") != c1b.String() {
		t.Fatalf("chuyển HTX: %d %v", r.status, r.body)
	}
	if r := officer.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": c1b.String(), "reason": "Hộ xin chuyển sang HTX Lúa Vàng"}); r.status != http.StatusConflict {
		t.Fatalf("đổi lại cùng HTX: %d", r.status)
	}
	// Quản lý HTX Lúa Vàng cho hộ rời HTX của mình.
	mgrB := f.as(f.tenant, c1b, port.RoleCoopManager)
	if r := mgrB.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": nil, "reason": "Hộ không còn canh tác lúa"}); r.status != http.StatusOK || r.body["change"] != "left" {
		t.Fatalf("rời HTX: %d %v", r.status, r.body)
	}
	// Cán bộ xã khác không thấy hộ.
	if r := f.as(f.tenant, tr.t30002, port.RoleKNOfficer).appDo(http.MethodPost, path, map[string]any{"cooperative_ref": tr.c1.String(), "reason": "Vào lại HTX Lúa Xanh"}); r.status != http.StatusNotFound {
		t.Fatalf("ngoài xã: %d", r.status)
	}
	// Quản lý HTX Lúa Xanh cho hộ (chưa thuộc HTX nào) vào lại HTX của mình.
	if r := mgr.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": tr.c1.String(), "reason": "Hộ đăng ký vào lại HTX"}); r.status != http.StatusOK || r.body["change"] != "joined" {
		t.Fatalf("vào HTX: %d %v", r.status, r.body)
	}
	ch := f.householdEvents(t, port.EventHouseholdMembershipChanged, ref)
	want := []struct{ change, coop, prev string }{
		{"transferred", c1b.String(), tr.c1.String()}, {"left", "", c1b.String()}, {"joined", tr.c1.String(), ""},
	}
	if len(ch) != len(want) {
		t.Fatalf("membership_changed: %v", ch)
	}
	for i, w := range want {
		if ch[i]["change"] != w.change || ch[i]["cooperative_ref"] != w.coop || ch[i]["prev"] != w.prev {
			t.Fatalf("sự kiện %d: %v, muốn %+v", i, ch[i], w)
		}
		if strings.Contains(ch[i]["raw"], "canh tác") {
			t.Fatal("lý do không đi vào sự kiện")
		}
	}
	// Tenant khác không thấy hộ.
	if r := f.as(uuid.New(), tr.t30001, port.RoleKNOfficer).appDo(http.MethodPost, path, map[string]any{"cooperative_ref": nil, "reason": "Thử từ tenant khác"}); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác: %d", r.status)
	}
}

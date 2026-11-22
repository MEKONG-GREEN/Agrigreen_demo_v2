package hcplan_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// NOTIFY-HCTH-KN (#50, W11.27 FN-11, prototype "Nhắc phòng chưa lập") qua HTTP trên PostgreSQL + cap/forms thật: chỉ cấp
// Trung tâm có quyền GHI (Phòng HCTH, Admin) nhắc; nhắc đúng các đơn vị CHƯA gửi; sổ plan_reminder và sự kiện
// pf.hrm.plan.reminded ghi cùng giao dịch, mang Nút của phiên người nhắc (không suy toàn tenant), không PII; kỳ đã chốt →
// 409; mọi đơn vị đã gửi → 409 không ghi; tenant khác không thấy kỳ.
func TestHRMPlanRemind_NotifyHCTH(t *testing.T) {
	f, _ := newPlanFixture(t)
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	hcth := f.who["hcth"]
	tid, org := hcth.TenantID, hcth.KN.UnitRef
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", planStaff("Lê Thị Hưu", "0912100001", "trong_trot", "nu", "1970-05-01"), http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/plans", map[string]any{"year": 2027, "due_on": "2026-11-30", "cutoff_on": "2026-10-12",
		"units": []string{"trong_trot", "chan_nuoi", "hcth"}}, http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2030/remind", nil, http.StatusNotFound)
	f.call("other", http.MethodPost, "/v1/hrm/plans/2027/remind", nil, http.StatusNotFound)

	// Trồng trọt đã gửi; Chăn nuôi chỉ có nháp ("Đang lập") — vẫn là chưa gửi.
	in := map[string]any{"leaving": 0, "additional": 1}
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", in, http.StatusCreated)
	f.call("ld_cn", http.MethodPut, "/v1/hrm/plans/2027/units/chan_nuoi/draft", in, http.StatusOK)

	// Quyền: lãnh đạo đơn vị, viên chức, Cấp TW, phiên chỉ xem (kể cả HCTH/Admin chỉ xem) không nhắc được.
	for _, as := range []string{"ld_tt", "vc_tt", "xa", "tw", "vo_hcth", "vo_admin", "realmonly"} {
		f.call(as, http.MethodPost, "/v1/hrm/plans/2027/remind", nil, http.StatusForbidden)
	}
	events := func() []map[string]any {
		t.Helper()
		rows, err := admin.Query(context.Background(), `SELECT payload::text FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2
		    AND producer = 'pf/hrm' ORDER BY id`, tid, "pf.hrm.plan.reminded")
		if err != nil {
			t.Fatal(err)
		}
		raws, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		var out []map[string]any
		for _, raw := range raws {
			for _, pii := range []string{"Lê Thị Hưu", "0912100001", "Phòng Chăn nuôi"} {
				if strings.Contains(raw, pii) {
					t.Fatalf("sự kiện chứa %q: %s", pii, raw)
				}
			}
			var m map[string]any
			_ = json.Unmarshal([]byte(raw), &m)
			out = append(out, m)
		}
		return out
	}
	if n := len(events()); n != 0 {
		t.Fatalf("chưa nhắc đã có %d sự kiện", n)
	}

	r := f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/remind", nil, http.StatusCreated)
	var got []string
	for _, u := range r["units"].([]any) {
		got = append(got, u.(map[string]any)["unit"].(string))
	}
	if !slices.Equal(got, []string{"chan_nuoi", "hcth"}) || r["units"].([]any)[0].(map[string]any)["unit_name"] != "Phòng Chăn nuôi" ||
		r["due_at"] != "2026-11-30T16:59:59Z" {
		t.Fatalf("nhắc: %v", r)
	}
	evs := events()
	if len(evs) != 1 {
		t.Fatalf("sự kiện: %v", evs)
	}
	e := evs[0]
	if e["node_ref"] != org.String() || e["actor_ref"] != hcth.ActorRef.String() || e["reminder_id"] != r["id"] || e["period_ref"] != r["period_ref"] ||
		e["year"] != float64(2027) || e["due_at"] != "2026-11-30T16:59:59Z" {
		t.Fatalf("payload: %v", e)
	}
	if us := e["units"].([]any); len(us) != 2 || us[0] != "chan_nuoi" || us[1] != "hcth" {
		t.Fatalf("đơn vị trong sự kiện: %v", us)
	}
	// Sổ nhắc: một dòng, cùng mã với sự kiện; sổ chỉ ghi thêm.
	var ledger int
	if err := admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_hrm.plan_reminder WHERE tenant_id = $1 AND id::text = $2
	    AND node_ref = $3 AND units = ARRAY['chan_nuoi','hcth']`, tid, r["id"], org).Scan(&ledger); err != nil || ledger != 1 {
		t.Fatalf("sổ nhắc: %d %v", ledger, err)
	}
	if _, err := admin.Exec(context.Background(), `UPDATE pf_hrm.plan_reminder SET units = ARRAY['x'] WHERE tenant_id = $1`, tid); err == nil {
		t.Fatal("sổ nhắc phải chặn UPDATE")
	}

	// Admin nhắc lần nữa (lần nhắc mới — khóa idem khác) → thêm một sự kiện.
	f.call("admin", http.MethodPost, "/v1/hrm/plans/2027/remind", nil, http.StatusCreated)
	if n := len(events()); n != 2 {
		t.Fatalf("nhắc lại: %d sự kiện", n)
	}

	// Kỳ đã chốt → 409, không ghi.
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/close", nil, http.StatusOK)
	if e := f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/remind", nil, http.StatusConflict); e["code"] != "hrm.plan_closed" {
		t.Fatalf("nhắc sau chốt: %v", e)
	}
	// Mọi đơn vị đã gửi → 409 hrm.plan_all_submitted, không ghi.
	f.call("hcth", http.MethodPost, "/v1/hrm/plans", map[string]any{"year": 2028, "due_on": "2027-11-30", "units": []string{"trong_trot"}}, http.StatusCreated)
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2028/units/trong_trot/submit", in, http.StatusCreated)
	if e := f.call("hcth", http.MethodPost, "/v1/hrm/plans/2028/remind", nil, http.StatusConflict); e["code"] != "hrm.plan_all_submitted" {
		t.Fatalf("nhắc khi đã gửi đủ: %v", e)
	}
	if n := len(events()); n != 2 {
		t.Fatalf("sau chốt/đủ: %d sự kiện", n)
	}
}

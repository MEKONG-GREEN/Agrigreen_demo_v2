package hc30_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// This fixture adds subjects at the authentication boundary, using the actual
// owner session API. No HRM app/domain/store helper supplies the acceptance oracle.
func (f *fixture) appraisalSubject(name, unit string) {
	f.t.Helper()
	session, err := knport.NewSessionContext(knport.SessionContext{
		ID: ids.New(), Role: knport.SessionRoleProvince, Department: unit,
		InternalUnit: unit, OrgRef: f.people["hcth"].SelectedSession.Scope().NodeID,
		UnitRef: f.people["hcth"].SelectedSession.Scope().NodeID, Scope: authz.Scope{All: true},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	f.people[name] = authz.Principal{
		ActorRef: ids.New(), TenantID: f.people["hcth"].TenantID, MFA: true, SelectedSession: &session,
	}
}

// The paper column keys are an explicit fixture contract, not generated from the
// production default. Official labels/layout still await Q-070.
func appraisalPaper(kind string) map[string]any {
	keys := []string{"ordinal", "staff", "position_unit", "self_rating", "collective_rating", "reason", "personnel_proposal", "note"}
	columns := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		columns = append(columns, map[string]string{"key": key, "title": key})
	}
	return map[string]any{
		"code": strings.ToLower(kind), "kind": kind, "number": strings.TrimPrefix(kind, "MS"),
		"name": "Mẫu tổng hợp kiểm thử " + kind, "title": "Đánh giá quý {QUY} năm {NAM}",
		"audience": "Dữ liệu tổng hợp W11.31", "signature": "Tập thể lãnh đạo ký ngoài hệ thống",
		"columns": columns,
		"ratings": map[string]string{"HTXSNV": "Xuất sắc", "HTTNV": "Tốt", "HTNV": "Hoàn thành", "KHTNV": "Không hoàn thành"},
	}
}

func (f *fixture) appraisalTemplates() (map[string]string, map[string]string) {
	f.t.Helper()
	summaries, forms := map[string]string{}, map[string]string{}
	for _, kind := range []string{"MS05", "MS06"} {
		r := f.request("hcth", http.MethodPost, "/v1/hrm/appraisals/templates/summary/"+strings.ToLower(kind),
			"hc30-summary-"+kind, map[string]any{"status": "published", "summary": appraisalPaper(kind)}, http.StatusCreated)
		summaries[kind] = r.body["id"].(string)
	}
	for _, group := range []string{"vien_chuc", "lanh_dao", "can_bo_xa"} {
		criteria := make([]map[string]string, 0, 6)
		for i := 1; i <= 6; i++ {
			criteria = append(criteria, map[string]string{"code": fmt.Sprintf("tc_%d", i), "title": fmt.Sprintf("Tiêu chí %d", i), "maximum": "15"})
		}
		form := map[string]any{"code": "hc30_" + group, "name": "Phiếu tổng hợp W11.31", "group": group, "criteria": criteria,
			"bands": []map[string]string{{"minimum": "81", "rating": "HTXSNV"}, {"minimum": "63", "rating": "HTTNV"}, {"minimum": "45", "rating": "HTNV"}, {"minimum": "0", "rating": "KHTNV"}}}
		r := f.request("hcth", http.MethodPost, "/v1/hrm/appraisals/templates/form/hc30_"+group,
			"hc30-form-"+group, map[string]any{"status": "published", "form": form}, http.StatusCreated)
		forms[group] = r.body["id"].(string)
	}
	return summaries, forms
}

type countMember struct {
	id, unit, managementUnit, summary string
}

// assertAppraisalCounts uses an immutable membership/visibility table and expected
// states set by the scenario. It never infers expected visibility from a response,
// calls a production policy, or reads a ledger. Draft contents are deliberately
// outside this test's oracle pending PO.
func assertAppraisalCounts(t *testing.T, f *fixture, period string, members map[string]countMember, states map[string]string, visible map[string][]string) {
	t.Helper()
	for _, actor := range []string{"hcth", "manager_tt", "manager_cn", "self", "commune", "civil_cn", "leader_cn"} {
		wantCounts := map[string]int{}
		wantIDs := []string{}
		for _, key := range visible[actor] {
			wantIDs = append(wantIDs, members[key].id)
			wantCounts[states[key]]++
			wantCounts[members[key].summary]++
		}
		list := f.request(actor, http.MethodGet, "/v1/hrm/appraisals/periods/"+period+"/entries", "", nil, http.StatusOK)
		gotIDs := []string{}
		for _, raw := range list.body["items"].([]any) {
			item := raw.(map[string]any)
			member := item["member"].(map[string]any)
			id := member["staff_id"].(string)
			gotIDs = append(gotIDs, id)
			key := ""
			for candidate, expected := range members {
				if expected.id == id {
					key = candidate
					break
				}
			}
			if key == "" || !slices.Contains(visible[actor], key) {
				t.Fatalf("%s received a member outside the fixed assignment", actor)
			}
			want := members[key]
			if item["status"] != states[key] || member["unit"] != want.unit || member["management_unit"] != want.managementUnit || member["summary_kind"] != want.summary {
				t.Fatalf("%s member %s status/assignment differs from fixed oracle", actor, key)
			}
		}
		slices.Sort(wantIDs)
		slices.Sort(gotIDs)
		if !slices.Equal(gotIDs, wantIDs) {
			t.Fatalf("%s visible IDs differ: got %v want %v", actor, gotIDs, wantIDs)
		}
		periods := f.request(actor, http.MethodGet, "/v1/hrm/appraisals/periods", "", nil, http.StatusOK)
		if len(periods.body["items"].([]any)) != 1 {
			t.Fatalf("%s expected exactly one visible period", actor)
		}
		p := periods.body["items"].([]any)[0].(map[string]any)
		gotCounts := map[string]int{}
		for key, raw := range p["counts"].(map[string]any) {
			n, err := raw.(json.Number).Int64()
			if err != nil {
				t.Fatal(err)
			}
			gotCounts[key] = int(n)
		}
		if p["id"] != period || !maps.Equal(gotCounts, wantCounts) {
			t.Fatalf("period counts for %s: got %v want %v", actor, gotCounts, wantCounts)
		}
	}
}

func TestHCTHAppraisalCountsByTrustedCaller_W1131(t *testing.T) {
	f := newFixture(t)
	f.appraisalSubject("civil_cn", "chan_nuoi")
	f.appraisalSubject("leader_cn", "chan_nuoi")
	f.request("admin", http.MethodPost, "/v1/hrm/defaults", "hc30-count-defaults", nil, http.StatusCreated)
	members := map[string]countMember{}
	subjects := []struct{ key, actor, typ, unit, position string }{
		{"tt", "self", "vien_chuc", "trong_trot", "vien_chuc"},
		{"xa", "commune", "can_bo_xa", "trong_trot", "vien_chuc"},
		{"cn", "civil_cn", "vien_chuc", "chan_nuoi", "vien_chuc"},
		{"leader", "leader_cn", "vien_chuc", "chan_nuoi", "pho_truong_phong"},
	}
	for i, s := range subjects {
		names := []string{"Trần Thị An", "Lê Văn Bình", "Nguyễn Thị Cúc", "Phạm Văn Dung"}
		body := staff(names[i], fmt.Sprintf("09120100%02d", i), s.typ, s.unit)
		body["account_ref"] = f.people[s.actor].ActorRef.String()
		if s.position != "vien_chuc" {
			events := body["events"].([]any)
			events[1] = event("position", "2020-01-01", map[string]any{"position": s.position, "appointed_on": "2020-01-01"})
		}
		id := f.request("hcth", http.MethodPost, "/v1/hrm/staff", "hc30-count-create-"+s.key, body, http.StatusCreated).body["id"].(string)
		m := countMember{id: id, unit: s.unit, managementUnit: s.unit, summary: "MS05"}
		if s.typ == "can_bo_xa" {
			m.unit = ""
		}
		if s.position != "vien_chuc" {
			m.summary = "MS06"
		}
		members[s.key] = m
	}
	summaries, forms := f.appraisalTemplates()
	period := f.request("hcth", http.MethodPost, "/v1/hrm/appraisals/periods", "hc30-count-open", map[string]any{
		"year": 2026, "quarter": 3, "cutoff_on": "2026-09-30", "self_due_at": "2026-10-05T16:59:59Z",
		"manager_due_at": "2026-10-20T16:59:59Z", "summary_due_at": "2026-10-25T16:59:59Z",
		"forms": forms, "summaries": summaries,
	}, http.StatusCreated).body["id"].(string)
	base := "/v1/hrm/appraisals/periods/" + period
	visible := map[string][]string{
		"hcth": {"tt", "xa", "cn", "leader"}, "manager_tt": {"tt", "xa"}, "manager_cn": {"cn"},
		"self": {"tt"}, "commune": {"xa"}, "civil_cn": {"cn"}, "leader_cn": {"leader"},
	}
	states := map[string]string{"tt": "not_started", "xa": "not_started", "cn": "not_started", "leader": "not_started"}
	versions := map[string]int{"tt": 1, "xa": 1, "cn": 1, "leader": 1}
	check := func(name string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			assertAppraisalCounts(t, f.withTest(t), period, members, states, visible)
		})
	}
	check("open")
	for _, actor := range []string{"realm_only", "wrong_owner", "national", "readonly"} {
		f.request(actor, http.MethodGet, base+"/entries", "", nil, http.StatusForbidden)
	}
	f.request("other_tenant", http.MethodGet, base+"/entries", "", nil, http.StatusNotFound)
	other := f.request("other_tenant", http.MethodGet, "/v1/hrm/appraisals/periods", "", nil, http.StatusOK)
	if len(other.body["items"].([]any)) != 0 {
		t.Fatal("other tenant received an appraisal period")
	}
	answers := []map[string]string{
		{"code": "tc_1", "points": "15"}, {"code": "tc_2", "points": "15"}, {"code": "tc_3", "points": "15"},
		{"code": "tc_4", "points": "15"}, {"code": "tc_5", "points": "15"}, {"code": "tc_6", "points": "7"},
	}
	rejected := f.request("self", http.MethodPost, base+"/entries/"+members["tt"].id, "hc30-count-khtnv-no-basis",
		map[string]any{"action": "self_submit", "version": 1, "answers": answers, "self_rating": "KHTNV"}, http.StatusUnprocessableEntity)
	if rejected.body["code"] != "validation_failed" {
		t.Fatalf("missing-basis request returned unexpected code %v", rejected.body["code"])
	}
	check("KHTNV_without_basis_is_unchanged")
	change := func(key, actor, action, wantState string, extra map[string]any) {
		t.Helper()
		body := map[string]any{"action": action, "version": versions[key], "reason": "Căn cứ tổng hợp từ hồ sơ kiểm thử"}
		maps.Copy(body, extra)
		r := f.request(actor, http.MethodPost, base+"/entries/"+members[key].id, "hc30-count-"+action+"-"+key+"-"+fmt.Sprint(versions[key]), body, http.StatusCreated)
		versions[key]++
		if r.body["version"] != json.Number(fmt.Sprint(versions[key])) {
			t.Fatal("successful action did not add exactly one entry version")
		}
		states[key] = wantState
	}
	for _, s := range subjects {
		change(s.key, s.actor, "self_submit", "self_submitted", map[string]any{"answers": answers})
		check("self_submit_" + s.key)
	}
	change("cn", "manager_cn", "return", "change_requested", map[string]any{"return_to": "self"})
	check("return_to_self")
	change("cn", "civil_cn", "self_submit", "self_submitted", map[string]any{"answers": answers})
	check("self_resubmit")
	for _, s := range []struct{ key, manager string }{{"tt", "manager_tt"}, {"xa", "manager_tt"}, {"cn", "manager_cn"}, {"leader", "hcth"}} {
		change(s.key, s.manager, "manager_submit", "manager_submitted", map[string]any{"answers": answers, "collective_rating": "HTTNV"})
		check("manager_submit_" + s.key)
	}
	periodVersion := 1
	for _, unit := range []struct {
		actor, code string
		keys        []string
	}{
		{"manager_tt", "trong_trot", []string{"tt", "xa"}},
		{"manager_cn", "chan_nuoi", []string{"cn"}},
	} {
		f.request(unit.actor, http.MethodPost, base, "hc30-count-unit-send-"+unit.code,
			map[string]any{"action": "send", "version": periodVersion, "unit": unit.code}, http.StatusCreated)
		periodVersion++
		for _, key := range unit.keys {
			states[key], versions[key] = "sent", versions[key]+1
		}
		check("unit_send_" + unit.code)
	}
	change("leader", "hcth", "send", "sent", nil)
	check("MS06_sent_by_HCTH")
	change("xa", "hcth", "return", "change_requested", map[string]any{"return_to": "manager"})
	check("HCTH_return_to_manager")
	change("xa", "manager_tt", "manager_submit", "manager_submitted", map[string]any{"answers": answers, "collective_rating": "HTTNV"})
	check("manager_resubmit")
	change("xa", "manager_tt", "send", "sent", nil)
	check("resent_after_correction")
	closed := f.request("hcth", http.MethodPost, base, "hc30-count-close",
		map[string]any{"action": "close", "version": periodVersion, "acknowledge_unsigned": true}, http.StatusCreated)
	if closed.body["closed"] != true {
		t.Fatal("period did not close")
	}
	for key := range states {
		states[key] = "closed"
	}
	check("closed_all_four")
}

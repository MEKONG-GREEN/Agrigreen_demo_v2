package hc30_test

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"testing"
	"time"
)

// HC-0 retains frozen cohort/idempotency coverage while removing personal submissions.
// Managers enter assigned MS05 records and HCTH enters MS06 using verified scans.
// These are sequential in-process HTTP attempts, not a concurrency or deployed-load claim.
func TestHCTHQuarterCohort_HC0(t *testing.T)            { runCohort(t, 494) }
func TestHCTH500ManagementSubmissions_HC0(t *testing.T) { runCohort(t, 500) }
func runCohort(t *testing.T, count int) {
	f := newFixture(t)
	staff := make([]string, count)
	for i := range count {
		as := fmt.Sprintf("person-%03d", i)
		role, typ, unit, pos := "kn_tinh", "vien_chuc", "trong_trot", "vien_chuc"
		if i == 0 {
			role, typ = "kn_xa", "can_bo_xa"
		}
		if i >= count-24 {
			unit, pos = "hcth", "truong_phong"
		}
		f.add(as, role, unit, false, f.tenant)
		u := map[string]any{"unit": unit}
		if typ == "can_bo_xa" {
			u = map[string]any{"commune": "30292", "mgmt_unit": unit}
		}
		position := map[string]any{"position": pos}
		if pos == "truong_phong" {
			position["appointed_on"] = "2020-01-01"
		}
		in := newStaff(typ, fmt.Sprintf("Người Nghiệm Thu %s", letters(i)), fmt.Sprintf("0912%06d", i), ev("unit", "2020-01-01", u), ev("position", "2020-01-01", position), ev("work_start", "2020-01-01", map[string]any{}))
		v := f.call("hcth", http.MethodPost, "/v1/hrm/staff", "", in, http.StatusCreated)
		staff[i] = v["id"].(string)
		f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+staff[i]+"/profile", "", map[string]any{"account_ref": f.who[as].ActorRef}, http.StatusOK)
	}
	in := periodInput{Year: 2026, Quarter: 3, CutoffOn: "2026-09-30", SelfDueAt: "2026-10-12T16:59:59Z", ManagerDueAt: "2026-10-20T16:59:59Z", SummaryDueAt: "2026-10-25T16:59:59Z", Forms: map[string]uuid.UUID{}, Summaries: map[string]uuid.UUID{}}
	for _, kind := range []string{"MS05", "MS06"} {
		summary := summaryTemplate(kind)
		v := f.call("hcth", http.MethodPost, "/v1/hrm/appraisals/templates/summary/"+strings.ToLower(kind), "summary-"+kind, templateChange{Status: "published", Summary: summary}, http.StatusCreated)
		in.Summaries[kind] = uuid.MustParse(v["id"].(string))
	}
	for _, group := range []string{"vien_chuc", "lanh_dao", "can_bo_xa"} {
		form := formInput{Code: "uat_" + group, Name: "Phiếu nghiệm thu", Group: group, Bands: []bandInput{{Minimum: "81", Rating: "HTXSNV"}, {Minimum: "63", Rating: "HTTNV"}, {Minimum: "45", Rating: "HTNV"}, {Minimum: "0", Rating: "KHTNV"}}}
		for j := range 6 {
			form.Criteria = append(form.Criteria, criterionInput{Code: fmt.Sprintf("tc_%d", j), Title: fmt.Sprintf("Tiêu chí %d", j+1), Maximum: "15"})
		}
		v := f.call("hcth", http.MethodPost, "/v1/hrm/appraisals/templates/form/"+form.Code, "form-"+group, templateChange{Status: "published", Form: &form}, http.StatusCreated)
		in.Forms[group] = uuid.MustParse(v["id"].(string))
	}
	start := time.Now()
	opened := f.call("hcth", http.MethodPost, "/v1/hrm/appraisals/periods", "w1131-q3-open", in, http.StatusCreated)
	period := opened["id"].(string)
	base := "/v1/hrm/appraisals/periods/" + period
	t.Logf("open frozen cohort: %s", time.Since(start))
	if replay := f.call("hcth", http.MethodPost, "/v1/hrm/appraisals/periods", "w1131-q3-open", in, http.StatusCreated); replay["id"] != period {
		t.Fatal("open retry duplicated period")
	}
	entries := f.call("hcth", http.MethodGet, base+"/entries", "", nil, http.StatusOK)["items"].([]any)
	kinds := map[string]int{}
	seen := map[string]bool{}
	for _, v := range entries {
		e := v.(map[string]any)
		m := e["member"].(map[string]any)
		id := m["staff_id"].(string)
		if seen[id] {
			t.Fatal("duplicate cohort member")
		}
		seen[id] = true
		kinds[m["summary_kind"].(string)]++
	}
	if len(entries) != count || kinds["MS05"] != count-24 || kinds["MS06"] != 24 {
		t.Fatalf("cohort=%d summaries=%v", len(entries), kinds)
	}
	for _, id := range staff {
		if !seen[id] {
			t.Fatalf("missing member %s", id)
		}
	}
	// FN-16 BR-04: the managing unit sees assigned staff, including the commune officer.
	for _, tc := range []struct {
		as string
		n  int
	}{{"ld_tt", count - 24}, {"hcth", count}} {
		v := f.call(tc.as, http.MethodGet, base+"/entries", "", nil, http.StatusOK)
		if len(v["items"].([]any)) != tc.n {
			t.Fatalf("scope %s: got%d want%d", tc.as, len(v["items"].([]any)), tc.n)
		}
	}
	f.call("ld_cn", http.MethodGet, base+"/entries", "", nil, http.StatusNotFound)
	f.call("other", http.MethodGet, base+"/entries", "", nil, http.StatusNotFound)
	for _, who := range []string{"person-000", "person-001"} {
		f.call(who, http.MethodGet, base+"/entries", "", nil, http.StatusForbidden)
		f.call(who, http.MethodGet, base+"/entries/"+staff[0], "", nil, http.StatusForbidden)
	}
	// Late staffing changes do not mutate the frozen cohort or the 470/24 split.
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", "", newStaff("vien_chuc", "Người Ngoài Đợt", "0999000001", ev("unit", "2020-01-01", map[string]any{"unit": "trong_trot"}), ev("position", "2020-01-01", map[string]any{"position": "vien_chuc"})), http.StatusCreated)
	answers := []answerInput{}
	for j := range 6 {
		points := "15"
		if j == 5 {
			points = "7"
		}
		answers = append(answers, answerInput{Code: fmt.Sprintf("tc_%d", j), Points: points, Note: "Hoàn thành nhiệm vụ"})
	}
	change := entryInput{Action: "proxy_self", Version: 1, Answers: answers, Reason: "Lãnh đạo nhập thay từ bản giấy đã đối chiếu"}
	ms05Proof := f.appraisalProof("trong_trot", staff[:count-24])
	ms06Proof := f.appraisalProof("hcth", staff[count-24:])
	for _, who := range []string{"person-000", "person-001"} {
		f.call(who, http.MethodPost, base+"/entries/"+staff[0], "removed-personal-submit", entryInput{Action: "self_submit", Version: 1, Answers: answers}, http.StatusForbidden)
	}
	f.call("hcth", http.MethodPost, base+"/entries/"+staff[0], "removed-management-self", entryInput{Action: "self_submit", Version: 1, Answers: answers}, http.StatusUnprocessableEntity)
	change.ProxyDocument = &ms06Proof
	f.call("ld_tt", http.MethodPost, base+"/entries/"+staff[0], "wrong-proof-unit", change, http.StatusNotFound)
	submitted := make([]string, count)
	start = time.Now()
	slowest := time.Duration(0)
	for i := range 500 {
		idx := i % count
		before := time.Now()
		actor := "ld_tt"
		change.ProxyDocument = &ms05Proof
		if idx >= count-24 {
			actor, change.ProxyDocument = "hcth", &ms06Proof
		}
		v := f.call(actor, http.MethodPost, base+"/entries/"+staff[idx], fmt.Sprintf("submit-%03d", idx), change, http.StatusCreated)
		if elapsed := time.Since(before); elapsed > slowest {
			slowest = elapsed
		}
		if v["self_rating"] != "HTXSNV" || v["self"].(map[string]any)["total"] != "82" {
			t.Fatalf("submission %d rating/score=%v", i, v)
		}
		if i < count {
			submitted[idx] = v["id"].(string)
		} else if v["id"] != submitted[idx] {
			t.Fatal("retry appended a version")
		}
	}
	t.Logf("500 sequential management HTTP attempts, %d assigned records, %d retries: total=%s slowest=%s", count, 500-count, time.Since(start), slowest)
	entries = f.call("hcth", http.MethodGet, base+"/entries", "", nil, http.StatusOK)["items"].([]any)
	if len(entries) != count {
		t.Fatalf("frozen cohort changed: %d", len(entries))
	}
	for _, v := range entries {
		e := v.(map[string]any)
		if e["status"] != "self_submitted" || e["version"] != json.Number("2") {
			t.Fatalf("incomplete or duplicated submission: %v", e)
		}
	}
	periods := f.call("hcth", http.MethodGet, "/v1/hrm/appraisals/periods", "", nil, http.StatusOK)["items"].([]any)
	for _, v := range periods {
		p := v.(map[string]any)
		if p["id"] == period {
			counts := p["counts"].(map[string]any)
			if counts["self_submitted"] != json.Number(fmt.Sprint(count)) || counts["MS05"] != json.Number(fmt.Sprint(count-24)) || counts["MS06"] != json.Number("24") {
				t.Fatalf("progress counts: %v", counts)
			}
			return
		}
	}
	t.Fatal("period missing from progress")
}

// Alphabetic generated names avoid embedding personal data or digits forbidden by the profile validator.
func letters(n int) string {
	s := ""
	for {
		s = string(rune('A'+n%26)) + s
		n = n/26 - 1
		if n < 0 {
			return s
		}
	}
}

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
)

type appraisalFixture struct {
	f       *recFixture
	input   app.AppraisalPeriodInput
	staff   map[string]string
	answers []domain.AppraisalAnswer
}

func newAppraisalFixture(t *testing.T) appraisalFixture {
	t.Helper()
	f := newRecFixture(t, true)
	ff := withDocs(t, f)
	f.svc.UseDocs(appraisalProofFiles{ff, f.tid})
	// Cấp Xã thật luôn có OrgRef của lần gán; fixture hồ sơ cũ không cần vì bị chặn trước truy vấn.
	xa := f.who["xa"]
	k := *xa.KN
	k.OrgRef = k.UnitRef
	xa.KN = &k
	f.who["xa"] = xa
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	a := appraisalFixture{f: f, staff: map[string]string{}, input: app.AppraisalPeriodInput{Year: 2026, Quarter: 3, CutoffOn: "2026-09-30",
		ManagerDueAt: "2026-10-20T16:59:59Z", Groups: []string{domain.AppraisalCivil, domain.AppraisalLeader, domain.AppraisalCommune}, Summaries: map[string]uuid.UUID{}}}
	for i, x := range []struct{ who, typ, unit, position string }{{"xa", "can_bo_xa", "trong_trot", "vien_chuc"}, {"vc_tt", "vien_chuc", "trong_trot", "vien_chuc"}, {"hcth2", "vien_chuc", "hcth", "truong_phong"}} {
		unit := map[string]any{"unit": x.unit}
		if x.typ == "can_bo_xa" {
			unit = map[string]any{"commune": "30292", "mgmt_unit": x.unit}
		}
		position := map[string]any{"position": x.position}
		if x.position == "truong_phong" {
			position["appointed_on"] = "2020-01-01"
		}
		id := f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff(x.typ, fmt.Sprintf("Người Đánh Giá %c", 'A'+i), fmt.Sprintf("09123456%02d", i),
			ev("unit", "2020-01-01", unit), ev("position", "2020-01-01", position), ev("work_start", "2020-01-01", map[string]any{})), http.StatusCreated)["id"].(string)
		f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id+"/profile", map[string]any{"account_ref": f.who[x.who].ActorRef.String()}, http.StatusOK)
		a.staff[x.who] = id
	}
	for _, kind := range []string{"MS05", "MS06"} {
		s := domain.DefaultAppraisalSummary(kind)
		v := appraisalCall(t, f, "hcth", http.MethodPost, "/v1/hrm/appraisals/templates/summary/"+strings.ToLower(kind), "summary-"+kind,
			app.AppraisalTemplateChange{Status: "published", Summary: &s}, http.StatusCreated)
		a.input.Summaries[kind] = uuid.MustParse(v["id"].(string))
	}
	for i := range 6 {
		points := "15"
		if i == 5 {
			points = "7"
		}
		a.answers = append(a.answers, domain.AppraisalAnswer{Code: fmt.Sprintf("tc_%d", i), Points: points, Note: "Đã hoàn thành"})
	}
	return a
}

func (a appraisalFixture) open(t *testing.T, key string) string {
	t.Helper()
	v := appraisalCall(t, a.f, "hcth", http.MethodPost, "/v1/hrm/appraisals/periods", key, a.input, http.StatusCreated)
	return v["id"].(string)
}
func (a appraisalFixture) detail(t *testing.T, period, who string) map[string]any {
	t.Helper()
	return appraisalCall(t, a.f, "hcth", http.MethodGet, "/v1/hrm/appraisals/periods/"+period+"/entries/"+a.staff[who], "", nil, http.StatusOK)["item"].(map[string]any)
}
func (a appraisalFixture) change(t *testing.T, period, staff, as, key string, in domain.AppraisalEntryChange, want int) map[string]any {
	t.Helper()
	return appraisalCall(t, a.f, as, http.MethodPost, "/v1/hrm/appraisals/periods/"+period+"/entries/"+a.staff[staff], key, in, want)
}

// HC-2 thay assertions điểm bằng các cột DG04–10 của FN17/18.
func TestHRMAppraisalWorkflowHTTP_W1126(t *testing.T) {
	a := newAppraisalFixture(t)
	f := a.f
	period := a.open(t, "open-q3-first")
	base := "/v1/hrm/appraisals/periods/" + period
	appraisalCall(t, f, "hcth", http.MethodPost, "/v1/hrm/appraisals/periods", "open-q3-duplicate", a.input, http.StatusConflict)
	if a.open(t, "open-q3-first") != period {
		t.Fatal("retry mở đợt đổi mã")
	}
	for _, tc := range []struct {
		who   string
		count int
	}{{"ld_tt", 2}, {"hcth", 3}, {"admin", 3}} {
		if n := len(appraisalCall(t, f, tc.who, http.MethodGet, base+"/entries", "", nil, http.StatusOK)["items"].([]any)); n != tc.count {
			t.Fatalf("%s thấy %d", tc.who, n)
		}
	}
	appraisalCall(t, f, "ld_cn", http.MethodGet, base+"/entries/"+a.staff["xa"], "", nil, http.StatusNotFound)
	appraisalCall(t, f, "other", http.MethodGet, base+"/entries", "", nil, http.StatusNotFound)
	for _, who := range []string{"tw", "viewonly", "realmonly", "xa", "vc_tt"} {
		appraisalCall(t, f, who, http.MethodGet, base+"/entries", "", nil, http.StatusForbidden)
	}
	foreign := f.who["hcth"]
	node := *foreign.KN
	node.UnitRef = uuid.New()
	foreign.KN = &node
	f.who["other_org"] = foreign
	for _, path := range []string{base + "/exports", base + "/paper/MS05", base + "/entries/" + a.staff["xa"] + "/history"} {
		appraisalCall(t, f, "other_org", http.MethodGet, path, "", nil, http.StatusNotFound)
	}
	appraisalCall(t, f, "other_org", http.MethodPost, "/v1/hrm/appraisals/periods", "open-q3-first", a.input, http.StatusNotFound)
	appraisalCall(t, f, "other_org", http.MethodGet, base+"/entries", "", nil, http.StatusNotFound)
	appraisalCall(t, f, "other_org", http.MethodGet, base+"/entries/"+a.staff["xa"], "", nil, http.StatusNotFound)
	if xs := appraisalCall(t, f, "other_org", http.MethodGet, "/v1/hrm/appraisals/periods", "", nil, http.StatusOK)["items"].([]any); len(xs) != 0 {
		t.Fatal("lộ đợt nút khác")
	}
	permission := func(scope string) {
		f.call("admin", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "user", "grantee": f.who["hcth2"].ActorRef.String(), "perms": map[string]any{"assessment": scope}}, http.StatusCreated)
	}
	permission("self")
	if n := len(appraisalCall(t, f, "hcth2", http.MethodGet, base+"/entries", "", nil, http.StatusOK)["items"].([]any)); n != 1 {
		t.Fatal("override self bị mở rộng")
	}
	permission("none")
	appraisalCall(t, f, "hcth2", http.MethodGet, base+"/entries", "", nil, http.StatusForbidden)
	permission("all")
	save := domain.AppraisalEntryChange{Action: "manager_submit", Version: 1, SelfRating: "HTXSNV", CollectiveRating: "HTTNV", Reason: "Căn cứ bản tự đề xuất và biên bản tập thể", PersonnelProposal: "Tiếp tục phân công nhiệm vụ", Note: "Đề xuất của đơn vị"}
	bad := save
	bad.Reason = ""
	a.change(t, period, "xa", "ld_tt", "missing-basis", bad, http.StatusUnprocessableEntity)
	bad = save
	bad.SelfRating = ""
	a.change(t, period, "xa", "ld_tt", "missing-self-rating", bad, http.StatusUnprocessableEntity)
	bad = save
	bad.CollectiveRating = ""
	a.change(t, period, "xa", "ld_tt", "missing-collective-rating", bad, http.StatusUnprocessableEntity)
	appraisalCall(t, f, "ld_tt", http.MethodPost, base+"/entries/"+a.staff["xa"], "removed-scores", map[string]any{"action": "manager_submit", "version": 1, "answers": a.answers}, http.StatusUnprocessableEntity)
	a.change(t, period, "hcth2", "ld_tt", "manager-no-ms06", save, http.StatusNotFound)
	first := a.change(t, period, "xa", "ld_tt", "proposal-xa-first", save, http.StatusCreated)
	if first["status"] != "entered" || first["self_rating"] != "HTXSNV" || first["manager"].(map[string]any)["total"] != "" {
		t.Fatalf("DG04–10: %v", first)
	}
	if again := a.change(t, period, "xa", "ld_tt", "proposal-xa-first", save, http.StatusCreated); again["id"] != first["id"] {
		t.Fatal("retry thêm bản")
	}
	a.change(t, period, "xa", "ld_tt", "proposal-stale", save, http.StatusConflict)
	n, hash := appraisalEntryFingerprint(t, a)
	appraisalCall(t, f, "ld_tt", http.MethodPost, base, "send-incomplete-unit", app.AppraisalPeriodChange{Action: "send", Version: 1, Unit: "trong_trot"}, http.StatusUnprocessableEntity)
	if after, h := appraisalEntryFingerprint(t, a); after != n || h != hash {
		t.Fatal("gửi thiếu đã ghi từng phần")
	}
	for _, who := range []string{"vc_tt", "hcth2"} {
		as := "ld_tt"
		if who == "hcth2" {
			as = "hcth"
		}
		a.change(t, period, who, as, "proposal-"+who, save, http.StatusCreated)
	}
	appraisalCall(t, f, "ld_tt", http.MethodPost, base, "send-unit-tt", app.AppraisalPeriodChange{Action: "send", Version: 1, Unit: "trong_trot"}, http.StatusCreated)
	a.change(t, period, "hcth2", "hcth", "send-ms06", domain.AppraisalEntryChange{Action: "send", Version: 2}, http.StatusCreated)
	save.Version = 3
	a.change(t, period, "xa", "ld_tt", "sent-no-direct-edit", save, http.StatusUnprocessableEntity)
	a.change(t, period, "xa", "hcth", "official-result", domain.AppraisalEntryChange{Action: "official", Version: 3, OfficialRating: "HTNV"}, http.StatusCreated)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "close-no-warning-ack", app.AppraisalPeriodChange{Action: "close", Version: 2}, http.StatusUnprocessableEntity)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "close-q3", app.AppraisalPeriodChange{Action: "close", Version: 2, AcknowledgeUnsigned: true}, http.StatusCreated)
	if a.detail(t, period, "xa")["closed_rating"] != "HTNV" || a.detail(t, period, "vc_tt")["closed_rating"] != "HTTNV" {
		t.Fatal("chốt không dùng DG09→DG05")
	}
	result := appraisalCall(t, f, "hcth", http.MethodGet, "/v1/hrm/staff/"+a.staff["xa"]+"/appraisal-results", "", nil, http.StatusOK)["items"].([]any)
	if len(result) != 1 || result[0].(map[string]any)["rating"] != "HTNV" {
		t.Fatal("kết quả quý chưa vào hồ sơ")
	}
	var ratings int
	err := db.WithModuleTenantTx(context.Background(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_hrm.annual_rating").Scan(&ratings)
	})
	if err != nil || ratings != 0 {
		t.Fatalf("kết quả ghi salary ledger: %d %v", ratings, err)
	}
	version, _ := a.detail(t, period, "xa")["version"].(json.Number).Int64()
	a.change(t, period, "xa", "hcth", "reopen-one", domain.AppraisalEntryChange{Action: "reopen", Version: version, Reason: "Điều chỉnh kết luận của riêng cán bộ"}, http.StatusCreated)
	save.Version = version + 1
	a.change(t, period, "xa", "ld_tt", "correct-one", save, http.StatusCreated)
	a.change(t, period, "xa", "ld_tt", "resend-one", domain.AppraisalEntryChange{Action: "send", Version: version + 2}, http.StatusCreated)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "reclose-one", app.AppraisalPeriodChange{Action: "close", Version: 3, AcknowledgeUnsigned: true}, http.StatusCreated)
	if a.detail(t, period, "vc_tt")["status"] != domain.AppraisalClosed {
		t.Fatal("mở một người mở người khác")
	}
	h := appraisalCall(t, f, "hcth", http.MethodGet, base+"/entries/"+a.staff["xa"]+"/history", "", nil, http.StatusOK)
	if len(h["items"].([]any)) < 8 {
		t.Fatal("mất lịch sử sửa/chốt/mở lại")
	}
	appraisalCall(t, f, "hcth", http.MethodGet, base+"/entries/"+a.staff["xa"]+"/history?before="+strings.Repeat("9", 100000), "", nil, http.StatusUnprocessableEntity)
}

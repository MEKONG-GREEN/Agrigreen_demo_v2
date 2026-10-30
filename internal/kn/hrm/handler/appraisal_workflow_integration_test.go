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
	// Cấp Xã thật luôn có OrgRef của lần gán; fixture hồ sơ cũ không cần vì bị chặn trước truy vấn.
	xa := f.who["xa"]
	k := *xa.KN
	k.OrgRef = k.UnitRef
	xa.KN = &k
	f.who["xa"] = xa
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	a := appraisalFixture{f: f, staff: map[string]string{}, input: app.AppraisalPeriodInput{Year: 2026, Quarter: 3, CutoffOn: "2026-09-30",
		SelfDueAt: "2026-10-05T16:59:59Z", ManagerDueAt: "2026-10-20T16:59:59Z", SummaryDueAt: "2026-10-25T16:59:59Z",
		Forms: map[string]uuid.UUID{}, Summaries: map[string]uuid.UUID{}}}
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
	for _, group := range []string{domain.AppraisalCivil, domain.AppraisalLeader, domain.AppraisalCommune} {
		form := domain.AppraisalForm{Code: "phieu_" + group, Name: "Phiếu đánh giá", Group: group,
			Bands: []domain.AppraisalBand{{Minimum: "81", Rating: "HTXSNV"}, {Minimum: "63", Rating: "HTTNV"}, {Minimum: "45", Rating: "HTNV"}, {Minimum: "0", Rating: "KHTNV"}}}
		for i := range 6 {
			form.Criteria = append(form.Criteria, domain.AppraisalCriterion{Code: fmt.Sprintf("tc_%d", i), Title: fmt.Sprintf("Tiêu chí %d", i+1), Maximum: "15"})
		}
		v := appraisalCall(t, f, "hcth", http.MethodPost, "/v1/hrm/appraisals/templates/form/"+form.Code, "form-"+group,
			app.AppraisalTemplateChange{Status: "published", Form: &form}, http.StatusCreated)
		a.input.Forms[group] = uuid.MustParse(v["id"].(string))
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

func TestHRMAppraisalWorkflowHTTP_W1126(t *testing.T) {
	a := newAppraisalFixture(t)
	f := a.f
	period := a.open(t, "open-q3-first")
	base := "/v1/hrm/appraisals/periods/" + period
	appraisalCall(t, f, "hcth", http.MethodPost, "/v1/hrm/appraisals/periods", "open-q3-duplicate", a.input, http.StatusConflict)
	if a.open(t, "open-q3-first") != period {
		t.Fatal("replay mở đợt tạo mã khác")
	}
	for _, tc := range []struct {
		who   string
		count int
	}{{"xa", 1}, {"vc_tt", 1}, {"ld_tt", 2}, {"hcth", 3}, {"admin", 3}} {
		v := appraisalCall(t, f, tc.who, http.MethodGet, base+"/entries", "", nil, http.StatusOK)
		if len(v["items"].([]any)) != tc.count {
			t.Fatalf("%s thấy sai số phiếu: %v", tc.who, v)
		}
	}
	appraisalCall(t, f, "ld_cn", http.MethodGet, base+"/entries/"+a.staff["xa"], "", nil, http.StatusNotFound)
	appraisalCall(t, f, "xa", http.MethodGet, base+"/entries/"+a.staff["vc_tt"], "", nil, http.StatusNotFound)
	appraisalCall(t, f, "other", http.MethodGet, base+"/entries", "", nil, http.StatusNotFound)
	for _, as := range []string{"tw", "viewonly", "realmonly"} {
		appraisalCall(t, f, as, http.MethodGet, base+"/entries", "", nil, http.StatusForbidden)
	}
	// Ma trận người dùng phải thắng quyền mặc định HCTH, không tự mở rộng nhờ có vai trung tâm.
	permission := func(scope string) {
		f.call("admin", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "user", "grantee": f.who["hcth2"].ActorRef.String(), "perms": map[string]any{"assessment": scope}}, http.StatusCreated)
	}
	permission("self")
	if xs := appraisalCall(t, f, "hcth2", http.MethodGet, base+"/entries", "", nil, http.StatusOK)["items"].([]any); len(xs) != 1 {
		t.Fatal("HCTH self thấy người khác")
	}
	appraisalCall(t, f, "hcth2", http.MethodGet, base+"/entries/"+a.staff["xa"], "", nil, http.StatusNotFound)
	permission("none")
	appraisalCall(t, f, "hcth2", http.MethodGet, base+"/entries", "", nil, http.StatusForbidden)
	permission("all")
	// Phiếu của đợt giữ phiên bản đã phát hành, dù HCTH đã sửa mẫu mới.
	form := appraisalCall(t, f, "hcth", http.MethodGet, base+"/entries/"+a.staff["xa"], "", nil, http.StatusOK)["form"].(map[string]any)
	var original domain.AppraisalForm
	raw, _ := json.Marshal(form["form"])
	_ = json.Unmarshal(raw, &original)
	original.Name = "Phiếu đã đổi sau ngày chốt"
	appraisalCall(t, f, "hcth", http.MethodPost, "/v1/hrm/appraisals/templates/form/"+original.Code, "form-updated-after-open", app.AppraisalTemplateChange{Status: "published", Version: 1, Form: &original}, http.StatusCreated)
	detail := appraisalCall(t, f, "xa", http.MethodGet, base+"/entries/"+a.staff["xa"], "", nil, http.StatusOK)
	if detail["form"].(map[string]any)["id"] != form["id"] || detail["item"].(map[string]any)["self_late"] != true {
		t.Fatal("mẫu không đóng băng hoặc thiếu cảnh báo muộn")
	}
	if len(detail["period"].(map[string]any)["forms"].(map[string]any)) != 0 {
		t.Fatal("lộ cấu hình nhóm khác cho người tự chấm")
	}
	for _, bad := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "15.01", "-1"} {
		answers := append([]domain.AppraisalAnswer(nil), a.answers...)
		answers[0].Points = bad
		a.change(t, period, "xa", "xa", "bad-entry-points", domain.AppraisalEntryChange{Action: "self_submit", Version: 1, Answers: answers, Reason: "Căn cứ nhiệm vụ được giao"}, http.StatusUnprocessableEntity)
	}
	self := domain.AppraisalEntryChange{Action: "self_submit", Version: 1, Answers: a.answers, Reason: "Căn cứ nhiệm vụ được giao"}
	first := a.change(t, period, "xa", "xa", "submit-xa-first", self, http.StatusCreated)
	if first["self_rating"] != "HTXSNV" || first["self"].(map[string]any)["total"] != "82" || first["self_late"] != true {
		t.Fatalf("điểm/gợi ý/muộn: %v", first)
	}
	if replay := a.change(t, period, "xa", "xa", "submit-xa-first", self, http.StatusCreated); replay["id"] != first["id"] {
		t.Fatal("replay thêm phiên bản")
	}
	a.change(t, period, "xa", "xa", "submit-stale-xa", self, http.StatusConflict)
	a.change(t, period, "vc_tt", "xa", "submit-another-person", self, http.StatusNotFound)
	// Quản lý không được dùng đường tự chấm để bỏ bước chứng từ của nhập thay.
	a.change(t, period, "vc_tt", "ld_tt", "manager-self-bypass", self, http.StatusForbidden)
	for _, who := range []string{"vc_tt", "hcth2"} {
		a.change(t, period, who, who, "submit-"+who, self, http.StatusCreated)
	}
	for _, who := range []string{"xa", "vc_tt", "hcth2"} {
		as := "ld_tt"
		if who == "hcth2" {
			as = "hcth"
		}
		a.change(t, period, who, as, "manager-"+who, domain.AppraisalEntryChange{Action: "manager_submit", Version: 2, Answers: a.answers, CollectiveRating: "HTTNV", Reason: "Căn cứ họp tập thể đề xuất"}, http.StatusCreated)
	}
	// Gửi đơn vị giữ cả hai bản chấm; MS06 chỉ HCTH được gửi.
	p := appraisalCall(t, f, "ld_tt", http.MethodPost, base, "send-unit-tt", app.AppraisalPeriodChange{Action: "send", Version: 1, Unit: "trong_trot"}, http.StatusCreated)
	if p["version"] != json.Number("2") {
		t.Fatal(p)
	}
	a.change(t, period, "hcth2", "hcth", "send-ms06", domain.AppraisalEntryChange{Action: "send", Version: 3}, http.StatusCreated)
	if item := a.detail(t, period, "xa"); len(item["manager"].(map[string]any)["answers"].([]any)) != 6 {
		t.Fatal("gửi đơn vị làm mất phiếu điểm")
	}
	a.change(t, period, "xa", "hcth", "official-result", domain.AppraisalEntryChange{Action: "official", Version: 4, OfficialRating: "HTNV", Reason: "Kết luận chính thức của Giám đốc"}, http.StatusCreated)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "close-no-warning-ack", app.AppraisalPeriodChange{Action: "close", Version: 2}, http.StatusUnprocessableEntity)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "close-q3", app.AppraisalPeriodChange{Action: "close", Version: 2, AcknowledgeUnsigned: true}, http.StatusCreated)
	if a.detail(t, period, "xa")["closed_rating"] != "HTNV" || a.detail(t, period, "vc_tt")["closed_rating"] != "HTTNV" {
		t.Fatal("chốt không dùng official→collective")
	}
	var ratings int
	err := db.WithModuleTenantTx(context.Background(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_hrm.annual_rating").Scan(&ratings)
	})
	if err != nil || ratings != 0 {
		t.Fatalf("kết luận quý tự biến thành kết luận năm: %d %v", ratings, err)
	}
	closed := a.detail(t, period, "xa")
	version, _ := closed["version"].(json.Number).Int64()
	a.change(t, period, "xa", "hcth", "reopen-one", domain.AppraisalEntryChange{Action: "reopen", Version: version, Reason: "Điều chỉnh kết luận của riêng cán bộ"}, http.StatusCreated)
	a.change(t, period, "xa", "ld_tt", "correct-one", domain.AppraisalEntryChange{Action: "manager_submit", Version: version + 1, Answers: a.answers, CollectiveRating: "HTTNV", Reason: "Căn cứ biên bản điều chỉnh mới"}, http.StatusCreated)
	a.change(t, period, "xa", "ld_tt", "resend-one", domain.AppraisalEntryChange{Action: "send", Version: version + 2}, http.StatusCreated)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "reclose-one", app.AppraisalPeriodChange{Action: "close", Version: 3, AcknowledgeUnsigned: true}, http.StatusCreated)
	if a.detail(t, period, "vc_tt")["status"] != domain.AppraisalClosed {
		t.Fatal("mở một người làm mở người khác")
	}
	h := appraisalCall(t, f, "xa", http.MethodGet, base+"/entries/"+a.staff["xa"]+"/history", "", nil, http.StatusOK)
	if len(h["items"].([]any)) < 8 {
		t.Fatal("mất lịch sử sửa/chốt/mở lại")
	}
	appraisalCall(t, f, "xa", http.MethodGet, base+"/entries/"+a.staff["xa"]+"/history?before="+strings.Repeat("9", 100000), "", nil, http.StatusUnprocessableEntity)
	appraisalCall(t, f, "hcth", http.MethodPost, base+"/entries/"+a.staff["xa"]+"/guest-link", "guest-unavailable", nil, http.StatusServiceUnavailable)
}

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
)

func appraisalRequest(f *recFixture, as, method, path, key string, body any) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("X-Test-As", as)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, r)
	return w
}

func appraisalCall(t *testing.T, f *recFixture, as, method, path, key string, body any, want int) map[string]any {
	t.Helper()
	w := appraisalRequest(f, as, method, path, key, body)
	if w.Code != want {
		t.Fatalf("%s %s %s: %d want%d: %s", as, method, path, w.Code, want, w.Body.String())
	}
	var v map[string]any
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHRMAppraisalTemplatesHTTP_W1126(t *testing.T) {
	f := newRecFixture(t, true)
	base := "/v1/hrm/appraisals/templates/summary"
	for _, who := range []string{"tw", "xa", "ld_tt", "viewonly", "realmonly"} {
		appraisalCall(t, f, who, http.MethodGet, base, "", nil, http.StatusForbidden)
	}
	appraisalCall(t, f, "", http.MethodGet, base, "", nil, http.StatusUnauthorized)
	summary := domain.DefaultAppraisalSummary("MS05")
	in := app.AppraisalTemplateChange{Status: "published", Version: 0, Summary: &summary}
	created := appraisalCall(t, f, "hcth", http.MethodPost, base+"/ms05", "appraisal-summary-first", in, http.StatusCreated)
	if created["version"] != json.Number("1") || created["status"] != "published" {
		t.Fatalf("%v", created)
	}
	originalID := created["id"]
	if xs := appraisalCall(t, f, "other", http.MethodGet, base, "", nil, http.StatusOK)["items"].([]any); len(xs) != 0 {
		t.Fatal("lộ mẫu tenant khác")
	}
	appraisalCall(t, f, "hcth", http.MethodPost, base+"/ms05", "", in, http.StatusUnprocessableEntity)
	summary.Title = "TIÊU ĐỀ MẪU MỚI {QUY}/{NAM}"
	in.Version = 1
	appraisalCall(t, f, "hcth", http.MethodPost, base+"/ms05", "appraisal-summary-second", in, http.StatusCreated)
	appraisalCall(t, f, "hcth", http.MethodPost, base+"/ms05", "appraisal-summary-stale", in, http.StatusConflict)
	original := domain.DefaultAppraisalSummary("MS05")
	replay := appraisalCall(t, f, "hcth", http.MethodPost, base+"/ms05", "appraisal-summary-first", app.AppraisalTemplateChange{Status: "published", Summary: &original}, http.StatusCreated)
	if replay["id"] != originalID || replay["version"] != json.Number("1") {
		t.Fatal("replay trả bản mới thay vì bản đầu")
	}
	appraisalCall(t, f, "hcth2", http.MethodPost, base+"/ms05", "appraisal-summary-first", app.AppraisalTemplateChange{Status: "published", Summary: &original}, http.StatusConflict)
	appraisalCall(t, f, "hcth", http.MethodPost, base+"/ms05", "appraisal-summary-first", in, http.StatusConflict)
	history := appraisalCall(t, f, "hcth", http.MethodGet, base+"/ms05/history", "", nil, http.StatusOK)["items"].([]any)
	if len(history) != 2 || history[0].(map[string]any)["summary"].(map[string]any)["title"] == summary.Title {
		t.Fatal("sửa phiên bản làm đổi lịch sử")
	}
	// Hai người lưu cùng phiên bản: chỉ một người thành công, không mất cập nhật.
	in.Version = 2
	responses := make(chan int, 2)
	for _, key := range []string{"appraisal-concurrent-a", "appraisal-concurrent-b"} {
		go func(key string) {
			responses <- appraisalRequest(f, "hcth", http.MethodPost, base+"/ms05", key, in).Code
		}(key)
	}
	a, b := <-responses, <-responses
	if !((a == http.StatusCreated && b == http.StatusConflict) || (b == http.StatusCreated && a == http.StatusConflict)) {
		t.Fatalf("concurrent=%d,%d", a, b)
	}
	// Đầu vào số sai bị chặn trước ghi sổ/Decimal, kể cả gọi API trực tiếp.
	form := domain.AppraisalForm{Code: "phieu_xa", Name: "Phiếu cán bộ xã", Group: domain.AppraisalCommune,
		Criteria: []domain.AppraisalCriterion{{Code: "tc_1", Title: "Kết quả nhiệm vụ", Maximum: "90"}},
		Bands:    []domain.AppraisalBand{{Minimum: "81", Rating: "HTXSNV"}, {Minimum: "63", Rating: "HTTNV"}, {Minimum: "45", Rating: "HTNV"}, {Minimum: "0", Rating: "KHTNV"}}}
	for _, bad := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "-1", "0", "1.001", "1000001"} {
		form.Criteria[0].Maximum = bad
		p := appraisalCall(t, f, "hcth", http.MethodPost, "/v1/hrm/appraisals/templates/form/phieu_xa", "appraisal-invalid-points", app.AppraisalTemplateChange{Status: "published", Form: &form}, http.StatusUnprocessableEntity)
		if p["code"] != "validation_failed" {
			t.Fatalf("problem=%v", p)
		}
	}
	form.Criteria[0].Maximum = "90"
	appraisalCall(t, f, "hcth", http.MethodPost, "/v1/hrm/appraisals/templates/form/phieu_xa", "appraisal-form-first", app.AppraisalTemplateChange{Status: "published", Form: &form}, http.StatusCreated)
	appraisalCall(t, f, "hcth", http.MethodPost, "/v1/hrm/appraisals/templates/form/phieu_xa", "appraisal-summary-first", app.AppraisalTemplateChange{Status: "published", Form: &form}, http.StatusConflict)
	for _, q := range []string{"UPDATE pf_hrm.appraisal_event SET action='draft'", "DELETE FROM pf_hrm.appraisal_event"} {
		err := db.WithModuleTenantTx(context.Background(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error { _, e := tx.Exec(context.Background(), q); return e })
		if err == nil {
			t.Fatal("sổ cho sửa/xóa")
		}
	}
}

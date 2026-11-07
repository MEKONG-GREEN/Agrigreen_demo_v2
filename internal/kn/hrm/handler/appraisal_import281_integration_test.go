package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/xuri/excelize/v2"
)

func appraisalImportRequest281(t *testing.T, a appraisalFixture, as, period, key, version string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("version", version); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "de-xuat.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/hrm/appraisals/periods/"+period+"/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Test-As", as)
	req.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	a.f.r.ServeHTTP(response, req)
	return response
}
func appraisalImportExcel281(t *testing.T, header []string, rows [][]string) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := tabular.WriteXLSX(&data, "MS05", header, rows); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
func TestHRMAppraisalImportAtomicScopeAndRetry281(t *testing.T) {
	a := newAppraisalFixture(t)
	f := a.f
	f.svc.DB = db.ForModule(f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), app.DBModule)
	period := a.open(t, "281-import-open")
	base := "/v1/hrm/appraisals/periods/" + period
	template := appraisalRequest(f, "ld_tt", http.MethodGet, base+"/import-template/MS05", "", nil)
	if template.Code != http.StatusOK {
		t.Fatalf("template=%d %s", template.Code, template.Body.String())
	}
	table, err := tabular.ReadBytes(template.Body.Bytes(), tabular.Limits{MaxCols: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Header) != 8 || len(table.Rows) != 2 {
		t.Fatal("mẫu nhập không có đúng tám cột/người trong HS37")
	}
	for _, tc := range []struct {
		as, kind string
		want     int
	}{{"xa", "MS05", 403}, {"vc_tt", "MS05", 403}, {"viewonly", "MS05", 403}, {"ld_cn", "MS05", 404}, {"ld_tt", "MS06", 404}, {"other", "MS05", 404}} {
		response := appraisalRequest(f, tc.as, http.MethodGet, base+"/import-template/"+tc.kind, "", nil)
		if response.Code != tc.want {
			t.Fatalf("%s template%d want%d", tc.as, response.Code, tc.want)
		}
	}
	rows := [][]string{}
	for _, row := range table.Rows {
		cells := append([]string(nil), row.Cells...)
		for len(cells) < 8 {
			cells = append(cells, "")
		}
		cells[3], cells[4] = "HTTNV", "HTNV"
		rows = append(rows, cells)
	}
	rows[0][3] = "HTXSNV"
	data := appraisalImportExcel281(t, table.Header, rows)
	beforeCount, beforeHash := appraisalEntryFingerprint(t, a)
	response := appraisalImportRequest281(t, a, "ld_tt", period, "281-import-no-basis", "1", data)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu căn cứ vẫn Lưu: %d %s", response.Code, response.Body.String())
	}
	rows[0][5] = "x" // BRD yêu cầu có căn cứ; không tự đặt tối thiểu 10 ký tự cho DG06.
	rows[1][4] = ""
	response = appraisalImportRequest281(t, a, "ld_tt", period, "281-import-missing-second", "1", appraisalImportExcel281(t, table.Header, rows))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("dòng sau thiếu mức vẫn lưu: %d", response.Code)
	}
	if n, h := appraisalEntryFingerprint(t, a); n != beforeCount || h != beforeHash {
		t.Fatal("nhập lỗi đã ghi một phần")
	}
	rows[1][4] = "HTNV"
	valid := appraisalImportExcel281(t, table.Header, rows)
	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "0"} {
		r := appraisalImportRequest281(t, a, "ld_tt", period, "281-import-version-invalid", raw, valid)
		if r.Code != 422 {
			t.Fatalf("version không bị chặn trước parse: %d", r.Code)
		}
	}
	for _, who := range []string{"xa", "vc_tt", "tw", "viewonly"} {
		r := appraisalImportRequest281(t, a, who, period, "281-import-no-permission", "1", valid)
		if r.Code != 403 {
			t.Fatalf("%s còn nhập: %d", who, r.Code)
		}
	}
	for _, who := range []string{"other", "ld_cn"} {
		r := appraisalImportRequest281(t, a, who, period, "281-import-wrong-scope", "1", valid)
		if r.Code != 404 {
			t.Fatalf("%s scope=%d", who, r.Code)
		}
	}
	response = appraisalImportRequest281(t, a, "ld_tt", period, "281-import-valid", "1", valid)
	if response.Code != 201 {
		t.Fatalf("import=%d %s", response.Code, response.Body.String())
	}
	var result app.AppraisalImportResult
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Count != 2 || result.Version != 2 {
		t.Fatalf("result=%+v %v", result, err)
	}
	replay := appraisalImportRequest281(t, a, "ld_tt", period, "281-import-valid", "1", valid)
	if replay.Code != 201 || !bytes.Equal(response.Body.Bytes(), replay.Body.Bytes()) {
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
	rows[0][5] = "Căn cứ thay đổi"
	changed := appraisalImportRequest281(t, a, "ld_tt", period, "281-import-valid", "1", appraisalImportExcel281(t, table.Header, rows))
	if changed.Code != 409 {
		t.Fatal("khóa cũ nhận nội dung mới")
	}
	stale := appraisalImportRequest281(t, a, "ld_tt", period, "281-import-stale", "1", valid)
	if stale.Code != 409 {
		t.Fatalf("stale=%d", stale.Code)
	}
	appraisalCall(t, f, "ld_tt", http.MethodPost, base, "281-import-send-unit", app.AppraisalPeriodChange{Action: "send", Version: 2, Unit: "trong_trot"}, 201)
	// Mẫu05 đã đủ nhưng Mẫu06 còn người chưa nhập: FN18 chặn xuất toàn đợt.
	appraisalCall(t, f, "hcth", http.MethodPost, base+"/exports", "281-import-export-incomplete", app.AppraisalExportInput{Kind: "MS05", Format: "xlsx", Version: 3}, 422)
	ms06 := appraisalRequest(f, "hcth", http.MethodGet, base+"/import-template/MS06", "", nil)
	six, err := tabular.ReadBytes(ms06.Body.Bytes(), tabular.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	row := append([]string(nil), six.Rows[0].Cells...)
	for len(row) < 8 {
		row = append(row, "")
	}
	row[3], row[4] = "HTTNV", "HTNV"
	if r := appraisalImportRequest281(t, a, "hcth", period, "281-import-ms06", "3", appraisalImportExcel281(t, six.Header, [][]string{row})); r.Code != 201 {
		t.Fatalf("MS06 import=%d %s", r.Code, r.Body.String())
	}
	a.change(t, period, "hcth2", "hcth", "281-import-send-ms06", domain.AppraisalEntryChange{Action: "send", Version: 2}, 201)
	appraisalCall(t, f, "hcth", http.MethodPost, base+"/exports", "281-import-export-complete", app.AppraisalExportInput{Kind: "MS05", Format: "xlsx", Version: 4}, 201)
}
func TestHRMAppraisalImportRejectsFormulaAndWrongFormat281(t *testing.T) {
	a := newAppraisalFixture(t)
	period := a.open(t, "281-import-file-open")
	for _, data := range [][]byte{[]byte("STT,Họ và tên\n1,Không phải Excel"), []byte("PKnot-a-workbook")} {
		if r := appraisalImportRequest281(t, a, "ld_tt", period, "281-import-invalid-format", "1", data); r.Code != 422 {
			t.Fatalf("badformat=%d", r.Code)
		}
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetCellFormula("Sheet1", "A1", "1+1"); err != nil {
		t.Fatal(err)
	}
	data, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if r := appraisalImportRequest281(t, a, "ld_tt", period, "281-import-formula", "1", data.Bytes()); r.Code != 422 {
		t.Fatalf("formula=%d", r.Code)
	}
}

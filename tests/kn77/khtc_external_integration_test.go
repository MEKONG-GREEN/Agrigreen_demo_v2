package kn77_test

// W11.67 — Số liệu nguồn ngoài KHTC (BRD KHTC v2.2 FN-TI-02.11, BR-DL-19, BR-DL-04) trên PostgreSQL thật qua HTTP:
// nhập tay có nguồn + minh chứng, "Nạp Excel tổng hợp" hai bước (4 dòng, 1 thiếu nguồn → ghi 3, lỗi 1), sửa có lý do
// (lịch sử cũ → mới), ô Nhập ngoài xuất hiện trong bản soạn và Excel Mẫu 1, đổi sau khi xuất → "Nguồn đã thay đổi",
// kỳ hủy → chỉ xem, quyền.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

const extBase = "/v1/extension/khtc/external"

type extFigure struct {
	EntryID     string `json:"entry_id"`
	Version     int64  `json:"version"`
	PeriodID    string `json:"period_id"`
	IndicatorID string `json:"indicator_id"`
	Name        string `json:"name"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	Agency      string `json:"agency"`
	Document    string `json:"document"`
	SourceDate  string `json:"source_date"`
	UsedIn      string `json:"used_in"`
	Editable    bool   `json:"editable"`
	Evidence    []struct {
		ID       string `json:"id"`
		FileName string `json:"file_name"`
	} `json:"evidence"`
}

type extImport struct {
	ImportID string `json:"import_id"`
	Status   string `json:"status"`
	Recorded int    `json:"recorded"`
	Skipped  int    `json:"skipped"`
	Result   struct {
		Valid      int `json:"valid"`
		Overwrites int `json:"overwrites"`
		Errors     int `json:"errors"`
		Rows       []struct {
			Line   int      `json:"line"`
			Status string   `json:"status"`
			Errors []string `json:"errors"`
		} `json:"rows"`
	} `json:"result"`
}

type extReport struct {
	Version       int64 `json:"version"`
	SourceChanged bool  `json:"source_changed"`
	Content       struct {
		Data struct {
			InputHash string      `json:"input_hash"`
			External  []extFigure `json:"external"`
		} `json:"data"`
	} `json:"content"`
}

// raw gửi thân request là tệp (không JSON) như web gửi tệp nạp/tệp minh chứng.
func (f *reportFx) raw(path string, body []byte, ct string) (int, []byte) {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", ct)
	kj, _ := json.Marshal(authz.KNContext{Role: authz.KNTinh, Department: khtcDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}})
	req.Header.Set("X-Test-KN", string(kj))
	req.Header.Set("X-Test-Actor", f.khtcUser.String())
	req.Header.Set(idempotency.Header, "t-"+ids.New().String())
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func (f *reportFx) xcall(method, path string, body any) (int, []byte) {
	return f.do(method, extBase+path, authz.KNTinh, khtcDept, f.khtcUser, "", body)
}

func (f *reportFx) indicator(name, unit string) string {
	var out struct{ ID string }
	c, b := f.xcall(http.MethodPost, "/indicators", map[string]any{"name": name, "unit": unit, "group": "Sản xuất"})
	f.ok(c, b, &out)
	return out.ID
}

func (f *reportFx) figures(pid string) map[string]extFigure {
	var out struct{ Items []extFigure }
	c, b := f.xcall(http.MethodGet, "/figures?year=2026&period_id="+pid, nil)
	f.ok(c, b, &out)
	m := map[string]extFigure{}
	for _, x := range out.Items {
		m[x.Name] = x
	}
	return m
}

func TestKH_FN_TI_02_11_ImportWithSource(t *testing.T) {
	f := newReportFx(t)
	store, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.ext.UseObjectStore(store)
	p := f.open(39, dTT, dTTTT)

	shrimp := f.indicator("Diện tích nuôi tôm nước lợ", "ha")
	for _, x := range [][2]string{{"Diện tích lúa Thu Đông", "ha"}, {"Số hộ nuôi tôm càng xanh", "hộ"}, {"Số HTX tham gia chuỗi lúa CLC", "HTX"}, {"Giá trị xuất khẩu gạo", "triệu USD"}} {
		f.indicator(x[0], x[1])
	}
	c, b := f.xcall(http.MethodPost, "/indicators", map[string]any{"name": "  diện tích NUÔI tôm nước lợ ", "unit": "ha"})
	f.fail(c, b, "extension.khtc_ext_indicator_duplicate")

	// Minh chứng ở kho đối tượng; nội dung phải khớp phần mở rộng.
	pdf := []byte("%PDF-1.4\n% công văn Chi cục Thủy sản\n%%EOF\n")
	var ev struct{ ID, FileName string }
	c, b = f.raw(extBase+"/evidence?file_name="+url.QueryEscape("CV-ChiCuc.pdf"), pdf, "application/pdf")
	f.ok(c, b, &ev)
	c, b = f.raw(extBase+"/evidence?file_name=gia.pdf", []byte("không phải pdf"), "application/pdf")
	f.fail(c, b, "extension.khtc_ext_evidence_invalid")

	// B2: nhập tay — thiếu nguồn → không lưu; đủ nguồn + minh chứng → lưu.
	c, b = f.xcall(http.MethodPost, "/figures", map[string]any{"period_id": p.ID, "indicator_id": shrimp, "value": "26100"})
	f.fail(c, b, "extension.khtc_ext_invalid")
	var first extFigure
	c, b = f.xcall(http.MethodPost, "/figures", map[string]any{"period_id": p.ID, "indicator_id": shrimp, "value": "26100", "agency": "Chi cục Thủy sản",
		"document": "Email 29/9", "source_date": "2026-09-29", "evidence_ids": []string{ev.ID}})
	f.ok(c, b, &first)
	if first.Value != "26100" || first.Unit != "ha" || len(first.Evidence) != 1 || first.Evidence[0].FileName != "CV-ChiCuc.pdf" || first.UsedIn == "" || !first.Editable {
		t.Fatalf("số liệu nhập tay: %+v", first)
	}

	// B3: nạp Excel hai bước — bước 1 chỉ kiểm, chưa ghi.
	csv := "Chỉ tiêu,Kỳ,Giá trị,ĐVT,Cơ quan cung cấp,Văn bản / kênh,Ngày (YYYY-MM-DD)\n" +
		"Diện tích lúa Thu Đông,Tuần 39/2026,12450,ha,Cục Thống kê,File tổng hợp tuần 39,2026-09-29\n" +
		"Số hộ nuôi tôm càng xanh,Tuần 39/2026,812,hộ,Chi cục Thủy sản,Email,29/09/2026\n" +
		"Số HTX tham gia chuỗi lúa CLC,Tuần 39/2026,34,HTX,Sở NN&MT,CV 512/SNNMT,2026-09-29\n" +
		"Giá trị xuất khẩu gạo,Tuần 39/2026,210,triệu USD,,,\n"
	var staged extImport
	c, b = f.raw(extBase+"/imports?year=2026&file_name=tuan39.csv", []byte(csv), "text/csv")
	f.ok(c, b, &staged)
	if staged.Status != "staged" || staged.Result.Valid != 3 || staged.Result.Errors != 1 || staged.Result.Rows[3].Line != 5 || staged.Result.Rows[3].Status != "error" {
		t.Fatalf("kiểm tệp tuần 39: %+v", staged)
	}
	if got := f.figures(p.ID); len(got) != 1 {
		t.Fatalf("bước kiểm đã ghi số liệu: %+v", got)
	}
	var done extImport
	c, b = f.xcall(http.MethodPost, "/imports/"+staged.ImportID+"/commit", map[string]any{})
	f.ok(c, b, &done)
	if done.Status != "committed" || done.Recorded != 3 {
		t.Fatalf("hoàn tất: %+v", done)
	}
	c, b = f.xcall(http.MethodPost, "/imports/"+staged.ImportID+"/commit", map[string]any{})
	f.fail(c, b, "extension.khtc_ext_import_committed")
	figs := f.figures(p.ID)
	if len(figs) != 4 || figs["Diện tích lúa Thu Đông"].Value != "12450" || figs["Số hộ nuôi tôm càng xanh"].SourceDate != "2026-09-29" {
		t.Fatalf("sau nạp: %+v", figs)
	}
	if _, ok := figs["Giá trị xuất khẩu gạo"]; ok {
		t.Fatal("dòng thiếu nguồn đã được ghi")
	}

	// Nạp lại trùng chỉ tiêu và kỳ → "ghi đè"; không xác nhận → bỏ qua, giữ giá trị cũ.
	c, b = f.raw(extBase+"/imports?year=2026&file_name=lai.csv", []byte("Chỉ tiêu,Kỳ,Giá trị,Cơ quan cung cấp,Văn bản / kênh,Ngày\nDiện tích lúa Thu Đông,2026-W39,12500,Cục Thống kê,File bổ sung,2026-09-30\n"), "text/csv")
	f.ok(c, b, &staged)
	if staged.Result.Overwrites != 1 {
		t.Fatalf("phải hỏi ghi đè: %+v", staged)
	}
	c, b = f.xcall(http.MethodPost, "/imports/"+staged.ImportID+"/commit", map[string]any{"overwrite": false})
	f.ok(c, b, &done)
	if done.Recorded != 0 || done.Skipped != 1 || f.figures(p.ID)["Diện tích lúa Thu Đông"].Value != "12450" {
		t.Fatalf("không ghi đè: %+v", done)
	}

	// Màn soạn Mẫu 1: ô Nhập ngoài có giá trị và nguồn; Excel có khối nguồn ngoài.
	d := f.refreshExt(p.ID, 0)
	if len(d.Content.Data.External) != 4 {
		t.Fatalf("Mẫu 1 thiếu ô Nhập ngoài: %+v", d.Content.Data.External)
	}
	var shrimpCell extFigure
	for _, x := range d.Content.Data.External {
		if x.Name == "Diện tích nuôi tôm nước lợ" {
			shrimpCell = x
		}
	}
	if shrimpCell.Value != "26100" || shrimpCell.Agency != "Chi cục Thủy sản" || shrimpCell.Document != "Email 29/9" || len(shrimpCell.Evidence) != 1 {
		t.Fatalf("Nguồn dữ liệu của ô Nhập ngoài: %+v", shrimpCell)
	}
	x := f.export(p.ID, d.Version, true)
	body := f.download(p.ID, x.ID)
	if got := externalCell(t, body, "Diện tích lúa Thu Đông"); got[1] != "12450" || got[3] != "Cục Thống kê" || got[4] != "File tổng hợp tuần 39" {
		t.Fatalf("Excel Mẫu 1 ô Nhập ngoài: %v", got)
	}
	if f.readExt(p.ID).SourceChanged {
		t.Fatal("chưa đổi nguồn ngoài")
	}

	// B4: sửa — trùng mà không xác nhận → hỏi ghi đè; không lý do → lỗi; có lý do → lịch sử cũ → mới.
	c, b = f.xcall(http.MethodPost, "/figures", map[string]any{"period_id": p.ID, "indicator_id": shrimp, "value": "26400", "agency": "Chi cục Thủy sản",
		"document": "Email 30/9", "source_date": "2026-09-30"})
	f.fail(c, b, "extension.khtc_ext_duplicate")
	c, b = f.xcall(http.MethodPost, "/figures", map[string]any{"period_id": p.ID, "indicator_id": shrimp, "value": "26400", "agency": "Chi cục Thủy sản",
		"document": "Email 30/9", "source_date": "2026-09-30", "base_version": first.Version})
	f.fail(c, b, "extension.khtc_ext_invalid")
	c, b = f.xcall(http.MethodPost, "/figures", map[string]any{"period_id": p.ID, "indicator_id": shrimp, "value": "26400", "agency": "Chi cục Thủy sản",
		"document": "Email 30/9", "source_date": "2026-09-30", "base_version": first.Version + 5, "reason": "Chi cục cập nhật số liệu"})
	f.fail(c, b, "extension.khtc_ext_conflict")
	var second extFigure
	c, b = f.xcall(http.MethodPost, "/figures", map[string]any{"period_id": p.ID, "indicator_id": shrimp, "value": "26400", "agency": "Chi cục Thủy sản",
		"document": "Email 30/9", "source_date": "2026-09-30", "base_version": first.Version, "reason": "Chi cục cập nhật số liệu", "evidence_ids": []string{ev.ID}})
	f.ok(c, b, &second)
	if second.Value != "26400" || second.Version != first.Version+1 {
		t.Fatalf("sửa: %+v", second)
	}
	var hist struct {
		Items []struct {
			IndicatorName string  `json:"indicator_name"`
			Old           *string `json:"old_value"`
			New           *string `json:"new_value"`
			Reason        string  `json:"reason"`
		}
	}
	c, b = f.xcall(http.MethodGet, "/history?year=2026", nil)
	f.ok(c, b, &hist)
	if len(hist.Items) == 0 || hist.Items[0].Old == nil || *hist.Items[0].Old != "26100" || *hist.Items[0].New != "26400" || hist.Items[0].Reason != "Chi cục cập nhật số liệu" {
		t.Fatalf("lịch sử cũ → mới: %+v", hist.Items)
	}

	// BR-DL-04: đổi sau khi xuất → "Nguồn đã thay đổi"; tệp đã xuất giữ nguyên; "Cập nhật số liệu" lấy giá trị mới.
	r := f.readExt(p.ID)
	if !r.SourceChanged {
		t.Fatal("đổi số liệu nguồn ngoài sau khi xuất phải cảnh báo Nguồn đã thay đổi")
	}
	if again := f.download(p.ID, x.ID); !bytes.Equal(again, body) {
		t.Fatal("tệp đã xuất bị dựng lại")
	}
	d = f.refreshExt(p.ID, r.Version)
	for _, x := range d.Content.Data.External {
		if x.Name == "Diện tích nuôi tôm nước lợ" && x.Value != "26400" {
			t.Fatalf("Cập nhật số liệu chưa lấy giá trị mới: %+v", x)
		}
	}
	if f.readExt(p.ID).SourceChanged {
		t.Fatal("sau cập nhật không còn nguồn đổi")
	}

	// Rút số liệu nhập nhầm (bản ghi bù) → ô biến mất sau Cập nhật số liệu.
	c, b = f.xcall(http.MethodPost, "/figures/withdraw", map[string]any{"period_id": p.ID, "indicator_id": figs["Số HTX tham gia chuỗi lúa CLC"].IndicatorID,
		"base_version": figs["Số HTX tham gia chuỗi lúa CLC"].Version, "reason": "Nhập nhầm kỳ báo cáo"})
	if c != http.StatusNoContent {
		t.Fatalf("rút: %d %s", c, b)
	}
	if got := f.figures(p.ID); len(got) != 3 {
		t.Fatalf("sau rút: %+v", got)
	}

	// Tệp minh chứng tải lại đúng byte.
	c, b = f.xcall(http.MethodGet, "/evidence/"+ev.ID, nil)
	if c != http.StatusOK || !bytes.Equal(b, pdf) {
		t.Fatalf("tải minh chứng: %d", c)
	}

	// Quyền: phòng khác không xem; Admin chỉ xem; tenant khác không thấy.
	c, b = f.do(http.MethodGet, extBase+"/figures?year=2026", authz.KNTinh, dTT, uuid.Nil, "", nil)
	if c != http.StatusForbidden {
		t.Fatalf("phòng khác: %d %s", c, b)
	}
	c, b = f.do(http.MethodGet, extBase+"/figures?year=2026", authz.KNAdmin, "", uuid.Nil, "", nil)
	f.ok(c, b, nil)
	c, b = f.do(http.MethodPost, extBase+"/figures", authz.KNAdmin, "", uuid.Nil, "", map[string]any{"period_id": p.ID, "indicator_id": shrimp, "value": "1"})
	if c != http.StatusForbidden {
		t.Fatalf("Admin ghi: %d %s", c, b)
	}

	// BR-DL-19: kỳ đã hủy/đóng → chỉ xem (trigger CSDL chặn cả khi bỏ qua app).
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ để kiểm tra chỉ xem"})
	f.ok(c, b, nil)
	c, b = f.xcall(http.MethodPost, "/figures", map[string]any{"period_id": p.ID, "indicator_id": shrimp, "value": "1", "agency": "a", "document": "b",
		"source_date": "2026-09-30", "base_version": second.Version, "reason": "Thử sửa sau khi hủy kỳ"})
	f.fail(c, b, "extension.khtc_ext_period_closed")
	if got := f.figures(p.ID)["Diện tích nuôi tôm nước lợ"]; got.Editable {
		t.Fatal("kỳ đã hủy vẫn cho sửa")
	}
}

func (f *reportFx) readExt(pid string) extReport {
	var d extReport
	c, b := f.call(http.MethodGet, pid, "", nil)
	f.ok(c, b, &d)
	return d
}

func (f *reportFx) refreshExt(pid string, base int64) extReport {
	var d extReport
	c, b := f.call(http.MethodPost, pid, "/refresh", map[string]any{"base_version": base})
	f.ok(c, b, &d)
	return d
}

// externalCell trả dòng của chỉ tiêu trong khối "Số liệu nguồn ngoài" của trang Báo cáo.
func externalCell(t *testing.T, body []byte, name string) []string {
	t.Helper()
	file, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	rows, err := file.GetRows("Báo cáo")
	if err != nil {
		t.Fatal(err)
	}
	in := false
	for _, row := range rows {
		if len(row) > 0 && strings.HasPrefix(row[0], "Số liệu nguồn ngoài (Nhập ngoài)") {
			in = true
			continue
		}
		if in && len(row) > 4 && row[0] == name {
			return row
		}
	}
	t.Fatalf("Excel thiếu ô Nhập ngoài %s", name)
	return nil
}

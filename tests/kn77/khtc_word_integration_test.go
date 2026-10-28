package kn77_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type wordRun struct {
	ID            string `json:"id"`
	Seq           int64  `json:"seq"`
	Template      string `json:"template"`
	DocNumber     string `json:"doc_number"`
	DocNumberAuto bool   `json:"doc_number_auto"`
	FileName      string `json:"file_name"`
	BodyHash      string `json:"body_hash"`
}

func withExporter(t *testing.T, f *reportFx) {
	t.Helper()
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	ex, err := export.New(pool, f.clk, export.Deps{Access: exportport.AccessFunc(func(context.Context) bool { return false }),
		Templates: []exportport.TemplateRegistration{extension.ExportTemplates()}})
	if err != nil {
		t.Fatal(err)
	}
	f.ext.UseExporter(ex.Exporter())
}

func (f *reportFx) word(pid, exportID, idem string) wordRun {
	f.t.Helper()
	var w wordRun
	c, b := f.do(http.MethodPost, reportPath(pid)+"/exports/"+exportID+"/word", authz.KNTinh, khtcDept, f.khtcUser, idem, map[string]any{})
	f.ok(c, b, &w)
	return w
}

// TestKH_W10_17_WordFromFrozenExcel: Word dựng từ nội dung đóng băng của bản Excel qua pf/export — số {n}/BC-TTKN tự cấp
// khi ô số trống (một số mỗi báo cáo, không hở với bản Tạm tính), dòng "An Giang, ngày …" khi ngày trống, đọc lại đúng số
// liệu, xuất lại cùng byte, tải lại qua điểm kiểm soát xuất, chỉ cán bộ phụ trách tạo, vẫn xuất được sau Bản cuối.
func TestKH_W10_17_WordFromFrozenExcel(t *testing.T) {
	f := newReportFx(t)
	withExporter(t, f)
	// Kỳ Tạm tính trước: không được cấp số.
	pv := f.open(40, dTT, dTTTT)
	dv := f.refresh(pv.ID, 0)
	xv := f.export(pv.ID, dv.Version, true)
	wv := f.word(pv.ID, xv.ID, "")
	if wv.DocNumber != "" || wv.DocNumberAuto {
		t.Fatalf("bản Tạm tính không cấp số: %+v", wv)
	}

	p := f.open(41, dTT, dTTTT)
	initial := f.refresh(p.ID, 0)
	f.submit(p.ID, "10")
	d := f.refresh(p.ID, initial.Version)
	c, b := f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version,
		"admin": map[string]any{"report_date": "", "document_no": "", "signer": "Nguyễn Văn A", "signer_title": "GIÁM ĐỐC"}, "texts": map[string]string{"B": "Đánh giá đã biên tập"}})
	f.ok(c, b, &d)
	x := f.export(p.ID, d.Version, false)
	c, b = f.do(http.MethodPost, reportPath(p.ID)+"/exports/"+x.ID+"/word", authz.KNTinh, khtcDept, ids.New(), "", map[string]any{})
	if c != http.StatusForbidden {
		t.Fatalf("người khác tạo Word: %d %s", c, b)
	}
	w1 := f.word(p.ID, x.ID, "")
	if w1.DocNumber != "1/BC-TTKN" || !w1.DocNumberAuto || w1.Seq != 1 || w1.Template != "khtc.mau_1" || !strings.HasSuffix(w1.FileName, "-v1.docx") {
		t.Fatalf("Word đầu: %+v", w1)
	}
	checkpoint := &reportMeter{}
	f.ext.UseEgress(checkpoint, meter.ModeEnforce, nil)
	c, body := f.call(http.MethodGet, p.ID, "/words/"+w1.ID+"/download", nil)
	f.ok(c, body, nil)
	if len(checkpoint.events) != 1 || checkpoint.events[0].Bytes != int64(len(body)) || checkpoint.events[0].Product != "kn_khtc_report" {
		t.Fatalf("tải Word bỏ qua điểm kiểm soát: %+v", checkpoint.events)
	}
	content, err := docx.Extract(body)
	if err != nil {
		t.Fatal(err)
	}
	text := content.Text()
	for _, want := range []string{"Số: 1/BC-TTKN", "An Giang, ngày 14 tháng 10 năm 2026", "Đánh giá đã biên tập", "Nguyễn Văn A", "GIÁM ĐỐC", "bản xuất Excel phiên bản 1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("thiếu %q:\n%s", want, text)
		}
	}
	found := false
	for _, tb := range content.Tables {
		for _, row := range tb {
			if len(row) == 8 && row[1] == "Mô hình lúa chất lượng cao" {
				found = row[4] == "10" && row[3] == "100"
			}
		}
	}
	if !found {
		t.Fatalf("Word đọc lại không có dòng hạng mục đúng số:\n%v", content.Tables)
	}
	// Xuất lại cùng bản Excel: lần thứ hai, cùng số, cùng byte (tất định).
	w2 := f.word(p.ID, x.ID, "")
	c, again := f.call(http.MethodGet, p.ID, "/words/"+w2.ID+"/download", nil)
	f.ok(c, again, nil)
	if w2.Seq != 2 || w2.DocNumber != "1/BC-TTKN" || w2.BodyHash != w1.BodyHash || !bytes.Equal(again, body) {
		t.Fatalf("xuất lại: %+v", w2)
	}
	var hist struct {
		Items []wordRun `json:"items"`
	}
	c, b = f.call(http.MethodGet, p.ID, "/words", nil)
	f.ok(c, b, &hist)
	if len(hist.Items) != 2 || hist.Items[0].ID != w2.ID {
		t.Fatalf("lịch sử Word: %+v", hist.Items)
	}
	// Bản cuối khóa Excel mới nhưng Word của bản đã chốt vẫn xuất được, dùng lại số.
	c, b = f.call(http.MethodPost, p.ID, "/exports/"+x.ID+"/final", map[string]any{})
	f.ok(c, b, nil)
	if w3 := f.word(p.ID, x.ID, ""); w3.DocNumber != "1/BC-TTKN" || w3.Seq != 3 {
		t.Fatalf("Word sau Bản cuối: %+v", w3)
	}
	// Báo cáo khác: số kế tiếp, không hở dù bản Tạm tính đã xuất Word.
	p2 := f.open(42, dTT, dTTTT)
	i2 := f.refresh(p2.ID, 0)
	f.submit(p2.ID, "5")
	d2 := f.refresh(p2.ID, i2.Version)
	x2 := f.export(p2.ID, d2.Version, false)
	if w := f.word(p2.ID, x2.ID, ""); w.DocNumber != "2/BC-TTKN" {
		t.Fatalf("số kế tiếp: %+v", w)
	}
	// Số nhập tay ở ô hành chính được dùng nguyên văn.
	c, b = f.call(http.MethodPost, p2.ID, "/draft", map[string]any{"base_version": d2.Version, "admin": map[string]any{"document_no": "77"}})
	f.ok(c, b, &d2)
	x3 := f.export(p2.ID, d2.Version, false)
	if w := f.word(p2.ID, x3.ID, ""); w.DocNumber != "77/BC-TTKN" || w.DocNumberAuto {
		t.Fatalf("số tay: %+v", w)
	}
	c, b = f.call(http.MethodGet, p.ID, "/words/"+ids.New().String()+"/download", nil)
	f.fail(c, b, "export.run_not_found")
}

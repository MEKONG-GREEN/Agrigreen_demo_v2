package kn77_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	golden "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/golden/khtc"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

type reportTestRow struct {
	Ref      uuid.UUID `json:"ref"`
	InPeriod string    `json:"in_period"`
	Missing  bool      `json:"missing"`
	Planned  *string   `json:"planned"`
}

type reportDetail struct {
	Version int64 `json:"version"`
	Content struct {
		Texts map[string]string `json:"texts"`
		Data  struct {
			Plan        extport.KHTCPlan `json:"plan"`
			Rows        []reportTestRow  `json:"rows"`
			Provisional []string         `json:"provisional"`
			Submissions []map[string]any `json:"submissions"`
		} `json:"data"`
	} `json:"content"`
	Editable      bool `json:"editable"`
	SourceChanged bool `json:"source_changed"`
	Final         *struct {
		ExportID string `json:"export_id"`
	} `json:"final"`
}
type reportExport struct {
	ID          string `json:"id"`
	Version     int64  `json:"version"`
	Provisional bool   `json:"provisional"`
	BodyHash    string `json:"body_hash"`
}
type reportFx struct {
	*kfFx
	src *goldenReader
}

func newReportFx(t *testing.T) *reportFx {
	f := newKHTCFormFx(t)
	f.planBody["base_version"] = 1
	f.planBody["decision_no"] = "02/QD-KH"
	f.planBody["decision_date"] = "2026-01-01"
	f.planBody["effective_from"] = "2026-01-01"
	for _, entry := range f.planBody["entries"].([]map[string]any) {
		if entry["ref"] == f.itemMod {
			entry["source_key"] = "season:trong_trot:pl01:lua_td:luy_ke"
		}
		if entry["ref"] == f.price {
			entry["source_key"] = "sum:thong_tin_truyen_thong:price-tickets:tickets_collected"
		}
	}
	c, b := f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", f.planBody)
	f.ok(c, b, nil)
	src := &goldenReader{}
	src.set([]golden.SourcePeriod{{Key: "2026-W40", To: "2026-10-06", Closed: true, Cumulative: decimal.NewFromInt(100)}, {Key: "2026-W41", To: "2026-10-13", Closed: true, Cumulative: decimal.NewFromInt(120)}}, "before")
	f.ext.UseKHTCSourceReader(src)
	return &reportFx{kfFx: f, src: src}
}
func reportPath(id string) string { return "/v1/extension/khtc/periods/" + id + "/report" }
func (f *reportFx) call(method, pid, suffix string, body any) (int, []byte) {
	return f.do(method, reportPath(pid)+suffix, authz.KNTinh, khtcDept, f.khtcUser, "", body)
}
func (f *reportFx) read(pid string) reportDetail {
	var d reportDetail
	c, b := f.call(http.MethodGet, pid, "", nil)
	f.ok(c, b, &d)
	return d
}
func (f *reportFx) refresh(pid string, base int64) reportDetail {
	var d reportDetail
	c, b := f.call(http.MethodPost, pid, "/refresh", map[string]any{"base_version": base})
	f.ok(c, b, &d)
	return d
}
func (f *reportFx) submit(pid, amount string) {
	c, b := f.send(pid, dTT, "submit", 0, ttSheet(f.item, amount))
	f.ok(c, b, nil)
	c, b = f.send(pid, dTTTT, "submit", 0, map[string]any{"cells": map[string]any{f.news: map[string]string{"done": "0"}}, "texts": map[string]string{"cong_tac_khac": "Không phát sinh"}})
	f.ok(c, b, nil)
}
func (f *reportFx) export(pid string, base int64, ack bool) reportExport {
	var x reportExport
	c, b := f.call(http.MethodPost, pid, "/exports", map[string]any{"base_version": base, "acknowledge_provisional": ack, "filter": map[string]any{}})
	f.ok(c, b, &x)
	return x
}
func (f *reportFx) download(pid, exportID string) []byte {
	c, b := f.call(http.MethodGet, pid, "/exports/"+exportID+"/download", nil)
	f.ok(c, b, nil)
	return b
}
func reportQuantity(t *testing.T, body []byte, name string) string {
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
	for _, row := range rows {
		if len(row) > 5 && row[1] == name {
			return row[5]
		}
	}
	t.Fatalf("xlsx missing %s", name)
	return ""
}
func reportRow(t *testing.T, d reportDetail, ref string) reportTestRow {
	t.Helper()
	for _, row := range d.Content.Data.Rows {
		if row.Ref.String() == ref {
			return row
		}
	}
	t.Fatal("missing item", ref)
	return reportTestRow{}
}

type reportMeter struct{ events []meter.Egress }

func (m *reportMeter) Pass(_ context.Context, e meter.Egress) (meter.Decision, error) {
	m.events = append(m.events, e)
	return meter.Decision{Allowed: false, Reason: "quota_exceeded"}, nil
}

func TestKH_FN_TI_02_05_ReportExcelFrozenAndFinalCancel(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	initial := f.refresh(p.ID, 0)
	c, b := f.send(p.ID, dTT, "draft", 0, ttSheet(f.item, "999"))
	f.ok(c, b, nil)
	if row := reportRow(t, f.read(p.ID), f.item); row.InPeriod != "0" || !row.Missing {
		t.Fatalf("unsubmitted draft leaked: %+v", row)
	}
	f.submit(p.ID, "10")
	d := f.refresh(p.ID, initial.Version)
	if row := reportRow(t, d, f.item); row.InPeriod != "10" {
		t.Fatalf("submitted quantity: %+v", row)
	}
	if len(d.Content.Data.Provisional) != 0 {
		t.Fatalf("unexpected provisional: %+v", d.Content.Data.Provisional)
	}
	c, b = f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "admin": map[string]any{"signer": "=1+1"}, "texts": map[string]string{"B": "Đánh giá đã biên tập"}})
	f.ok(c, b, &d)
	x := f.export(p.ID, d.Version, false)
	checkpoint := &reportMeter{}
	f.ext.UseEgress(checkpoint, meter.ModeEnforce, nil)
	body := f.download(p.ID, x.ID)
	if len(checkpoint.events) != 1 || !checkpoint.events[0].Internal || checkpoint.events[0].Bytes != int64(len(body)) || checkpoint.events[0].Product != "kn_khtc_report" {
		t.Fatalf("download bypassed internal export checkpoint: %+v", checkpoint.events)
	}
	if got := reportQuantity(t, body, "Mô hình lúa chất lượng cao"); got != "10" {
		t.Fatalf("reopened Excel %s", got)
	}
	file, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, sheet := range file.GetSheetList() {
		rows, e := file.GetRows(sheet)
		if e != nil {
			t.Fatal(e)
		}
		for rowNo, row := range rows {
			for col := range row {
				cell, _ := excelize.CoordinatesToCellName(col+1, rowNo+1)
				formula, e := file.GetCellFormula(sheet, cell)
				if e != nil || formula != "" {
					t.Fatalf("text became a formula: %s %s %v", sheet, cell, e)
				}
			}
		}
	}
	_ = file.Close()
	c, b = f.call(http.MethodPost, p.ID, "/exports/"+x.ID+"/final", map[string]any{})
	f.ok(c, b, &d)
	if d.Final == nil || d.Editable {
		t.Fatalf("final state: %+v", d)
	}
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ sau khi có bản cuối"})
	f.fail(c, b, "extension.khtc_period_not_cancellable")
	if state := f.get(p.ID); state.State == "cancelled" || state.FormsStatus == "cancelled" {
		t.Fatalf("cancel side effect occurred before final guard: %+v", state)
	}
	c, b = f.call(http.MethodPost, p.ID, "/refresh", map[string]any{"base_version": d.Version})
	f.fail(c, b, "extension.khtc_report_final")
	c, b = f.do(http.MethodPost, "/v1/extension/khtc/periods/"+p.ID+"/sources/refresh", authz.KNTinh, khtcDept, f.khtcUser, "", map[string]any{})
	if c < 400 {
		t.Fatalf("source refresh after final: %d %s", c, b)
	}
	f.src.set([]golden.SourcePeriod{{Key: "2026-W41", To: "2026-10-13", Closed: true, Cumulative: decimal.NewFromInt(130)}}, "changed")
	if !f.read(p.ID).SourceChanged {
		t.Fatal("must warn about changed sources without rewriting release")
	}
	if again := f.download(p.ID, x.ID); !bytes.Equal(body, again) {
		t.Fatal("historical file was regenerated from live data")
	}
}

func TestKH_ReportProvisionalAndAccess(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	c, b := f.call(http.MethodPost, p.ID, "/exports", map[string]any{"base_version": d.Version})
	f.fail(c, b, "extension.khtc_report_provisional")
	x := f.export(p.ID, d.Version, true)
	if !x.Provisional {
		t.Fatal("must label provisional")
	}
	c, b = f.call(http.MethodPost, p.ID, "/exports/"+x.ID+"/final", map[string]any{})
	f.fail(c, b, "extension.khtc_report_provisional")
	c, b = f.do(http.MethodPost, reportPath(p.ID)+"/draft", authz.KNTinh, khtcDept, ids.New(), "", map[string]any{"base_version": d.Version})
	if c != http.StatusForbidden {
		t.Fatalf("non-owner %d %s", c, b)
	}
	c, b = f.do(http.MethodGet, reportPath(p.ID), authz.KNTinh, dTT, uuid.Nil, "", nil)
	if c != http.StatusForbidden {
		t.Fatalf("other department %d %s", c, b)
	}
	c, b = f.do(http.MethodGet, reportPath(p.ID), authz.KNAdmin, "", uuid.Nil, "", nil)
	f.ok(c, b, &d)
	if d.Editable {
		t.Fatal("admin must read only")
	}
	c, b = f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "rows": []any{}})
	if c != http.StatusBadRequest {
		t.Fatalf("numeric/system field accepted: %d %s", c, b)
	}
	other := newReportFx(t)
	c, b = other.call(http.MethodGet, p.ID, "", nil)
	if c != http.StatusNotFound {
		t.Fatalf("cross tenant read: %d %s", c, b)
	}
}

func TestKH_ReportLatestSubmittedResend(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	f.submit(p.ID, "10")
	d = f.refresh(p.ID, d.Version)
	first := f.export(p.ID, d.Version, false)
	old := f.download(p.ID, first.ID)
	c, b := f.reviewForm(p.ID, dTT, "returned", "Bổ sung số liệu đã thực hiện")
	f.ok(c, b, nil)
	c, b = f.send(p.ID, dTT, "draft", 1, ttSheet(f.item, "999"))
	f.ok(c, b, nil)
	d = f.refresh(p.ID, d.Version)
	if row := reportRow(t, d, f.item); row.InPeriod != "10" {
		t.Fatalf("report used unsubmitted revision: %+v", row)
	}
	if len(d.Content.Data.Provisional) == 0 {
		t.Fatal("returned form must keep final disabled")
	}
	c, b = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "12"))
	f.ok(c, b, nil)
	d = f.refresh(p.ID, d.Version)
	if row := reportRow(t, d, f.item); row.InPeriod != "12" {
		t.Fatalf("not latest submission: %+v", row)
	}
	next := f.export(p.ID, d.Version, false)
	if next.Version != 2 {
		t.Fatalf("version %+v", next)
	}
	if got := reportQuantity(t, f.download(p.ID, next.ID), "Mô hình lúa chất lượng cao"); got != "12" {
		t.Fatal("xlsx latest", got)
	}
	if !bytes.Equal(old, f.download(p.ID, first.ID)) {
		t.Fatal("old version changed after resend")
	}
	raw, _ := json.Marshal(d.Content.Data.Submissions)
	if bytes.Contains(raw, []byte(`"Sheet"`)) || bytes.Contains(raw, []byte(`"cells"`)) {
		t.Fatal("draft content leaked into report provenance")
	}
}

func TestKH_ReportCancelBeforeFinalDoesNotWaitOnItself(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ chưa phát hành báo cáo"})
	f.ok(c, b, nil)
	state := f.get(p.ID)
	if state.State != "cancelled" {
		t.Fatalf("not cancelled: %+v", state)
	}
}
func TestKH_ReportFinalAndCancelConcurrent(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	f.submit(p.ID, "10")
	d := f.refresh(p.ID, 0)
	x := f.export(p.ID, d.Version, false)
	start := make(chan struct{})
	type result struct {
		op   string
		code int
		body []byte
	}
	done := make(chan result, 2)
	go func() {
		<-start
		c, b := f.call(http.MethodPost, p.ID, "/exports/"+x.ID+"/final", map[string]any{})
		done <- result{"final", c, b}
	}()
	go func() {
		<-start
		c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy đồng thời với đánh dấu bản cuối"})
		done <- result{"cancel", c, b}
	}()
	close(start)
	first, second := <-done, <-done
	successes := 0
	for _, r := range []result{first, second} {
		if r.code < 300 {
			successes++
		} else if r.code < 400 || r.code >= 500 {
			t.Fatalf("unexpected %s response %d %s", r.op, r.code, r.body)
		}
	}
	if successes != 1 {
		t.Fatalf("must serialize with exactly one winner: %+v %+v", first, second)
	}
	state := f.get(p.ID)
	if state.State == "cancelled" {
		c, b := f.call(http.MethodGet, p.ID, "", nil)
		if c == 200 {
			var d reportDetail
			f.ok(c, b, &d)
			if d.Final != nil {
				t.Fatal("cancelled period has a final report")
			}
		}
	} else {
		if f.read(p.ID).Final == nil {
			t.Fatal("final winner not persisted")
		}
		if state.FormsStatus == "cancelled" {
			t.Fatal("losing cancel changed native forms")
		}
	}
}
func TestKH_ReportClosedSnapshotKeepsPlanVersion(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	f.submit(p.ID, "10")
	d := f.refresh(p.ID, 0)
	oldPlan := d.Content.Data.Plan.VersionID
	c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", f.khtcUser, "", map[string]any{})
	f.ok(c, b, nil)
	f.planBody["base_version"] = 2
	f.planBody["decision_no"] = "03/QD-KH"
	f.planBody["decision_date"] = "2026-10-14"
	f.planBody["effective_from"] = "2026-10-14"
	for _, entry := range f.planBody["entries"].([]map[string]any) {
		if entry["ref"] == f.item {
			entry["planned_qty"] = "200"
		}
	}
	c, b = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", f.planBody)
	f.ok(c, b, nil)
	current := f.read(p.ID)
	if current.Content.Data.Plan.VersionID != oldPlan || current.SourceChanged {
		t.Fatalf("closed period did not retain snapshot plan: %+v", current)
	}
	if row := reportRow(t, current, f.item); row.Planned == nil || *row.Planned != "100" {
		t.Fatalf("closed plan quantity changed: %+v", row)
	}
}

func TestKH_ReportLivePublishedReaderWithoutData(t *testing.T) {
	f := newKHTCFormFx(t) // Keep the production source adapter, including published price metadata.
	p := f.open(41, dTT, dTTTT)
	c, b := f.do(http.MethodGet, reportPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	var detail reportDetail
	f.ok(c, b, &detail)
	if len(detail.Content.Data.Provisional) == 0 {
		t.Fatal("absent published sources and submitted forms must remain provisional")
	}
}

func TestKH_ReportProgramCopiesPreviousNarrativeOnly(t *testing.T) {
	f := newReportFx(t)
	open := func(day string) string {
		var p khtcPeriod
		c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "program", "year": 2026, "as_of_date": day, "due_at": "2026-12-31T03:00:00Z", "departments": []string{dTT, dTTTT}})
		f.ok(c, b, &p)
		return p.ID
	}
	first := open("2026-10-06")
	c, b := f.call(http.MethodPost, first, "/draft", map[string]any{"base_version": 0, "texts": map[string]string{"I": "Đánh giá cũ", "II": "Kế hoạch kỳ trước"}, "admin": map[string]string{"signer": "Không sao chép"}})
	f.ok(c, b, nil)
	second := open("2026-10-13")
	d := f.read(second)
	if d.Content.Texts["II"] != "Kế hoạch kỳ trước" || d.Content.Texts["I"] == "Đánh giá cũ" {
		t.Fatalf("copy only II: %+v", d.Content.Texts)
	}
	c, b = f.call(http.MethodPost, second, "/draft", map[string]any{"base_version": 0, "texts": map[string]string{"II": ""}})
	f.ok(c, b, &d)
	d = f.refresh(second, d.Version)
	if d.Content.Texts["II"] != "" {
		t.Fatal("refresh overwrote explicit blank")
	}
	c, b = f.call(http.MethodPost, second, "/exports", map[string]any{"base_version": d.Version, "acknowledge_provisional": true, "filter": map[string]string{"level": "bogus"}})
	f.fail(c, b, "extension.khtc_report_invalid")
}

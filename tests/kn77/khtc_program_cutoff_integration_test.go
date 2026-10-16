package kn77_test

import (
	"bytes"
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	golden "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/golden/khtc"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
	"net/http"
	"testing"
	"time"
)

func TestKH_M5_ProgramCutoffHTTP(t *testing.T) {
	f := newReportFx(t)
	f.clk.Advance(-7 * 24 * time.Hour)
	before := f.open(40, dTT, dTTTT)
	f.submit(before.ID, "10")
	f.clk.Advance(7 * 24 * time.Hour)
	after := f.open(41, dTT, dTTTT)
	f.submit(after.ID, "90")
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{
		"report_type": "program", "year": 2026, "as_of_date": "2026-10-13", "cutoff_date": "2026-10-06",
		"due_at": "2026-12-31T03:00:00Z", "departments": []string{dTT, dTTTT}})
	f.ok(c, b, &p)
	d := f.refresh(p.ID, 0)
	if row := reportRow(t, d, f.item); row.InPeriod != "10" {
		t.Errorf("manual weekly figures after cutoff entered appendix: %+v", row)
	}
	if row := reportRow(t, d, f.itemMod); row.InPeriod != "100" {
		t.Errorf("module week after cutoff entered appendix: %+v", row)
	}
	if d.Content.Data.Period.To != "2026-10-13T00:00:00Z" || d.Content.Data.Period.Cutoff != "2026-10-06T00:00:00Z" {
		t.Fatalf("report identity/cutoff changed: %+v", d.Content.Data.Period)
	}
	savedHash := d.Content.Data.InputHash
	if len(savedHash) != 64 {
		t.Fatalf("missing input fingerprint: %q", savedHash)
	}
	export := f.export(p.ID, d.Version, true)
	original := f.download(p.ID, export.ID)
	checkM5CutoffExcel(t, original)
	// A submitted revision and a source correction after cutoff must not alter
	// this report's provenance, totals, warning, or any historical export.
	c, b = f.reviewForm(after.ID, dTT, "returned", "Điều chỉnh số liệu của tuần sau ngày chốt")
	f.ok(c, b, nil)
	c, b = f.send(after.ID, dTT, "submit", 1, ttSheet(f.item, "999"))
	f.ok(c, b, nil)
	f.src.set([]golden.SourcePeriod{{Key: "2026-W40", To: "2026-10-06", Closed: true, Cumulative: decimal.NewFromInt(100)},
		{Key: "2026-W41", To: "2026-10-13", Closed: true, Cumulative: decimal.NewFromInt(999)}}, "before")
	unchanged := f.read(p.ID)
	if unchanged.SourceChanged || unchanged.Content.Data.InputHash != savedHash {
		t.Fatalf("after-cutoff changes affected frozen report: %+v", unchanged)
	}
	if !bytes.Equal(original, f.download(p.ID, export.ID)) {
		t.Fatal("old Excel changed after future source/form edits")
	}
	updated := f.refresh(p.ID, d.Version)
	if row := reportRow(t, updated, f.item); row.InPeriod != "10" {
		t.Fatalf("refresh included future form: %+v", row)
	}
	if row := reportRow(t, updated, f.itemMod); row.InPeriod != "100" {
		t.Fatalf("refresh included future module: %+v", row)
	}
	next := f.export(p.ID, updated.Version, true)
	checkM5CutoffExcel(t, f.download(p.ID, next.ID))
	if !bytes.Equal(original, f.download(p.ID, export.ID)) {
		t.Fatal("explicit refresh overwrote historical Excel")
	}
}

func checkM5CutoffExcel(t *testing.T, body []byte) {
	t.Helper()
	file, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	rows, err := file.GetRows("Phụ lục")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if len(row) >= 4 && row[1] == "Mô hình lúa chất lượng cao" {
			if row[3] != "10/100; đạt 10% kế hoạch" {
				t.Fatalf("program appendix past cutoff: %+v", row)
			}
			return
		}
	}
	t.Fatal("program appendix missing planned item")
}

func TestKH_M5_LiveProgramSourceCrosswalk(t *testing.T) {
	f := newKHTCFormFx(t)
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: f.khtcUser, TenantID: f.tid})
	seedM4Boards(t, f, f.tid, []string{"2026-10-06", "2026-10-07"}, 1, 1000)
	from, _ := time.Parse(time.DateOnly, "2025-12-31")
	to, _ := time.Parse(time.DateOnly, "2026-10-08")
	refs, err := f.ext.KHTCSourceCrosswalk().Sources(ctx, "adhoc", "2026-CT-1013", from, to, []string{dTT, dTTTT})
	if err != nil {
		t.Fatalf("production program crosswalk: %v", err)
	}
	if len(refs) != 1 || refs[0].NativeKey != "2026-10-06" {
		t.Fatalf("program source bounds/custom key: %+v", refs)
	}
}

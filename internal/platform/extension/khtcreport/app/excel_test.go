package app_test

import (
	"bytes"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcreport/domain"
	source "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcsource/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestKH_FN_TI_02_05_AllFiveWorkbooks(t *testing.T) {
	day := time.Date(2026, 12, 29, 0, 0, 0, 0, time.UTC)
	qty, pct := "100", "85"
	for _, kind := range []string{"week", "month", "quarter", "year", "program"} {
		t.Run(kind, func(t *testing.T) {
			parent := uuid.New()
			child := uuid.New()
			data := domain.Data{Period: domain.Period{ID: uuid.New(), Type: kind, Year: 2026, Name: "Kỳ kiểm chứng", From: day.AddDate(0, 0, -6), To: day, Cutoff: day}, Template: domain.Template(kind), Rows: []domain.ProgramRow{
				{Ref: parent, Kind: "program", Name: "Chương trình XIII", Roman: "XIII"},
				{Ref: child, ParentRef: &parent, Kind: "item", Name: "Mô hình lúa", Tab: "programs", Unit: "ha", Planned: &qty, InPeriod: "20", Adjustment: "-5", YearToDate: "85", Percent: &pct, Content: "Xã A", Communes: []string{"A"}},
			}, Metrics: []domain.SourceMetric{{Label: "Dưa leo", Unit: "đ/kg", Value: source.Value{Mode: source.ModePrice, PriceDays: []source.PriceDay{{Day: "2026-12-28", Min: decimal.NewFromInt(8000), Max: decimal.NewFromInt(13500)}, {Day: "2026-12-29", Min: decimal.NewFromInt(8500), Max: decimal.NewFromInt(14000)}}}}}}
			draft := domain.Refresh(domain.Draft{}, data, map[string]string{"signer": "=1+1", "document_no": "27"})
			body, err := app.RenderXLSX(draft, domain.Filter{}, 2)
			if err != nil {
				t.Fatal(err)
			}
			file, err := excelize.OpenReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			for _, name := range []string{"Báo cáo", "Số liệu nguồn", "Mốc dữ liệu"} {
				if i, e := file.GetSheetIndex(name); e != nil || i < 0 {
					t.Fatalf("missing sheet %s: %v", name, e)
				}
			}
			sheet := "Báo cáo"
			if kind == "program" {
				sheet = "Số liệu chương trình"
			}
			rows, err := file.GetRows(sheet)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for ri, r := range rows {
				if len(r) > 8 && r[1] == "Mô hình lúa" {
					found = true
					for _, col := range []int{4, 6, 7, 8, 9} {
						cell, _ := excelize.CoordinatesToCellName(col, ri+1)
						typ, e := file.GetCellType(sheet, cell)
						if e != nil || typ == excelize.CellTypeSharedString || typ == excelize.CellTypeInlineString {
							t.Fatalf("numeric cell exported as text %s!%s type=%v err=%v", sheet, cell, typ, e)
						}
					}
					_, _ = file.NewSheet("Kiểm chứng")
					formula := "SUM('" + sheet + "'!F" + strconv.Itoa(ri+1) + ":H" + strconv.Itoa(ri+1) + ")"
					if e := file.SetCellFormula("Kiểm chứng", "A1", formula); e != nil {
						t.Fatal(e)
					}
					total, e := file.CalcCellValue("Kiểm chứng", "A1")
					if e != nil || total != "100" {
						t.Fatalf("numbers not summable: %s %v", total, e)
					}
					_ = file.DeleteSheet("Kiểm chứng")
					if r[3] != "100" || r[5] != "20" || r[6] != "-5" || r[7] != "85" || r[8] != "85.0%" {
						t.Fatalf("exact quantities: %v", r)
					}
				}
			}
			if !found {
				t.Fatal("missing dynamic program item")
			}
			for _, name := range file.GetSheetList() {
				rs, e := file.GetRows(name)
				if e != nil {
					t.Fatal(e)
				}
				for ri, r := range rs {
					for ci := range r {
						cell, _ := excelize.CoordinatesToCellName(ci+1, ri+1)
						formula, e := file.GetCellFormula(name, cell)
						if e != nil || formula != "" {
							t.Fatalf("unexpected formula %s!%s %q %v", name, cell, formula, e)
						}
					}
				}
			}
			if kind == "week" {
				rows, _ := file.GetRows("Báo cáo")
				found := false
				for _, r := range rows {
					if len(r) == 8 && r[0] == "Dưa leo" {
						found = true
						if strings.Join(r, "|") != "Dưa leo|đ/kg|2026-12-28|8000|13500|2026-12-29|8500|14000" {
							t.Fatalf("two approved days: %v", r)
						}
					}
				}
				if !found {
					t.Fatal("missing price range")
				}
			}
			if kind == "quarter" || kind == "year" || kind == "program" {
				if i, e := file.GetSheetIndex("Phụ lục"); e != nil || i < 0 {
					t.Fatal("missing appendix")
				}
				appendix, e := file.GetRows("Phụ lục")
				if e != nil {
					t.Fatal(e)
				}
				header, progress := false, false
				for _, row := range appendix {
					if len(row) > 0 && row[0] == "TT" {
						header = strings.Join(row, "|") == "TT|Tên chương trình|Quy mô|Tiến độ|Ghi chú"
					}
					if len(row) == 5 && row[1] == "Mô hình lúa" {
						progress = row[3] == "85/100; đạt 85% kế hoạch"
					}
				}
				if !header || !progress {
					t.Fatalf("five-column appendix: %+v", appendix)
				}
			}
		})
	}
}

func TestKH_UR_TI_12_SourceTraceExcel(t *testing.T) {
	day := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	v := decimal.NewFromInt(120)
	base := decimal.NewFromInt(100)
	metric := domain.SourceMetric{Key: "cum:cnts:fish", Form: "BM18", Trace: []domain.SourceTrace{{Module: "pf/forms", Calendar: "chan_nuoi_thuy_san", NativeKey: "2026-W41", TargetKey: "2026-W41", From: &day, To: &day, Closed: true}}, Value: source.Value{Mode: source.ModeCumulative, Weeks: []source.Week{{Key: "2026-W41", From: day.AddDate(0, 0, -6), To: day, Cumulative: v, Base: base, BaseRef: "frozen:2026-W40", Increment: v.Sub(base), YearToDate: v, Closed: true}}}}
	draft := domain.Refresh(domain.Draft{}, domain.Data{Template: "mau_1", Period: domain.Period{Type: "week", Year: 2026, To: day}, Metrics: []domain.SourceMetric{metric}}, nil)
	body, err := app.RenderXLSX(draft, domain.Filter{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	rows, err := f.GetRows("Số liệu nguồn")
	if err != nil {
		t.Fatal(err)
	}
	trace, calculation := false, false
	for _, r := range rows {
		if len(r) > 8 && r[1] == "pf/forms" {
			trace = r[3] == "chan_nuoi_thuy_san" && r[4] == "2026-W41" && r[5] == "2026-10-13" && r[8] == "2026-W41"
		}
		if len(r) > 12 && r[6] == "frozen:2026-W40" {
			calculation = r[4] == "120" && r[5] == "100" && r[7] == "20" && r[12] == "lũy kế − gốc"
		}
	}
	if !trace || !calculation {
		t.Fatalf("missing frozen trace/base: %+v", rows)
	}
}

package closedfig

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	rcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
)

func hashOf(s string) *string { return &s }

// D2: dòng "Chưa xác nhận" vẫn vào số, tách riêng; nhận HTX (unconfirmed → confirmed) chỉ đổi StatusHash, không đổi
// FiguresHash (W11.64 không báo "Nguồn đã thay đổi" giả).
func TestCrosswalk_ConfirmationSplitAndHashes_PFPUB(t *testing.T) {
	r := cntsRule(t)
	mk := func(status, statusHash string) port.ClosedFigure {
		f := weekFig(r, d(2026, 9, 14), "BM-04", nil)
		f.Rows, _ = json.Marshal([]map[string]any{
			{"key": "X1", "unit": "X1", "cells": map[string]string{"heo": "10"}, "confirmation": "confirmed"},
			{"key": "HTX1", "unit": "HTX1", "cells": map[string]string{"heo": "2.5"}, "confirmation": status}})
		f.FiguresHash, f.StatusHash = hashOf("fig-1"), hashOf(statusHash)
		return f
	}
	before := convert(t, []port.ClosedFigure{mk(port.ConfirmationUnconfirmed, "st-1")}, port.CrosswalkTarget{})[0]
	if before.Sum["heo"] != "12.5" || before.SumConfirmed["heo"] != "10" || before.SumUnconfirmed["heo"] != "2.5" ||
		before.LastUnconfirmed["heo"] != "2.5" || before.FiguresHash == "" || before.StatusHash == "" {
		t.Fatalf("tách xác nhận: %+v", before)
	}
	after := convert(t, []port.ClosedFigure{mk(port.ConfirmationConfirmed, "st-2")}, port.CrosswalkTarget{})[0]
	if after.SumConfirmed["heo"] != "12.5" || len(after.SumUnconfirmed) != 0 || after.FiguresHash != before.FiguresHash ||
		after.StatusHash == before.StatusHash {
		t.Fatalf("nhận HTX chỉ đổi băm trạng thái: %+v", after)
	}
	f := mk(port.ConfirmationConfirmed, "st-2")
	f.FiguresHash = hashOf("fig-2")
	if changed := convert(t, []port.ClosedFigure{f}, port.CrosswalkTarget{})[0]; changed.FiguresHash == after.FiguresHash {
		t.Fatal("số đổi phải đổi băm số")
	}
}

// Báo giá: bảng giá ngày → Last của kỳ KHTC là ngày duyệt cuối của tuần (D22); số phiếu cộng theo tuần; tuần không có
// ngày duyệt → lấy ngày duyệt gần nhất trước đó (Fallback). Phòng được yêu cầu mà không có số → Missing; view giá chưa
// có → Unavailable; kỳ KHTC chốt khi mọi phòng chốt.
func TestKHTCAssemble_PriceAndDepartments_PFPUB(t *testing.T) {
	avg := "15000"
	boards := []PriceBoardRow{
		{BoardDate: d(2026, 9, 10), BoardVersion: 1, ApprovedAt: d(2026, 9, 10), ItemCode: "MH00001", PriceMin: "10000", PriceMax: "20000", PriceAvg: &avg, PointsReported: 5},
		{BoardDate: d(2026, 9, 14), BoardVersion: 2, ApprovedAt: d(2026, 9, 14), ItemCode: "MH00001", PriceMin: "11000", PriceMax: "21000", PointsReported: 6},
	}
	tickets := []PriceTicketRow{
		{WeekKey: "2026-W37", WeekFrom: d(2026, 9, 9), WeekTo: d(2026, 9, 15), PointCode: "XBG0001", CommuneCode: "30001", TicketsCollected: 4, DaysScheduled: 5, DaysApproved: 5, WeekClosed: true},
		{WeekKey: "2026-W37", WeekFrom: d(2026, 9, 9), WeekTo: d(2026, 9, 15), PointCode: "XBG0002", CommuneCode: "30002", TicketsCollected: 3, DaysScheduled: 5, DaysApproved: 5, WeekClosed: true},
	}
	figs := PriceFigures("89", boards, tickets)
	r := cntsRule(t)
	cnts := weekFig(r, d(2026, 9, 14), "BM-04", map[string]string{"heo": "7"})
	figs = append(figs, cnts)
	tgt, rule, err := normTarget(port.CrosswalkTarget{})
	if err != nil {
		t.Fatal(err)
	}
	conv := convert(t, figs, tgt)
	from, to := d(2026, 9, 9), d(2026, 9, 22) // tuần W37, W38
	ps := Assemble(conv, tgt, rule, from, to, []string{PriceDepartment, "chan_nuoi_thuy_san", "trong_trot"}, true, true)
	if len(ps) != 2 || ps[0].TargetKey != "2026-W37" || ps[1].TargetKey != "2026-W38" || ps[0].RuleVersion != port.CrosswalkV1 {
		t.Fatalf("kỳ KHTC: %+v", ps)
	}
	dept := func(p port.KHTCPeriod, code string) port.DepartmentFigures {
		for _, x := range p.Departments {
			if x.DepartmentCode == code {
				return x
			}
		}
		t.Fatalf("thiếu phòng %s", code)
		return port.DepartmentFigures{}
	}
	form := func(df port.DepartmentFigures, code string) port.TargetFigure {
		for _, f := range df.Forms {
			if f.FormCode == code {
				return f
			}
		}
		t.Fatalf("thiếu biểu %s", code)
		return port.TargetFigure{}
	}
	p37 := ps[0]
	price := dept(p37, PriceDepartment)
	if b := form(price, port.PriceFormBoard); b.Last["price_min"] != "11000" || b.Last["price_max"] != "21000" || len(b.Sources) != 2 || price.Fallback {
		t.Fatalf("bảng giá tuần 37 = ngày duyệt cuối: %+v", b)
	}
	if tk := form(price, port.PriceFormTickets); tk.Sum["tickets_collected"] != "7" || !tk.Closed || tk.Sources[0].SourceModule != "pf/price" {
		t.Fatalf("số phiếu tuần 37: %+v", tk)
	}
	if !price.Closed || price.SourceModule != "pf/price" {
		t.Fatalf("Báo giá tuần 37: %+v", price)
	}
	if c := dept(p37, "chan_nuoi_thuy_san"); !c.Closed || c.SourceModule != "pf/forms" || form(c, "BM-04").Sources[0].NativeKey != cnts.PeriodKey {
		t.Fatalf("CNTS tuần 37: %+v", c)
	}
	if tt := dept(p37, "trong_trot"); !tt.Missing || tt.Closed || p37.Closed {
		t.Fatalf("Trồng trọt không có số → Missing, kỳ chưa chốt: %+v %v", tt, p37.Closed)
	}
	// Tuần 38: không ngày duyệt → ngày duyệt gần nhất (14/09) — Fallback; không có phiếu tuần.
	p38 := dept(ps[1], PriceDepartment)
	if b := form(p38, port.PriceFormBoard); !p38.Fallback || b.Last["price_min"] != "11000" || b.TargetKey != "2026-W38" || b.Sum != nil {
		t.Fatalf("fallback D22: %+v %+v", p38, b)
	}
	// View giá chưa có (trước W11.76) → Unavailable, kỳ không chốt.
	un := Assemble(convert(t, []port.ClosedFigure{cnts}, tgt), tgt, rule, from, from, nil, false, true)
	if p := dept(un[0], PriceDepartment); !p.Unavailable || p.Closed || p.Missing || un[0].Closed {
		t.Fatalf("giá chưa công bố: %+v", p)
	}
	// Tháng của KHTC: cùng dữ liệu, kỳ đích tháng.
	mt, mrule, _ := normTarget(port.CrosswalkTarget{PeriodType: rcdomain.PeriodMonth})
	if m := Assemble(convert(t, figs, mt), mt, mrule, from, from, nil, true, true); len(m) != 1 || m[0].TargetKey != "2026-09" {
		t.Fatalf("kỳ tháng: %+v", m)
	}
	_ = time.UTC
}

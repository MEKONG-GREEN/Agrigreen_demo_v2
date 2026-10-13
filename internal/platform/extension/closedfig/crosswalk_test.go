package closedfig

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	rcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// cntsTag: lịch CNTS Thứ Ba – Thứ Hai (D8), khác lịch KHTC mặc định Thứ Tư – Thứ Ba.
const cntsTag = "kn.period_rule/v2;week_start=tue;week=year;month=week_end"

func cntsRule(t *testing.T) rcdomain.PeriodRule {
	t.Helper()
	r, err := rcdomain.ParseRule(cntsTag)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }

// fig dựng một kỳ nguồn đã chốt của CNTS với ô cells (một đơn vị).
func fig(pt, key string, from, to time.Time, form string, closed bool, cells map[string]string) port.ClosedFigure {
	rows, _ := json.Marshal([]map[string]any{{"key": "X1", "unit": "X1", "unit_kind": "commune", "indicator": nil, "cells": cells}})
	snap := ids.New()
	f := port.ClosedFigure{DepartmentCode: "chan_nuoi_thuy_san", ProvinceCode: "89", CalendarID: "chan_nuoi_thuy_san", CalendarVersion: "v2",
		PeriodType: pt, PeriodKey: key, PeriodFrom: &from, PeriodTo: &to, Source: port.ClosedSourceForms, SourceModule: "pf/forms", SourcePeriodID: ids.New(),
		FormCode: form, FormVersionID: uuid.Nil, FormClosed: closed, PeriodClosed: closed, SnapshotID: &snap, Rows: rows}
	return f
}

func weekFig(r rcdomain.PeriodRule, end time.Time, form string, cells map[string]string) port.ClosedFigure {
	s, _ := r.PeriodOf(rcdomain.PeriodWeek, end, nil)
	return fig(rcdomain.PeriodWeek, s.Key, s.From, s.To, form, true, cells)
}

func convert(t *testing.T, figs []port.ClosedFigure, tgt port.CrosswalkTarget) []port.TargetFigure {
	t.Helper()
	out, err := Convert(figs, tgt)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sumOf: Σ một ô trên các kỳ đích (Sum) — bất biến so với Σ của nguồn.
func sumOf(out []port.TargetFigure, cell string) decimal.Decimal {
	s := decimal.Zero
	for _, o := range out {
		s = s.Add(decimal.RequireFromString(o.Sum[cell]))
	}
	return s
}

func srcSum(figs []port.ClosedFigure, cell string) decimal.Decimal {
	s := decimal.Zero
	for _, f := range figs {
		var rows []struct{ Cells map[string]string }
		_ = json.Unmarshal(f.Rows, &rows)
		for _, r := range rows {
			if v, ok := r.Cells[cell]; ok {
				s = s.Add(decimal.RequireFromString(v))
			}
		}
	}
	return s
}

// Tuần CNTS (Thứ Ba – Thứ Hai) nằm vắt hai tuần KHTC (Thứ Tư – Thứ Ba) → thuộc tuần KHTC chứa ngày cuối, giữ khóa gốc,
// Converted; không chia tỷ lệ.
func TestCrosswalk_StraddlingWeek_PFPUB(t *testing.T) {
	r := cntsRule(t)
	f := weekFig(r, d(2026, 9, 14), "BM-04", map[string]string{"heo": "7"}) // Thứ Ba 08/09 – Thứ Hai 14/09
	if !f.PeriodFrom.Equal(d(2026, 9, 8)) {
		t.Fatalf("tuần CNTS: %v", f.PeriodFrom)
	}
	out := convert(t, []port.ClosedFigure{f}, port.CrosswalkTarget{})
	if len(out) != 1 {
		t.Fatalf("%+v", out)
	}
	o := out[0]
	if o.TargetKey != "2026-W37" || !o.TargetFrom.Equal(d(2026, 9, 9)) || !o.TargetTo.Equal(d(2026, 9, 15)) || !o.Converted || !o.Closed ||
		o.RuleVersion != port.CrosswalkV1 || o.TargetCalendarID != "ke_hoach_tai_chinh" || o.TargetCalendarVersion != "v2" ||
		o.Sum["heo"] != "7" || o.Last["heo"] != "7" || len(o.Sources) != 1 || o.Sources[0].NativeKey != f.PeriodKey ||
		o.LastSourceKey != "chan_nuoi_thuy_san:v2:"+f.PeriodKey {
		t.Fatalf("tuần vắt: %+v", o)
	}
	// Lịch nguồn trùng lịch đích → không Converted.
	k := weekFig(rcdomain.DefaultRule, d(2026, 9, 15), "BM-04", map[string]string{"heo": "1"})
	if out := convert(t, []port.ClosedFigure{k}, port.CrosswalkTarget{}); out[0].Converted || out[0].TargetKey != k.PeriodKey {
		t.Fatalf("cùng lịch: %+v", out[0])
	}
}

// Kỳ ngày (BM-01) cộng CHÍNH XÁC theo tuần KHTC; tổng sau quy đổi = tổng nguồn (decimal, không trôi làm tròn).
func TestCrosswalk_DaysSumExactly_PFPUB(t *testing.T) {
	var figs []port.ClosedFigure
	vals := []string{"0.1", "0.2", "0.3", "1.000001", "2.5", "3", "0.7"}
	for i, v := range vals { // 08/09 (Thứ Ba) … 14/09 (Thứ Hai)
		day := d(2026, 9, 8+i)
		figs = append(figs, fig(rcdomain.PeriodDay, day.Format(time.DateOnly), day, day, "BM-01", true, map[string]string{"trau_bo": v}))
	}
	out := convert(t, figs, port.CrosswalkTarget{})
	if len(out) != 2 || out[0].TargetKey != "2026-W36" || out[1].TargetKey != "2026-W37" {
		t.Fatalf("hai tuần KHTC: %+v", out)
	}
	if out[0].Sum["trau_bo"] != "0.1" || out[1].Sum["trau_bo"] != "7.700001" || out[0].Converted != true {
		t.Fatalf("cộng theo tuần KHTC: %v %v", out[0].Sum, out[1].Sum)
	}
	if !sumOf(out, "trau_bo").Equal(srcSum(figs, "trau_bo")) {
		t.Fatalf("bất biến tổng: %s ≠ %s", sumOf(out, "trau_bo"), srcSum(figs, "trau_bo"))
	}
}

// Ranh giới năm và tuần 53: mọi tuần CNTS từ 12/2026 tới 01/2033 quy vào đúng một tuần KHTC; tổng giữ nguyên; tuần
// vắt năm thuộc năm của ngày cuối; có năm 53 tuần ở cả hai lịch trong khoảng kiểm.
func TestCrosswalk_YearBoundaryAndWeek53_PFPUB(t *testing.T) {
	r := cntsRule(t)
	var figs []port.ClosedFigure
	src53, dst53 := false, false
	for w := r.WeekOf(d(2026, 12, 1)); w.From.Before(d(2033, 1, 31)); w = r.WeekOf(w.To.AddDate(0, 0, 1)) {
		f := weekFig(r, w.To, "BM-04", map[string]string{"heo": fmt.Sprintf("%d.%02d", w.No, w.Year%100)})
		src53 = src53 || strings.HasSuffix(f.PeriodKey, "-W53")
		figs = append(figs, f)
	}
	out := convert(t, figs, port.CrosswalkTarget{})
	n := 0
	for _, o := range out {
		n += len(o.Sources)
		dst53 = dst53 || strings.HasSuffix(o.TargetKey, "-W53")
		for _, s := range o.Sources {
			if s.To.Before(o.TargetFrom) || s.To.After(o.TargetTo) {
				t.Fatalf("ngày cuối %v ngoài tuần đích %s", s.To, o.TargetKey)
			}
		}
	}
	if n != len(figs) || !src53 || !dst53 {
		t.Fatalf("mỗi tuần nguồn đúng một tuần đích: %d/%d, có W53 nguồn %v đích %v", n, len(figs), src53, dst53)
	}
	if !sumOf(out, "heo").Equal(srcSum(figs, "heo")) {
		t.Fatal("bất biến tổng qua ranh giới năm")
	}
	// Tuần CNTS Thứ Ba 29/12/2026 – Thứ Hai 04/01/2027 → tuần KHTC 30/12/2026 – 05/01/2027 = 2027-W01.
	one := convert(t, []port.ClosedFigure{weekFig(r, d(2027, 1, 4), "BM-04", map[string]string{"heo": "1"})}, port.CrosswalkTarget{})
	if one[0].TargetKey != "2027-W01" || !one[0].TargetFrom.Equal(d(2026, 12, 30)) {
		t.Fatalf("vắt năm: %+v", one[0])
	}
}

// Cuối tháng, cuối quý: kỳ đích tháng/quý của lịch KHTC (tháng theo ngày cuối tuần KHTC — BR-DL-01).
func TestCrosswalk_MonthQuarterEnds_PFPUB(t *testing.T) {
	r := cntsRule(t)
	sep := weekFig(r, d(2026, 9, 28), "BM-04", map[string]string{"heo": "2"}) // 22/09 – 28/09 → tuần KHTC 23/09 – 29/09
	oct := weekFig(r, d(2026, 10, 5), "BM-04", map[string]string{"heo": "3"}) // 29/09 – 05/10 → tuần KHTC 30/09 – 06/10
	for _, c := range []struct{ pt, a, b string }{{"month", "2026-09", "2026-10"}, {"quarter", "2026-Q3", "2026-Q4"}} {
		out := convert(t, []port.ClosedFigure{sep, oct}, port.CrosswalkTarget{PeriodType: c.pt})
		if len(out) != 2 || out[0].TargetKey != c.a || out[1].TargetKey != c.b || out[0].Sum["heo"] != "2" || out[1].Sum["heo"] != "3" {
			t.Fatalf("%s: %+v", c.pt, out)
		}
	}
}

// Lũy kế của nguồn là chuẩn: Last = kỳ nguồn cuối của tuần đích; số tăng tuần KHTC = hiệu hai lũy kế. Đã chốt phía
// KHTC = mọi kỳ nguồn (mọi biểu của phòng) quy vào tuần đích đã chốt.
func TestCrosswalk_CumulativeAndClosed_PFPUB(t *testing.T) {
	r := cntsRule(t)
	w1 := weekFig(r, d(2026, 9, 14), "BM-04", map[string]string{"heo_luy_ke": "100"})
	w2 := weekFig(r, d(2026, 9, 21), "BM-04", map[string]string{"heo_luy_ke": "130.5"})
	other := weekFig(r, d(2026, 9, 21), "BM-17", map[string]string{"ca": "1"})
	other.FormClosed = false // biểu khác của phòng còn mở trong tuần đích 2
	out := convert(t, []port.ClosedFigure{w1, w2, other}, port.CrosswalkTarget{})
	by := map[string]port.TargetFigure{}
	for _, o := range out {
		by[o.TargetKey+"/"+o.FormCode] = o
	}
	a, b, c := by["2026-W37/BM-04"], by["2026-W38/BM-04"], by["2026-W38/BM-17"]
	if inc := Increment(a.Last, b.Last); inc["heo_luy_ke"] != "30.5" {
		t.Fatalf("số tăng: %v", inc)
	}
	if !a.Closed || !a.KeyClosed || !b.Closed || b.KeyClosed || c.Closed || c.KeyClosed {
		t.Fatalf("cờ chốt: %+v %+v %+v", a, b, c)
	}
	if _, err := Convert(nil, port.CrosswalkTarget{Version: "kn.crosswalk/v9"}); err == nil {
		t.Fatal("phiên bản lạ phải lỗi")
	}
}

package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// BG-FN-TW-01.02 AC-02: ngưỡng đi ngang 1 %, Giá trung bình 10.000 → 10.080 (+0,8 %) "Đi ngang"; → 10.100 (+1,0 %) "Tăng";
// giảm; kỳ trước không có giá → không so được.
func TestBG_FN_TW_01_02_TrendFlatThreshold(t *testing.T) {
	d := decimal.RequireFromString
	flat := d("1")
	for _, c := range []struct {
		prev, cur, pct, want string
	}{
		{"10000", "10080", "0.8", TrendFlat},
		{"10000", "10100", "1", TrendUp},
		{"10000", "9920", "-0.8", TrendFlat},
		{"10000", "9900", "-1", TrendDown},
		{"10666.6667", "10833.3333", "1.5625", TrendUp},
	} {
		got, pct, ok := Trend(d(c.prev), d(c.cur), flat)
		if !ok || got != c.want || !pct.Equal(d(c.pct)) {
			t.Errorf("%s → %s: %s %s %v (muốn %s %s)", c.prev, c.cur, got, pct, ok, c.want, c.pct)
		}
	}
	if got, _, ok := Trend(decimal.Zero, d("10"), flat); ok || got != TrendNone {
		t.Errorf("kỳ trước không có giá: %s %v", got, ok)
	}
	// BR-02: giá kỳ tuần / tháng = trung bình Giá trung bình các ngày công bố.
	if m, ok := Mean([]decimal.Decimal{d("10000"), d("10100"), d("10200")}); !ok || !m.Equal(d("10100")) {
		t.Errorf("Mean = %s %v", m, ok)
	}
	if _, ok := Mean(nil); ok {
		t.Error("Mean rỗng phải false")
	}
	if from, to := MonthOf(time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC)); from.Format(time.DateOnly) != "2026-02-01" ||
		to.Format(time.DateOnly) != "2026-02-28" {
		t.Errorf("MonthOf: %s %s", from, to)
	}
}

// BG-FN-TW-01.01 AC-01: ba xã Dưa leo 8.000–10.000, 9.000–11.000, 12.000–14.000 → Thấp nhất 8.000, Cao nhất 14.000, Trung
// bình 10.667 (4 số lẻ: 10.666,6667), Số xã có giá 3.
func TestBG_FN_TW_01_01_SummaryBoard(t *testing.T) {
	d := decimal.RequireFromString
	src := []BoardSource{
		{Point: "XBG0001", Item: "MH00001", Min: d("8000"), Max: d("10000"), Mid: Mid(d("8000"), d("10000"))},
		{Point: "XBG0002", Item: "MH00001", Min: d("9000"), Max: d("11000"), Mid: Mid(d("9000"), d("11000"))},
		{Point: "XBG0003", Item: "MH00001", Min: d("12000"), Max: d("14000"), Mid: Mid(d("12000"), d("14000"))},
	}
	got := AggregateBoard(src)
	if len(got) != 1 || !got[0].Min.Equal(d("8000")) || !got[0].Max.Equal(d("14000")) || !got[0].Avg.Equal(d("10666.6667")) ||
		got[0].Points != 3 || got[0].Avg.Round(0).String() != "10667" {
		t.Fatalf("CT-04: %+v", got)
	}
}

// BG-FN-TI-02.08 AC-01: tháng 22 ngày báo giá, cán bộ Xã gửi đúng hạn 19, nộp muộn 1, không nộp 2 → Phải nộp 22, Đúng
// hạn 19, Nộp muộn 1, Không nộp 2; BR-02 phiếu nhập thay tính cho cán bộ phụ trách tại ngày; cán bộ đổi giữa kỳ → mỗi cán
// bộ một dòng; phiếu bị xóa, dòng hiệu chỉnh cộng dồn.
func TestBG_FN_TI_02_08_TicketStatsPerOfficer(t *testing.T) {
	a, b := uuid.MustParse("00000000-0000-0000-0000-00000000000a"), uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	day0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var days []StatDay
	for i := 0; i < 22; i++ {
		r := StatOnTime
		switch i {
		case 5:
			r = StatLate
		case 9, 17:
			r = StatMissed
		}
		sd := StatDay{Day: day0.AddDate(0, 0, i), Point: "XBG0001", Region: "an_giang", Officer: a, Result: r}
		if i == 3 {
			sd.Deleted, sd.Adjusted = 1, 2
		}
		days = append(days, sd)
	}
	// Xã 2 đổi cán bộ giữa kỳ: b phụ trách 3 ngày đầu, a 2 ngày sau.
	for i := 0; i < 5; i++ {
		off := b
		if i >= 3 {
			off = a
		}
		days = append(days, StatDay{Day: day0.AddDate(0, 0, i), Point: "XBG0002", Region: "kien_giang", Officer: off, Result: StatOf(TicketLate)})
	}
	rows := TallyStats(days)
	if len(rows) != 3 {
		t.Fatalf("số dòng: %+v", rows)
	}
	r := rows[0]
	if r.Officer != a || r.Point != "XBG0001" || r.Required != 22 || r.OnTime != 19 || r.Late != 1 || r.Missed != 2 || r.Deleted != 1 ||
		r.AdjustedLines != 2 {
		t.Fatalf("AC-01: %+v", r)
	}
	if rows[1].Officer != a || rows[1].Point != "XBG0002" || rows[1].Required != 2 || rows[1].Late != 2 ||
		rows[2].Officer != b || rows[2].Required != 3 || rows[2].Late != 3 {
		t.Fatalf("cán bộ đổi giữa kỳ: %+v", rows[1:])
	}
	if StatOf(TicketSubmitted) != StatOnTime || StatOf(TicketDraft) != StatMissed || StatOf("") != StatMissed {
		t.Fatal("StatOf")
	}
}

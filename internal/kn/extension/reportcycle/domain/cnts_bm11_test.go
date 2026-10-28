package domain

import (
	"slices"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func mustDec(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBM11Next(t *testing.T) {
	ok := [][3]string{
		{"", BM11EvCreated, BM11Draft}, {BM11Draft, BM11EvEdited, BM11Draft}, {BM11Returned, BM11EvRefreshed, BM11Draft},
		{BM11Draft, BM11EvSubmitted, BM11Pending}, {BM11Returned, BM11EvSubmitted, BM11Pending}, {BM11Pending, BM11EvReturned, BM11Returned},
		{BM11Pending, BM11EvApproved, BM11Sent}, {BM11Sent, BM11EvReceived, BM11Sent},
	}
	for _, c := range ok {
		if got, ok := BM11Next(c[0], c[1]); !ok || got != c[2] {
			t.Fatalf("%s + %s = %q %v, muốn %s", c[0], c[1], got, ok, c[2])
		}
	}
	bad := [][2]string{{BM11Draft, BM11EvCreated}, {BM11Pending, BM11EvEdited}, {BM11Sent, BM11EvEdited}, {BM11Draft, BM11EvApproved},
		{BM11Sent, BM11EvReturned}, {BM11Pending, BM11EvReceived}, {"", BM11EvSubmitted}}
	for _, c := range bad {
		if _, ok := BM11Next(c[0], c[1]); ok {
			t.Fatalf("%s + %s phải bị chặn (đã gửi không sửa — BR-02; chỉ duyệt/trả lại khi Chờ duyệt)", c[0], c[1])
		}
	}
}

func TestBM11Recipients(t *testing.T) {
	if r := BM11DefaultRecipients(BM11Week); !slices.Equal(r, []string{"bgd", "hcth"}) {
		t.Fatalf("tuần: %v", r)
	}
	if r := BM11DefaultRecipients(BM11Quarter); !slices.Equal(r, []string{"bgd", "khtc"}) {
		t.Fatalf("quý: %v", r)
	}
	if r, err := BM11NormalizeRecipients(BM11Month, []string{"khtc", "hcth"}); err != nil || !slices.Equal(r, []string{"hcth", "khtc"}) {
		t.Fatalf("sắp theo thứ tự in: %v %v", r, err)
	}
	if _, err := BM11NormalizeRecipients(BM11Month, []string{"chi_cuc"}); err == nil {
		t.Fatal("phòng không gửi báo cáo cho Chi cục — nơi nhận lạ phải bị chặn")
	}
}

func TestBM11CalendarRangeAndWeeks(t *testing.T) {
	from, to, label, err := BM11CalendarRange(BM11Quarter, "2026-Q3")
	if err != nil || !from.Equal(Date(2026, 7, 1)) || !to.Equal(Date(2026, 9, 30)) || label != "Quý III/2026" {
		t.Fatalf("quý: %v %v %q %v", from, to, label, err)
	}
	if _, _, _, err := BM11CalendarRange(BM11Month, "2026-13"); err == nil {
		t.Fatal("tháng 13 phải lỗi")
	}
	cal := ResolveCalendar(DeptChanNuoiThuySan, "", "")
	from, to, _, _ = BM11CalendarRange(BM11Month, "2026-09")
	ws, err := BM11WeeksEnding(cal.Rule, from, to)
	if err != nil || len(ws) == 0 {
		t.Fatalf("tuần tháng 9: %v %v", ws, err)
	}
	for _, w := range ws {
		if w.To.Before(from) || w.To.After(to) {
			t.Fatalf("tuần %s có ngày cuối %v ngoài tháng 9", w.Key, w.To)
		}
	}
	// Tuần 40 (29/09–05/10) kết thúc tháng 10 → thuộc tháng 10, không thuộc tháng 9 (quy tắc ngày cuối tuần W11.44).
	if slices.ContainsFunc(ws, func(w Span) bool { return w.Key == "2026-W40" }) {
		t.Fatalf("tuần 40 không thuộc tháng 9: %+v", ws)
	}
	if due := BM11DueAt(ws[len(ws)-1], time.UTC); due.Hour() != 12 || !due.After(ws[len(ws)-1].To) {
		t.Fatalf("hạn: %v", due)
	}
}

func TestBM11CoverageAndLivestock(t *testing.T) {
	w1 := Span{Key: "2026-W39", From: Date(2026, 9, 22), To: Date(2026, 9, 28)}
	w2 := Span{Key: "2026-W40", From: Date(2026, 9, 29), To: Date(2026, 10, 5)}
	ps := []SourcePeriod{
		{Key: w1.Key, From: w1.From, To: w1.To, Status: "closed", Units: []string{"X1", "X2", "X3"}, Data: map[string]SourceUnit{
			"X1": {Values: map[string]string{"tong_dan_heo": "100", "tiem_ta_heo_cp": "5", "tiem_lmlm_heo": "2", "hoa_chat_gia_suc": "1.5"}},
			"X2": {Values: map[string]string{"tong_dan_heo": "50", "ksgm_gia_cam": "7"}},
			"X3": {NilReport: true},
		}},
		{Key: w2.Key, From: w2.From, To: w2.To, Status: "open", Units: []string{"X1", "X2", "X3"}, Data: map[string]SourceUnit{
			"X1": {Values: map[string]string{"tong_dan_heo": "110", "tiem_ta_heo_cp": "3", "hoa_chat_gia_suc": "0.25"}},
			"X3": {Values: map[string]string{"tong_dan_heo": "20"}},
		}},
		{Key: "2026-W41", From: Date(2026, 10, 6), To: Date(2026, 10, 12), Units: []string{"X1"}, Data: map[string]SourceUnit{
			"X1": {Values: map[string]string{"tong_dan_heo": "999"}}}},
	}
	weeks := []Span{w1, w2}
	c := BM11CoverageOf(CNTSBM04, weeks, ps)
	// AC-03: n/N — X2 thiếu tuần 40, không ước tính phần thiếu.
	if c.Expected != 3 || c.Reported != 2 || !slices.Equal(c.Missing, []string{"X2"}) || c.Periods != 2 || c.Closed != 1 || c.AllClosed() {
		t.Fatalf("độ phủ: %+v", c)
	}
	if c.Pct() != 66 {
		t.Fatalf("tỷ lệ: %d", c.Pct())
	}
	l := BM11Livestock(weeks, ps)
	// Tổng đàn = tuần cuối của kỳ (110 + 20), không cộng dồn tuần; tuần 41 ngoài kỳ không tính.
	if l["tong_dan_heo"].String() != "130" {
		t.Fatalf("tổng đàn heo: %s", l["tong_dan_heo"])
	}
	// Tiêm phòng heo = tổng các tuần của 4 cột heo; dòng con giữ số từng cột.
	if l["tiem_heo"].String() != "10" || l["tiem_ta_heo_cp"].String() != "8" || l["tiem_lmlm_heo"].String() != "2" {
		t.Fatalf("tiêm phòng: %v %v %v", l["tiem_heo"], l["tiem_ta_heo_cp"], l["tiem_lmlm_heo"])
	}
	if l["hoa_chat_gia_suc"].String() != "1.75" || l["ksgm_gia_cam"].String() != "7" {
		t.Fatalf("hóa chất/KSGM: %v %v", l["hoa_chat_gia_suc"], l["ksgm_gia_cam"])
	}
	if got, ok := BM11Communes([]BM11Coverage{{Form: CNTSBM17, Periods: 1, Expected: 5, Reported: 5}, c}); !ok || got.Form != CNTSBM04 {
		t.Fatalf("biểu chính của 'Tổng hợp từ n/N xã' là BM-04: %+v", got)
	}
}

func TestBM11DiffAndText(t *testing.T) {
	sent := BM11Content{Livestock: map[string]string{"tong_dan_heo": "100", "tiem_heo": "5"}, Fishery: BM11Catch{CatchCur: "2.5"}}
	now := Dec{}
	for _, l := range BM11Lines {
		now[l.Key] = mustDec(t, "0")
	}
	now["tong_dan_heo"] = mustDec(t, "100")
	now["tiem_heo"] = mustDec(t, "7")
	d := BM11Diff("Tuần 39", sent, now, BM11Catch{CatchCur: "2.50", AquaCur: "1"})
	if len(d) != 2 || d[0].Key != "tiem_heo" || d[0].Sent != "5" || d[0].Now != "7" || d[1].Key != "nuoi_trong" {
		t.Fatalf("bổ sung: %+v", d)
	}
	if _, ok := BM11CheckText(string(make([]rune, BM11MaxSectionRunes+1))); ok {
		t.Fatal("đoạn viết quá dài phải bị chặn")
	}
}

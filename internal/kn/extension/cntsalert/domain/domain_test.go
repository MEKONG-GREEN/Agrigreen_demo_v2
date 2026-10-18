package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestChangeFindingsHerd(t *testing.T) {
	pct := d("15")
	cur := map[string]string{"tong_dan_heo": "116", "tong_dan_bo": "84", "tong_dan_ga": "10", "tong_dan_vit": "50"}
	prev := map[string]string{"tong_dan_heo": "100", "tong_dan_bo": "100", "tong_dan_ga": "0", "tong_dan_vit": "50"}
	fs := ChangeFindings(cur, prev, HerdFields, pct)
	got := map[string]Finding{}
	for _, f := range fs {
		got[f.Field] = f
	}
	if len(fs) != 3 {
		t.Fatalf("muốn 3 vi phạm (heo +16 %%, bò −16 %%, gà phát sinh mới), có %d: %+v", len(fs), fs)
	}
	if got["tong_dan_heo"].Change == nil || !got["tong_dan_heo"].Change.Equal(d("16")) {
		t.Fatalf("heo: %+v", got["tong_dan_heo"])
	}
	if !got["tong_dan_ga"].NewEntry {
		t.Fatal("gà: kỳ gần nhất = 0, kỳ này > 0 → Phát sinh mới")
	}
	// Đúng 15 % không cảnh báo ("quá 15 %").
	if fs := ChangeFindings(map[string]string{"tong_dan_heo": "115"}, map[string]string{"tong_dan_heo": "100"}, HerdFields, pct); len(fs) != 0 {
		t.Fatalf("đúng ngưỡng không cảnh báo: %+v", fs)
	}
	// Không có kỳ gần nhất → không cảnh báo.
	if fs := ChangeFindings(cur, nil, HerdFields, pct); fs != nil {
		t.Fatal("thiếu kỳ gần nhất phải không cảnh báo")
	}
}

func TestSlaughterSpike(t *testing.T) {
	hist := make([]map[string]string, 6)
	for i := range hist {
		hist[i] = map[string]string{"heo": "10", "trau_bo": "2", "gia_cam": "0", "de": "0"}
	}
	cur := map[string]string{"heo": "31", "trau_bo": "6", "gia_cam": "5", "de": "0"}
	if fs := SlaughterSpike(cur, hist, d("3"), 7); fs != nil {
		t.Fatal("dưới 7 ngày có số liệu: không cảnh báo")
	}
	hist = append(hist, map[string]string{"heo": "10", "trau_bo": "2", "gia_cam": "0", "de": "0"})
	fs := SlaughterSpike(cur, hist, d("3"), 7)
	if len(fs) != 2 { // heo 31 > 30; gia cầm 5 > 0; trâu bò 6 = 3 × 2 không vượt
		t.Fatalf("muốn heo + gia cầm, có %+v", fs)
	}
}

func TestSlaughterZeroAndMismatch(t *testing.T) {
	zero := map[string]string{"trau_bo": "0", "heo": "0", "gia_cam": "0", "de": "0"}
	if !SlaughterZero(zero, false) || SlaughterZero(zero, true) {
		t.Fatal("KSGM = 0 khi hoạt động → cảnh báo; không hoạt động → không")
	}
	bm04 := map[string]string{"ksgm_gia_suc": "12", "ksgm_gia_cam": "40"}
	bm01 := []map[string]string{{"trau_bo": "1", "heo": "5", "de": "0", "gia_cam": "20"}, {"trau_bo": "0", "heo": "6", "de": "0", "gia_cam": "20"}}
	if fs := KSGMMismatch(bm04, bm01); len(fs) != 0 {
		t.Fatalf("khớp → không cảnh báo: %+v", fs)
	}
	bm01[1]["heo"] = "7"
	if fs := KSGMMismatch(bm04, bm01); len(fs) != 1 || fs[0].Field != "ksgm_gia_suc" {
		t.Fatalf("lệch gia súc: %+v", fs)
	}
	if fs := KSGMMismatch(bm04, nil); fs != nil {
		t.Fatal("chưa có BM-01 → không so")
	}
}

func TestLogRules(t *testing.T) {
	lo, hi := d("7.5"), d("8.5")
	if _, ok := IndicatorOutOfRange("pH", d("8.6"), &lo, &hi); !ok {
		t.Fatal("pH 8,6 ngoài 7,5–8,5 (AC-02)")
	}
	if _, ok := IndicatorOutOfRange("pH", d("8.5"), &lo, &hi); ok {
		t.Fatal("pH 8,5 trong ngưỡng")
	}
	std, prev := d("500"), d("480")
	if _, ok := LowGrowth(d("490"), &std, nil); !ok {
		t.Fatal("dưới chuẩn")
	}
	if _, ok := LowGrowth(d("480"), nil, &prev); !ok {
		t.Fatal("không chuẩn: không tăng so lần trước → cảnh báo")
	}
	if _, ok := MortalityExceeded(6, 100, nil); ok {
		t.Fatal("chưa có ngưỡng → không cảnh báo")
	}
	m := d("5")
	if _, ok := MortalityExceeded(6, 100, &m); !ok {
		t.Fatal("6 % > 5 %")
	}
	dist, acc := d("250"), d("10")
	if _, ok := LocationIssue(&dist, &acc, d("200"), d("50")); !ok {
		t.Fatal("cách 250 m > 200 m")
	}
	if _, ok := LocationIssue(nil, nil, d("200"), d("50")); !ok {
		t.Fatal("điểm chưa có tọa độ → báo chủ nhiệm")
	}
	day := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	if LogOverdue(day, day.AddDate(0, 0, 8), 7, 1) || !LogOverdue(day, day.AddDate(0, 0, 9), 7, 1) {
		t.Fatal("quá tần suất + 1 ngày")
	}
	if !VaccineOverdue(day, nil, day.AddDate(0, 0, 1)) || VaccineOverdue(day, &day, day.AddDate(0, 0, 1)) {
		t.Fatal("mũi quá hạn chưa có ngày thực tế")
	}
	if LateStreak([]bool{true, true, false, true}) != 2 {
		t.Fatal("trễ liên tiếp tính từ kỳ mới nhất")
	}
}

func TestTransition(t *testing.T) {
	cases := []struct {
		cur, act, want string
		err            bool
	}{
		{StatusNew, ActionAssigned, StatusAssigned, false},
		{StatusNew, ActionStarted, "", true},
		{StatusAssigned, ActionStarted, StatusInProgress, false},
		{StatusInProgress, ActionAssigned, StatusAssigned, false},
		{StatusInProgress, ActionClosed, StatusClosed, false},
		{StatusNew, ActionAutoClosed, StatusClosed, false},
		{StatusAssigned, ActionOverdue, StatusAssigned, false},
		{StatusClosed, ActionClosed, "", true},
		{StatusClosed, ActionAssigned, "", true},
	}
	for _, c := range cases {
		got, err := Transition(c.cur, c.act)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("%s + %s = %q, %v; muốn %q", c.cur, c.act, got, err, c.want)
		}
	}
	if len(Kinds) != 13 || Kinds[11].No != 12 || Kinds[11].Level != LevelUrgent {
		t.Fatal("bảng 12 cảnh báo Mục III.5 + trễ liên tiếp")
	}
}

func TestFormReminderConfig(t *testing.T) {
	r := DefaultFormReminder(FormBM01)
	if r.LateStreak != 3 || DefaultFormReminder(FormBM04).LateStreak != 2 {
		t.Fatal("mặc định trễ liên tiếp: BM-01 3 kỳ, biểu tuần 2 kỳ")
	}
	r.Channels = []string{"email"}
	if es := r.Normalize(); len(es) != 0 || r.Channels[0] != ChannelInApp || len(r.Channels) != 2 {
		t.Fatalf("in_app luôn có: %v %v", es, r.Channels)
	}
	bad := FormReminder{BeforeDueHours: -1, RepeatDaily: true, RepeatAt: "25:00", Channels: []string{"sms"}}
	if es := bad.Normalize(); len(es) != 3 {
		t.Fatalf("muốn 3 lỗi, có %v", es)
	}
	loc := time.FixedZone("ICT", 7*3600)
	due := time.Date(2026, 10, 13, 5, 0, 0, 0, time.UTC)
	if got := DefaultFormReminder(FormBM04).Render("BM-04", "T41", due, loc); got != "Nhắc nộp BM-04 kỳ T41, hạn 12:00 13/10/2026." {
		t.Fatal(got)
	}
	s := AlertSetting{SLAHours: 24, Urgent: true}
	if es := s.Normalize(KindHerdChange); len(es) != 1 {
		t.Fatal("mức tổng đàn cố định")
	}
	if es := s.Normalize(KindWaterQuality); len(es) != 0 || LevelFor(KindWaterQuality, s) != LevelUrgent {
		t.Fatal("môi trường nước: Cấp Tỉnh đặt Khẩn được")
	}
}

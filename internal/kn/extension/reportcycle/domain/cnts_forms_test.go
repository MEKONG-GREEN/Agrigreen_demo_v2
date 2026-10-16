package domain

import (
	"testing"
	"time"
)

func TestSplitCNTS_W1142(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	bm := func(code string) CNTSForm {
		f, ok := CNTSFormByCode(code)
		if !ok {
			t.Fatalf("không có %s", code)
		}
		return f
	}
	// BM-01: kỳ ngày, hạn 09:00 D+1 giờ VN.
	ps, err := SplitCNTS(CNTSSeriesInput{Form: bm(CNTSBM01), From: Date(2026, 10, 1), To: Date(2026, 10, 3), Rule: DefaultRule, Loc: loc})
	if err != nil || len(ps) != 3 || ps[0].Key != "2026-10-01" || ps[0].Kind != PeriodDay ||
		!ps[0].DueAt.Equal(time.Date(2026, 10, 2, 9, 0, 0, 0, loc)) {
		t.Fatalf("BM-01: %+v %v", ps, err)
	}
	if _, err := SplitCNTS(CNTSSeriesInput{Form: bm(CNTSBM01), From: Date(2026, 10, 1), To: Date(2026, 10, 3), Rule: LegacyRule, Loc: loc}); err == nil {
		t.Fatal("quy tắc v1 không chia kỳ ngày")
	}
	// BM-04: tuần Thứ Tư – Thứ Ba đầy đủ (mở rộng khoảng), hạn 12:00 Thứ Tư sau.
	ps, err = SplitCNTS(CNTSSeriesInput{Form: bm(CNTSBM04), From: Date(2026, 10, 9), To: Date(2026, 10, 15), Rule: DefaultRule, Loc: loc})
	if err != nil || len(ps) != 2 || ps[0].From.Weekday() != time.Wednesday || ps[0].To.Weekday() != time.Tuesday ||
		!ps[0].From.Equal(Date(2026, 10, 7)) || !ps[0].DueAt.Equal(time.Date(2026, 10, 14, 12, 0, 0, 0, loc)) {
		t.Fatalf("BM-04: %+v %v", ps, err)
	}
	// BM-05: tuần làm việc; Phần I hạn Thứ Hai sau, Phần II hạn Thứ Sáu trước.
	ps, err = SplitCNTS(CNTSSeriesInput{Form: bm(CNTSBM05I), From: Date(2026, 10, 12), To: Date(2026, 10, 18), Rule: DefaultRule, Loc: loc})
	if err != nil || len(ps) != 1 || ps[0].Key != "2026-LV42" || !ps[0].To.Equal(Date(2026, 10, 16)) ||
		!ps[0].DueAt.Equal(time.Date(2026, 10, 19, 17, 0, 0, 0, loc)) {
		t.Fatalf("BM-05 I: %+v %v", ps, err)
	}
	ps, err = SplitCNTS(CNTSSeriesInput{Form: bm(CNTSBM05II), From: Date(2026, 10, 12), To: Date(2026, 10, 16), Rule: DefaultRule, Loc: loc})
	if err != nil || len(ps) != 1 || !ps[0].DueAt.Equal(time.Date(2026, 10, 9, 17, 0, 0, 0, loc)) {
		t.Fatalf("BM-05 II: %+v %v", ps, err)
	}
	// Hạn khác mặc định.
	ps, _ = SplitCNTS(CNTSSeriesInput{Form: bm(CNTSBM01), From: Date(2026, 10, 1), To: Date(2026, 10, 1), Rule: DefaultRule,
		Deadline: &Deadline{OffsetDays: 1, Time: "10:30"}, Loc: loc})
	if !ps[0].DueAt.Equal(time.Date(2026, 10, 2, 10, 30, 0, 0, loc)) {
		t.Fatalf("hạn riêng: %v", ps[0].DueAt)
	}
}

func TestValidateCNTS_W1142(t *testing.T) {
	vals := map[string]string{"tong_dan_heo": "100", "tong_dan_trau": "3", "tong_dan_bo": "2", "tong_dan_vit": "0", "tong_dan_ga": "10",
		"tiem_ta_heo_cp": "100", "tiem_lmlm_bo": "5", "tiem_cum_gia_cam": "11", "tiem_dai": "0"}
	is := ValidateCNTS(CNTSBM04, vals)
	if len(is) != 1 || is[0].Field != "tiem_cum_gia_cam" {
		t.Fatalf("liều > tổng đàn: %+v", is)
	}
	b5 := map[string]string{"t2_loai": WorkTypes[1], "t3_ket_qua": string(make([]rune, 501))}
	if is := ValidateCNTS(CNTSBM05I, b5); len(is) != 2 {
		t.Fatalf("BM-05: %+v", is)
	}
	for _, f := range CNTSForms {
		seen := map[string]bool{}
		for _, x := range f.Fields {
			if seen[x.Key] || len(x.Key) > 40 {
				t.Fatalf("%s: khóa %s", f.Code, x.Key)
			}
			seen[x.Key] = true
		}
	}
}

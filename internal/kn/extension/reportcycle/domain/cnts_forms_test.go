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
	// BM-05: tuần Thứ Hai–Chủ Nhật (bảng T2–CN, UP-06); Phần I hạn 08:00 Thứ Hai sau, Phần II hạn 17:00 Thứ Sáu trước.
	ps, err = SplitCNTS(CNTSSeriesInput{Form: bm(CNTSBM05I), From: Date(2026, 10, 12), To: Date(2026, 10, 18), Rule: DefaultRule, Loc: loc})
	if err != nil || len(ps) != 1 || ps[0].Key != "2026-LV42" || !ps[0].To.Equal(Date(2026, 10, 18)) ||
		!ps[0].DueAt.Equal(time.Date(2026, 10, 19, 8, 0, 0, 0, loc)) {
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

// BF-CN-WEEKLY (UP-06): bảng T2–CN — Thứ Bảy, Chủ Nhật không bắt buộc nhưng đã chọn loại thì phải ghi nội dung; Phần II
// đi công tác ("Công tác khác") bắt buộc Địa điểm; mỗi ô ≤ 500 ký tự.
func TestValidateCNTS_BM05Weekly(t *testing.T) {
	for _, code := range []string{CNTSBM05I, CNTSBM05II} {
		f, _ := CNTSFormByCode(code)
		req := map[string]bool{}
		for _, x := range f.Fields {
			req[x.Key] = x.Required
		}
		if !req["t2_loai"] || !req["t6_loai"] || req["t7_loai"] || req["cn_loai"] {
			t.Fatalf("%s: Thứ Hai–Thứ Sáu bắt buộc, Thứ Bảy/Chủ Nhật không: %v", code, req)
		}
	}
	plan := map[string]string{"t2_loai": WorkTypes[0], "t2_ke_hoach": "Kiểm tra đàn", "t2_ket_qua_du_kien": "Xong",
		"t3_loai": WorkTypeOther, "t3_cong_tac_khac": "Họp tại tỉnh", "t3_ke_hoach": "Họp", "t3_ket_qua_du_kien": "Nắm chỉ đạo",
		"t7_loai": WorkTypes[0]}
	got := map[string]bool{}
	for _, is := range ValidateCNTS(CNTSBM05II, plan) {
		got[is.Field] = true
	}
	if len(got) != 3 || !got["t3_dia_diem"] || !got["t7_ke_hoach"] || !got["t7_ket_qua_du_kien"] {
		t.Fatalf("Phần II: %v", got)
	}
	plan["t3_dia_diem"], plan["t7_ke_hoach"], plan["t7_ket_qua_du_kien"] = "Sở NN&MT", "Trực", "Đủ"
	if is := ValidateCNTS(CNTSBM05II, plan); len(is) != 0 {
		t.Fatalf("Phần II hợp lệ: %+v", is)
	}
	// Phần I: không đòi Địa điểm; Chủ Nhật đã chọn loại phải ghi kết quả.
	res := map[string]string{"t3_loai": WorkTypeOther, "t3_cong_tac_khac": "Họp", "cn_loai": WorkTypes[0]}
	if is := ValidateCNTS(CNTSBM05I, res); len(is) != 1 || is[0].Field != "cn_ket_qua" {
		t.Fatalf("Phần I: %+v", is)
	}
	days := PlanDays(plan)
	if len(days) != 3 || days[0].Offset != 0 || days[1].Place != "Sở NN&MT" || days[2].Offset != 5 || days[2].Label != "Thứ Bảy" {
		t.Fatalf("PlanDays: %+v", days)
	}
}

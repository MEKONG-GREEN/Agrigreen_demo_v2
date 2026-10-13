package domain

// Biểu kỳ Cấp Xã Phòng Chăn nuôi – Thủy sản (W11.42, BRD CNTS v3.7 Mục III.3.0, UR-XA-06…09 — D4 CN2; Q-045).
//
// Đường kỳ: kỳ NATIVE của `cap/forms` (profile kn_cnts, W11.05) — không phải vòng đời KN v1 (report_batch ×
// report_task): công việc của reportcycle là mẫu × XÃ × kỳ, còn BM-01 cần đơn vị = cơ sở giết mổ; kỳ native có sẵn
// đơn vị lá tổng quát (xã | cơ sở | hộ), trạng thái Không nộp / Đã chốt – chưa điều chỉnh / Chờ mở kỳ, hủy kỳ và ảnh
// chụp đóng băng (D4 §X4, đề xuất "chuyển kỳ CNTS thẳng sang kỳ native cap/forms" của bản đối chiếu). pf/extension chỉ
// TÍNH LỊCH KỲ (quy tắc W11.03 v2 — kỳ ngày, tuần mới, hạn riêng từng biểu) và định nghĩa biểu; phát hành, nộp, chốt
// qua formsport.Publisher và /v1/forms.
//
// Mỗi biểu là một mã biểu cap/forms (phiên bản do Admin cài, Cấp Tỉnh sửa được bằng phiên bản mới — UR-TINH-07):
//   - BM-01 Báo cáo KSGM hằng ngày: đơn vị = CƠ SỞ GIẾT MỔ (facility_ref W11.41), kỳ ngày, hạn 09:00 D+1;
//   - BM-04 Nhập liệu tuần chăn nuôi: đơn vị = xã, kỳ tuần mới (W11.03 v2), hạn 12:00 ngày sau ngày cuối tuần;
//   - BM-17 Sản lượng khai thác thủy sản: như BM-04 (cùng kỳ, cùng hạn);
//   - BM-05 Công việc & kế hoạch tuần: hai biểu (Phần I kết quả tuần làm việc Thứ Hai–Thứ Sáu, hạn 17:00 Thứ Hai
//     sau tuần; Phần II kế hoạch tuần sau, hạn 17:00 Thứ Sáu trước tuần) — "mỗi phần là một kỳ riêng". Đơn vị = xã
//     phụ trách của cán bộ tăng cường (đơn vị = người của cap/forms còn CHỪA CHỖ — giả định, xem gia-dinh W11.42).

import (
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"
)

// ProfileCNTS là profile cap/forms của Phòng CNTS (cmd/api-platform/assembly KNCNTSProfile).
const ProfileCNTS = "kn_cnts"

// Mã biểu cap/forms của các biểu kỳ Cấp Xã CNTS.
const (
	CNTSBM01   = "CNTS-BM01"
	CNTSBM04   = "CNTS-BM04"
	CNTSBM05I  = "CNTS-BM05-I"
	CNTSBM05II = "CNTS-BM05-II"
	CNTSBM17   = "CNTS-BM17"
)

// Loại đơn vị lá (khớp forms/port.Unit*).
const (
	UnitCommune  = "commune"
	UnitFacility = "facility"
)

// CNTSRuleWorkWeek là thẻ quy tắc của kỳ tuần làm việc BM-05 (Thứ Hai – Thứ Sáu, khóa "YYYY-LVww" theo tuần ISO).
const CNTSRuleWorkWeek = "cnts.work_week/v1"

// CNTSField là một trường biểu (khớp forms/domain.Field — JSON gửi sang cap/forms).
type CNTSField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Options     []string `json:"options,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	NonNegative bool     `json:"non_negative,omitempty"`
	Group       string   `json:"group,omitempty"`
}

// CNTSForm là một biểu kỳ Cấp Xã.
type CNTSForm struct {
	Code       string
	BM         string // mã biểu BRD (BM-01…)
	Title      string
	UnitKind   string
	PeriodType string   // PeriodDay | PeriodWeek (BM-05: tuần làm việc riêng)
	Deadline   Deadline // hạn mặc định (BM-05 II: OffsetDays âm tính từ ngày ĐẦU tuần)
	Due        string   // mô tả hạn (hiển thị)
	NilLabel   string   // nhãn "Báo không phát sinh" của biểu
	Fields     []CNTSField
}

func cntsInt(key, label, unit, group string) CNTSField {
	return CNTSField{Key: key, Label: label, Type: "integer", Required: true, Unit: unit, NonNegative: true, Group: group}
}

func cntsDec(key, label, unit, group string) CNTSField {
	return CNTSField{Key: key, Label: label, Type: "decimal", Required: true, Unit: unit, NonNegative: true, Group: group}
}

// WorkTypes là lựa chọn loại công việc của BM-05 (CV-02, CV-04).
var WorkTypes = []string{"Làm việc chuyên môn thường xuyên tại xã", "Công tác khác"}

// WorkDays là 5 ngày làm việc của BM-05 (khóa trường, nhãn).
var WorkDays = [5][2]string{{"t2", "Thứ Hai"}, {"t3", "Thứ Ba"}, {"t4", "Thứ Tư"}, {"t5", "Thứ Năm"}, {"t6", "Thứ Sáu"}}

// Nhóm cột tiêm phòng của BM-04 (CN-02, mẫu 2026: 9 cột, "Dịch tả – THT heo" một cột) và loài so sánh (Mục III.5 —
// bảng ánh xạ vaccine – loài: số liều trong tuần ≤ tổng đàn loài tương ứng). Loài so sánh là tổng các trường tổng đàn.
var bm04Vaccines = []struct {
	Key, Label string
	Herd       []string
}{
	{"tiem_ta_heo_cp", "Tả heo Châu Phi", []string{"tong_dan_heo"}},
	{"tiem_dich_ta_tht_heo", "Dịch tả – THT heo", []string{"tong_dan_heo"}},
	{"tiem_lmlm_heo", "LMLM heo", []string{"tong_dan_heo"}},
	{"tiem_tai_xanh_heo", "Tai xanh heo", []string{"tong_dan_heo"}},
	{"tiem_vdnc_bo", "Viêm da nổi cục bò", []string{"tong_dan_trau", "tong_dan_bo"}},
	{"tiem_tht_bo", "THT bò", []string{"tong_dan_trau", "tong_dan_bo"}},
	{"tiem_lmlm_bo", "LMLM bò", []string{"tong_dan_trau", "tong_dan_bo"}},
	{"tiem_cum_gia_cam", "Cúm gia cầm", []string{"tong_dan_vit", "tong_dan_ga"}},
	{"tiem_dai", "Dại chó, mèo", []string{"tong_dan_cho_meo"}},
}

func bm04Fields() []CNTSField {
	g1, g2, g3, g4, g5 := "CN-01 Tổng đàn hiện có", "CN-02 Tiêm phòng trong tuần", "CN-04 Hóa chất trong tuần", "CN-06 KSGM trong tuần",
		"CN-08/09 Vịt chạy đồng"
	fs := []CNTSField{
		cntsInt("tong_dan_heo", "Heo", "con", g1), cntsInt("tong_dan_de", "Dê", "con", g1), cntsInt("tong_dan_trau", "Trâu", "con", g1),
		cntsInt("tong_dan_bo", "Bò", "con", g1), cntsInt("tong_dan_vit", "Vịt", "con", g1), cntsInt("tong_dan_ga", "Gà", "con", g1),
		cntsInt("tong_dan_cho_meo", "Chó mèo", "con", g1), cntsInt("tong_dan_yen", "Yến", "nhà", g1),
	}
	for _, v := range bm04Vaccines {
		fs = append(fs, cntsInt(v.Key, v.Label, "liều", g2))
	}
	return append(fs,
		cntsDec("hoa_chat_gia_suc", "Gia súc", "lít", g3), cntsDec("hoa_chat_gia_cam", "Gia cầm", "lít", g3),
		cntsInt("ksgm_gia_suc", "Gia súc", "con", g4), cntsInt("ksgm_gia_cam", "Gia cầm", "con", g4),
		cntsInt("vit_chay_dong_dia_phuong", "Địa phương", "con", g5), cntsInt("vit_chay_dong_noi_khac", "Nơi khác đến", "con", g5),
		CNTSField{Key: "ghi_chu", Label: "CN-10 Ghi chú", Type: "text"},
	)
}

func bm05Fields(plan bool) []CNTSField {
	var fs []CNTSField
	for _, d := range WorkDays {
		k, l := d[0], d[1]
		fs = append(fs, CNTSField{Key: k + "_loai", Label: "Loại công việc", Type: "choice", Required: true, Options: WorkTypes, Group: l},
			CNTSField{Key: k + "_cong_tac_khac", Label: "Công tác khác (ghi rõ)", Type: "text", Group: l})
		if plan {
			fs = append(fs, CNTSField{Key: k + "_ke_hoach", Label: "Kế hoạch công việc", Type: "text", Required: true, Group: l},
				CNTSField{Key: k + "_ket_qua_du_kien", Label: "Kết quả dự kiến", Type: "text", Required: true, Group: l},
				CNTSField{Key: k + "_dia_diem", Label: "Địa điểm công tác", Type: "text", Group: l})
		} else {
			fs = append(fs, CNTSField{Key: k + "_ket_qua", Label: "Kết quả thực hiện", Type: "text", Required: true, Group: l})
		}
	}
	return fs
}

// CNTSForms là các biểu kỳ Cấp Xã của W11.42 (thứ tự màn).
var CNTSForms = []CNTSForm{
	{Code: CNTSBM01, BM: "BM-01", Title: "Báo cáo kiểm soát giết mổ hằng ngày", UnitKind: UnitFacility, PeriodType: PeriodDay,
		Deadline: Deadline{OffsetDays: 1, Time: "09:00"}, Due: "09:00 ngày hôm sau", NilLabel: "Cơ sở không hoạt động trong ngày",
		Fields: []CNTSField{cntsInt("trau_bo", "Trâu bò", "con", "Số con kiểm soát giết mổ"), cntsInt("heo", "Heo", "con", "Số con kiểm soát giết mổ"),
			cntsInt("gia_cam", "Gia cầm", "con", "Số con kiểm soát giết mổ"), cntsInt("de", "Dê", "con", "Số con kiểm soát giết mổ")}},
	{Code: CNTSBM04, BM: "BM-04", Title: "Nhập liệu tuần chăn nuôi", UnitKind: UnitCommune, PeriodType: PeriodWeek,
		Deadline: Deadline{OffsetDays: 1, Time: "12:00"}, Due: "12:00 ngày sau ngày cuối tuần", NilLabel: "Không phát sinh trong tuần",
		Fields: bm04Fields()},
	{Code: CNTSBM17, BM: "BM-17", Title: "Sản lượng khai thác thủy sản hằng tuần", UnitKind: UnitCommune, PeriodType: PeriodWeek,
		Deadline: Deadline{OffsetDays: 1, Time: "12:00"}, Due: "12:00 ngày sau ngày cuối tuần", NilLabel: "Không phát sinh khai thác trong tuần",
		Fields: []CNTSField{cntsDec("ca", "TS-01 Cá các loại", "tấn", "Sản lượng khai thác trong tuần"),
			cntsDec("tom", "TS-02 Tôm các loại", "tấn", "Sản lượng khai thác trong tuần"), cntsDec("muc", "TS-03 Mực", "tấn", "Sản lượng khai thác trong tuần"),
			cntsDec("khac", "TS-04 Thủy sản khác", "tấn", "Sản lượng khai thác trong tuần")}},
	{Code: CNTSBM05I, BM: "BM-05 Phần I", Title: "Kết quả công việc tuần (cán bộ tăng cường)", UnitKind: UnitCommune, PeriodType: PeriodWeek,
		Deadline: Deadline{OffsetDays: 3, Time: "17:00"}, Due: "17:00 Thứ Hai sau tuần làm việc", NilLabel: "", Fields: bm05Fields(false)},
	{Code: CNTSBM05II, BM: "BM-05 Phần II", Title: "Kế hoạch công việc tuần sau (cán bộ tăng cường)", UnitKind: UnitCommune,
		PeriodType: PeriodWeek, Deadline: Deadline{OffsetDays: -3, Time: "17:00"}, Due: "17:00 Thứ Sáu trước tuần kế hoạch", NilLabel: "",
		Fields: bm05Fields(true)},
}

// CNTSFormByCode tìm biểu theo mã.
func CNTSFormByCode(code string) (CNTSForm, bool) {
	for _, f := range CNTSForms {
		if f.Code == code {
			return f, true
		}
	}
	return CNTSForm{}, false
}

// workWeek: BM-05 dùng tuần làm việc riêng (không phải tuần báo cáo W11.03).
func (f CNTSForm) workWeek() bool { return f.Code == CNTSBM05I || f.Code == CNTSBM05II }

// IsWorkWeek: biểu chia theo tuần làm việc thứ Hai–thứ Sáu (BM-05) — không theo lịch tuần của phòng (D8).
func (f CNTSForm) IsWorkWeek() bool { return f.workWeek() }

// CNTSSeriesInput là yêu cầu chia kỳ một lô của một biểu.
type CNTSSeriesInput struct {
	Form     CNTSForm
	From, To time.Time // ngày dương lịch (Date/ParseDate)
	Rule     PeriodRule
	Deadline *Deadline // nil = hạn mặc định của biểu
	Loc      *time.Location
}

// CNTSPeriod là một kỳ của lô CNTS.
type CNTSPeriod struct {
	Kind, Key, Label string
	From, To         time.Time
	DueAt            time.Time // UTC
	Rule             string
}

// SplitCNTS chia [From, To] thành kỳ của biểu: BM-01 kỳ ngày; BM-04/BM-17 tuần báo cáo ĐẦY ĐỦ (mở rộng From/To tới
// ranh giới tuần của quy tắc — một tuần không bị cắt đôi giữa hai lô); BM-05 tuần làm việc Thứ Hai–Thứ Sáu có giao với
// khoảng. Quy tắc phải là v2 (kỳ ngày; tuần Thứ Tư – Thứ Ba theo QĐ-6).
func SplitCNTS(in CNTSSeriesInput) ([]CNTSPeriod, error) {
	f := in.Form
	dl := f.Deadline
	if in.Deadline != nil {
		dl = *in.Deadline
	}
	if _, _, err := ParseClock(dl.Time); err != nil {
		return nil, ErrPeriod{"deadline.time", err.Error()}
	}
	from, to := dateOf(in.From), dateOf(in.To)
	if to.Before(from) {
		return nil, ErrPeriod{"to_date", "phải không trước from_date"}
	}
	if f.workWeek() {
		return splitWorkWeeks(f, from, to, dl, in.Loc)
	}
	if !in.Rule.v2() {
		return nil, ErrPeriod{"period_rule", "biểu kỳ CNTS cần quy tắc chia kỳ v2"}
	}
	if f.PeriodType == PeriodWeek {
		a, err := in.Rule.PeriodOf(PeriodWeek, from, nil)
		if err != nil {
			return nil, err
		}
		b, err := in.Rule.PeriodOf(PeriodWeek, to, nil)
		if err != nil {
			return nil, err
		}
		from, to = a.From, b.To
	}
	if dl.OffsetDays < 0 || dl.OffsetDays > 60 {
		return nil, ErrPeriod{"deadline.offset_days", "0–60 ngày"}
	}
	ps, err := Split(SplitInput{PeriodType: f.PeriodType, From: from, To: to, Rule: in.Rule, Deadline: dl, Loc: in.Loc})
	if err != nil {
		return nil, err
	}
	out := make([]CNTSPeriod, 0, len(ps))
	for _, p := range ps {
		out = append(out, CNTSPeriod{Kind: f.PeriodType, Key: p.Key, Label: p.Label, From: p.From, To: p.To, DueAt: p.DueAt, Rule: in.Rule.Tag()})
	}
	return out, nil
}

// splitWorkWeeks: tuần làm việc Thứ Hai – Thứ Sáu. Phần I hạn = Thứ Sáu + OffsetDays (mặc định Thứ Hai sau); Phần II
// hạn = Thứ Hai + OffsetDays (âm — mặc định Thứ Sáu trước).
func splitWorkWeeks(f CNTSForm, from, to time.Time, dl Deadline, loc *time.Location) ([]CNTSPeriod, error) {
	if dl.OffsetDays < -14 || dl.OffsetDays > 14 {
		return nil, ErrPeriod{"deadline.offset_days", "−14…14 ngày"}
	}
	mon := from.AddDate(0, 0, -((int(from.Weekday()) + 6) % 7))
	var out []CNTSPeriod
	for d := mon; !d.After(to); d = d.AddDate(0, 0, 7) {
		if len(out) >= MaxPeriods {
			return nil, ErrPeriod{"to_date", fmt.Sprintf("tối đa %d kỳ mỗi đợt", MaxPeriods)}
		}
		fri := d.AddDate(0, 0, 4)
		if fri.Before(from) {
			continue
		}
		base := fri
		if dl.OffsetDays < 0 {
			base = d
		}
		due, err := dl.DueAt(base, loc)
		if err != nil {
			return nil, ErrPeriod{"deadline.time", err.Error()}
		}
		y, w := d.ISOWeek()
		out = append(out, CNTSPeriod{Kind: PeriodWeek, Key: fmt.Sprintf("%d-LV%02d", y, w),
			Label: fmt.Sprintf("Tuần làm việc %d/%d (%s)", w, y, rangeLabel(d, fri)), From: d, To: fri, DueAt: due, Rule: CNTSRuleWorkWeek})
	}
	if len(out) == 0 {
		return nil, ErrPeriod{"to_date", "khoảng ngày không chứa tuần làm việc nào"}
	}
	return out, nil
}

// CNTSIssue là một lỗi chặn của quy tắc nghiệp vụ CNTS.
type CNTSIssue struct{ Field, Reason string }

// ValidateCNTS kiểm quy tắc chặn của biểu CNTS (sau kiểm kiểu của cap/forms):
//   - BM-04 (UR-XA-07, Mục III.5 quy tắc 4): số liều tiêm trong tuần của một cột > tổng đàn loài tương ứng → Chặn;
//   - BM-05: mỗi ô văn bản tối đa 500 ký tự (CV-03, CV-05); "Công tác khác" phải ghi rõ.
//
// Cảnh báo (biến động ±15 %, 50 %, lệch KSGM…) thuộc W11.43 — không chặn ở đây.
func ValidateCNTS(code string, values map[string]string) []CNTSIssue {
	var out []CNTSIssue
	switch code {
	case CNTSBM04:
		for _, v := range bm04Vaccines {
			dose, ok := ratOf(values[v.Key])
			if !ok {
				continue
			}
			herd := new(big.Rat)
			for _, h := range v.Herd {
				if x, ok := ratOf(values[h]); ok {
					herd.Add(herd, x)
				}
			}
			if dose.Cmp(herd) > 0 {
				out = append(out, CNTSIssue{v.Key, fmt.Sprintf("số liều %s vượt tổng đàn %s", values[v.Key], herd.FloatString(0))})
			}
		}
	case CNTSBM05I, CNTSBM05II:
		for k, v := range values {
			if utf8.RuneCountInString(v) > 500 {
				out = append(out, CNTSIssue{k, "tối đa 500 ký tự"})
			}
		}
		for _, d := range WorkDays {
			if values[d[0]+"_loai"] == WorkTypes[1] && strings.TrimSpace(values[d[0]+"_cong_tac_khac"]) == "" {
				out = append(out, CNTSIssue{d[0] + "_cong_tac_khac", "ghi rõ công tác khác"})
			}
		}
	}
	return out
}

func ratOf(s string) (*big.Rat, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	return new(big.Rat).SetString(s)
}

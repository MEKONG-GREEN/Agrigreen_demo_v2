package domain

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
)

// Danh mục mặc định cài khi triển khai (FN-19: "các danh sách còn lại … và quy tắc nâng bậc được khai báo sẵn khi
// triển khai"). Nguồn:
//   - BRD HCTH v3.0 (Placeholder = false): mẫu ngạch có tên (FN-19 "Dữ liệu mẫu ngạch"), nhóm tương đương (B3), trình độ
//     và nhóm chuyên ngành theo trình độ (NS05, NS06), LLCT (HS-26), QLNN (HS-27), ngoại ngữ (HS-28), tin học (HS-29),
//     dân tộc Kinh/tôn giáo "Không" (TK-01, TK-02).
//   - Còn lại là MẪU (Placeholder = true) chờ Phòng HCTH gửi dữ liệu thật (Q-070): chức vụ, chuyên ngành, dân tộc/tôn
//     giáo khác, khen thưởng, kỷ luật, biến động, lý do nghỉ, tên của 06.031 và V.03.09.27, MỌI bảng hệ số (NĐ
//     204/2004 — Phòng HCTH xác nhận) và hai tham số nâng bậc/quy đổi thành tích.
// Mã ổn định; cài lại chỉ ghi mã chưa có (mã đã có, kể cả đã ngừng, giữ nguyên).

// DefaultItem là một mục danh mục mặc định.
type DefaultItem struct {
	Item
	Placeholder bool
}

func item(k Kind, code, name string, order int, ph bool, a Attrs) DefaultItem {
	return DefaultItem{Item: Item{Kind: k, Code: code, Name: name, SortOrder: order, Active: true, Attrs: a}, Placeholder: ph}
}

func list(k Kind, ph bool, pairs ...string) []DefaultItem { return listFrom(k, ph, 10, pairs...) }

// listFrom tạo mục từ cặp (mã, tên), thứ tự từ first, bước 10.
func listFrom(k Kind, ph bool, first int, pairs ...string) []DefaultItem {
	out := make([]DefaultItem, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, item(k, pairs[i], pairs[i+1], first+i/2*10, ph, Attrs{}))
	}
	return out
}

// DefaultItems trả danh mục mặc định theo thứ tự ghi (trình độ trước nhóm chuyên ngành, nhóm trước chuyên ngành).
func DefaultItems() []DefaultItem {
	var out []DefaultItem
	add := func(xs ...DefaultItem) { out = append(out, xs...) }

	// Ngạch/CDNN — FN-19 "Dữ liệu mẫu ngạch".
	add(
		item(KindGrade, "01.002", "Chuyên viên chính", 10, false, Attrs{EquivGroup: EquivCVC, GradeType: GradeTypeCongChuc, SalaryClass: "A2.1"}),
		item(KindGrade, "01.003", "Chuyên viên", 20, false, Attrs{EquivGroup: EquivCV, GradeType: GradeTypeCongChuc, SalaryClass: "A1"}),
		item(KindGrade, "06.031", "Kế toán viên", 30, true, Attrs{EquivGroup: EquivCV, GradeType: GradeTypeCongChuc, SalaryClass: "A1"}),
		item(KindGrade, "V.05.02.07", "Kỹ sư hạng III", 40, false, Attrs{EquivGroup: EquivCV, GradeType: GradeTypeVienChuc, SalaryClass: "A1"}),
		item(KindGrade, "V.03.09.26", "Khuyến nông viên hạng III", 50, false, Attrs{EquivGroup: EquivCV, GradeType: GradeTypeVienChuc, SalaryClass: "A1"}),
		item(KindGrade, "V.03.09.27", "Kỹ thuật viên khuyến nông hạng IV", 60, true, Attrs{EquivGroup: EquivCS, GradeType: GradeTypeVienChuc, SalaryClass: "B"}),
	)

	// Chức vụ — mẫu (hệ số PCCV để trống: chờ Phòng HCTH).
	add(
		item(KindPosition, "giam_doc", "Giám đốc", 10, true, Attrs{Leader: true, MgmtLevel: "giam_doc"}),
		item(KindPosition, "pho_giam_doc", "Phó Giám đốc", 20, true, Attrs{Leader: true, MgmtLevel: "pho_giam_doc"}),
		item(KindPosition, "truong_phong", "Trưởng phòng", 30, true, Attrs{Leader: true, MgmtLevel: "truong_phong"}),
		item(KindPosition, "pho_truong_phong", "Phó Trưởng phòng", 40, true, Attrs{Leader: true, MgmtLevel: "pho_truong_phong"}),
		item(KindPosition, "tram_truong", "Trạm trưởng", 50, true, Attrs{Leader: true, MgmtLevel: "truong_phong"}),
		item(KindPosition, "pho_tram_truong", "Phó Trạm trưởng", 60, true, Attrs{Leader: true, MgmtLevel: "pho_truong_phong"}),
		item(KindPosition, "vien_chuc", "Viên chức", 70, true, Attrs{}),
	)

	// Trình độ chuyên môn — NS05, NS06 (thứ hạng: cao hơn = lớn hơn).
	add(
		item(KindEducationLevel, "tien_si", "Tiến sĩ", 10, false, Attrs{Rank: 60}),
		item(KindEducationLevel, "thac_si", "Thạc sĩ", 20, false, Attrs{Rank: 50}),
		item(KindEducationLevel, "dai_hoc", "Đại học", 30, false, Attrs{Rank: 40}),
		item(KindEducationLevel, "cao_dang", "Cao đẳng", 40, false, Attrs{Rank: 30}),
		item(KindEducationLevel, "trung_cap", "Trung cấp", 50, false, Attrs{Rank: 20}),
		item(KindEducationLevel, "khac", "Khác", 60, false, Attrs{Rank: 10}),
	)

	// Nhóm chuyên ngành theo trình độ — cột NS05.
	groups := []struct{ level, code, name string }{
		{"tien_si", "ts_kh_dat", "Khoa học đất"}, {"tien_si", "ts_chan_nuoi", "Chăn nuôi"}, {"tien_si", "ts_ntts", "Nuôi trồng thủy sản"},
		{"thac_si", "ths_nong_hoc", "Nông học"}, {"thac_si", "ths_chan_nuoi", "Chăn nuôi"}, {"thac_si", "ths_ntts", "Nuôi trồng thủy sản"},
		{"thac_si", "ths_khac", "Khác"},
		{"dai_hoc", "dh_tt_bvtv", "Trồng trọt + BVTV"}, {"dai_hoc", "dh_nong_hoc", "Nông học"}, {"dai_hoc", "dh_bsty", "Bác sĩ thú y"},
		{"dai_hoc", "dh_cnty", "Chăn nuôi thú y"}, {"dai_hoc", "dh_ntts", "Nuôi trồng thủy sản"}, {"dai_hoc", "dh_khac", "Khác"},
		{"cao_dang", "cd_tt_bvtv", "Trồng trọt + BVTV"}, {"cao_dang", "cd_dvty", "Dịch vụ thú y"}, {"cao_dang", "cd_cnty", "Chăn nuôi thú y"},
		{"cao_dang", "cd_ntts", "Nuôi trồng thủy sản"}, {"cao_dang", "cd_khac", "Khác"},
		{"trung_cap", "tc_trong_trot", "Trồng trọt"}, {"trung_cap", "tc_tt_bvtv", "Trồng trọt & BVTV"}, {"trung_cap", "tc_ktnn", "Kinh tế nông nghiệp"},
		{"trung_cap", "tc_cnty", "Chăn nuôi thú y"}, {"trung_cap", "tc_ntts", "Nuôi trồng thủy sản"}, {"trung_cap", "tc_khac", "Khác"},
	}
	for i, g := range groups {
		add(item(KindMajorGroup, g.code, g.name, (i+1)*10, false, Attrs{Level: g.level}))
	}

	// Chuyên ngành — mẫu, gắn nhóm theo từng trình độ.
	major := func(code, name string, order int, ts, ths, dh, cd, tc string) DefaultItem {
		m := map[string]string{}
		for lv, g := range map[string]string{"tien_si": ts, "thac_si": ths, "dai_hoc": dh, "cao_dang": cd, "trung_cap": tc} {
			if g != "" {
				m[lv] = g
			}
		}
		return item(KindMajor, code, name, order, true, Attrs{Groups: m})
	}
	add(
		major("trong_trot", "Trồng trọt", 10, "", "ths_nong_hoc", "dh_tt_bvtv", "cd_tt_bvtv", "tc_trong_trot"),
		major("bao_ve_thuc_vat", "Bảo vệ thực vật", 20, "", "ths_nong_hoc", "dh_tt_bvtv", "cd_tt_bvtv", "tc_tt_bvtv"),
		major("nong_hoc", "Nông học", 30, "", "ths_nong_hoc", "dh_nong_hoc", "cd_tt_bvtv", "tc_trong_trot"),
		major("khoa_hoc_dat", "Khoa học đất", 40, "ts_kh_dat", "ths_khac", "dh_khac", "cd_khac", "tc_khac"),
		major("chan_nuoi", "Chăn nuôi", 50, "ts_chan_nuoi", "ths_chan_nuoi", "dh_cnty", "cd_cnty", "tc_cnty"),
		major("thu_y", "Thú y", 60, "ts_chan_nuoi", "ths_chan_nuoi", "dh_bsty", "cd_dvty", "tc_cnty"),
		major("nuoi_trong_thuy_san", "Nuôi trồng thủy sản", 70, "ts_ntts", "ths_ntts", "dh_ntts", "cd_ntts", "tc_ntts"),
		major("kinh_te_nong_nghiep", "Kinh tế nông nghiệp", 80, "", "ths_khac", "dh_khac", "cd_khac", "tc_ktnn"),
		major("ke_toan", "Kế toán", 90, "", "ths_khac", "dh_khac", "cd_khac", "tc_khac"),
	)

	// Danh sách khai báo sẵn.
	add(item(KindEthnicity, "kinh", "Kinh", 10, false, Attrs{}))
	for i, p := range [][2]string{{"khmer", "Khmer"}, {"hoa", "Hoa"}, {"cham", "Chăm"}, {"khac", "Dân tộc khác"}} {
		add(item(KindEthnicity, p[0], p[1], (i+2)*10, true, Attrs{Minority: true}))
	}
	add(item(KindReligion, "khong", "Không", 10, false, Attrs{NoReligion: true}))
	add(listFrom(KindReligion, true, 20, "phat_giao", "Phật giáo", "cong_giao", "Công giáo", "phat_giao_hoa_hao", "Phật giáo Hòa Hảo",
		"cao_dai", "Cao Đài", "tin_lanh", "Tin Lành", "hoi_giao", "Hồi giáo", "khac", "Tôn giáo khác")...)
	add(list(KindPoliticalTheory, false, "cu_nhan", "Cử nhân", "cao_cap", "Cao cấp", "trung_cap", "Trung cấp", "so_cap", "Sơ cấp")...)
	add(list(KindStateMgmt, false, "cvcc", "Chuyên viên cao cấp", "cvc", "Chuyên viên chính", "cv", "Chuyên viên", "can_su", "Cán sự")...)
	add(list(KindForeignLanguage, false, "tieng_anh", "Tiếng Anh", "ngoai_ngu_khac", "Ngoại ngữ khác", "tieng_dtts", "Tiếng dân tộc thiểu số")...)
	add(list(KindLanguageLevel, false, "a", "A", "b", "B", "c", "C", "b1", "B1", "bac_1", "Bậc 1", "bac_2", "Bậc 2", "bac_3", "Bậc 3",
		"bac_4", "Bậc 4", "bac_5", "Bậc 5", "bac_6", "Bậc 6", "cu_nhan", "Cử nhân")...)
	add(list(KindITLevel, false, "trung_cap_tro_len", "Trung cấp trở lên", "chung_chi_a", "Chứng chỉ A", "chung_chi_b", "Chứng chỉ B",
		"co_ban", "Ứng dụng CNTT cơ bản", "nang_cao", "Ứng dụng CNTT nâng cao", "tin_hoc_van_phong", "Tin học văn phòng")...)
	add(list(KindAwardForm, true, "giay_khen_giam_doc", "Giấy khen của Giám đốc", "giay_khen_so", "Giấy khen của Giám đốc Sở",
		"bang_khen_ubnd_tinh", "Bằng khen của UBND tỉnh", "bang_khen_bo", "Bằng khen của Bộ trưởng", "cstd_co_so", "Chiến sĩ thi đua cơ sở",
		"cstd_tinh", "Chiến sĩ thi đua cấp tỉnh", "lao_dong_tien_tien", "Lao động tiên tiến")...)
	add(list(KindDisciplineForm, true, "khien_trach", "Khiển trách", "canh_cao", "Cảnh cáo", "cach_chuc", "Cách chức", "buoc_thoi_viec", "Buộc thôi việc")...)
	add(list(KindChangeType, true, "tuyen_dung", "Tuyển dụng", "chuyen_den", "Chuyển đến", "chuyen_di", "Chuyển đi", "nghi_huu", "Nghỉ hưu",
		"thoi_viec", "Thôi việc", "tang_cuong", "Tăng cường", "bo_nhiem", "Bổ nhiệm")...)
	add(list(KindLeaveReason, true, "nghi_huu", "Nghỉ hưu", "thoi_viec", "Thôi việc theo nguyện vọng", "chuyen_cong_tac", "Chuyển công tác",
		"buoc_thoi_viec", "Buộc thôi việc", "tu_tran", "Từ trần", "khac", "Lý do khác")...)
	return out
}

// DefaultScaleValidFrom là ngày hiệu lực của bảng hệ số mẫu (NĐ 204/2004/NĐ-CP, hiệu lực 01/10/2004).
var DefaultScaleValidFrom = time.Date(2004, 10, 1, 0, 0, 0, 0, time.UTC)

func coefs(start string, step string, n int) []decimal.Decimal {
	c, d := decimal.RequireFromString(start), decimal.RequireFromString(step)
	out := make([]decimal.Decimal, n)
	for i := range out {
		out[i] = c.Add(d.Mul(decimal.NewFromInt(int64(i))))
	}
	return out
}

// DefaultScales trả bảng hệ số MẪU của các ngạch mặc định (NĐ 204/2004: A2.1 4,40 + 0,34 × 8 bậc; A1 2,34 + 0,33 × 9
// bậc; B 1,86 + 0,20 × 12 bậc; giữ bậc A 36 tháng, B 24 tháng) — Placeholder, chờ Phòng HCTH (Q-070).
func DefaultScales() []GradeScale {
	const note = "Mẫu theo NĐ 204/2004 — chờ Phòng HCTH xác nhận (Q-070)"
	a1 := func(g string) GradeScale {
		return GradeScale{GradeCode: g, ValidFrom: DefaultScaleValidFrom, Coefficients: coefs("2.34", "0.33", 9), HoldMonths: 36, Note: note}
	}
	return []GradeScale{
		{GradeCode: "01.002", ValidFrom: DefaultScaleValidFrom, Coefficients: coefs("4.40", "0.34", 8), HoldMonths: 36, Note: note},
		a1("01.003"), a1("06.031"), a1("V.05.02.07"), a1("V.03.09.26"),
		{GradeCode: "V.03.09.27", ValidFrom: DefaultScaleValidFrom, Coefficients: coefs("1.86", "0.20", 12), HoldMonths: 24, Note: note},
	}
}

// DefaultRuleValidFrom là ngày hiệu lực của tham số mẫu.
var DefaultRuleValidFrom = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// DefaultRules trả hai tham số MẪU (Placeholder, Q-070): quy tắc nâng bậc theo quy định hiện hành phổ biến (NLTX xếp
// loại từ HTNV, 3 năm xếp loại, 5 năm thành tích; kéo dài 6/12 tháng khi kỷ luật; NLTH ≤ 10 %, nghỉ hưu ≤ 12 tháng trong
// 12 tháng; TNVK 5 % + 1 %/12 tháng sau 36 tháng ở bậc cuối; nhắc 1, 3, 6 tháng; tuổi nghỉ hưu năm 2026: nam 61 tuổi 6
// tháng, nữ 57 tuổi) và bảng quy đổi thành tích (mẫu 12 và 9 tháng).
func DefaultRules() []RuleVersionInput {
	p := PromotionRules{
		MinRating: "HTNV", RatingYears: 3, AchievementYears: 5,
		DisciplineExtensionMonths: map[string]int{"khien_trach": 6, "canh_cao": 6, "cach_chuc": 12},
		NLTHQuotaPct:              decimal.NewFromInt(10), NLTHRetirementMonths: 12, NLTHRetirementWithinM: 12,
		TNVKStartPct: decimal.NewFromInt(5), TNVKStepPct: decimal.NewFromInt(1), TNVKCycleMonths: 12, TNVKFirstMonths: 36,
		RemindMonths:            []int{1, 3, 6},
		RetirementAgeMaleMonths: 61*12 + 6, RetirementAgeFemaleMonths: 57 * 12,
	}
	c := AchievementConversion{Rules: []ConversionRule{
		{AwardCode: "bang_khen_bo", Months: 12}, {AwardCode: "bang_khen_ubnd_tinh", Months: 12}, {AwardCode: "cstd_tinh", Months: 12},
		{AwardCode: "cstd_co_so", Months: 9}, {AwardCode: "giay_khen_so", Months: 9},
	}}
	pj, _ := json.Marshal(p)
	cj, _ := json.Marshal(c)
	const note = "Mẫu — chờ Phòng HCTH gửi quy chế (Q-070)"
	return []RuleVersionInput{
		{Key: RulePromotion, ValidFrom: DefaultRuleValidFrom, Payload: pj, Note: note},
		{Key: RuleAchievementConversion, ValidFrom: DefaultRuleValidFrom, Payload: cj, Note: note},
	}
}

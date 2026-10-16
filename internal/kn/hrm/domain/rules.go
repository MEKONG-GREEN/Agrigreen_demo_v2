package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// RuleKey là khóa tham số có phiên bản (cột key của pf_hrm.rule_param).
type RuleKey string

// Hai tham số của FN-19 ("quy tắc nâng bậc được khai báo sẵn khi triển khai") — W11.25 đọc as-of ngày xét.
const (
	RulePromotion             RuleKey = "promotion"              // quy tắc nâng bậc (NLTX, NLTH, TNVK, nhắc, tuổi nghỉ hưu)
	RuleAchievementConversion RuleKey = "achievement_conversion" // bảng quy đổi thành tích → số tháng nâng trước hạn
)

// RuleKeys là mọi khóa tham số.
var RuleKeys = []RuleKey{RulePromotion, RuleAchievementConversion}

// ParseRuleKey kiểm khóa tham số.
func ParseRuleKey(s string) (RuleKey, bool) {
	for _, k := range RuleKeys {
		if string(k) == s {
			return k, true
		}
	}
	return "", false
}

// Mức xếp loại chất lượng (FN-15 BR-02), theo thứ tự giảm dần.
var Ratings = []string{"HTXSNV", "HTTNV", "HTNV", "KHTNV"}

// PromotionRules là quy tắc nâng bậc (BRD FN-09 BR-02, BR-03; Q-070 chờ Phòng HCTH — mặc định là giả định).
// Số tháng là số nguyên; phần trăm là decimal (chuỗi ở JSON).
type PromotionRules struct {
	// NLTX: xếp loại tối thiểu của mỗi năm trong thời gian giữ bậc; số năm xếp loại xét (cột Mẫu số 1).
	MinRating   string `json:"min_rating"`
	RatingYears int    `json:"rating_years"`
	// Số năm thành tích xét cho NLTH (cột Mẫu số 2; FN-09 BR-01).
	AchievementYears int `json:"achievement_years"`
	// Kéo dài thời gian giữ bậc theo hình thức kỷ luật (mã discipline_form → số tháng).
	DisciplineExtensionMonths map[string]int `json:"discipline_extension_months"`
	// NLTH: tỷ lệ chỉ tiêu tối đa (% số người trong danh sách trả lương) và số tháng nâng trước hạn tối đa khi có
	// thông báo nghỉ hưu.
	NLTHQuotaPct          decimal.Decimal `json:"nlth_quota_pct"`
	NLTHRetirementMonths  int             `json:"nlth_retirement_months"`
	NLTHRetirementWithinM int             `json:"nlth_retirement_within_months"`
	// TNVK: % khởi điểm, % tăng mỗi chu kỳ, chu kỳ (tháng), điều kiện thời gian giữ bậc cuối (tháng).
	TNVKStartPct    decimal.Decimal `json:"tnvk_start_pct"`
	TNVKStepPct     decimal.Decimal `json:"tnvk_step_pct"`
	TNVKCycleMonths int             `json:"tnvk_cycle_months"`
	TNVKFirstMonths int             `json:"tnvk_first_months"`
	// Nhắc đến kỳ (tháng tới — FN-09 bước 1: 1, 3, 6 tháng).
	RemindMonths []int `json:"remind_months"`
	// Tuổi nghỉ hưu (tháng tuổi) áp dụng tại ngày hiệu lực của phiên bản (lộ trình tăng dần — mỗi năm một phiên bản).
	RetirementAgeMaleMonths   int `json:"retirement_age_male_months"`
	RetirementAgeFemaleMonths int `json:"retirement_age_female_months"`
}

// ConversionRule là một dòng bảng quy đổi thành tích: hình thức khen thưởng → số tháng nâng bậc trước hạn.
type ConversionRule struct {
	AwardCode string `json:"award_code"`
	Months    int    `json:"months"`
}

// AchievementConversion là bảng quy đổi thành tích (NL03-06; mẫu có 9 và 12 tháng).
type AchievementConversion struct {
	Rules []ConversionRule `json:"rules"`
}

// RuleVersionInput là một phiên bản tham số cần ghi.
type RuleVersionInput struct {
	Key       RuleKey
	ValidFrom time.Time
	Payload   json.RawMessage
	Note      string
}

// decodeStrict giải JSON, chặn trường lạ.
func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalid("tham số: %v", err)
	}
	return nil
}

// ParseRule giải và kiểm payload của khóa k; trả payload đã chuẩn hóa (JSON của cấu trúc). Tham chiếu tới danh mục
// (mã kỷ luật, mã khen thưởng) trả ở refs để app kiểm trong giao dịch.
func ParseRule(k RuleKey, raw []byte) (canon []byte, refs map[Kind][]string, err error) {
	refs = map[Kind][]string{}
	var v any
	switch k {
	case RulePromotion:
		var p PromotionRules
		if err := decodeStrict(raw, &p); err != nil {
			return nil, nil, err
		}
		if err := validatePromotion(p); err != nil {
			return nil, nil, err
		}
		for code := range p.DisciplineExtensionMonths {
			refs[KindDisciplineForm] = append(refs[KindDisciplineForm], code)
		}
		v = p
	case RuleAchievementConversion:
		var c AchievementConversion
		if err := decodeStrict(raw, &c); err != nil {
			return nil, nil, err
		}
		if len(c.Rules) == 0 || len(c.Rules) > 50 {
			return nil, nil, invalid("bảng quy đổi có 1–50 dòng")
		}
		seen := map[string]bool{}
		for _, r := range c.Rules {
			if !reCode.MatchString(r.AwardCode) || seen[r.AwardCode] {
				return nil, nil, invalid("hình thức khen thưởng %q thiếu hoặc trùng", r.AwardCode)
			}
			seen[r.AwardCode] = true
			if r.Months < 1 || r.Months > 36 {
				return nil, nil, invalid("số tháng quy đổi 1–36")
			}
			refs[KindAwardForm] = append(refs[KindAwardForm], r.AwardCode)
		}
		v = c
	default:
		return nil, nil, invalid("khóa tham số %q", k)
	}
	canon, err = json.Marshal(v)
	return canon, refs, err
}

func validatePromotion(p PromotionRules) error {
	if !contains(Ratings, p.MinRating) {
		return invalid("xếp loại tối thiểu phải là một trong %v", Ratings)
	}
	if p.RatingYears < 1 || p.RatingYears > 10 || p.AchievementYears < 1 || p.AchievementYears > 10 {
		return invalid("số năm xếp loại/thành tích 1–10")
	}
	for code, m := range p.DisciplineExtensionMonths {
		if !reCode.MatchString(code) || m < 0 || m > 60 {
			return invalid("kéo dài theo kỷ luật %q: %d tháng (0–60)", code, m)
		}
	}
	pct := func(what string, d decimal.Decimal) error {
		if d.IsNegative() || d.GreaterThan(decimal.NewFromInt(100)) || !d.Equal(d.Round(2)) {
			return invalid("%s %s: 0–100, tối đa 2 chữ số thập phân", what, d)
		}
		return nil
	}
	if err := pct("tỷ lệ chỉ tiêu NLTH", p.NLTHQuotaPct); err != nil {
		return err
	}
	if err := pct("% TNVK khởi điểm", p.TNVKStartPct); err != nil {
		return err
	}
	if err := pct("% TNVK tăng", p.TNVKStepPct); err != nil {
		return err
	}
	if p.NLTHRetirementMonths < 0 || p.NLTHRetirementMonths > 36 || p.NLTHRetirementWithinM < 0 || p.NLTHRetirementWithinM > 36 {
		return invalid("NLTH nghỉ hưu: 0–36 tháng")
	}
	if p.TNVKCycleMonths < 1 || p.TNVKCycleMonths > 60 || p.TNVKFirstMonths < 1 || p.TNVKFirstMonths > 120 {
		return invalid("chu kỳ TNVK 1–60 tháng, điều kiện lần đầu 1–120 tháng")
	}
	if len(p.RemindMonths) == 0 || len(p.RemindMonths) > 6 {
		return invalid("mốc nhắc: 1–6 giá trị")
	}
	for i, m := range p.RemindMonths {
		if m < 1 || m > 24 || (i > 0 && m <= p.RemindMonths[i-1]) {
			return invalid("mốc nhắc tăng dần, 1–24 tháng")
		}
	}
	for _, a := range []int{p.RetirementAgeMaleMonths, p.RetirementAgeFemaleMonths} {
		if a < 50*12 || a > 70*12 {
			return invalid("tuổi nghỉ hưu (tháng) %d ngoài 600–840", a)
		}
	}
	return nil
}

// ValidateRuleVersion kiểm phần chung của một phiên bản tham số.
func ValidateRuleVersion(in RuleVersionInput) error {
	if _, ok := ParseRuleKey(string(in.Key)); !ok {
		return invalid("khóa tham số %q", in.Key)
	}
	if in.ValidFrom.IsZero() {
		return invalid("thiếu ngày hiệu lực")
	}
	if utf8.RuneCountInString(in.Note) > 500 {
		return invalid("ghi chú tối đa 500 ký tự")
	}
	if len(in.Payload) == 0 {
		return invalid("thiếu nội dung tham số")
	}
	return nil
}

// Promotion giải payload đã lưu của khóa promotion (W11.25).
func Promotion(raw []byte) (PromotionRules, error) {
	var p PromotionRules
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("hrm: tham số nâng bậc đã lưu: %w", err)
	}
	return p, nil
}

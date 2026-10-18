package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// FN-19 AC-03: mã ngạch " V.03.04.12 " (kèm xuống dòng) được lưu là "V.03.04.12"; mã danh mục khác về chữ thường.
func TestNormalizeCode_AC03(t *testing.T) {
	if got := NormalizeCode(KindGrade, " V.03.04.12 \n"); got != "V.03.04.12" {
		t.Fatalf("mã ngạch: %q", got)
	}
	if got := NormalizeCode(KindGrade, "V.03 .04.12"); got != "V.03.04.12" {
		t.Fatalf("khoảng trắng giữa: %q", got)
	}
	if got := NormalizeCode(KindPosition, " Truong_Phong "); got != "truong_phong" {
		t.Fatalf("mã chức vụ: %q", got)
	}
	if !ValidCode(KindGrade, "01.002") || ValidCode(KindPosition, "01.002") || ValidCode(KindGrade, ".x") {
		t.Fatal("ValidCode")
	}
}

func TestValidateItemAttrs(t *testing.T) {
	pc := decimal.RequireFromString("0.70")
	ok := []Item{
		{Kind: KindPosition, Code: "giam_doc", Name: "Giám đốc", Attrs: Attrs{Leader: true, Allowance: &pc, MgmtLevel: "giam_doc"}},
		{Kind: KindGrade, Code: "01.003", Name: "Chuyên viên", Attrs: Attrs{EquivGroup: EquivCV, GradeType: GradeTypeCongChuc}},
		{Kind: KindEducationLevel, Code: "dai_hoc", Name: "Đại học", Attrs: Attrs{Rank: 40}},
		{Kind: KindMajorGroup, Code: "dh_khac", Name: "Khác", Attrs: Attrs{Level: "dai_hoc"}},
		{Kind: KindMajor, Code: "ke_toan", Name: "Kế toán", Attrs: Attrs{Groups: map[string]string{"dai_hoc": "dh_khac"}}},
		{Kind: KindReligion, Code: "khong", Name: "Không", Attrs: Attrs{NoReligion: true}},
		{Kind: KindLeaveReason, Code: "nghi_huu", Name: "Nghỉ hưu"},
	}
	for _, it := range ok {
		if err := Validate(it); err != nil {
			t.Errorf("%s/%s: %v", it.Kind, it.Code, err)
		}
	}
	bad3 := decimal.RequireFromString("0.705")
	bad := []Item{
		{Kind: "salary", Code: "x", Name: "x"},
		{Kind: KindPosition, Code: "x", Name: " x"},
		{Kind: KindPosition, Code: "x", Name: "x", Attrs: Attrs{Allowance: &pc}},                             // PCCV mà không lãnh đạo
		{Kind: KindPosition, Code: "x", Name: "x", Attrs: Attrs{Leader: true, Allowance: &bad3}},             // 3 chữ số thập phân
		{Kind: KindGrade, Code: "01.003", Name: "x", Attrs: Attrs{EquivGroup: "A1", GradeType: "cong_chuc"}}, // nhóm lạ
		{Kind: KindGrade, Code: "01.003", Name: "x", Attrs: Attrs{EquivGroup: EquivCV}},                      // thiếu loại ngạch
		{Kind: KindEducationLevel, Code: "dh", Name: "x"},                                                    // thiếu thứ hạng
		{Kind: KindMajorGroup, Code: "g", Name: "x"},                                                         // thiếu trình độ
		{Kind: KindLeaveReason, Code: "k", Name: "x", Attrs: Attrs{Rank: 3}},                                 // thuộc tính không thuộc loại
		{Kind: KindEthnicity, Code: "k", Name: "x", Attrs: Attrs{Leader: true}},
	}
	for i, it := range bad {
		if err := Validate(it); !errors.Is(err, ErrInvalid) {
			t.Errorf("#%d %s phải lỗi: %v", i, it.Kind, err)
		}
	}
}

func TestValidateScale(t *testing.T) {
	d := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	good := GradeScale{GradeCode: "01.003", ValidFrom: d, Coefficients: coefs("2.34", "0.33", 9), HoldMonths: 36}
	if err := ValidateScale(good); err != nil {
		t.Fatal(err)
	}
	if c, ok := good.Coefficient(9); !ok || c.String() != "4.98" || good.Steps() != 9 {
		t.Fatalf("bậc 9: %s", c)
	}
	if _, ok := good.Coefficient(10); ok {
		t.Fatal("bậc 10 không có")
	}
	cases := map[string]GradeScale{
		"không tăng":      {GradeCode: "01.003", ValidFrom: d, Coefficients: []decimal.Decimal{decimal.RequireFromString("2.34"), decimal.RequireFromString("2.34")}, HoldMonths: 36},
		"3 chữ số":        {GradeCode: "01.003", ValidFrom: d, Coefficients: []decimal.Decimal{decimal.RequireFromString("2.345")}, HoldMonths: 36},
		"không bậc":       {GradeCode: "01.003", ValidFrom: d, HoldMonths: 36},
		"giữ bậc 0":       {GradeCode: "01.003", ValidFrom: d, Coefficients: coefs("2.34", "0.33", 2), HoldMonths: 0},
		"thiếu ngày":      {GradeCode: "01.003", Coefficients: coefs("2.34", "0.33", 2), HoldMonths: 36},
		"âm":              {GradeCode: "01.003", ValidFrom: d, Coefficients: []decimal.Decimal{decimal.RequireFromString("-1")}, HoldMonths: 36},
		"mã có khoảng":    {GradeCode: "01 003", ValidFrom: d, Coefficients: coefs("2.34", "0.33", 2), HoldMonths: 36},
		"quá 20 bậc":      {GradeCode: "01.003", ValidFrom: d, Coefficients: coefs("1.00", "0.10", 21), HoldMonths: 36},
		"hệ số quá lớn":   {GradeCode: "01.003", ValidFrom: d, Coefficients: []decimal.Decimal{decimal.RequireFromString("100")}, HoldMonths: 36},
		"ghi chú quá dài": {GradeCode: "01.003", ValidFrom: d, Coefficients: coefs("2.34", "0.33", 2), HoldMonths: 36, Note: strings.Repeat("x", 501)},
	}
	for name, s := range cases {
		if err := ValidateScale(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s phải lỗi: %v", name, err)
		}
	}
}

func TestParseRule(t *testing.T) {
	for _, r := range DefaultRules() {
		canon, refs, err := ParseRule(r.Key, r.Payload)
		if err != nil {
			t.Fatalf("%s: %v", r.Key, err)
		}
		if err := ValidateRuleVersion(RuleVersionInput{Key: r.Key, ValidFrom: r.ValidFrom, Payload: canon}); err != nil {
			t.Fatal(err)
		}
		if len(refs) == 0 {
			t.Fatalf("%s: phải có tham chiếu danh mục", r.Key)
		}
	}
	p, err := Promotion(DefaultRules()[0].Payload)
	if err != nil || p.TNVKStartPct.String() != "5" || p.RemindMonths[2] != 6 {
		t.Fatalf("Promotion: %+v %v", p, err)
	}
	// Phần trăm dạng chuỗi thập phân giữ nguyên (không qua float).
	raw := []byte(`{"min_rating":"HTNV","rating_years":3,"achievement_years":5,"discipline_extension_months":{},"nlth_quota_pct":"10.25",
	  "nlth_retirement_months":12,"nlth_retirement_within_months":12,"tnvk_start_pct":"5","tnvk_step_pct":"1","tnvk_cycle_months":12,
	  "tnvk_first_months":36,"remind_months":[1,3,6],"retirement_age_male_months":738,"retirement_age_female_months":684}`)
	canon, _, err := ParseRule(RulePromotion, raw)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	_ = json.Unmarshal(canon, &back)
	if back["nlth_quota_pct"] != "10.25" {
		t.Fatalf("chuẩn hóa: %s", canon)
	}
	bad := map[RuleKey]string{
		RulePromotion:             `{"min_rating":"XUAT_SAC"}`,
		RuleAchievementConversion: `{"rules":[{"award_code":"cstd_co_so","months":9},{"award_code":"cstd_co_so","months":12}]}`,
		"luong":                   `{}`,
	}
	for k, s := range bad {
		if _, _, err := ParseRule(k, []byte(s)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s phải lỗi: %v", k, err)
		}
	}
	if _, _, err := ParseRule(RuleAchievementConversion, []byte(`{"rules":[],"x":1}`)); !errors.Is(err, ErrInvalid) {
		t.Error("trường lạ phải lỗi")
	}
}

// Dữ liệu mặc định hợp lệ, mã không trùng, tham chiếu chéo khớp (nhóm chuyên ngành ↔ trình độ, chuyên ngành ↔ nhóm cùng
// trình độ, tham số ↔ mã kỷ luật/khen thưởng), có bảng hệ số cho mọi ngạch mặc định; dữ liệu BRD không là mẫu.
func TestDefaults(t *testing.T) {
	seen := map[string]Item{}
	for _, d := range DefaultItems() {
		if err := Validate(d.Item); err != nil {
			t.Fatalf("%s/%s: %v", d.Kind, d.Code, err)
		}
		key := string(d.Kind) + ":" + d.Code
		if _, dup := seen[key]; dup {
			t.Fatalf("trùng %s", key)
		}
		seen[key] = d.Item
	}
	for _, it := range seen {
		switch it.Kind {
		case KindMajorGroup:
			if _, ok := seen["education_level:"+it.Attrs.Level]; !ok {
				t.Errorf("nhóm %s: trình độ %s", it.Code, it.Attrs.Level)
			}
		case KindMajor:
			for lv, g := range it.Attrs.Groups {
				if seen["major_group:"+g].Attrs.Level != lv {
					t.Errorf("chuyên ngành %s: nhóm %s không thuộc %s", it.Code, g, lv)
				}
			}
		}
	}
	for _, s := range DefaultScales() {
		if err := ValidateScale(s); err != nil {
			t.Fatal(err)
		}
		if _, ok := seen["grade:"+s.GradeCode]; !ok {
			t.Errorf("bảng hệ số cho ngạch lạ %s", s.GradeCode)
		}
	}
	if n := len(DefaultScales()); n != 6 {
		t.Fatalf("bảng hệ số mẫu: %d", n)
	}
	for _, r := range DefaultRules() {
		_, refs, _ := ParseRule(r.Key, r.Payload)
		for k, codes := range refs {
			for _, c := range codes {
				if _, ok := seen[string(k)+":"+c]; !ok {
					t.Errorf("%s tham chiếu %s:%s", r.Key, k, c)
				}
			}
		}
	}
	// Mẫu ngạch BRD (FN-19): sáu mã, nhóm tương đương đúng NS06.
	if seen["grade:01.002"].Attrs.EquivGroup != EquivCVC || seen["grade:V.03.09.26"].Name != "Khuyến nông viên hạng III" {
		t.Fatal("mẫu ngạch BRD")
	}
}

// Package domain: quy tắc thuần của mkg/acctbridge — bảng ánh xạ tài khoản, dựng bút toán tổng hợp
// tất định từ PeriodSummary của mkg/costing, xuất tệp CSV/XML byte-cho-byte, giãn cách thử lại.
// Không I/O, không đồng hồ hệ thống, không float (R-10).
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	costingport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
)

// Phiên bản quy tắc ghim vào bản ghi sổ.
const (
	RuleMapping = "acctbridge.mapping.v1"
	RuleJournal = "acctbridge.journal.v1"
)

// DimensionAny khớp mọi DimensionKind của dòng tổng hợp (quy tắc dự phòng của một nhóm).
const DimensionAny = "*"

// Bên mang mã chiều (đối tượng công nợ…) của một quy tắc.
const (
	DimOnNone   = "none"
	DimOnDebit  = "debit"
	DimOnCredit = "credit"
)

// Rule ánh xạ (nhóm chi phí/doanh thu × loại chiều) → cặp tài khoản Nợ/Có. Dòng tổng hợp dương ghi
// Nợ DebitAccount / Có CreditAccount; dòng âm (điều chỉnh) ghi đảo bên với trị tuyệt đối.
type Rule struct {
	Category      costingport.CostCategory `json:"category"`
	DimensionKind string                   `json:"dimension_kind"` // "*" = mọi chiều
	DebitAccount  string                   `json:"debit_account"`
	CreditAccount string                   `json:"credit_account"`
	// DimensionOn: tài khoản nào mang mã chiều (vd công nợ HTX ở bên Có 331) — none | debit | credit.
	DimensionOn string `json:"dimension_on"`
}

// Mapping là một phiên bản bảng ánh xạ đã duyệt.
type Mapping struct {
	Version     int
	Rules       []Rule
	Provisional bool
}

var (
	accountRe  = regexp.MustCompile(`^[0-9]{3,4}[0-9A-Za-z.]{0,16}$`)
	categoryRe = regexp.MustCompile(`^[a-z][a-z_]{1,39}$`)
	dimKindRe  = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)
)

// ErrRules là lỗi kiểm bảng ánh xạ (trường + lý do).
type ErrRules struct {
	Index int
	Msg   string
}

func (e ErrRules) Error() string { return fmt.Sprintf("rules[%d]: %s", e.Index, e.Msg) }

// ValidateRules kiểm một bảng ánh xạ: tài khoản dạng số hiệu (3–4 chữ số cấp 1, có thể thêm tiết khoản),
// Nợ ≠ Có, nhóm + chiều hợp lệ và không trùng (Category, DimensionKind).
func ValidateRules(rules []Rule) error {
	if len(rules) == 0 || len(rules) > 500 {
		return ErrRules{Index: 0, Msg: "cần 1..500 quy tắc"}
	}
	seen := map[string]bool{}
	for i, r := range rules {
		switch {
		case !categoryRe.MatchString(string(r.Category)):
			return ErrRules{i, "category không hợp lệ"}
		case r.DimensionKind != DimensionAny && !dimKindRe.MatchString(r.DimensionKind):
			return ErrRules{i, "dimension_kind không hợp lệ (\"*\" hoặc chữ thường)"}
		case !accountRe.MatchString(r.DebitAccount):
			return ErrRules{i, "debit_account phải là số hiệu tài khoản (vd 152, 6278)"}
		case !accountRe.MatchString(r.CreditAccount):
			return ErrRules{i, "credit_account phải là số hiệu tài khoản (vd 331)"}
		case r.DebitAccount == r.CreditAccount:
			return ErrRules{i, "tài khoản Nợ và Có phải khác nhau"}
		case r.DimensionOn != DimOnNone && r.DimensionOn != DimOnDebit && r.DimensionOn != DimOnCredit:
			return ErrRules{i, "dimension_on phải là none | debit | credit"}
		}
		k := string(r.Category) + "|" + r.DimensionKind
		if seen[k] {
			return ErrRules{i, "trùng (category, dimension_kind)"}
		}
		seen[k] = true
	}
	return nil
}

// CanonicalRules sắp quy tắc theo (Category, DimensionKind) — thứ tự lưu và băm tất định.
func CanonicalRules(rules []Rule) []Rule {
	out := append([]Rule(nil), rules...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].DimensionKind < out[j].DimensionKind
	})
	return out
}

// RulesSHA256 là băm tất định của bảng ánh xạ (sau CanonicalRules).
func RulesSHA256(rules []Rule) string {
	var b strings.Builder
	for _, r := range CanonicalRules(rules) {
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s\n", r.Category, r.DimensionKind, r.DebitAccount, r.CreditAccount, r.DimensionOn)
	}
	s := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(s[:])
}

// Lookup tìm quy tắc cho (nhóm, loại chiều): khớp đúng trước, rồi "*".
func (m Mapping) Lookup(cat costingport.CostCategory, dimKind string) (Rule, bool) {
	var any *Rule
	for i := range m.Rules {
		r := m.Rules[i]
		if r.Category != cat {
			continue
		}
		if r.DimensionKind == dimKind {
			return r, true
		}
		if r.DimensionKind == DimensionAny {
			any = &m.Rules[i]
		}
	}
	if any != nil {
		return *any, true
	}
	return Rule{}, false
}

// ErrMappingMissing: dòng tổng hợp không có quy tắc ánh xạ.
var ErrMappingMissing = errors.New("acctbridge: thiếu quy tắc ánh xạ tài khoản")

// DefaultRules là bảng ánh xạ mặc định — TẠM, CHỜ KHÁCH (chế độ kế toán, hệ thống tài khoản, tiết khoản
// chưa chốt; g4-conventions §1.2). Số hiệu theo hệ thống tài khoản doanh nghiệp VAS (kiểu Thông tư
// 200/2014/TT-BTC, cấp 1). Kế toán trưởng phải duyệt một phiên bản (có thể chính bảng này) trước khi
// dựng bút toán đầu tiên. proto D03: phải trả là NỢ PHẢI TRẢ (331/338), không cộng vào chi phí lần hai.
func DefaultRules() []Rule {
	return CanonicalRules([]Rule{
		{Category: costingport.CatStrawPurchase, DimensionKind: DimensionAny, DebitAccount: "152", CreditAccount: "331", DimensionOn: DimOnCredit},
		{Category: costingport.CatCollectionLabour, DimensionKind: DimensionAny, DebitAccount: "622", CreditAccount: "334", DimensionOn: DimOnNone},
		{Category: costingport.CatConsumables, DimensionKind: DimensionAny, DebitAccount: "627", CreditAccount: "152", DimensionOn: DimOnNone},
		{Category: costingport.CatTransportRoad, DimensionKind: DimensionAny, DebitAccount: "627", CreditAccount: "331", DimensionOn: DimOnNone},
		{Category: costingport.CatTransportWaterway, DimensionKind: DimensionAny, DebitAccount: "627", CreditAccount: "331", DimensionOn: DimOnNone},
		{Category: costingport.CatHubProcessing, DimensionKind: DimensionAny, DebitAccount: "627", CreditAccount: "331", DimensionOn: DimOnNone},
		{Category: costingport.CatStorage, DimensionKind: DimensionAny, DebitAccount: "627", CreditAccount: "331", DimensionOn: DimOnNone},
		{Category: costingport.CatHandling, DimensionKind: DimensionAny, DebitAccount: "627", CreditAccount: "331", DimensionOn: DimOnNone},
		{Category: costingport.CatLoss, DimensionKind: DimensionAny, DebitAccount: "632", CreditAccount: "154", DimensionOn: DimOnNone},
		{Category: costingport.CatSalesRevenue, DimensionKind: DimensionAny, DebitAccount: "131", CreditAccount: "511", DimensionOn: DimOnDebit},
		{Category: costingport.CatCarbonRevenue, DimensionKind: DimensionAny, DebitAccount: "131", CreditAccount: "511", DimensionOn: DimOnNone},
		{Category: costingport.CatCarbonShare, DimensionKind: DimensionAny, DebitAccount: "632", CreditAccount: "338", DimensionOn: DimOnNone},
	})
}

package domain

import (
	"strings"
	"unicode/utf8"

	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
)

// ValidateAdhocAnswer bounds raw input before every decimal parse. A new request is
// a fresh response namespace, not a writable copy of existing source/program cells.
func ValidateAdhocAnswer(sh Sheet, indicators []pd.AdhocIndicator, submit bool) []Issue {
	issues := []Issue{}
	add := func(key, message string) {
		issues = append(issues, Issue{Rule: RuleTemplate, Severity: SevError, Section: key, Adjustment: -1, Message: message})
	}
	if len(sh.Cells) > 0 || len(sh.Adjustments) > 0 || len(sh.Texts) > 0 {
		add("adhoc_values", "Phiếu bổ sung chỉ nhận câu trả lời của yêu cầu hiện tại")
	}
	if len(sh.AdhocValues) > 20 {
		add("adhoc_values", "Tối đa 20 chỉ tiêu")
		return issues
	}
	allowed := map[string]bool{}
	for _, d := range indicators {
		allowed[d.Code] = true
	}
	for key, raw := range sh.AdhocValues {
		if !allowed[key] {
			add("adhoc_values", "Chỉ tiêu không được giao")
			continue
		}
		if raw == "" {
			continue
		}
		if len(raw) > 23 {
			add("adhoc_values."+key, "Số quá dài")
			continue
		}
		n, ok := ParseNumber(raw)
		if !ok || n.IsNegative() {
			add("adhoc_values."+key, "Số không âm, tối đa 15 chữ số và 6 chữ số thập phân")
		}
	}
	if submit {
		for _, d := range indicators {
			if strings.TrimSpace(sh.AdhocValues[d.Code]) == "" {
				add("adhoc_values."+d.Code, "Bắt buộc nhập")
			}
		}
	}
	if utf8.RuneCountInString(sh.AdhocText) > 8000 || strings.ContainsRune(sh.AdhocText, 0) {
		add("adhoc_text", "Lời văn tối đa 8000 ký tự")
	}
	if submit && len(indicators) == 0 && strings.TrimSpace(sh.AdhocText) == "" {
		add("adhoc_text", "Bắt buộc nhập nội dung bổ sung")
	}
	return issues
}

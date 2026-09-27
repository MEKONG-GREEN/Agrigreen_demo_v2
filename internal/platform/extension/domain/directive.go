package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Directive là một chỉ đạo khẩn đã ban hành (FR-19, US-TASK-03).
type Directive struct {
	ID            uuid.UUID
	Title         string
	Body          string
	Severity      string
	AudienceUnits []string
	ProvinceCodes []string
	IssuerRef     uuid.UUID
	IssuedAt      time.Time
	ExpiresAt     *time.Time
	TargetCount   int
	AckCount      int
	AckedByMe     *time.Time
	IsTarget      bool
	SyncSeq       int64
}

// ValidateDirective kiểm nội dung chỉ đạo; trả danh sách đơn vị nhận đã chuẩn hóa (không trùng).
func ValidateDirective(d *Directive, now time.Time) error {
	d.Title, d.Body = strings.TrimSpace(d.Title), strings.TrimSpace(d.Body)
	if d.Severity == "" {
		d.Severity = "urgent"
	}
	seen := map[string]bool{}
	var units []string
	for _, u := range d.AudienceUnits {
		u = strings.TrimSpace(u)
		if u != "" && !seen[u] {
			seen[u] = true
			units = append(units, u)
		}
	}
	d.AudienceUnits = units
	switch {
	case len([]rune(d.Title)) < 5 || len([]rune(d.Title)) > 200:
		return fieldErr("title", "5–200 ký tự")
	case len([]rune(d.Body)) < 10 || len([]rune(d.Body)) > 5000:
		return fieldErr("body", "10–5000 ký tự")
	case d.Severity != "urgent" && d.Severity != "normal":
		return fieldErr("severity", "urgent hoặc normal")
	case len(units) == 0:
		// US-TASK-03 AC: chỉ gửi đúng vùng đã chọn, không gửi tràn toàn hệ thống.
		return fieldErr("audience_units", "chọn ít nhất một tỉnh/xã nhận")
	case len(units) > 500:
		return fieldErr("audience_units", "tối đa 500 đơn vị")
	case d.ExpiresAt != nil && !d.ExpiresAt.After(now):
		return fieldErr("expires_at", "phải sau thời điểm ban hành")
	case ContainsPII(d.Body):
		return FieldError{Field: "body", Err: ErrPIIInText}
	}
	return nil
}

// AckRate = đã xác nhận / đối tượng nhận, 4 chữ số thập phân; nil khi không có đối tượng.
func AckRate(acks, targets int64) *decimal.Decimal {
	if targets <= 0 {
		return nil
	}
	r := decimal.NewFromInt(acks).DivRound(decimal.NewFromInt(targets), 4)
	return &r
}

// Package domain: thực thể và quy tắc của pf/sync — thiết bị, con trỏ pull, bản
// ghi chiều lên, tệp minh chứng. Không phụ thuộc hạ tầng, không gọi time.Now (R-12).
package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// RuleVersion ghi vào sổ inbound (mục 8.1).
const RuleVersion = "sync-v1"

// Nền tảng thiết bị được hỗ trợ.
var platforms = map[string]bool{"android": true, "ios": true}

var appVersionRe = regexp.MustCompile(`^[0-9A-Za-z.+_-]{1,40}$`)

// Device là một thiết bị hiện trường đã đăng ký.
type Device struct {
	TenantID     uuid.UUID
	ID           uuid.UUID
	ActorRef     uuid.UUID
	OrgRef       *uuid.UUID
	Platform     string
	AppVersion   string
	RegisteredAt time.Time
	LastSeenAt   *time.Time
	RevokedAt    *time.Time
	RevokedBy    *uuid.UUID
	RevokeReason *string
}

// Revoked cho biết thiết bị đã bị thu hồi.
func (d Device) Revoked() bool { return d.RevokedAt != nil }

// Registration là dữ liệu đăng ký thiết bị máy gửi lên.
type Registration struct {
	DeviceID   string
	Platform   string
	AppVersion string
}

// ParseRegistration kiểm dữ liệu đăng ký. device_id phải là UUIDv7 sinh trên máy.
func ParseRegistration(r Registration) (id uuid.UUID, errs []problem.FieldError) {
	id, err := ids.Parse(r.DeviceID)
	if err != nil {
		errs = append(errs, problem.FieldError{Field: "device_id", Reason: "phải là UUIDv7 sinh trên máy"})
	}
	if !platforms[r.Platform] {
		errs = append(errs, problem.FieldError{Field: "platform", Reason: "phải là android hoặc ios"})
	}
	if !appVersionRe.MatchString(r.AppVersion) {
		errs = append(errs, problem.FieldError{Field: "app_version", Reason: "1–40 ký tự [0-9A-Za-z.+_-]"})
	}
	return id, errs
}

// ValidReason kiểm lý do thu hồi/đóng xung đột: 10–500 ký tự sau khi bỏ khoảng trắng.
func ValidReason(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= 10 && n <= 500
}

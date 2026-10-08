package app

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// OrgRecipients liệt kê tài khoản đang bật, email đã xác minh, thuộc tổ chức orgRef
// (HTX US-DASH-03: người nhận lịch báo cáo của quản lý HTX chỉ là tài khoản đã đăng ký
// của HTX). Email trả về đã che; địa chỉ đầy đủ chỉ tra lúc gửi (EmailOf).
func (m MailDirectory) OrgRecipients(ctx context.Context, tenantID, orgRef uuid.UUID) ([]port.Recipient, error) {
	if m.Dir == nil {
		return nil, ErrNoDirectory
	}
	users, err := m.Dir.FindUsers(ctx, port.Query{Attributes: map[string]string{
		port.AttrTenantID: tenantID.String(), port.AttrOrgRef: orgRef.String()}, Max: 200})
	if err != nil {
		return nil, err
	}
	out := []port.Recipient{}
	for _, u := range users {
		if u.TenantID != tenantID || u.OrgRef != orgRef || !u.Enabled || !u.EmailVerified || strings.TrimSpace(u.Email) == "" {
			continue
		}
		name := strings.TrimSpace(strings.TrimSpace(u.LastName) + " " + strings.TrimSpace(u.FirstName))
		out = append(out, port.Recipient{ActorRef: u.ID, DisplayName: name, MaskedEmail: MaskEmail(u.Email)})
	}
	return out, nil
}

// MaskEmail che phần tên hộp thư: "quan.ly@htx.vn" → "qu•••@htx.vn".
func MaskEmail(e string) string {
	e = strings.TrimSpace(e)
	at := strings.LastIndexByte(e, '@')
	if at <= 0 {
		return "•••"
	}
	local := []rune(e[:at])
	keep := 2
	if len(local) <= 2 {
		keep = 1
	}
	return string(local[:keep]) + "•••" + e[at:]
}

package domain

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SupportInput contains PII and stays in memory until sealed. Never log or put it in a ledger as plaintext.
type SupportInput struct {
	Name    string `json:"name" pii:"true"`
	Contact string `json:"contact" pii:"true"`
	Message string `json:"message" pii:"true"`
}

var supportPhone = regexp.MustCompile(`^\+?[0-9]{8,15}$`)

// NormalizeSupport validates bounded strings before parsing an email/contact.
func NormalizeSupport(in SupportInput) (SupportInput, error) {
	in.Name, in.Contact, in.Message = strings.TrimSpace(in.Name), strings.TrimSpace(in.Contact), strings.TrimSpace(in.Message)
	for _, f := range []struct {
		name, value string
		min, max    int
	}{
		{"name", in.Name, 1, 100}, {"contact", in.Contact, 1, 254}, {"message", in.Message, 10, 2000},
	} {
		n := utf8.RuneCountInString(f.value)
		if !utf8.ValidString(f.value) || n < f.min || n > f.max {
			return SupportInput{}, problem.Validation.WithField(f.name, "bắt buộc và không vượt giới hạn ký tự")
		}
		for _, ch := range f.value {
			if unicode.IsControl(ch) && (f.name != "message" || (ch != '\n' && ch != '\t')) {
				return SupportInput{}, problem.Validation.WithField(f.name, "không chứa ký tự điều khiển")
			}
		}
	}
	if !supportPhone.MatchString(in.Contact) {
		addr, err := mail.ParseAddress(in.Contact)
		if err != nil || addr.Address != in.Contact || !strings.Contains(addr.Address, ".") {
			return SupportInput{}, problem.Validation.WithField("contact", "nhập email hoặc số điện thoại hợp lệ")
		}
	}
	return in, nil
}

package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"strings"
)

// Removed with the legacy adapter in the second migration step.
func legacyDispatchProfile() port.DispatchProfile {
	return port.DispatchProfile{SubjectDepartment: "Phòng Trồng trọt", Salutation: "Kính gửi Chi cục,", SenderName: "Trung tâm Khuyến nông", TemplatePrefix: "kn.", SheetTemplateKey: "kn.gsheet"}
}
func (s *Service) dispatchProfile() (port.DispatchProfile, error) {
	if s.Policies == nil {
		return legacyDispatchProfile(), nil
	}
	if profile, ok := s.Policies.DispatchProfile(); ok {
		return profile, nil
	}
	return port.DispatchProfile{}, ErrDependency.WithDetail("chưa đăng ký cấu hình gửi báo cáo")
}
func dispatchFileKey(profile port.DispatchProfile, formCode string) string {
	return profile.TemplatePrefix + strings.ToLower(formCode)
}

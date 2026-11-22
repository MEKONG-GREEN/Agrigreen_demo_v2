package extension

// Nhật ký hệ thống — phần chủ KN (AUDIT-LOG-KN, #73) trên cổng truy vấn dùng chung #70: nguồn nhật ký KN, quyền xem
// (chỉ phiên Admin KN) và sổ lượt xem bền pf_extension.kn_audit_view. Gốc lắp ráp đăng ký AuditSources() cùng nguồn
// gốc của pf/accounts vào AuditRegistry rồi gọi accounts.Module.UseAudit(registry, AuditAccess()).

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knaudit"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func newKNAudit(moduleDB tenant.Beginner, clk clock.Clock) *knaudit.Service {
	return &knaudit.Service{DB: moduleDB, Clock: clk}
}

// AuditSources trả các nguồn nhật ký của KN (sổ lượt xem, hồ sơ KN, kỳ/duyệt báo cáo, kỳ KHTC, cấp quyền CNTS).
func (m *Module) AuditSources() []accport.AuditSource { return m.audit.Sources() }

// AuditAccess trả quyền xem (chỉ phiên Admin KN, ghi cả lần bị từ chối) và bộ ghi bền lượt xem.
func (m *Module) AuditAccess() accport.AuditAccess { return m.audit.Access() }

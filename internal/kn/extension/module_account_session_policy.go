package extension

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// AccountHomeUnits giải đơn vị (Nút cấp 1/2) chứa một Nút — pf/party Departments thỏa giao diện này.
type AccountHomeUnits interface {
	DepartmentUnit(ctx context.Context, orgRef uuid.UUID) (uuid.UUID, error)
}

// AccountSessionPolicies (#60 KNSPLIT-2-ACCOUNTS-POLICY-KN) là adapter chủ KN của cổng accountsport.SessionPolicies:
// pf/accounts chỉ nhận hai phép chiếu hẹp (nhãn nhật ký hỗ trợ, đơn vị mặc định) đọc từ payload phiên KN đã được
// middleware giải ở server và chiếu qua sổ đăng ký phiên. Không đọc Principal.KN hay vai realm để suy quyền, không
// chiếu danh tính: actor, tenant, MFA luôn do xác thực giữ. Thiếu/sai ngữ cảnh → từ chối, không quay về đường cũ.
//
// home (có thể nil) giữ quy tắc KN cho phiên Admin không gắn đơn vị (lần gán Admin không có Nút/đơn vị): đơn vị mặc
// định là đơn vị chứa Nút đã xác thực của người gọi, đúng như cầu nối cũ. nil → phiên đó không có đơn vị mặc định.
func AccountSessionPolicies(home AccountHomeUnits) accountsport.SessionPolicies {
	return accountsport.SessionPolicies{SupportAudit: accountSupportAudit, DefaultUnit: func(ctx context.Context) (uuid.UUID, bool, error) {
		return accountDefaultUnit(ctx, home)
	}}
}

// accountSupportAudit trả nhãn nhật ký hỗ trợ của phiên chủ KN đã chọn. Phiên không ở chế độ hỗ trợ (hoặc mã phiên hỗ
// trợ rỗng) bị từ chối: chỉ phiên hỗ trợ mới có dòng nhật ký.
func accountSupportAudit(ctx context.Context) (accountsport.SupportAuditContext, error) {
	_, k, err := port.RequireSession(ctx)
	if err != nil {
		return accountsport.SupportAuditContext{}, err
	}
	if k.Support == nil || k.Support.SessionID == uuid.Nil {
		return accountsport.SupportAuditContext{}, problem.Forbidden.WithDetail("ngữ cảnh phiên không ở chế độ hỗ trợ")
	}
	return accountsport.SupportAuditContext{ContextID: k.ID, SessionID: k.Support.SessionID, OnBehalfOf: k.Support.OnBehalfOf,
		Audience: k.Role, Department: k.Department, Reason: k.Support.Reason}, nil
}

// accountDefaultUnit trả đơn vị mặc định của phiên chủ KN. handled=false chỉ khi request thật sự native (không chọn
// phiên, không có ngữ cảnh KN cũ). Mọi phiên đã chọn — kể cả hỏng, lạ profile, hoặc chỉ có ngữ cảnh KN cũ mà chưa gắn
// phiên chủ — đều handled=true: lỗi là kết quả cuối, pf/accounts không tự quay về đơn vị của token.
func accountDefaultUnit(ctx context.Context, home AccountHomeUnits) (uuid.UUID, bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return uuid.Nil, false, problem.Unauthenticated
	}
	// Principal.KN chỉ dùng để phát hiện ràng buộc dở dang và từ chối; không cấp vai, đơn vị hay quyền nào.
	if p.SelectedSession == nil && p.KN == nil {
		return uuid.Nil, false, nil
	}
	_, k, err := port.RequireSession(ctx)
	if err != nil {
		return uuid.Nil, true, err
	}
	if k.UnitRef != uuid.Nil || k.Role != port.SessionRoleAdmin || k.Support != nil || home == nil || p.OrgRef == uuid.Nil {
		return k.UnitRef, true, nil
	}
	// Admin toàn hệ thống (không chế độ hỗ trợ) không gắn đơn vị: dùng đơn vị chứa Nút đã xác thực (OrgRef của token,
	// không phải giá trị client gửi) — chỉ để chọn danh sách mặc định, không cấp thêm vai hay phạm vi nào. Nút không
	// thuộc đơn vị nào → không có đơn vị mặc định (pf/accounts trả 422; tham số org_ref tường minh vẫn dùng được).
	unit, err := home.DepartmentUnit(ctx, p.OrgRef)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return uuid.Nil, true, nil
	}
	if err != nil {
		return uuid.Nil, true, err
	}
	return unit, true, nil
}

package extension

import (
	"context"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// SessionProjection bridges the validated legacy account resolver to the KN
// owner WITHOUT the CNTS FN-14 grant scope. Kept for tests and packages that do
// not depend on grants; the API assembly registers Module.SessionProjection.
// New consumers read only the immutable owner payload. No payload comes from a
// client or overrides identity.
func SessionProjection() authz.SessionRegistration {
	return authz.SessionRegistration{Profile: "kn", Project: func(_ context.Context, p authz.Principal) (authz.Session, bool, error) {
		owned, ok := legacySessionContext(p)
		if !ok {
			return authz.Session{}, false, nil
		}
		s, err := port.NewSessionContext(owned)
		return s, true, err
	}}
}

// SessionProjection (#71 KN-GRANT-OWNER) là phép chiếu phiên chủ KN của module này: ngữ cảnh đã được pf/accounts giải
// ở server + phạm vi quyền CNTS FN-14 hiện hành (sổ cnts_grant) của người được cấp. Instance-owned (pool, đồng hồ của
// module), không trạng thái toàn cục. Lỗi đọc sổ → lỗi phiên (pf/accounts trả 403), không bao giờ quay về quyền realm.
func (m *Module) SessionProjection() authz.SessionRegistration {
	return authz.SessionRegistration{Profile: "kn", Project: func(ctx context.Context, p authz.Principal) (authz.Session, bool, error) {
		owned, ok := legacySessionContext(p)
		if !ok {
			return authz.Session{}, false, nil
		}
		scope, err := m.cntsGrantScope(ctx, p, owned)
		if err != nil {
			return authz.Session{}, true, err
		}
		owned.CNTSGrant = scope
		s, err := port.NewSessionContext(owned)
		return s, true, err
	}}
}

// cntsGrantScope chọn người được cấp và đọc phạm vi quyền:
//   - chỉ phiên Cấp Xã của phòng Chăn nuôi – Thủy sản (quyền FN-14 là quyền trong module CNTS); vai/phòng khác → nil;
//   - người được cấp = người gọi; chế độ hỗ trợ có tài khoản được hỗ trợ → tài khoản đó; hỗ trợ không gắn tài khoản
//     (Admin chỉ chọn vai + xã) → nil (không có người được cấp; giữ phạm vi Admin đã chọn — câu hỏi mở #71);
//   - xã của quyền giao với Scope của lần gán đã chọn (cùng người nhiều lần gán: mỗi phiên chỉ thấy xã của mình; người
//     đã từng có quyền CNTS thì phiên ở xã chưa được cấp là phạm vi rỗng, không quay về W11.07);
//   - tenant = tenant đã xác thực (RLS). Ngữ cảnh chỉ xem vẫn được chiếu (chỉ thu hẹp; ghi đã bị chặn tập trung).
func (m *Module) cntsGrantScope(ctx context.Context, p authz.Principal, k port.SessionContext) (*port.CNTSGrantScope, error) {
	if k.Role != port.SessionRoleCommune || k.Department != partyport.DeptChanNuoiThuySan || m == nil || m.model == nil {
		return nil, nil
	}
	grantee := p.ActorRef
	if k.Support != nil {
		grantee = k.Support.OnBehalfOf
	}
	if grantee == uuid.Nil {
		return nil, nil
	}
	return m.model.SessionGrants(ctx, p.TenantID, grantee, k.Scope.Allows)
}

func legacySessionContext(p authz.Principal) (port.SessionContext, bool) {
	k := p.KN
	if k == nil {
		return port.SessionContext{}, false
	}
	owned := port.SessionContext{
		ID: k.ID, AssignmentID: k.AssignmentID, Role: k.Role, Department: k.Department,
		OrgRef: k.OrgRef, UnitRef: k.UnitRef, Communes: k.Communes, Scope: k.Scope,
		DefaultRoute: k.DefaultRoute, ExpiresAt: k.ExpiresAt, MgmtLevel: k.MgmtLevel,
		Manager: k.Manager, ProvinceGroup: k.ProvinceGroup, Duties: k.Duties,
		InternalUnit: k.InternalUnit, Facilities: k.Facilities, Households: k.Households,
		Powers: k.Powers, ViewOnly: k.ViewOnly,
	}
	if k.Support != nil {
		owned.Support = &port.SessionSupport{SessionID: k.Support.SessionID, OnBehalfOf: k.Support.OnBehalfOf, Reason: k.Support.Reason}
	}
	return owned, true
}

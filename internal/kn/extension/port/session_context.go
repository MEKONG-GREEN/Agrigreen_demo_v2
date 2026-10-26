// Package port defines the KN-owned session contract. The shared authentication
// kernel only carries opaque bytes and a separate database projection.
package port

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

// Vai nghiệp vụ KN v7.7 trong ngữ cảnh phiên — KHÔNG phải vai realm (không mang tiền tố Role để
// realm_test không đòi khai trong Keycloak).
const (
	SessionRoleNational = "kn_tw"   // Cấp Quốc gia (chỉ xem)
	SessionRoleProvince = "kn_tinh" // Cấp Tỉnh
	SessionRoleCommune  = "kn_xa"   // Cấp Xã
	SessionRoleAdmin    = "admin"   // Admin — không phòng ban, toàn hệ thống
)

// SessionSupport mô tả chế độ hỗ trợ (FN-AD-04.07): Admin thao tác trong menu của cấp khác.
type SessionSupport struct {
	SessionID  uuid.UUID
	OnBehalfOf uuid.UUID // tài khoản được hỗ trợ; Nil khi Admin chỉ chọn vai + xã
	Reason     string    // lý do của request hiện tại (header X-AG-Reason), rỗng với request đọc
}

// SessionContext là ngữ cảnh phiên đã được server giải.
type SessionContext struct {
	ID           uuid.UUID
	AssignmentID uuid.UUID // Nil với ngữ cảnh hỗ trợ không gắn tài khoản
	Role         string    // SessionRoleNational | SessionRoleProvince | SessionRoleCommune | SessionRoleAdmin
	Department   string    // mã phòng ban; rỗng với Admin
	OrgRef       uuid.UUID // Nút của lần gán; Nil với Admin không gắn Nút
	UnitRef      uuid.UUID // đơn vị sở hữu phòng ban (Nút cấp 1/2)
	Communes     []string  // xã phụ trách (Cấp Xã)
	// Scope = Nút xuống dưới (Cấp Xã: chỉ các xã phụ trách). Phòng ban lọc riêng bằng AllowsDepartment.
	Scope        authz.Scope
	DefaultRoute string
	ExpiresAt    time.Time
	Support      *SessionSupport // khác nil: Admin đang hỗ trợ (thao tác ghi ghi nhật ký hỗ trợ)

	// Phân quyền trong vai (W11.07, D4 X3, D3 H0/C3–C5). Giải ở server từ sổ phụ thuộc tính vai
	// (pf_accounts.assignment_attr_current) và sổ ủy quyền — không lấy từ client.

	// MgmtLevel là mã cấp quản lý của lần gán (accounts/port.MgmtLevels, Q-059).
	MgmtLevel string
	// Manager: cấp quản lý ≥ phó trưởng phòng (kể cả phụ trách, lãnh đạo đơn vị) hoặc nhóm Cấp Tỉnh "Lãnh đạo
	// phòng" — quyền của lãnh đạo phòng, menu "Nhân sự của phòng" (Q-067).
	Manager bool
	// ProvinceGroup là nhóm trong Cấp Tỉnh (vien_chuc, tong_hop, chu_nhiem, lanh_dao_phong, ban_giam_doc).
	ProvinceGroup string
	// Duties là cờ nhiệm vụ của lần gán (SessionDutyPriceAdmin, SessionDutyFormOwner).
	Duties []string
	// InternalUnit là mã đơn vị nội bộ (phòng/trạm/trại/BGĐ — pf/party internal_unit) của người gọi: "UnitRef
	// nội bộ" của D3 H0. Cấp Xã: đơn vị quản lý (HS-37). KHÁC UnitRef ở trên (Nút sở hữu phòng ban).
	InternalUnit string
	// Facilities, Households: Cấp Xã giới hạn trong cơ sở (vd cơ sở giết mổ) / hộ (CNTS: phạm vi = xã HOẶC cơ
	// sở HOẶC hộ). Cả hai rỗng = cả các xã phụ trách. Chỉ thu hẹp, không bao giờ mở rộng Scope.
	Facilities []uuid.UUID
	Households []uuid.UUID
	// Powers là quyền nhận qua ủy quyền có hạn, còn hiệu lực, trong phòng ban của phiên.
	Powers []string
	// ViewOnly: ngữ cảnh chỉ xem — Lãnh đạo đơn vị xem phòng ban ngoài lần gán (BR-04 HCTH, Q-059), nhóm
	// "Ban Giám đốc". Middleware pf/accounts chặn mọi thao tác ghi; module dùng ReadOnly().
	ViewOnly bool
}

// AllowsDepartment cho biết dữ liệu của phòng ban code thuộc ngữ cảnh: Admin (không ở chế độ hỗ trợ)
// thấy mọi phòng ban; vai khác chỉ phòng ban của phiên.
func (k SessionContext) AllowsDepartment(code string) bool {
	if k.Role == SessionRoleAdmin {
		return true
	}
	return k.Department != "" && k.Department == code
}

// Allows cho biết ngữ cảnh được thao tác dữ liệu của đơn vị hành chính unit thuộc phòng ban dept.
func (k SessionContext) Allows(dept, unit string) bool {
	return k.AllowsDepartment(dept) && k.Scope.Allows(unit)
}

// ReadOnly: Cấp Quốc gia chỉ xem (BRD §5.1); ngữ cảnh chỉ xem của W11.07 (ViewOnly).
func (k SessionContext) ReadOnly() bool { return k.Role == SessionRoleNational || k.ViewOnly }

// Quyền nghiệp vụ ủy quyền được (W11.07, ủy quyền có hạn). Mặc định thuộc lãnh đạo phòng.
const (
	SessionPowerReopenPeriod = "mo_lai_ky"     // mở lại kỳ đã chốt (CNTS III.3.0, AC-04)
	SessionPowerApprove      = "duyet_bao_cao" // duyệt/xác thực báo cáo của phòng
)

// Cờ nhiệm vụ trong vai (W11.07).
const (
	SessionDutyPriceAdmin = "qtnv_bao_gia"   // Quản trị nghiệp vụ báo giá (Báo giá v1.3 FN-AD-04.03…06)
	SessionDutyFormOwner  = "phu_trach_bieu" // được chọn làm người phụ trách biểu (CNTS UR-TINH-08)
	// SessionDutyScheduleFollower: người theo dõi được phân công — duyệt lịch công tác của cán bộ xã khi phòng ban dùng quy
	// tắc duyệt "follower" (CNTS FN-12 BR-04, Q-057; W11.46).
	SessionDutyScheduleFollower = "theo_doi_lich"
)

// HasDuty cho biết lần gán có cờ nhiệm vụ d. Ngữ cảnh chỉ xem không mang nhiệm vụ.
func (k SessionContext) HasDuty(d string) bool { return !k.ReadOnly() && slices.Contains(k.Duties, d) }

// Can cho biết ngữ cảnh có quyền nghiệp vụ power: Admin (ngoài chế độ hỗ trợ) và lãnh đạo phòng Cấp Tỉnh có
// sẵn; người khác chỉ khi được ủy quyền còn hạn trong phòng ban của phiên. Ngữ cảnh chỉ xem không có quyền nào.
func (k SessionContext) Can(power string) bool {
	switch {
	case k.ReadOnly():
		return false
	case k.Role == SessionRoleAdmin && k.Support == nil:
		return true
	case k.Role == SessionRoleProvince && k.Manager:
		return true
	}
	return slices.Contains(k.Powers, power)
}

// limitedTargets: Cấp Xã có giới hạn cơ sở/hộ.
func (k SessionContext) limitedTargets() bool {
	return k.Role == SessionRoleCommune && (len(k.Facilities) > 0 || len(k.Households) > 0)
}

// AllowsFacility cho biết ngữ cảnh được thao tác cơ sở ref. Cấp Xã có giới hạn cơ sở/hộ → chỉ cơ sở trong
// danh sách; không giới hạn → mọi cơ sở (người gọi vẫn kiểm Scope theo xã của cơ sở).
func (k SessionContext) AllowsFacility(ref uuid.UUID) bool {
	return !k.limitedTargets() || slices.Contains(k.Facilities, ref)
}

// AllowsHousehold như AllowsFacility cho hộ.
func (k SessionContext) AllowsHousehold(ref uuid.UUID) bool {
	return !k.limitedTargets() || slices.Contains(k.Households, ref)
}

const sessionProfile = "kn"

type sessionPayload struct {
	Version int
	Context SessionContext
}

func sessionRoleValid(role string) bool {
	switch role {
	case SessionRoleNational, SessionRoleProvince, SessionRoleCommune, SessionRoleAdmin:
		return true
	default:
		return false
	}
}

func (k SessionContext) databaseScope() authz.SessionScope {
	s := authz.SessionScope{NodeID: k.OrgRef, Department: k.Department, UnitCodes: k.Communes, RequiresNode: k.Role == SessionRoleCommune}
	if k.Support != nil {
		s.OnBehalfOfRef = k.Support.OnBehalfOf
	}
	return s
}

// NewSessionContext projects already validated owner state. This is a server
// factory, never a decoder of headers, request bodies or identity claims.
func NewSessionContext(k SessionContext) (authz.Session, error) {
	if !sessionRoleValid(k.Role) {
		return authz.Session{}, authz.ErrInvalidSession
	}
	s, err := authz.NewSession(sessionProfile, k.ID, k.databaseScope())
	if err != nil {
		return authz.Session{}, err
	}
	payload, err := json.Marshal(sessionPayload{Version: 1, Context: k})
	if err != nil {
		return authz.Session{}, err
	}
	return s.WithPayload(payload), nil
}

// ReadSessionContext requires a registered owner payload. It never falls back to
// Principal.KN or token roles, even when those would grant broader permissions.
func ReadSessionContext(p authz.Principal) (SessionContext, error) {
	if p.SelectedSession == nil {
		return SessionContext{}, errSessionContextRequired
	}
	s := p.SelectedSession
	if !s.Valid() || s.Profile() != sessionProfile {
		return SessionContext{}, authz.ErrInvalidSession
	}
	var data sessionPayload
	decoder := json.NewDecoder(bytes.NewReader(s.Payload()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return SessionContext{}, authz.ErrInvalidSession
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return SessionContext{}, authz.ErrInvalidSession
	}
	k := data.Context
	if data.Version != 1 || !sessionRoleValid(k.Role) || k.ID != s.ID() {
		return SessionContext{}, authz.ErrInvalidSession
	}
	want, got := k.databaseScope(), s.Scope()
	if want.NodeID != got.NodeID || want.Department != got.Department || want.OnBehalfOfRef != got.OnBehalfOfRef || want.RequiresNode != got.RequiresNode || !slices.Equal(want.UnitCodes, got.UnitCodes) {
		return SessionContext{}, authz.ErrInvalidSession
	}
	return k, nil
}

var errSessionContextRequired = problem.New(428, "accounts.context_required", "Cần chọn vai trò và phòng ban (header X-AG-Context)")

// RequireSession applies KN role policy after trusted middleware bound the owner
// payload. Authentication and the actor remain the kernel's responsibility.
func RequireSession(ctx context.Context, roles ...string) (authz.Principal, SessionContext, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Principal{}, SessionContext{}, problem.Unauthenticated
	}
	k, err := ReadSessionContext(p)
	if err != nil {
		if p.SelectedSession == nil {
			return p, k, err
		}
		return p, k, problem.Forbidden.WithCause(err)
	}
	if len(roles) > 0 && !slices.Contains(roles, k.Role) {
		return p, k, problem.Forbidden.WithDetail("vai trò của ngữ cảnh phiên không được thao tác này")
	}
	return p, k, nil
}

package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Ngữ cảnh và danh bạ KN do pf/accounts (P1 — k/access ResolveContext, §25) cung cấp cho module nghiệp vụ
// (pf/extension lịch công tác, danh bạ trực) qua gốc lắp ráp. Khai ở lõi để phụ thuộc chỉ đi xuống (R-MOD-05).

// KNContext là ngữ cảnh KN của người gọi: vai trò, phòng ban, tỉnh, xã phụ trách, cấp quản lý.
type KNContext struct {
	Role         string // một trong Audience* của pf/party (pf/extension đặt tên KNRole*)
	Department   string // mã phòng ban (P2); rỗng với Admin
	ProvinceCode string // tỉnh của Nút (Cấp Tỉnh, Cấp Xã)
	Communes     []string
	// MgmtLevel là cấp quản lý của vai trò. Rỗng = không biết (chưa có P1) → mọi Cấp Tỉnh cùng phòng ban,
	// cùng tỉnh được coi là người duyệt (tạm, chờ khách Q-05).
	MgmtLevel string
}

// KNContextResolver (do P1 cung cấp qua gốc lắp ráp) trả ngữ cảnh KN của request hiện tại (X-AG-Context).
// ok = false: request không mang ngữ cảnh KN → module suy ngữ cảnh từ vai realm + phạm vi đơn vị (đường cũ).
type KNContextResolver interface {
	KNContext(ctx context.Context) (c KNContext, ok bool, err error)
}

// KNOfficer là một cán bộ có vai KN trong một phòng ban của một tỉnh.
type KNOfficer struct {
	ActorRef     uuid.UUID
	Level        string // ScheduleLevelCommune | ScheduleLevelProvince
	ProvinceCode string
	Communes     []string
	MgmtLevel    string // rỗng = không biết
}

// KNDirectory (do P1 cung cấp qua gốc lắp ráp, từ role_assignment_current) liệt kê cán bộ KN của phòng ban ×
// tỉnh: nguồn tab "Chưa đăng ký", người nhận nhắc và người duyệt nhận thông báo. Thiếu → module dùng danh bạ
// trực pf_extension.staff (mọi cán bộ coi là Phòng Trồng trọt, cấp quản lý không biết). Tenant truyền tường
// minh vì job của worker không có Principal.
type KNDirectory interface {
	Officers(ctx context.Context, tenantID uuid.UUID, department, provinceCode string) ([]KNOfficer, error)
}

// Cấp của người đăng ký lịch.
const (
	ScheduleLevelCommune  = "commune"
	ScheduleLevelProvince = "province"
)

// StaffContacts tra số điện thoại công vụ của cán bộ để "gọi/nhắn 1 chạm" từ danh
// bạ trực (KN US-DIR-02). Số nằm ở tài khoản Keycloak (pf/accounts), được đọc lúc
// người dùng bấm liên hệ — pf/extension không lưu, không ghi log (R-04, R-14).
type StaffContacts interface {
	// PhoneOf trả số điện thoại của tài khoản actorRef thuộc tenantID. Tài khoản
	// không có, đã khóa, khác tenant hoặc chưa khai số → ErrNoPhone.
	PhoneOf(ctx context.Context, tenantID, actorRef uuid.UUID) (string, error)
}

// ErrNoPhone: cán bộ chưa có số điện thoại dùng được.
var ErrNoPhone = errors.New("accounts: cán bộ chưa có số điện thoại")

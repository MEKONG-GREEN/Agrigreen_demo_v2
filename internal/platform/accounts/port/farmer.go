package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// RoleFarmer: nông dân dùng App HTX (g2-conventions §5). Phạm vi = thửa/yêu cầu của
// (các) nông hộ liên kết với tài khoản (FarmerHouseholds), KHÔNG theo org_ref: token
// nông dân không mang org_ref nên phạm vi pf/party của họ rỗng (mặc định chặn).
// pf/accounts là chủ duy nhất của hằng này; module khác import accounts/port.
const RoleFarmer = "farmer"

// Sự kiện tài khoản nông dân dùng lại pf.accounts.user.created (roles = [farmer],
// org_ref = HTX của nông hộ).

// HouseholdLink là một liên kết tài khoản nông dân ↔ nông hộ đang dùng. Không có dữ liệu cá nhân.
type HouseholdLink struct {
	HouseholdRef   uuid.UUID
	CooperativeRef uuid.UUID // HTX của nông hộ lúc liên kết
	CommuneCode    string
}

// FarmerHouseholds là cổng đọc liên kết tài khoản ↔ nông hộ cho module khác
// (cropcycle: phạm vi thửa của nông dân; extension: yêu cầu hỗ trợ). Tenant lấy từ
// context (tenant.WithID do middleware xác thực gắn).
//
// HouseholdsForSubject trả các nông hộ đang liên kết với tài khoản subject (= sub
// của token, Principal.ActorRef); không có liên kết → slice rỗng, nil error. Module
// gọi TỰ kiểm vai farmer — cổng không đọc Principal.
type FarmerHouseholds interface {
	HouseholdsForSubject(ctx context.Context, subject uuid.UUID) ([]HouseholdLink, error)
}

// ActionLinks phát liên kết hành động MỘT LẦN của Keycloak (action token
// execute-actions, vd UPDATE_PASSWORD) và TRẢ VỀ liên kết thay vì gửi email — để
// quản lý HTX in QR / gửi liên kết cho nông dân không có email (g2-conventions §1).
// Liên kết chứa token: bên gọi không được lưu, ghi log hay đưa vào lỗi.
//
// Keycloak gốc không có Admin REST trả liên kết; bản HTTP gọi phần mở rộng realm
// cấu hình ở AG_KC_ACTION_LINK_URL. Directory không hiện thực cổng này → ErrActionLinkUnsupported.
//
// grantID là mã lượt kích hoạt ở pf_accounts.activation_grant: phần mở rộng PHẢI
// gắn token với lượt này và từ chối token của lượt cũ khi đã phát lượt mới (phát lại
// thu hồi lượt trước — Keycloak gốc không thu hồi được action token đã phát).
type ActionLinks interface {
	ActionLink(ctx context.Context, id, grantID uuid.UUID, actions []string, lifespan time.Duration) (string, error)
}

// ErrActionLinkUnsupported: Keycloak chưa có phần mở rộng phát liên kết hành động.
var ErrActionLinkUnsupported = errors.New("accounts: chưa cấu hình phát liên kết hành động (AG_KC_ACTION_LINK_URL)")

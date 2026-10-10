// Package port là hợp đồng của pf/price (`cap/price`, ADR-0049) cho module khác và gốc lắp ráp (R-02: module khác chỉ
// import gói này).
//
// Điểm mở rộng cho các gói sau (ADR-0049 mục 4):
//   - Profile: cấu hình theo bên dùng (vai → cấp, vai quản trị danh mục, phòng ban chủ) ở gốc lắp ráp — không so tên ứng
//     dụng (R-MOD-04). W11.72 thêm cờ Quản trị nghiệp vụ, W11.74 thêm Profile.Schedule (giờ khóa, hạn phản hồi, nhắc, ngưỡng mặc định).
//   - Catalog: danh mục nền (đơn vị tính, nhóm hàng) có hiệu lực theo ngày — W11.73 (mặt hàng) và KHTC (Q-082) đọc qua đây.
//   - Series, Publish (v4.1 §26) và sự kiện price.published: W11.76 khai ở gói này khi có bảng giá ngày.
//   - PubSchema: schema view công bố; view đầu tiên v1_board_daily (W11.76) khai bằng chỉ thị //ag:pubview.
package port

import (
	"context"
	"net/http"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// PubSchema là schema view công bố của pf/price cho cap/analytics và KHTC (R-DB-10, ADR-0031 §2). Chưa có view
// (W11.76 thêm v1_board_daily bằng //ag:pubview pf_price_pub.board_daily v1). Chỉ vai r_price_pub đọc được.
const PubSchema = "pf_price_pub"

// Profile là cấu hình cap/price của một bên dùng (KN: cmd/api-platform/assembly/price.go).
type Profile struct {
	Name string
	// Department: mã phòng ban chủ dữ liệu giá của bên dùng (KN: Phòng Thông tin – Truyền thông). Menu là cấu hình
	// phòng ban của dom/extension; dữ liệu do pf/price sở hữu. Thông tin cho bên dùng — khung chưa lọc theo phòng ban.
	Department string
	// Roles: vai ngữ cảnh (KN: authz.KN*) hoặc vai realm → cấp (TW, TINH, XA). Vai không có ở đây không đọc được.
	Roles map[string]string
	// ManageBy: vai được ghi danh mục nền (đơn vị tính, nhóm hàng). W11.72 thêm cờ Quản trị nghiệp vụ báo giá.
	ManageBy []string
	// Schedule: lịch báo giá, giờ khóa, hạn phản hồi, nhắc, ngưỡng MẶC ĐỊNH (W11.74, port/calendar.go).
	Schedule ScheduleDefaults
}

// Unit là một đơn vị tính đang hiệu lực (phiên bản mới nhất có valid_from ≤ ngày hỏi).
type Unit struct {
	Code      string
	Name      string
	SortOrder int
	Version   int64
	ValidFrom time.Time // ngày (00:00 UTC của ngày giờ VN)
}

// Group là một nhóm hàng đang hiệu lực.
type Group struct {
	Code      string
	Name      string
	SortOrder int
	Version   int64
	ValidFrom time.Time
}

// Catalog là danh mục nền của tenant người gọi (tenant lấy từ ctx — tenant.WithID).
type Catalog interface {
	// Units trả đơn vị tính đang hiệu lực tại ngày at (giờ VN), sắp theo sort_order rồi mã.
	Units(ctx context.Context, at time.Time) ([]Unit, error)
	// Groups trả nhóm hàng đang hiệu lực tại ngày at, sắp theo sort_order rồi mã.
	Groups(ctx context.Context, at time.Time) ([]Group, error)
}

// Lỗi của module (docs/errors.md, mục pf/price).
var (
	ErrProfileNotFound = problem.New(http.StatusNotFound, "price.profile_not_found", "Không có cấu hình giá cho bên dùng này")
)

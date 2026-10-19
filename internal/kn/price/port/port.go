// Package port là hợp đồng của pf/price (`cap/price`, ADR-0049) cho module khác và gốc lắp ráp (R-02: module khác chỉ
// import gói này).
//
// Điểm mở rộng cho các gói sau (ADR-0049 mục 4):
//   - Profile: cấu hình theo bên dùng (vai → cấp, vai quản trị danh mục, phòng ban chủ) ở gốc lắp ráp — không so tên ứng
//     dụng (R-MOD-04). W11.72 thêm Profile.StewardDuty (cờ Quản trị nghiệp vụ), W11.74 thêm Profile.Schedule (giờ khóa, hạn phản hồi, nhắc, ngưỡng mặc định).
//   - Catalog: danh mục nền (đơn vị tính, nhóm hàng) có hiệu lực theo ngày — W11.73 (mặt hàng) và KHTC (Q-082) đọc qua đây.
//   - Boards (bảng giá ngày đã duyệt — vai trò Series/Publish của v4.1 §26) và sự kiện pf.price.board.published (W11.76,
//     port/board.go).
//   - PubSchema: schema view công bố — v1_board_daily, v1_ticket_week (W11.76, pf 000152).
package port

import (
	"context"
	"net/http"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// PubSchema là schema view công bố của pf/price cho cap/analytics và KHTC (R-DB-10, ADR-0031 §2). W11.76 (pf 000152):
// bảng giá đã duyệt theo ngày × mặt hàng (min/max toàn tỉnh — Q-084) và số phiếu theo tuần báo cáo × xã báo giá, kèm cờ
// tuần đã chốt. Hợp đồng cột: contracts/pubview/pf_price_pub.board_daily.v1.json, pf_price_pub.ticket_week.v1.json.
// Chỉ vai r_price_pub (thành viên: r_metrics, r_agg) đọc được.
//
//ag:pubview pf_price_pub.board_daily v1
//ag:pubview pf_price_pub.ticket_week v1
const PubSchema = "pf_price_pub"

// Profile là cấu hình cap/price của một bên dùng (KN: cmd/api-platform/assembly/price.go).
type Profile struct {
	// Session is the configured owner policy. Nil selects native realm roles only.
	Session SessionResolver `json:"-"`
	Name    string
	// Department: mã phòng ban chủ dữ liệu giá của bên dùng (KN: Phòng Thông tin – Truyền thông). Menu là cấu hình
	// phòng ban của dom/extension; dữ liệu do pf/price sở hữu. Thông tin cho bên dùng — khung chưa lọc theo phòng ban.
	Department string
	// Roles: vai ngữ cảnh (KN: authz.KN*) hoặc vai realm → cấp (TW, TINH, XA). Vai không có ở đây không đọc được.
	Roles map[string]string
	// ManageBy: vai quản trị (Admin) — ghi danh mục nền (đơn vị tính, nhóm hàng, nhóm vùng), xã báo giá, phân công;
	// cũng có mọi quyền của Quản trị nghiệp vụ.
	ManageBy []string
	// StewardDuty (W11.72): cờ nhiệm vụ trên lần gán Cấp Tỉnh cho quyền Quản trị nghiệp vụ báo giá — cấu hình lịch, giờ
	// khóa, nhắc, ngưỡng (W11.74), mặt hàng theo thời kỳ (W11.73). KN: authz.DutyPriceAdmin ("qtnv_bao_gia", đọc bằng
	// KNContext.HasDuty — W11.07). Rỗng = chỉ vai ManageBy.
	StewardDuty string
	// Schedule: lịch báo giá, giờ khóa, hạn phản hồi, nhắc, ngưỡng MẶC ĐỊNH (W11.74, port/calendar.go).
	Schedule ScheduleDefaults
	// Board: duyệt / mở duyệt bảng giá ngày (W11.76, port/board.go).
	Board BoardSettings
}

// Unit là một đơn vị tính đang hiệu lực (phiên bản mới nhất có valid_from ≤ ngày hỏi).
type Unit struct {
	Code      string
	Name      string
	SortOrder int
	Version   int64
	ValidFrom time.Time // ngày (00:00 UTC của ngày giờ VN)
}

// Region là một nhóm vùng đang hiệu lực (W11.72 — An Giang, Kiên Giang).
type Region struct {
	Code      string
	Name      string
	SortOrder int
	Version   int64
	ValidFrom time.Time
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

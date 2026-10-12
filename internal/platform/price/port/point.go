package port

// Xã báo giá + phân công (W11.72, pf 000144; BRD Báo giá v1.3 FN-AD-04.02, FN-AD-04.01): xã báo giá liên kết mã ĐVHC có
// hiệu lực (ADR-0032), nhóm vùng, cán bộ Xã phụ trách chính và cán bộ Cấp Tỉnh được phân công theo ngày hiệu lực.
// Phiếu báo giá (W11.75), tiến độ (W11.77) đọc qua Points.

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Point là một xã báo giá có hiệu lực tại một ngày.
type Point struct {
	Code          string // XBG + 4 chữ số, hệ thống sinh
	Name          string // tên xã theo ĐVHC
	AdminUnitCode string
	RegionCode    string    // nhóm vùng tại ngày (BR-04: báo cáo theo nhóm vùng của xã tại thời điểm báo giá)
	Since         time.Time // ngày bắt đầu báo giá
	Officer       uuid.UUID // cán bộ Xã phụ trách chính tại ngày (Nil = "Chưa có cán bộ Xã")
	Owner         uuid.UUID // cán bộ Cấp Tỉnh được phân công tại ngày (Nil = "Chưa phân công" — mọi Cấp Tỉnh xử lý)
}

// HandledBy cho biết cán bộ Cấp Tỉnh ref xử lý được xã (BR-02 FN-AD-04.01, BR-02 FN-TI-02.02): xã được phân công cho ref
// hoặc xã chưa phân công.
func (p Point) HandledBy(ref uuid.UUID) bool { return p.Owner == uuid.Nil || p.Owner == ref }

// Points là danh mục xã báo giá của tenant người gọi (tenant lấy từ ctx — tenant.WithID).
type Points interface {
	// PointsAt trả các xã báo giá đang hoạt động tại ngày at (giờ VN), sắp theo nhóm vùng rồi mã.
	PointsAt(ctx context.Context, at time.Time) ([]Point, error)
	// PointAt trả xã báo giá code tại ngày at; ok = false nếu không có hoặc không hoạt động tại ngày.
	PointAt(ctx context.Context, code string, at time.Time) (p Point, ok bool, err error)
}

// Officer là một cán bộ chọn được khi phân công (danh bạ do gốc lắp ráp nối — pf/accounts).
type Officer struct {
	Ref      uuid.UUID
	Name     string   // tên hiển thị (dữ liệu cá nhân — chỉ trả cho vai quản trị/Cấp Tỉnh)
	Level    string   // XA | TINH
	Communes []string // mã ĐVHC xã của lần gán (Cấp Xã)
}

// Officers là danh bạ cán bộ KN của bên dùng (tenant từ ctx): level XA = cán bộ Cấp Xã, TINH = cán bộ Cấp Tỉnh, của
// phòng ban Profile.Department. Không nối → phân công không kiểm người, màn chọn người nhập mã tham chiếu.
type Officers interface {
	Officers(ctx context.Context, profile Profile, level string) ([]Officer, error)
}

// Lỗi xã báo giá (docs/errors.md, mục pf/price).
var (
	ErrPointNotFound = problem.New(http.StatusNotFound, "price.point_not_found", "Không có xã báo giá này")
	ErrPointInactive = problem.New(http.StatusUnprocessableEntity, "price.point_inactive",
		"Xã báo giá không hoạt động tại ngày hiệu lực")
	ErrAdminUnitInvalid = problem.New(http.StatusUnprocessableEntity, "price.admin_unit_invalid",
		"Mã ĐVHC không phải xã hoặc không có hiệu lực tại ngày hiệu lực")
	ErrAdminUnitTaken = problem.New(http.StatusConflict, "price.admin_unit_taken",
		"Mã ĐVHC đã gắn với xã báo giá khác đang hoạt động")
	ErrRegionNotFound = problem.New(http.StatusUnprocessableEntity, "price.region_not_found",
		"Nhóm vùng không có hiệu lực tại ngày hiệu lực")
	ErrRegionInUse = problem.New(http.StatusConflict, "price.region_in_use",
		"Nhóm vùng còn xã báo giá đang hoạt động — không ngưng được")
	ErrOfficerInvalid = problem.New(http.StatusUnprocessableEntity, "price.officer_invalid",
		"Người được phân công không phải cán bộ đúng cấp của phòng ban báo giá")
	ErrPointNotAssigned = problem.New(http.StatusForbidden, "price.point_not_assigned",
		"Cán bộ Xã chỉ thao tác cho xã báo giá mình phụ trách")
)

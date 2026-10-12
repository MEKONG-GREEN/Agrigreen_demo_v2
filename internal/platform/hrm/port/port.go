// Package port là hợp đồng của pf/hrm (`dom/hrm`, ADR-0050) cho module khác và gốc lắp ráp (R-02: module khác chỉ
// import gói này).
//
// W11.21: danh mục nhân sự (FN-19) — Catalog đọc mục danh mục, bảng hệ số có hiệu lực, tham số có phiên bản. Gói sau của
// cùng module (W11.22 hồ sơ, W11.25 nâng bậc, W11.28 báo cáo Sở) đọc qua app trực tiếp; port này cho module khác (vd
// Contact W11.23 hiển thị chức vụ) và kiểm thử.
package port

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Access là cấu hình quyền của bên dùng (KN: cmd/api-platform/assembly/hrm.go) — không so tên ứng dụng.
type Access struct {
	// Department: mã phòng ban chủ (KN: hanh_chinh_tong_hop). Vai đọc phải có ngữ cảnh ở phòng ban này.
	Department string
	// ManageBy: vai ghi danh mục (FN-19: Admin — CRUD).
	ManageBy []string
	// ReadBy: vai chỉ xem trong phòng ban chủ (FN-19: Cấp Tỉnh – HCTH — R).
	ReadBy []string
}

// Item là phiên bản hiện hành của một mục danh mục.
type Item struct {
	Kind        string
	Code        string
	Name        string
	SortOrder   int
	Active      bool
	Placeholder bool // dữ liệu mẫu chưa được Phòng HCTH xác nhận (Q-070)
	Version     int64
	Attrs       json.RawMessage
	RecordedAt  time.Time
}

// Scale là bảng hệ số có hiệu lực của một ngạch/CDNN tại một ngày.
type Scale struct {
	GradeCode    string
	ValidFrom    time.Time
	Coefficients []decimal.Decimal // bậc 1..n
	HoldMonths   int
	Placeholder  bool
	Version      int64
}

// Rule là phiên bản có hiệu lực của một tham số (payload JSON đã kiểm — domain.PromotionRules, AchievementConversion).
type Rule struct {
	Key         string
	ValidFrom   time.Time
	Payload     json.RawMessage
	Placeholder bool
	Version     int64
}

// Catalog là danh mục nhân sự của tenant người gọi (tenant lấy từ ctx — tenant.WithID).
type Catalog interface {
	// Items trả các mục ĐANG DÙNG của loại kind (position, grade, …), sắp theo sort_order rồi mã.
	Items(ctx context.Context, kind string) ([]Item, error)
	// ScaleAt trả bảng hệ số của ngạch có hiệu lực tại ngày at (giờ VN); ok = false nếu chưa có.
	ScaleAt(ctx context.Context, grade string, at time.Time) (Scale, bool, error)
	// RuleAt trả tham số key có hiệu lực tại ngày at; ok = false nếu chưa có.
	RuleAt(ctx context.Context, key string, at time.Time) (Rule, bool, error)
}

// Lỗi của module (docs/errors.md, mục pf/hrm).
var (
	ErrKindUnknown   = problem.New(http.StatusNotFound, "hrm.kind_unknown", "Không có loại danh mục nhân sự này")
	ErrItemNotFound  = problem.New(http.StatusNotFound, "hrm.item_not_found", "Không có mục danh mục này")
	ErrItemExists    = problem.New(http.StatusConflict, "hrm.item_exists", "Mã đã có trong danh mục")
	ErrItemReference = problem.New(http.StatusUnprocessableEntity, "hrm.item_reference", "Tham chiếu tới mục danh mục không có hoặc đã ngừng sử dụng")
	ErrRuleUnknown   = problem.New(http.StatusNotFound, "hrm.rule_unknown", "Không có tham số nhân sự này")
)

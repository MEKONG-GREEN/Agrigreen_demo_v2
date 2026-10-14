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

	"github.com/google/uuid"
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

// --- W11.22 (pf 000150): hồ sơ nhân sự. ---

// Unit là một đơn vị nội bộ (phòng/trạm/trại/BGĐ) — đơn vị công tác HS-16, đơn vị quản lý HS-37.
type Unit struct {
	Code   string
	Name   string
	Kind   string
	Active bool
}

// Units là danh mục đơn vị nội bộ do gốc lắp ráp nối (KN: pf/accounts internal_unit, W11.07). nil → chỉ kiểm dạng mã.
type Units interface {
	// InternalUnits trả đơn vị nội bộ (kể cả đã ngừng) của đơn vị unitRef (Nút sở hữu phòng ban), theo thứ tự hiển thị.
	InternalUnits(ctx context.Context, unitRef uuid.UUID) ([]Unit, error)
}

// EnvRecords bật/tắt hồ sơ nhân sự ngoài cổng module per_tenant: "1" bật, "0" tắt; vắng → TẮT ở production (chưa có
// DPIA cho hồ sơ có dân tộc/tôn giáo/Đảng — Q-005), BẬT ở môi trường khác (dữ liệu tổng hợp). Danh mục không bị ảnh hưởng.
const EnvRecords = "AG_HRM_RECORDS"

// Lỗi hồ sơ nhân sự (docs/errors.md, mục pf/hrm).
var (
	ErrRecordsDisabled = problem.New(http.StatusForbidden, "hrm.records_disabled",
		"Hồ sơ nhân sự chưa bật cho dữ liệu thật (chờ đánh giá tác động — Q-005)")
	ErrStaffNotFound    = problem.New(http.StatusNotFound, "hrm.staff_not_found", "Không có hồ sơ nhân sự này")
	ErrEventNotFound    = problem.New(http.StatusNotFound, "hrm.event_not_found", "Không có dòng quá trình này trong hồ sơ")
	ErrDuplicatePerson  = problem.New(http.StatusConflict, "hrm.duplicate_person", "Số điện thoại đã thuộc một hồ sơ khác")
	ErrStepOutOfRange   = problem.New(http.StatusUnprocessableEntity, "hrm.step_out_of_range", "Bậc vượt số bậc của ngạch hoặc ngạch chưa có bảng hệ số")
	ErrNotApplicable    = problem.New(http.StatusUnprocessableEntity, "hrm.not_applicable", "Thẻ hoặc thao tác không áp dụng cho loại nhân sự này")
	ErrPermissionLocked = problem.New(http.StatusUnprocessableEntity, "hrm.permission_locked", "Quyền của Admin cố định, không chỉnh được")
)

// --- W11.23 (Contact FN-13): nguồn cán bộ thuộc tỉnh cho danh bạ dùng chung pf/contact. ---

// DirectoryEntry là một hồ sơ nhân sự cho danh bạ — KHÔNG PII: họ tên, SĐT, năm sinh, email đọc ở kho định danh qua
// SubjectRef (một bản ghi/người — FN-13 BR-01). Trạng thái, đơn vị, chức vụ là phép chiếu tại hôm nay.
type DirectoryEntry struct {
	StaffID      uuid.UUID
	SubjectRef   uuid.UUID
	AccountRef   uuid.UUID // tài khoản (sub Keycloak) của chính nhân sự; Nil nếu chưa gắn
	Code         string    // mã hồ sơ HS-01
	Type         string    // vien_chuc | hop_dong | can_bo_xa
	Status       string    // active | left | none (FN-08)
	Unit         string    // đơn vị công tác HS-16 (mã đơn vị nội bộ)
	MgmtUnit     string    // đơn vị quản lý HS-37 (cán bộ xã)
	Commune      string    // xã/phường của cán bộ xã
	Position     string    // mã chức vụ
	PositionName string    // tên chức vụ (danh mục FN-19)
	Title        string    // chức danh ghi trong sự kiện chức vụ (nếu có)
}

// Directory là nguồn cán bộ thuộc tỉnh (gồm cán bộ khuyến nông cấp xã) cho Contact (W11.23). Tenant lấy từ Principal
// trong ctx. KHÔNG áp ma trận quyền hồ sơ FN-20 (Contact có quyền riêng — BRD FN-13, FN-20 "xem Contact"); CÓ áp cổng
// dữ liệu thật AG_HRM_RECORDS: tắt → ErrRecordsDisabled (Contact khi đó chỉ có liên hệ ngoài tỉnh).
type Directory interface {
	StaffDirectory(ctx context.Context) ([]DirectoryEntry, error)
}

// --- W11.28 (pf 20261014000335): báo cáo Sở NS01–NS06, dashboard nhân sự. ---

// Lỗi báo cáo Sở (docs/errors/pf-hrm-reports.md).
var (
	ErrReportUnknown  = problem.New(http.StatusNotFound, "hrm.report_unknown", "Không có biểu báo cáo Sở này (NS01–NS06)")
	ErrReportBlocked  = problem.New(http.StatusUnprocessableEntity, "hrm.report_blocked", "Số liệu không đạt quy tắc kiểm tra mức chặn — chưa xuất được biểu")
	ErrExportNotFound = problem.New(http.StatusNotFound, "hrm.export_not_found", "Không có lần xuất báo cáo này")
)

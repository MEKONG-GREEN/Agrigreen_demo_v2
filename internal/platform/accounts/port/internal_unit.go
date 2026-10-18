package port

// W11.07 (D3 C5/H0, Q-068): danh mục đơn vị nội bộ của TTKN tỉnh — Ban Giám đốc, văn phòng, phòng, trạm, trại,
// "Tăng cường" (dòng NS05). Cây tổ chức (pf/party) chỉ có TTKNQG → tỉnh → xã và phòng ban (P2) là chiều nghiệp
// vụ (menu, ngữ cảnh đăng nhập); đơn vị nội bộ là nơi CHỨA NGƯỜI: đơn vị công tác của viên chức, đơn vị quản lý
// cán bộ xã (HS-37). Phòng (kind = phong) trỏ mã phòng ban P2 qua DepartmentCode.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Loại đơn vị nội bộ (tham số có phiên bản trong mã — domain.InternalUnitRuleVersion).
const (
	UnitKindBanGiamDoc = "ban_giam_doc"
	UnitKindVanPhong   = "van_phong"
	UnitKindPhong      = "phong"
	UnitKindTram       = "tram"
	UnitKindTrai       = "trai"
	UnitKindTangCuong  = "tang_cuong"
)

// InternalUnitKinds là danh mục loại đơn vị nội bộ theo thứ tự hiển thị.
var InternalUnitKinds = []CatalogItem{
	{Code: UnitKindBanGiamDoc, Label: "Ban Giám đốc"},
	{Code: UnitKindVanPhong, Label: "Văn phòng"},
	{Code: UnitKindPhong, Label: "Phòng"},
	{Code: UnitKindTram, Label: "Trạm"},
	{Code: UnitKindTrai, Label: "Trại"},
	{Code: UnitKindTangCuong, Label: "Tăng cường"},
}

// InternalUnit là một đơn vị nội bộ của một đơn vị (Nút cấp 1/2 của pf/party).
type InternalUnit struct {
	ID             uuid.UUID
	OrgRef         uuid.UUID
	Code           string
	Kind           string
	Name           string
	DepartmentCode string // kind = phong: mã phòng ban P2 (rỗng nếu phòng không có module)
	SortOrder      int
	Active         bool
	UpdatedBy      uuid.UUID
	UpdatedAt      time.Time
}

// InternalUnits là cổng đọc đơn vị nội bộ cho module khác (pf/hrm W11.22: đơn vị công tác; đánh giá: đơn vị
// quản lý cán bộ xã). Tenant lấy từ context.
type InternalUnits interface {
	// UnitInternalUnits trả đơn vị nội bộ (kể cả đã ngừng nếu includeInactive) của đơn vị, theo thứ tự hiển thị.
	UnitInternalUnits(ctx context.Context, unitRef uuid.UUID, includeInactive bool) ([]InternalUnit, error)
	// InternalUnitByCode trả một đơn vị nội bộ; ErrInternalUnitNotFound nếu không có.
	InternalUnitByCode(ctx context.Context, unitRef uuid.UUID, code string) (InternalUnit, error)
}

// ErrInternalUnitNotFound: đơn vị không có đơn vị nội bộ với mã này.
var ErrInternalUnitNotFound = errors.New("accounts: không có đơn vị nội bộ")

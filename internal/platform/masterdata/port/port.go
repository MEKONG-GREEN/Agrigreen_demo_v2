// Package port là giao diện công khai của pf/masterdata (R-02): danh mục, đơn vị
// hành chính có hiệu lực theo thời gian, tham số có phiên bản và phê duyệt.
//
// Quy tắc mục 16: định mức và tham số tra THEO THỜI ĐIỂM CỦA SỰ VIỆC, không theo
// thời điểm hiện tại — vụ đã qua tính bằng giá trị có hiệu lực lúc đó.
package port

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Level là cấp đơn vị hành chính. Sau sắp xếp 2025: chỉ còn tỉnh và xã.
type Level string

// Các cấp.
const (
	LevelProvince Level = "province"
	LevelCommune  Level = "commune"
)

// AdminUnit là một đơn vị hành chính tại một thời điểm.
type AdminUnit struct {
	Code       string // mã theo danh mục của Tổng cục Thống kê / Bộ Nội vụ
	Name       string
	Level      Level
	ParentCode string     // tỉnh của xã; rỗng với tỉnh
	ValidFrom  time.Time  // ngày có hiệu lực (theo giờ Việt Nam, 00:00)
	ValidTo    *time.Time // nil = còn hiệu lực
	// SuccessorCodes: đơn vị kế thừa khi bị sáp nhập/chia tách (đóng hiệu lực, không xóa).
	SuccessorCodes []string
}

// ErrUnitNotFound: không có đơn vị với mã đó tại thời điểm tra.
var ErrUnitNotFound = errors.New("masterdata: không có đơn vị hành chính tại thời điểm tra")

// AdminUnits tra đơn vị hành chính tại thời điểm at.
type AdminUnits interface {
	Get(ctx context.Context, code string, at time.Time) (AdminUnit, error)
	// Descendants trả mã của chính đơn vị và mọi đơn vị con có hiệu lực tại at.
	Descendants(ctx context.Context, code string, at time.Time) ([]string, error)
	// Current trả mã hiện hành tương ứng với một mã cũ (đi theo SuccessorCodes).
	Current(ctx context.Context, code string, at time.Time) ([]string, error)
}

// ParamValue là giá trị của một tham số tại thời điểm tra.
type ParamValue struct {
	Key           string
	Value         decimal.Decimal // tham số số học dùng decimal (R-10)
	Text          string          // tham số chuỗi (vd tên nguồn bản đồ nền)
	Unit          string
	Version       int
	EffectiveFrom time.Time
	SourceDoc     string // số văn bản ban hành (vd văn bản của DCRD)
}

// ErrParamNotFound: không có phiên bản ĐÃ PHÊ DUYỆT có hiệu lực tại thời điểm tra.
var ErrParamNotFound = errors.New("masterdata: không có tham số đã phê duyệt có hiệu lực")

// Params tra tham số đã phê duyệt có hiệu lực tại thời điểm của sự việc.
// Khóa tham số có thể mang chiều, vd "cgh.norm_ha_per_machine:<loại máy>:<khâu>".
type Params interface {
	At(ctx context.Context, key string, at time.Time) (ParamValue, error)
}

// CatalogItem là một mục danh mục dùng chung (giống lúa, khâu, loại máy, vụ...).
type CatalogItem struct {
	Catalog string // vd "rice_variety", "farm_stage", "machine_type", "season"
	Code    string
	Name    string
	Attrs   map[string]string
	Active  bool
}

// Catalogs tra danh mục. Mục không bị xóa — chỉ ngừng dùng (Active = false).
type Catalogs interface {
	Get(ctx context.Context, catalog, code string) (CatalogItem, error)
	List(ctx context.Context, catalog string) ([]CatalogItem, error)
}

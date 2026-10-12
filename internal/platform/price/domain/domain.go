// Package domain là quy tắc thuần của pf/price (`cap/price`, ADR-0049): mã danh mục, ngày hiệu lực, danh mục mặc định.
// Không I/O, không đồng hồ (R-12: ngày truyền vào). Không float (R-CODE-01, agnofloat): giá, % ở các gói sau là decimal.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// RuleVersion ghi vào mọi dòng sổ của module (R-13). Đổi khi đổi quy tắc ghi.
const RuleVersion = "price.v1"

// Kind là loại danh mục nền.
type Kind string

// Hai danh mục nền của khung (pf 000138). Mặt hàng (W11.73) tham chiếu mã của chúng.
const (
	KindUnit  Kind = "unit"
	KindGroup Kind = "group"
)

var (
	reUnitCode  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,19}$`)
	reGroupCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

// Entry là một phiên bản danh mục cần ghi.
type Entry struct {
	Kind      Kind
	Code      string
	Name      string
	SortOrder int
	ValidFrom time.Time // ngày nghiệp vụ (BusinessDay)
}

// ErrInvalid: dữ liệu danh mục sai (app đổi sang problem.Validation).
var ErrInvalid = errors.New("price: danh mục không hợp lệ")

// Validate kiểm một phiên bản danh mục (cùng ràng buộc CHECK của pf 000138).
func Validate(e Entry) error {
	var re *regexp.Regexp
	maxName := 0
	switch e.Kind {
	case KindUnit:
		re, maxName = reUnitCode, 50
	case KindGroup:
		re, maxName = reGroupCode, 100
	case KindRegion: // W11.72 nhóm vùng (pf 000145)
		re, maxName = reRegionCode, 100
	default:
		return fmt.Errorf("%w: loại %q", ErrInvalid, e.Kind)
	}
	if !re.MatchString(e.Code) {
		return fmt.Errorf("%w: mã %q", ErrInvalid, e.Code)
	}
	if n := utf8.RuneCountInString(e.Name); n < 1 || n > maxName {
		return fmt.Errorf("%w: tên %q dài 1–%d ký tự", ErrInvalid, e.Name, maxName)
	}
	if e.SortOrder < 0 || e.SortOrder > 100000 {
		return fmt.Errorf("%w: thứ tự %d", ErrInvalid, e.SortOrder)
	}
	if e.ValidFrom.IsZero() {
		return fmt.Errorf("%w: thiếu ngày hiệu lực", ErrInvalid)
	}
	return nil
}

// BusinessDay trả ngày nghiệp vụ (giờ VN) của thời điểm t, dạng 00:00 UTC của ngày đó — khớp cột date của DB.
func BusinessDay(t time.Time) time.Time {
	y, m, d := t.In(clock.Zone()).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DefaultGroups là 5 nhóm hàng mặc định (BRD Báo giá v1.3 FN-AD-04.03: Lúa gạo, Rau màu, Chăn nuôi, Thủy hải sản, Vật tư
// nông nghiệp). Mã ổn định; người quản trị đổi tên/thêm nhóm sau (W11.73).
func DefaultGroups(validFrom time.Time) []Entry {
	return []Entry{
		{Kind: KindGroup, Code: "lua_gao", Name: "Lúa gạo", SortOrder: 10, ValidFrom: validFrom},
		{Kind: KindGroup, Code: "rau_mau", Name: "Rau màu", SortOrder: 20, ValidFrom: validFrom},
		{Kind: KindGroup, Code: "chan_nuoi", Name: "Chăn nuôi", SortOrder: 30, ValidFrom: validFrom},
		{Kind: KindGroup, Code: "thuy_hai_san", Name: "Thủy hải sản", SortOrder: 40, ValidFrom: validFrom},
		{Kind: KindGroup, Code: "vat_tu_nong_nghiep", Name: "Vật tư nông nghiệp", SortOrder: 50, ValidFrom: validFrom},
	}
}

// DefaultUnits là đơn vị tính thường gặp của báo giá nông sản (giả định — BRD không liệt kê; chủ danh mục sửa sau).
func DefaultUnits(validFrom time.Time) []Entry {
	return []Entry{
		{Kind: KindUnit, Code: "kg", Name: "kg", SortOrder: 10, ValidFrom: validFrom},
		{Kind: KindUnit, Code: "ta", Name: "tạ", SortOrder: 20, ValidFrom: validFrom},
		{Kind: KindUnit, Code: "tan", Name: "tấn", SortOrder: 30, ValidFrom: validFrom},
		{Kind: KindUnit, Code: "lit", Name: "lít", SortOrder: 40, ValidFrom: validFrom},
		{Kind: KindUnit, Code: "con", Name: "con", SortOrder: 50, ValidFrom: validFrom},
		{Kind: KindUnit, Code: "trai", Name: "trái", SortOrder: 60, ValidFrom: validFrom},
		{Kind: KindUnit, Code: "bao", Name: "bao", SortOrder: 70, ValidFrom: validFrom},
		{Kind: KindUnit, Code: "chai", Name: "chai", SortOrder: 80, ValidFrom: validFrom},
	}
}

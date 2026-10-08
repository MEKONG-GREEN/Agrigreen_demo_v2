package port

import (
	"context"
	"errors"
	"time"
)

// Đơn vị hành chính có hiệu lực theo thời gian là port của k/orgtree (§19, §25 AdminUnitAt) — khai ở pf/party
// (k/orgtree) để module ở mọi tầng tra được mà phụ thuộc chỉ đi xuống (R-MOD-05). Dữ liệu và hiện thực hiện
// còn ở pf/masterdata (pf_masterdata.admin_unit) — chuyển về k/orgtree là việc của kế hoạch v4.1; pf/masterdata
// nối hiện thực vào qua gốc lắp ráp.

// Level là cấp đơn vị hành chính. Sau sắp xếp 2025: chỉ còn tỉnh và xã.
type Level string

// Các cấp.
const (
	LevelProvince Level = "province"
	LevelCommune  Level = "commune"
	// LevelDistrict chỉ còn trong dữ liệu lịch sử (hiệu lực trước 01/07/2025).
	LevelDistrict Level = "district"
)

// AdminUnit là một đơn vị hành chính tại một thời điểm.
type AdminUnit struct {
	Code       string // mã theo danh mục của Tổng cục Thống kê / Bộ Nội vụ
	Name       string
	Level      Level
	ParentCode string     // tỉnh của xã; rỗng với tỉnh
	ValidFrom  time.Time  // ngày có hiệu lực (theo giờ Việt Nam, 00:00)
	ValidTo    *time.Time // nil = còn hiệu lực; khác nil = thời điểm hết hiệu lực (không bao gồm)
	// SuccessorCodes: đơn vị kế thừa khi bị sáp nhập/chia tách (đóng hiệu lực, không xóa).
	SuccessorCodes []string
}

// ErrUnitNotFound: không có đơn vị với mã đó tại thời điểm tra.
var ErrUnitNotFound = errors.New("orgtree: không có đơn vị hành chính tại thời điểm tra")

// AdminUnits tra đơn vị hành chính tại thời điểm at.
type AdminUnits interface {
	Get(ctx context.Context, code string, at time.Time) (AdminUnit, error)
	// Descendants trả mã của chính đơn vị và mọi đơn vị con có hiệu lực tại at.
	Descendants(ctx context.Context, code string, at time.Time) ([]string, error)
	// Current trả mã hiện hành tương ứng với một mã cũ (đi theo SuccessorCodes).
	Current(ctx context.Context, code string, at time.Time) ([]string, error)
}

// AdminUnitTree (tùy chọn, hiện thực bởi adapter AdminUnits của pf/masterdata)
// liệt kê đơn vị con trực tiếp có hiệu lực tại at; parent rỗng → các tỉnh.
// Module gọi kiểm bằng type assertion trên AdminUnits để giữ tương thích với
// các bản giả trong kiểm thử (G-05, G-02).
type AdminUnitTree interface {
	Children(ctx context.Context, parent string, at time.Time) ([]AdminUnit, error)
}

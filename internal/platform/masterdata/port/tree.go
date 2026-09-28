package port

import (
	"context"
	"time"
)

// AdminUnitTree (tùy chọn, thực hiện bởi adapter AdminUnits của pf/masterdata)
// liệt kê đơn vị con trực tiếp có hiệu lực tại at; parent rỗng → các tỉnh.
// Module gọi kiểm bằng type assertion trên AdminUnits để giữ tương thích với
// các bản giả trong kiểm thử (G-05, G-02).
type AdminUnitTree interface {
	Children(ctx context.Context, parent string, at time.Time) ([]AdminUnit, error)
}

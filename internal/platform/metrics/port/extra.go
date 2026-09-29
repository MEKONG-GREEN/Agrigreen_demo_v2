package port

// Phần THÊM của G3-A5 (không đổi chữ ký/ngữ nghĩa phần đóng băng ở port.go):
// grain, liệt kê phạm vi cho job ghi sổ, định nghĩa trong context, hằng lớp GIS.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Grain là độ hạt của chỉ số.
type Grain string

// Độ hạt.
const (
	GrainDay    Grain = "day"    // một giá trị mỗi ngày nghiệp vụ (Scope.Dimension rỗng)
	GrainSeason Grain = "season" // một giá trị mỗi ngày cho từng vụ (Scope.Dimension = "DX-2026")
)

// ScopeLister là phần tùy chọn của một Source: liệt kê các phạm vi con (đơn vị,
// chiều) có dữ liệu tới Cutoff(d) để job metrics_snapshot ghi sổ. Kết quả gồm cả
// phạm vi toàn tenant (Units rỗng). Source không có ScopeLister → job chỉ ghi
// phạm vi toàn tenant.
type ScopeLister interface {
	Scopes(ctx context.Context, key Key, businessDate clock.Date, tenantID uuid.UUID) ([]Scope, error)
}

// ErrNoData: Source không có gì để tính cho phạm vi/ngày (vd vụ chưa có lần cân
// đối nào trước Cutoff) — job không ghi giá trị, khác với giá trị 0.
var ErrNoData = errors.New("metrics: không có dữ liệu nguồn cho phạm vi và ngày này")

// SeasonDimension dựng chiều vụ "DX-2026".
func SeasonDimension(code string, year int) string {
	return strings.ToUpper(code) + "-" + strconv.Itoa(year)
}

// ParseSeason đọc chiều vụ "DX-2026" → ("DX", 2026).
func ParseSeason(dim string) (code string, year int, ok bool) {
	i := strings.LastIndexByte(dim, '-')
	if i <= 0 {
		return "", 0, false
	}
	y, err := strconv.Atoi(dim[i+1:])
	if err != nil || y < 2000 || y > 2100 {
		return "", 0, false
	}
	return dim[:i], y, true
}

type defKey struct{}

// WithDefinition gắn định nghĩa có hiệu lực vào context trước khi gọi Source.AsOf
// (pf/metrics làm việc này). Source dùng DefinitionFrom để ghim DefinitionVersion
// và chọn công thức theo phiên bản.
func WithDefinition(ctx context.Context, d Definition) context.Context {
	return context.WithValue(ctx, defKey{}, d)
}

// DefinitionFrom trả định nghĩa đã gắn (ok = false khi gọi thẳng Source).
func DefinitionFrom(ctx context.Context) (Definition, bool) {
	d, ok := ctx.Value(defKey{}).(Definition)
	return d, ok
}

// Lớp GIS mà nhật ký lớp nhận (GIS FN-18). Thời tiết chưa có nguồn — bỏ qua.
const (
	LayerPlot          = "plot"
	LayerCoopBoundary  = "coop_boundary"
	LayerFacility      = "facility"
	LayerSeasonStage   = "season_stage"
	LayerCGHBalance    = "cgh_balance"
	LayerWaterway      = "waterway"
	LayerLogConsumer   = "pf.metrics.layer_log" // consumer của worker (vai metrics_app)
	JobMetricsSnapshot = "metrics_snapshot"     // job 00:45 giờ VN (sau anchor), chạy ở leader
)

// Layers là mọi lớp theo thứ tự hiển thị.
var Layers = []string{LayerPlot, LayerCoopBoundary, LayerFacility, LayerSeasonStage, LayerCGHBalance, LayerWaterway}

// GeometryRef là tham chiếu tới một dòng sổ hình học BẤT BIẾN của pf/geo; vì dòng
// sổ không bao giờ sửa, tra theo id là tra "như lúc đó" (as-of).
type GeometryRef struct {
	Kind string    // "plot_boundary" | "cooperative_boundary" | "facility_event"
	ID   uuid.UUID // id dòng sổ
}

// GeometryResolver tra hình học GeoJSON theo tham chiếu (pf/geo thực hiện).
// Tenant lấy từ context. Không có → không có trong map.
type GeometryResolver interface {
	Geometries(ctx context.Context, refs []GeometryRef) (map[GeometryRef]json.RawMessage, error)
}

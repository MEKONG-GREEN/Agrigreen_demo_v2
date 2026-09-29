// Package port là cửa công khai DUY NHẤT của module pf/metrics (R-02): tầng chỉ
// số có lịch sử (tài liệu kiến trúc mục 16; Phụ lục A cổng G3), ảnh chụp theo
// ngày + watermark, và nhật ký thay đổi lớp GIS (GIS FN-18..21).
//
// ĐÓNG BĂNG ở G3-0 để các đợt G3-1 làm song song (docs/dev/g3-conventions.md §6).
// Thêm phương thức mới vào interface MỚI, không vào interface đã đóng băng.
//
// Nguyên tắc (R-13, mục 16):
//   - Định nghĩa chỉ số là tệp khai báo CÓ PHIÊN BẢN trong kho mã; mỗi giá trị
//     ghim DefinitionVersion đã dùng.
//   - Giá trị của ngày nghiệp vụ D là giá trị "như hệ biết lúc hết ngày D": chỉ
//     dùng bản ghi có event_time thuộc phạm vi của định nghĩa tính tới hết D VÀ
//     recorded_at ≤ Cutoff(D). Sửa sai ghi nhận SAU Cutoff(D) không đổi giá trị
//     của D — kỳ đã chốt không bao giờ bị sửa (KN US-DASH-01/02, CGH FN-09..11).
//   - Tính trên bản sao chỉ đọc; không join xuyên schema (R-06) — nguồn đọc qua
//     port của module chủ dữ liệu hoặc qua sự kiện đã nhận (inbox).
//   - Không có dữ liệu cá nhân (R-14); phạm vi là mã đơn vị hành chính / tổ chức.
package port

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// DBRole là vai của module ở cụm pf (schema pf_metrics).
const DBRole = "metrics_app"

// Vai nghiệp vụ.
const (
	// RoleMetricsViewer: xem chỉ số và lịch sử trong phạm vi cây TW → Tỉnh → Xã.
	RoleMetricsViewer = "metrics_viewer"
)

// Key là mã chỉ số ổn định, vd "kn.farm_logs_reviewed", "cgh.balance.shortage_ha".
type Key string

// Aggregation là cách gộp của định nghĩa.
type Aggregation string

// Cách gộp.
const (
	AggSum   Aggregation = "sum"
	AggCount Aggregation = "count"
	AggAvg   Aggregation = "avg"
	AggMax   Aggregation = "max"
	AggLast  Aggregation = "last" // giá trị mới nhất theo event_time tới hết ngày
)

// Definition là một phiên bản định nghĩa chỉ số (tệp khai báo trong kho mã,
// người đề xuất ≠ người duyệt — R-56). Đổi công thức = Version mới; giá trị cũ
// giữ Version cũ.
type Definition struct {
	Key         Key
	Version     int
	Title       string
	Unit        string // vd "ha", "t", "count", "tCO2e", "%"
	Aggregation Aggregation
	// SourceModule: module chủ dữ liệu, vd "pf/cropcycle"; Source: nguồn cụ thể
	// (sự kiện hoặc port) — tính tới hết ngày theo event_time.
	SourceModule string
	Source       string
	// RuleVersion: phiên bản quy tắc tính (vd hệ số CO2 có phiên bản — chờ khách ký phương pháp).
	RuleVersion   string
	EffectiveFrom clock.Date
	ProposedBy    string
	ApprovedBy    string
}

// Scope là phạm vi tính: tenant + đơn vị (cây phân cấp, ADR-0009). Units rỗng =
// toàn tenant (chỉ cho vai phạm vi TW).
type Scope struct {
	TenantID uuid.UUID
	Units    []string
	// OrgRef: HTX / tổ chức khi chỉ số theo tổ chức (tùy chọn).
	OrgRef *uuid.UUID
	// Dimension (G3-A5, thêm — tương thích ngược): chiều phụ của chỉ số có
	// grain khác ngày, vd mã vụ "DX-2026" cho chỉ số theo vụ (CGH FN-10/11).
	// Rỗng với chỉ số theo ngày.
	Dimension string
}

// Value là giá trị chỉ số của một ngày nghiệp vụ trong một phạm vi.
type Value struct {
	Key               Key
	DefinitionVersion int
	BusinessDate      clock.Date
	Scope             Scope
	Value             decimal.Decimal // decimal, không float (R-10)
	Unit              string
	// Cutoff: mốc recorded_at đã áp — Cutoff(BusinessDate).
	Cutoff time.Time
	// Watermark: sự kiện cuối cùng đã tính (tái lập, kiểm toán).
	Watermark Watermark
	// ComputedAt: lúc giá trị được ghi (giá trị là bản ghi chỉ ghi thêm).
	ComputedAt time.Time
}

// Watermark là vị trí trong luồng sự kiện đã được tính vào một kết quả.
type Watermark struct {
	LastEventID uuid.UUID // event_id UUIDv7 lớn nhất đã tính
	RecordedAt  time.Time // recorded_at của sự kiện đó
}

// Cutoff là mốc recorded_at của ngày nghiệp vụ d: đầu ngày d+1 giờ Việt Nam
// (loại trừ). Mọi Source phải dùng cùng mốc này.
func Cutoff(d clock.Date) time.Time {
	return clock.StartOfBusinessDate(d).AddDate(0, 0, 1)
}

// Source trả giá trị lịch sử của chỉ số.
//
// AsOf(ctx, key, businessDate, scope): giá trị của key cho ngày businessDate
// trong scope, dựng CHỈ từ bản ghi có recorded_at < Cutoff(businessDate) (và
// event_time trong cửa sổ của định nghĩa có hiệu lực vào businessDate). Tất định:
// gọi lại bất kỳ lúc nào sau Cutoff trả cùng giá trị. Ngày chưa kết thúc →
// ErrNotClosed. Không có định nghĩa hiệu lực → ErrUnknownMetric. Phạm vi vượt
// quyền người gọi → kiểm ở app/ bằng scope.Allows (không ở Source).
type Source interface {
	AsOf(ctx context.Context, key Key, businessDate clock.Date, scope Scope) (Value, error)
}

// Lỗi ổn định (mã metrics.* — docs/errors.md).
var (
	ErrUnknownMetric = errors.New("metrics: không có định nghĩa chỉ số hiệu lực")
	ErrNotClosed     = errors.New("metrics: ngày nghiệp vụ chưa kết thúc")
)

// ---------------------------------------------------------------------------
// Nhật ký lớp GIS + ảnh chụp + phát lại (GIS FN-18 US-EVL-01, FN-19 US-SNAP-01,
// FN-20 US-REPLAY-01, FN-21 US-RETAIN-01)
// ---------------------------------------------------------------------------

// LayerOp là loại thay đổi đối tượng trên lớp.
type LayerOp string

// Loại thay đổi.
const (
	LayerCreate  LayerOp = "create"
	LayerUpdate  LayerOp = "update"
	LayerRetire  LayerOp = "retire" // ngừng dùng — không xóa (RB-1)
	LayerRestore LayerOp = "restore"
)

// LayerChange là một dòng nhật ký thay đổi lớp (chỉ ghi thêm): trước/sau + nguồn.
// Nhận từ sự kiện của module chủ lớp (pf/geo, pf/cropcycle, pf/waterway, van hub…).
type LayerChange struct {
	ID         uuid.UUID // UUIDv7
	Layer      string    // vd "plot", "coop_boundary", "facility", "hub", "waterway", "season_stage"
	FeatureRef uuid.UUID
	Op         LayerOp
	// Before / After: thuộc tính + hình học GeoJSON của đối tượng (không dữ liệu cá nhân).
	Before json.RawMessage
	After  json.RawMessage
	// SourceEventID: event_id của sự kiện nguồn — khóa idempotency của Append.
	SourceEventID uuid.UUID
	SourceType    string // event_type nguồn
	EventTime     time.Time
	RecordedAt    time.Time
}

// LayerFeature là trạng thái một đối tượng tại một thời điểm.
type LayerFeature struct {
	FeatureRef uuid.UUID
	Attributes json.RawMessage
	Retired    bool
}

// LayerSnapshot là trạng thái một lớp cuối ngày nghiệp vụ, kèm watermark.
type LayerSnapshot struct {
	Layer        string
	BusinessDate clock.Date
	Watermark    Watermark
	Features     []LayerFeature
}

// LayerLog là nhật ký lớp + phát lại.
//
// Append ghi một thay đổi, idempotent theo SourceEventID (giao lặp → không ghi
// thêm, R-61).
//
// ReplayLayer(layer, d) = ảnh chụp mới nhất có BusinessDate ≤ d + mọi thay đổi có
// recorded_at > watermark của ảnh chụp và recorded_at < Cutoff(d), áp theo thứ tự
// (recorded_at, ID). Kết quả phải bằng ảnh chụp dựng thẳng từ nhật ký (kiểm thử
// bắt buộc). Ảnh chụp không thay nhật ký: lưu trữ/nén (FN-21) là chuyển tầng,
// không xóa (RB-1); số ngày lưu chờ khách.
type LayerLog interface {
	Append(ctx context.Context, tenantID uuid.UUID, c LayerChange) error
	ReplayLayer(ctx context.Context, tenantID uuid.UUID, layer string, d clock.Date) (LayerSnapshot, error)
}

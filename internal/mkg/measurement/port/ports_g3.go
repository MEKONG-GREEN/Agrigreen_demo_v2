package port

// Phần THÊM của G3-A2 (không đổi hợp đồng đóng băng ở port.go — chỉ interface
// mới và hằng mới, docs/dev/g3-conventions.md §6).

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Trạng thái ẩm khi cân (C-4 chưa chốt — TẠM, chờ khách).
const (
	MoistureNotMeasured = "not_measured" // không đo độ ẩm tại cân
	MoistureAsReceived  = "as_received"  // đo lúc nhận, chưa sấy
	MoistureAfterDrying = "after_drying" // đo sau mẻ sấy ở hub
)

// PricingBasis là cách tính giá của hợp đồng gắn với lô (prototype: cả hai có).
// Hợp đồng thật đến ở G4 (mkg/orders); G3 nhận như thuộc tính của lô.
type PricingBasis string

// Cách tính giá.
const (
	// PricingPerTonne: giá theo tấn nhà máy cân — đối chiếu số cuộn ↔ khối lượng cân
	// là kiểm chéo HAI BÊN chính thức (RB-6) → bản ghi đối soát có trạng thái cờ.
	PricingPerTonne PricingBasis = "per_tonne"
	// PricingPerBale: giá theo cuộn — số cuộn là căn cứ thanh toán; đối chiếu khối
	// lượng chỉ để tham khảo (không cờ).
	PricingPerBale PricingBasis = "per_bale"
)

// RollTotal là tổng số cuộn của một lô do mkg/fieldops đếm (công đoạn cuối, FM-04)
// kèm ước lượng kg/cuộn (học theo HTX → hệ thống → cấu hình → 20).
// yield_kg_per_roll KHÔNG BAO GIỜ qua van.
type RollTotal struct {
	LotRef            uuid.UUID
	Rolls             int64
	KgPerRollEstimate mass.Kg
	// EstimateSource: htx_learned | system | config | default.
	EstimateSource string
	// HTXConfirmed: có htx_confirmation_ref (đối soát chéo chiều mua, mục 13.3).
	HTXConfirmed bool
	AsOf         time.Time
}

// RollTotals là cửa đọc số cuộn của fieldops cho measurement (cùng tiến trình,
// hoặc bảng chiếu dựng từ sự kiện nội bộ mkg.fieldops.roll_count.recorded).
// ErrNotFound khi lô chưa có số cuộn. Hiện thực thật do đợt fieldops G3 cung cấp;
// measurement có bản giả để chạy khi chưa nối.
type RollTotals interface {
	RollTotal(ctx context.Context, tenantID, lotRef uuid.UUID) (RollTotal, error)
}

// HealthReport là sức khỏe thiết bị mà ingest thấy khi nhận lô (ADR-0019).
type HealthReport struct {
	DeviceID string
	Epoch    int
	// LastSeen: received_at của lô mới nhất (đồng hồ máy chủ).
	LastSeen time.Time
	// BufferPct: độ đầy bộ đệm của agent biên, decimal chuỗi "0.00".."100.00".
	BufferPct string
	// ClockSkewMs: độ lệch đồng hồ thiết bị so với máy chủ — ghi nhận, KHÔNG dùng làm thứ tự.
	ClockSkewMs int64
	// LastCounter: bộ đếm lớn nhất đã nhận.
	LastCounter int64
}

// DeviceHealth cập nhật bảng chiếu device_current từ ingest (vai ingest_mkg chỉ
// được UPDATE các cột sức khỏe). Không phải sổ: đây là trạng thái vận hành.
type DeviceHealth interface {
	ReportHealth(ctx context.Context, tenantID uuid.UUID, h HealthReport) error
}

// SessionStatus là trạng thái phiên cân.
type SessionStatus string

// Trạng thái phiên cân.
const (
	SessionOpen   SessionStatus = "open"
	SessionClosed SessionStatus = "closed"
)

// Session là một phiên cân của lô ở một điểm cân: thiết bị + cửa sổ bộ đếm.
type Session struct {
	ID          uuid.UUID
	LotRef      uuid.UUID
	Point       WeighPoint
	Route       Route
	FacilityRef uuid.UUID
	Status      SessionStatus
	// Window: CounterTo chỉ có nghĩa khi Status = SessionClosed.
	Window   Window
	OpenedAt time.Time
	ClosedAt *time.Time
}

// WeighingSessions cho ingest đọc cửa sổ lô đã đóng để tích phân (R-70).
// ErrNotFound khi không có; ErrSessionOpen khi phiên chưa đóng.
type WeighingSessions interface {
	Session(ctx context.Context, tenantID, sessionID uuid.UUID) (Session, error)
}

// ErrSessionOpen: phiên cân chưa đóng — chưa có cửa sổ để tích phân.
var ErrSessionOpen = errors.New("measurement: phiên cân chưa đóng")

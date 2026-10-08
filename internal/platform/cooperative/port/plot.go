package port

// Thửa, ranh giới thửa, xung đột ranh giới, ranh giới/vùng HTX (chuyển từ pf/geo ở W3.3, docs/design/cooperative.md
// mục 2.2, 6). Tên sự kiện v1 GIỮ tiền tố pf.geo.* (D-8): bên tiêu thụ (metrics) và contracts/events/*.json không
// đổi; chỉ chủ khai //ag:event chuyển sang module này. Mã lỗi geo.* và đường dẫn HTTP giữ nguyên.

import (
	"context"

	"github.com/google/uuid"
)

// Vai nghiệp vụ thửa (tên vai trong Keycloak). geo_admin (Admin API nhập tệp) và map_consumer vẫn do pf/geo khai;
// module kiểm hai vai đó bằng hằng của geoport.
const (
	// RolePlotEditor: vẽ/sửa/gán/ngừng dùng thửa trong phạm vi (cán bộ KN xã, Tổ KNCĐ, quản lý HTX).
	RolePlotEditor = "plot_editor"
	// RolePlotViewer: chỉ xem thửa trong phạm vi (TTKN QG, Cục KTHT, bản đồ GIS).
	RolePlotViewer = "plot_viewer"
)

// Tham số đọc qua masterdata/port.Params (có phiên bản + phê duyệt). Khóa giữ tiền tố geo.* (dữ liệu tham số đã có).
const (
	// ParamOverlapTolerancePct: ngưỡng chồng lấn (%) theo diện tích thửa nhỏ hơn; mặc định 1 khi chưa có phiên bản
	// được duyệt.
	ParamOverlapTolerancePct = "geo.overlap_tolerance_pct"
	// ParamAreaDeviationMaxPct: sai lệch tối đa (%) giữa diện tích đo và diện tích khai báo trước khi cần xác nhận
	// (HTX US-PLOT-02 AC-3); mặc định 5.
	ParamAreaDeviationMaxPct = "geo.area_deviation_max_pct"
)

// FarmerIdentity là cổng hẹp tới kho định danh cho tên nông dân của thửa KN (KN v7.7 FN-XA-03.05). Module không
// import vault client trực tiếp; bộ điều hợp: internal/platform/cooperative/port/farmerid (gốc lắp ráp nối).
type FarmerIdentity interface {
	// RegisterFarmer ghi họ tên vào kho định danh (chủ thể "người", mục đích extension.contact), trả ref;
	// tên đã có → ref đã có, không lỗi. Lỗi kho trả nguyên (problem 422 khi kho từ chối dữ liệu).
	RegisterFarmer(ctx context.Context, fullName, areaCode string) (uuid.UUID, error)
	// FarmerLabel trả nhãn đã che của nông dân; có thể trả nhãn kèm lỗi (nhãn dự phòng của kho).
	FarmerLabel(ctx context.Context, ref uuid.UUID) (string, error)
}

// Sự kiện thửa / ranh giới (schema trong contracts/events/).
const (
	EventPlotCreated          = "pf.geo.plot.created"
	EventPlotBoundaryRecorded = "pf.geo.plot_boundary.recorded"
	// EventPlotRetired / EventPlotRestored: ngừng dùng (xóa mềm, GIS BR-17) và khôi phục thửa — cùng giao dịch
	// với dòng sổ plot_event (GIS US-EVL-01).
	EventPlotRetired                 = "pf.geo.plot.retired"
	EventPlotRestored                = "pf.geo.plot.restored"
	EventCooperativeBoundaryRecorded = "pf.geo.cooperative_boundary.recorded"
)

// PlotRetiredV1 là payload của pf.geo.plot.retired, schema_version 1: status_before ∈ {draft, active},
// status_after = retired. Không có dữ liệu cá nhân (không household_ref), không mang hình (ranh giới không đổi).
// PlotEventID = dòng sổ plot_event vừa ghi.
//
//ag:event pf.geo.plot.retired v1
type PlotRetiredV1 struct {
	PlotRef        uuid.UUID  `json:"plot_ref"`
	PlotEventID    uuid.UUID  `json:"plot_event_id"`
	Code           string     `json:"code"`
	CommuneCode    string     `json:"commune_code"`
	CooperativeRef *uuid.UUID `json:"cooperative_ref"`
	StatusBefore   string     `json:"status_before"`
	StatusAfter    string     `json:"status_after"`
	Reason         string     `json:"reason"`
	ActorRef       uuid.UUID  `json:"actor_ref"`
	EventTime      string     `json:"event_time"`
}

// PlotRestoredV1 là payload của pf.geo.plot.restored, schema_version 1: status_before = retired, status_after ∈
// {draft, active} theo HTX. NeedsReview = khôi phục đã mở xung đột chồng lấn cùng HTX (người dùng xác nhận giữ nguyên).
//
//ag:event pf.geo.plot.restored v1
type PlotRestoredV1 struct {
	PlotRef        uuid.UUID  `json:"plot_ref"`
	PlotEventID    uuid.UUID  `json:"plot_event_id"`
	Code           string     `json:"code"`
	CommuneCode    string     `json:"commune_code"`
	CooperativeRef *uuid.UUID `json:"cooperative_ref"`
	StatusBefore   string     `json:"status_before"`
	StatusAfter    string     `json:"status_after"`
	Reason         string     `json:"reason"`
	NeedsReview    bool       `json:"needs_review"`
	ActorRef       uuid.UUID  `json:"actor_ref"`
	EventTime      string     `json:"event_time"`
}

// PlotCreatedV1 là payload của pf.geo.plot.created, schema_version 1.
//
//ag:event pf.geo.plot.created v1
type PlotCreatedV1 struct {
	PlotRef        uuid.UUID  `json:"plot_ref"`
	Code           string     `json:"code"`
	CommuneCode    string     `json:"commune_code"`
	CooperativeRef *uuid.UUID `json:"cooperative_ref"`
	HouseholdRef   *uuid.UUID `json:"household_ref"`
	Status         string     `json:"status"`
	BoundaryID     uuid.UUID  `json:"boundary_id"`
	AreaHa         string     `json:"area_ha"`
	Source         string     `json:"source"`
	ActorRef       uuid.UUID  `json:"actor_ref"`
	EventTime      string     `json:"event_time"`
}

// PlotBoundaryRecordedV1 là payload của pf.geo.plot_boundary.recorded, schema_version 1. Không mang hình (consumer
// đọc qua API); mang diện tích và phiên bản.
//
//ag:event pf.geo.plot_boundary.recorded v1
type PlotBoundaryRecordedV1 struct {
	PlotRef          uuid.UUID  `json:"plot_ref"`
	BoundaryID       uuid.UUID  `json:"boundary_id"`
	Version          int64      `json:"version"`
	BecameCurrent    bool       `json:"became_current"`
	AreaHa           string     `json:"area_ha"`
	PointCount       int        `json:"point_count"`
	Source           string     `json:"source"`
	CommuneCode      string     `json:"commune_code"`
	CooperativeRef   *uuid.UUID `json:"cooperative_ref"`
	SupersedesID     *uuid.UUID `json:"supersedes_id"`
	CorrectionReason *string    `json:"correction_reason"`
	ActorRef         uuid.UUID  `json:"actor_ref"`
	EventTime        string     `json:"event_time"`
	DeclaredAreaHa   *string    `json:"declared_area_ha,omitempty"`   // diện tích khai báo (nếu có)
	AreaDeviationPct *string    `json:"area_deviation_pct,omitempty"` // lệch so với khai báo (%)
}

// CooperativeBoundaryRecordedV1 là payload của pf.geo.cooperative_boundary.recorded. Không mang hình (consumer đọc
// qua API).
//
//ag:event pf.geo.cooperative_boundary.recorded v1
type CooperativeBoundaryRecordedV1 struct {
	CooperativeRef   uuid.UUID  `json:"cooperative_ref"`
	BoundaryID       uuid.UUID  `json:"boundary_id"`
	Version          int64      `json:"version"`
	Action           string     `json:"action"`
	Status           string     `json:"status"`
	AreaHa           string     `json:"area_ha"`
	PointCount       int        `json:"point_count"`
	Source           string     `json:"source"`
	CommuneCode      string     `json:"commune_code"`
	SupersedesID     *uuid.UUID `json:"supersedes_id"`
	CorrectionReason *string    `json:"correction_reason"`
	ActorRef         uuid.UUID  `json:"actor_ref"`
	EventTime        string     `json:"event_time"`
}

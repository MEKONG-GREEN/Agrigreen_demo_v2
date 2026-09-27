// Package port là giao diện công khai DUY NHẤT của module pf/geo mà module khác
// được import (R-02): vai, sự kiện, entity đồng bộ và tra cứu thửa.
package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Vai nghiệp vụ của module (tên vai trong Keycloak).
const (
	// RolePlotEditor: vẽ/sửa/gán/ngừng dùng thửa trong phạm vi (cán bộ KN xã, Tổ KNCĐ, quản lý HTX).
	RolePlotEditor = "plot_editor"
	// RolePlotViewer: chỉ xem thửa trong phạm vi (TTKN QG, Cục KTHT, bản đồ GIS).
	RolePlotViewer = "plot_viewer"
	// RoleGeoAdmin: Admin API — nhập tệp ranh giới thửa, ranh giới hành chính.
	RoleGeoAdmin = "geo_admin"
	// RoleCoopManager là vai quản lý HTX (khai ở pf/party). Người gọi có vai này
	// chỉ thấy/ghi thửa của chính HTX mình (Principal.OrgRef) và thửa nháp chưa
	// gán HTX trong xã (HTX US-PLOT-01 AC-1, AC-4).
	RoleCoopManager = "coop_manager"
)

// Entity đồng bộ ngoại tuyến (docs/dev/sync-contract.md).
const (
	EntityPlot         = "plot"          // chiều xuống
	EntityPlotBoundary = "plot_boundary" // chiều lên
)

// Tham số đọc qua masterdata/port.Params (có phiên bản + phê duyệt).
const (
	// ParamOverlapTolerancePct: ngưỡng chồng lấn (%) theo diện tích thửa nhỏ hơn;
	// mặc định 1 khi chưa có phiên bản được duyệt.
	ParamOverlapTolerancePct = "geo.overlap_tolerance_pct"
	// ParamAreaDeviationMaxPct: sai lệch tối đa (%) giữa diện tích đo và diện tích khai
	// báo trước khi cần xác nhận (HTX US-PLOT-02 AC-3); mặc định 5.
	ParamAreaDeviationMaxPct = "geo.area_deviation_max_pct"
)

// Trạng thái thửa.
const (
	StatusDraft   = "draft"   // KN vẽ trước, chưa gán HTX
	StatusActive  = "active"  // đã gán HTX
	StatusRetired = "retired" // ngừng dùng (xóa mềm, GIS BR-17)
)

// Plot là thông tin tối thiểu của thửa cho module khác (không có dữ liệu cá nhân).
type Plot struct {
	Ref            uuid.UUID
	Code           string
	CommuneCode    string
	CooperativeRef uuid.UUID // Nil = chưa gán HTX
	HouseholdRef   uuid.UUID // Nil = chưa gán nông hộ
	Status         string
	Version        int64
	AreaHa         decimal.Decimal
}

// ErrPlotNotFound: không có thửa trong tenant.
var ErrPlotNotFound = errors.New("geo: không có thửa")

// Plots tra cứu thửa cho module khác. Tenant lấy từ context. Không kiểm phạm vi
// người gọi — module gọi tự kiểm.
type Plots interface {
	Get(ctx context.Context, ref uuid.UUID) (Plot, error)
	// DistanceM trả khoảng cách trắc địa (mét, làm tròn) từ điểm WGS84 tới ranh
	// giới hiện hành của thửa; 0 nếu điểm nằm trong thửa (HTX US-GPS-01: ≤ 50 m).
	DistanceM(ctx context.Context, ref uuid.UUID, lon, lat decimal.Decimal) (decimal.Decimal, error)
}

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventPlotCreated          = "pf.geo.plot.created"
	EventPlotBoundaryRecorded = "pf.geo.plot_boundary.recorded"
)

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

// PlotBoundaryRecordedV1 là payload của pf.geo.plot_boundary.recorded, schema_version 1.
// Không mang hình (consumer đọc qua API); mang diện tích và phiên bản.
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

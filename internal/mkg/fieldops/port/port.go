// Package port là cửa công khai DUY NHẤT của module mkg/fieldops (R-02): lệnh
// gom, đếm cuộn tại thửa, xác nhận của HTX (docs/design/mkg.md §5.1).
//
// G3-0 chỉ đóng băng tên vai, tên sự kiện nội bộ, payload và tên hợp đồng van
// (dự thảo) để các đợt G3-1 làm song song. Interface đọc (vd cửa sổ lô cho
// measurement, số cuộn cho costing) do đợt fieldops G3 thêm vào package này.
package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// DBRole là vai của module ở cụm mkg (schema mkg_fieldops).
const DBRole = "fieldops_app"

// Vai nghiệp vụ (tài khoản tenant MKG).
const (
	// RoleFieldCollector: tổ gom — đếm cuộn tại thửa trên app hiện trường MKG (ngoại tuyến, mục 15).
	RoleFieldCollector = "field_collector"
	// RoleFieldSupervisor: lập / duyệt lệnh gom (người duyệt ≠ người lập, R-56), đối chiếu số cuộn.
	RoleFieldSupervisor = "field_supervisor"
)

// Sự kiện NỘI BỘ MKG (ADR-0018) — vhost mkg.internal, không bao giờ lên vhost nền tảng.
const EventRollCountRecorded = "mkg.fieldops.roll_count.recorded"

// Hợp đồng van (DỰ THẢO — contracts/publication/, chờ MKG phê duyệt người thật).
const (
	ContractCollectionSchedule     = "collection_schedule"
	ContractHTXCollectionStatement = "htx_collection_statement"
)

// RollCountRecordedV1 là payload khi một bản ghi đếm cuộn (hoặc bản ghi bù) được
// ghi vào sổ mkg_fieldops.roll_count. Người nhận nội bộ: mkg/measurement (đối
// chiếu cuộn ↔ khối lượng cho hợp đồng tính theo tấn — RB-6), mkg/costing (G4:
// yield_kg_per_roll — chỉ số biên lợi nhuận, không bao giờ qua van).
//
//ag:internal-event mkg.fieldops.roll_count.recorded v1
type RollCountRecordedV1 struct {
	RollCountID        uuid.UUID  `json:"roll_count_id"`
	LotRef             uuid.UUID  `json:"lot_ref"`
	PlotRef            uuid.UUID  `json:"plot_ref"`
	HTXRef             uuid.UUID  `json:"htx_ref"`
	Rolls              int        `json:"rolls"`
	Stage              string     `json:"stage"`
	HTXConfirmationRef *uuid.UUID `json:"htx_confirmation_ref"`
	SupersedesID       *uuid.UUID `json:"supersedes_id"`
	DeviceID           *string    `json:"device_id"`
	RuleVersion        string     `json:"rule_version"`
	EventTime          time.Time  `json:"event_time"`
}

// Sự kiện van (dự thảo) — payload là hợp đồng van, phát với outbox.KindPlatform.
const (
	EventCollectionSchedulePublished     = "mkg.fieldops.collection_schedule.published"
	EventHTXCollectionStatementPublished = "mkg.fieldops.htx_collection_statement.published"
)

// Publication là payload được phép đi qua van (chỉ struct //ag:publication cài đặt).
type Publication interface {
	PublicationName() string
}

// CollectionSchedulePublishedV1 là payload van collection_schedule v1 (dự thảo).
// Ngày dạng YYYY-MM-DD (ngày nghiệp vụ giờ Việt Nam).
//
//ag:publication collection_schedule
//ag:event mkg.fieldops.collection_schedule.published v1
type CollectionSchedulePublishedV1 struct {
	ScheduleRef uuid.UUID `json:"schedule_ref"`
	HTXRef      uuid.UUID `json:"htx_ref"`
	CommuneCode string    `json:"commune_code"`
	WindowFrom  string    `json:"window_from"`
	WindowTo    string    `json:"window_to"`
	Status      string    `json:"status"`
	Revision    int       `json:"revision"`
}

// PublicationName cài đặt Publication.
func (CollectionSchedulePublishedV1) PublicationName() string { return "collection_schedule" }

// HTXCollectionStatementPublishedV1 là payload van htx_collection_statement v1 (dự thảo): chỉ số cuộn.
//
//ag:publication htx_collection_statement
//ag:event mkg.fieldops.htx_collection_statement.published v1
type HTXCollectionStatementPublishedV1 struct {
	StatementRef uuid.UUID  `json:"statement_ref"`
	HTXRef       uuid.UUID  `json:"htx_ref"`
	PlotRef      uuid.UUID  `json:"plot_ref"`
	SeasonRef    *uuid.UUID `json:"season_ref"`
	CollectedOn  string     `json:"collected_on"`
	Rolls        int        `json:"rolls"`
	Revision     int        `json:"revision"`
}

// PublicationName cài đặt Publication.
func (HTXCollectionStatementPublishedV1) PublicationName() string { return "htx_collection_statement" }

// Công đoạn đếm (prototype FM-04: số cuộn mỗi công đoạn phải bằng công đoạn trước).
const (
	StageBale     = "bale"      // cuộn tại thửa
	StageGather   = "gather"    // tập kết
	StageBoatLoad = "boat_load" // xếp ghe / xe
)

// ---------------------------------------------------------------------------
// G3 (fieldops) — thêm vào, không đổi phần đã đóng băng ở G3-0.
// ---------------------------------------------------------------------------

// HTX là hồ sơ tối thiểu của một HTX đọc từ nền tảng (không có dữ liệu cá nhân).
type HTX struct {
	Ref         uuid.UUID
	CommuneCode string // admin_unit_code của HTX (mã xã 5 chữ số) — rỗng nếu nền tảng không có
	Active      bool
}

// Plot là thông tin tối thiểu của một thửa đọc từ nền tảng.
type Plot struct {
	Ref            uuid.UUID
	CooperativeRef *uuid.UUID // nil = thửa nháp chưa gán HTX
	CommuneCode    string
	Status         string // draft | active | retired
}

// PlatformDirectory là nguồn thửa/HTX của nền tảng. Hiện thực thật gọi
// Application API công khai bằng tài khoản dịch vụ tenant MKG (OIDC client
// credentials) — không bao giờ kết nối DB hay import vùng nền tảng (R-03).
//
// Câu hỏi RB-2 (g3-conventions §5): thửa/HTX thuộc tenant chương trình, token
// tenant MKG sẽ thấy rỗng (404) cho tới khi có cấp quyền chia sẻ tường minh.
// Vì vậy mọi hiện thực phải phân biệt ErrDirectoryNotFound với ErrDirectoryUnavailable.
type PlatformDirectory interface {
	Cooperative(ctx context.Context, htxRef uuid.UUID) (HTX, error)
	Plot(ctx context.Context, plotRef uuid.UUID) (Plot, error)
}

// Lỗi của PlatformDirectory.
var (
	ErrDirectoryNotFound    = errors.New("fieldops: nền tảng không có (hoặc không chia sẻ) thửa/HTX này")
	ErrDirectoryUnavailable = errors.New("fieldops: không gọi được Application API của nền tảng")
)

// WeighedMass cho fieldops đọc khối lượng CÂN của một lô gom (mkg/measurement,
// điểm cân đầu tiên của lô: HUB_IN, hoặc CUSTOMER_IN với tuyến DIRECT). Dùng để
// học kg/cuộn theo HTX (yield_kg_per_roll) — con số này KHÔNG BAO GIỜ qua van.
// ok = false khi lô chưa được cân.
type WeighedMass interface {
	WeighedKg(ctx context.Context, tenantID, lotRef uuid.UUID) (kg mass.Kg, weighedAt time.Time, ok bool, err error)
}

// LotRolls là số cuộn hiệu lực của một lô gom (= lệnh gom) ở công đoạn cuối có dữ liệu.
type LotRolls struct {
	LotRef  uuid.UUID
	HTXRef  uuid.UUID
	Stage   string // công đoạn cuối có dữ liệu (boat_load nếu đã xếp ghe/xe)
	Rolls   int
	PerPlot map[uuid.UUID]int
	// HTXConfirmed (G3-2): mọi thửa của lô ở công đoạn cuối đã được HTX xác nhận khớp
	// (đối soát chiều mua status = match, mục 13.3).
	HTXConfirmed bool
}

// RollTotals cho module mkg khác (measurement: cuộn ↔ khối lượng RB-6; costing G4)
// đọc số cuộn hiệu lực của lô gom (đã tính bản ghi bù). ErrLotNotFound khi không có lệnh gom.
type RollTotals interface {
	LotRolls(ctx context.Context, tenantID, lotRef uuid.UUID) (LotRolls, error)
}

// ErrLotNotFound: không có lệnh gom (lô gom) với mã này trong tenant.
var ErrLotNotFound = errors.New("fieldops: không có lô gom")

// ---------------------------------------------------------------------------
// G3-2 (tích hợp measurement) — thêm vào, không đổi phần đã có.
// ---------------------------------------------------------------------------

// KgPerRollEstimate là ước lượng kg/cuộn (yield_kg_per_roll — KHÔNG BAO GIỜ qua van).
type KgPerRollEstimate struct {
	Kg      mass.Kg // làm tròn 0,1 kg
	Source  string  // htx_learned | system | config | default
	Samples int
}

// KgPerRollEstimates cho measurement (đối soát cuộn ↔ khối lượng, RB-6) đọc ước lượng
// kg/cuộn của một HTX với tenant tường minh, chỉ học từ lô gom đã cân TRƯỚC mốc before
// và không gồm excludeLot (chính lô đang đối soát) — tất định khi chạy lại.
type KgPerRollEstimates interface {
	KgPerRollAsOf(ctx context.Context, tenantID, htxRef uuid.UUID, before time.Time, excludeLot uuid.UUID) (KgPerRollEstimate, error)
}

// WeighTicketConsumer: hàng đợi nội bộ mkg.measurement.weigh_ticket.recorded → bảng đệm
// học kg/cuộn (thay đường làm mới tay RefreshWeighings — đường tay vẫn giữ để chạy bù).
const WeighTicketConsumer = "mkg.fieldops.weigh_ticket"

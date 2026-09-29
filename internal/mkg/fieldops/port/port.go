// Package port là cửa công khai DUY NHẤT của module mkg/fieldops (R-02): lệnh
// gom, đếm cuộn tại thửa, xác nhận của HTX (docs/design/mkg.md §5.1).
//
// G3-0 chỉ đóng băng tên vai, tên sự kiện nội bộ, payload và tên hợp đồng van
// (dự thảo) để các đợt G3-1 làm song song. Interface đọc (vd cửa sổ lô cho
// measurement, số cuộn cho costing) do đợt fieldops G3 thêm vào package này.
package port

import (
	"time"

	"github.com/google/uuid"
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

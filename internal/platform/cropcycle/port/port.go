// Package port là giao diện công khai DUY NHẤT của module pf/cropcycle (App HTX:
// SOP, vụ mùa, nhật ký canh tác, sản lượng) mà module khác được import (R-02).
package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Vai nghiệp vụ của module (tên vai trong Keycloak).
const (
	// RoleFarmer: nông dân — phạm vi là thửa của nông hộ mình (liên kết tài khoản ↔
	// nông hộ qua FarmerHouseholds). Tự mở vụ, ghi nhật ký, khai sản lượng.
	RoleFarmer = "farmer"
	// RoleSOPAdmin: soạn quy trình SOP (Admin API). Phát hành cần param_approver
	// khác người soạn (R-56).
	RoleSOPAdmin = "sop_admin"
	// RoleCoopManager là vai quản lý HTX (khai ở pf/party): mở vụ hộ, ghi nhật ký
	// hộ, duyệt nhật ký, nhập sản lượng hàng loạt — chỉ HTX của mình (Principal.OrgRef).
	RoleCoopManager = "coop_manager"
)

// Entity đồng bộ ngoại tuyến (docs/dev/sync-contract.md).
const (
	EntitySOPVersion = "sop_version" // chiều xuống: phiên bản SOP đã phát hành
	EntitySeason     = "season"      // chiều xuống: vụ trong phạm vi
	EntityFarmLog    = "farm_log"    // chiều xuống (kèm trạng thái duyệt) và chiều lên
	EntitySeasonOpen = "season_open" // chiều lên: mở vụ / sửa vụ
	EntityLogBatch   = "farm_log_batch"
	EntityHarvest    = "harvest"
)

// Tham số đọc qua masterdata/port.Params (có phiên bản + phê duyệt). Chưa có
// phiên bản đã duyệt → dùng mặc định trong mã.
const (
	// ParamGPSMatchToleranceM: khoảng cách tối đa (m) từ điểm GPS tới ranh giới thửa
	// để gắn nhãn "khớp" (HTX US-GPS-01 AC-2); mặc định 50.
	ParamGPSMatchToleranceM = "cropcycle.gps_match_tolerance_m"
	// ParamYieldAnomalyMaxTPerHa: ngưỡng năng suất bất thường (t/ha) chung; mặc định 12.
	// Theo giống: khóa ParamYieldAnomalyMaxTPerHa + ":" + mã giống (HTX US-YIELD-03 AC-2).
	ParamYieldAnomalyMaxTPerHa = "cropcycle.yield_anomaly_max_t_per_ha"
)

// Trạng thái vụ.
const (
	SeasonOpen      = "open"
	SeasonCompleted = "completed"
	SeasonCancelled = "cancelled"
)

// Trạng thái duyệt nhật ký.
const (
	LogPending         = "pending"
	LogApproved        = "approved"
	LogNeedsSupplement = "needs_supplement"
	LogSuperseded      = "superseded"
)

// FarmerHouseholds ánh xạ tài khoản nông dân (actor_ref = sub của token) sang các
// nông hộ mà tài khoản được liên kết. pf/accounts hiện thực ở G2 (migration
// 000025, liên kết tài khoản ↔ nông hộ); chưa nối → vai farmer nhận 503.
type FarmerHouseholds interface {
	HouseholdsOf(ctx context.Context, actorRef uuid.UUID) ([]uuid.UUID, error)
}

// CoopActivity là số vụ đang mở và nhật ký chờ duyệt của một HTX — module khác
// dùng để cảnh báo trước khi vô hiệu hóa HTX. Không có dữ liệu cá nhân.
type CoopActivity struct {
	CooperativeRef uuid.UUID
	OpenSeasons    int64
	PendingLogs    int64
}

// CoopSeasonArea là tổng diện tích vụ (đang mở + đã hoàn thành) của HTX theo vụ/năm.
type CoopSeasonArea struct {
	CooperativeRef   uuid.UUID
	SeasonCode       string
	SeasonYear       int
	AreaHa           decimal.Decimal
	Seasons          int
	OpenSeasons      int
	CompletedSeasons int
}

// Stats là cổng đọc cho module khác. Tenant lấy từ context. Không kiểm phạm vi
// người gọi — module gọi tự giới hạn danh sách HTX theo phạm vi của mình.
type Stats interface {
	// ActivityByCooperative trả số vụ mở/nhật ký chờ duyệt; coops nil = mọi HTX có
	// hoạt động. HTX không có vụ mở lẫn nhật ký chờ duyệt không có trong kết quả.
	ActivityByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]CoopActivity, error)
	// SeasonAreas trả diện tích vụ của HTX; seasonCode rỗng / year 0 = mọi vụ / năm.
	SeasonAreas(ctx context.Context, coop uuid.UUID, seasonCode string, year int) ([]CoopSeasonArea, error)
}

// CoopSeasonSupply là sản lượng lúa theo HTX × vụ của một năm: vụ chưa khai sản
// lượng tính theo sản lượng dự kiến (diện tích × năng suất kỳ vọng của giống), vụ đã
// khai tính theo sản lượng thực tế; vụ bị hủy không tính. Không có dữ liệu cá nhân.
type CoopSeasonSupply struct {
	CooperativeRef  uuid.UUID
	SeasonCode      string
	SeasonYear      int
	AreaHa          decimal.Decimal
	PaddyKg         decimal.Decimal
	HarvestedAreaHa decimal.Decimal // phần diện tích đã khai sản lượng thực tế
	Seasons         int
}

// SupplyStats là cổng đọc nguồn cung cho mô phỏng chọn Hub (pf/simulation qua gốc lắp
// ráp, tham số #4, #5). Tenant lấy từ context; không kiểm phạm vi.
type SupplyStats interface {
	SeasonSupply(ctx context.Context, year int) ([]CoopSeasonSupply, error)
}

// GpsLogConsumer là consumer của worker ghi log GPS (pf/geo GpsLog) cho vụ, nhật ký,
// khai sản lượng SAU khi giao dịch nghiệp vụ đã commit (HTX US-GPS-03 AC-3): nhận sự
// kiện của chính module, đọc toạ độ ở sổ của mình, gọi RecordFix với khóa chống trùng
// theo bản ghi — lỗi tạm thời thì giao lại, không chặn nghiệp vụ.
const GpsLogConsumer = "pf.cropcycle.gps_log"

// GpsLogEvents là các sự kiện consumer GpsLogConsumer đăng ký.
var GpsLogEvents = []string{EventSeasonOpened, EventFarmLogRecorded, EventHarvestDeclared}

// ErrNotFound: không có bản ghi (dùng trong port khi cần).
var ErrNotFound = errors.New("cropcycle: không có bản ghi")

// Sự kiện module phát ra (schema trong contracts/events/, tên chốt ở g2-conventions §4).
const (
	EventSOPPublished          = "pf.cropcycle.sop.published"
	EventSeasonOpened          = "pf.cropcycle.season.opened"
	EventSeasonCompleted       = "pf.cropcycle.season.completed"
	EventSeasonCorrected       = "pf.cropcycle.season.corrected"
	EventFarmLogRecorded       = "pf.cropcycle.farm_log.recorded"
	EventFarmLogReviewed       = "pf.cropcycle.farm_log.reviewed"
	EventHarvestDeclared       = "pf.cropcycle.harvest.declared"
	EventCoopSeasonAreaChanged = "pf.cropcycle.coop_season_area.changed"
)

// SOPPublishedV1 là payload của pf.cropcycle.sop.published.
//
//ag:event pf.cropcycle.sop.published v1
type SOPPublishedV1 struct {
	TemplateRef       uuid.UUID  `json:"template_ref"`
	Code              string     `json:"code"`
	Name              string     `json:"name"`
	Scope             string     `json:"scope"`
	CooperativeRef    *uuid.UUID `json:"cooperative_ref"`
	VersionID         uuid.UUID  `json:"version_id"`
	VersionNo         int        `json:"version_no"`
	StepCount         int        `json:"step_count"`
	HarvestDayOffset  int        `json:"harvest_day_offset"`
	HarvestWindowDays int        `json:"harvest_window_days"`
	AuthoredBy        uuid.UUID  `json:"authored_by"`
	PublishedBy       uuid.UUID  `json:"published_by"`
	PublishedAt       string     `json:"published_at"`
}

// SeasonOpenedV1 là payload của pf.cropcycle.season.opened.
//
//ag:event pf.cropcycle.season.opened v1
type SeasonOpenedV1 struct {
	SeasonRef           uuid.UUID  `json:"season_ref"`
	EventID             uuid.UUID  `json:"event_id"`
	PlotRef             uuid.UUID  `json:"plot_ref"`
	PlotCode            string     `json:"plot_code"`
	HouseholdRef        *uuid.UUID `json:"household_ref"`
	CooperativeRef      uuid.UUID  `json:"cooperative_ref"`
	CommuneCode         string     `json:"commune_code"`
	SeasonCode          string     `json:"season_code"`
	SeasonYear          int        `json:"season_year"`
	VarietyCode         string     `json:"variety_code"`
	SOPVersionID        uuid.UUID  `json:"sop_version_id"`
	SowingDate          string     `json:"sowing_date"`
	ExpectedHarvestDate string     `json:"expected_harvest_date"`
	AreaHa              string     `json:"area_ha"`
	LocationStatus      string     `json:"location_status"`
	OnBehalf            bool       `json:"on_behalf"`
	ActorRef            uuid.UUID  `json:"actor_ref"`
	EventTime           string     `json:"event_time"`
}

// SeasonCompletedV1 là payload của pf.cropcycle.season.completed (khai sản lượng
// đóng vụ trong cùng giao dịch — HTX US-YIELD-01 AC-2).
//
//ag:event pf.cropcycle.season.completed v1
type SeasonCompletedV1 struct {
	SeasonRef      uuid.UUID  `json:"season_ref"`
	EventID        uuid.UUID  `json:"event_id"`
	PlotRef        uuid.UUID  `json:"plot_ref"`
	HouseholdRef   *uuid.UUID `json:"household_ref"`
	CooperativeRef uuid.UUID  `json:"cooperative_ref"`
	CommuneCode    string     `json:"commune_code"`
	SeasonCode     string     `json:"season_code"`
	SeasonYear     int        `json:"season_year"`
	VarietyCode    string     `json:"variety_code"`
	HarvestID      uuid.UUID  `json:"harvest_id"`
	HarvestDate    string     `json:"harvest_date"`
	AreaHa         string     `json:"area_ha"`
	ActorRef       uuid.UUID  `json:"actor_ref"`
	EventTime      string     `json:"event_time"`
}

// SeasonCorrectedV1 là payload của pf.cropcycle.season.corrected: sửa vụ
// (action = corrected) hoặc hủy vụ mở nhầm (action = cancelled).
//
//ag:event pf.cropcycle.season.corrected v1
type SeasonCorrectedV1 struct {
	SeasonRef           uuid.UUID  `json:"season_ref"`
	EventID             uuid.UUID  `json:"event_id"`
	Action              string     `json:"action"`
	Status              string     `json:"status"`
	PlotRef             uuid.UUID  `json:"plot_ref"`
	HouseholdRef        *uuid.UUID `json:"household_ref"`
	CooperativeRef      uuid.UUID  `json:"cooperative_ref"`
	CommuneCode         string     `json:"commune_code"`
	SeasonCode          string     `json:"season_code"`
	SeasonYear          int        `json:"season_year"`
	VarietyCode         string     `json:"variety_code"`
	SOPVersionID        uuid.UUID  `json:"sop_version_id"`
	SowingDate          string     `json:"sowing_date"`
	ExpectedHarvestDate string     `json:"expected_harvest_date"`
	SupersedesID        uuid.UUID  `json:"supersedes_id"`
	CorrectionReason    string     `json:"correction_reason"`
	ActorRef            uuid.UUID  `json:"actor_ref"`
	EventTime           string     `json:"event_time"`
}

// FarmLogRecordedV1 là payload của pf.cropcycle.farm_log.recorded (bản ghi mới
// hoặc bản ghi bù). Không mang toạ độ, ghi chú, vật tư — consumer đọc qua API.
//
//ag:event pf.cropcycle.farm_log.recorded v1
type FarmLogRecordedV1 struct {
	LogID          uuid.UUID  `json:"log_id"`
	LogRef         uuid.UUID  `json:"log_ref"`
	Version        int64      `json:"version"`
	SeasonRef      uuid.UUID  `json:"season_ref"`
	PlotRef        uuid.UUID  `json:"plot_ref"`
	HouseholdRef   *uuid.UUID `json:"household_ref"`
	CooperativeRef uuid.UUID  `json:"cooperative_ref"`
	CommuneCode    string     `json:"commune_code"`
	ActivityCode   string     `json:"activity_code"`
	SOPStepSeq     *int       `json:"sop_step_seq"`
	LocationStatus string     `json:"location_status"`
	GPSSource      string     `json:"gps_source"`
	BatchRef       *uuid.UUID `json:"batch_ref"`
	BecameCurrent  bool       `json:"became_current"`
	SupersedesID   *uuid.UUID `json:"supersedes_id"`
	OnBehalf       bool       `json:"on_behalf"`
	AuthorRef      uuid.UUID  `json:"author_ref"`
	EventTime      string     `json:"event_time"`
}

// FarmLogReviewedV1 là payload của pf.cropcycle.farm_log.reviewed. pf/notify báo
// cho người ghi (author_ref) khi yêu cầu bổ sung (HTX US-LOG-05 AC-3).
//
//ag:event pf.cropcycle.farm_log.reviewed v1
type FarmLogReviewedV1 struct {
	ReviewID       uuid.UUID `json:"review_id"`
	LogID          uuid.UUID `json:"log_id"`
	LogRef         uuid.UUID `json:"log_ref"`
	SeasonRef      uuid.UUID `json:"season_ref"`
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	Decision       string    `json:"decision"`
	Note           *string   `json:"note"`
	AuthorRef      uuid.UUID `json:"author_ref"`
	ReviewerRef    uuid.UUID `json:"reviewer_ref"`
	EventTime      string    `json:"event_time"`
}

// HarvestDeclaredV1 là payload của pf.cropcycle.harvest.declared (khai mới hoặc bản ghi bù).
//
//ag:event pf.cropcycle.harvest.declared v1
type HarvestDeclaredV1 struct {
	HarvestID        uuid.UUID  `json:"harvest_id"`
	SeasonRef        uuid.UUID  `json:"season_ref"`
	PlotRef          uuid.UUID  `json:"plot_ref"`
	HouseholdRef     *uuid.UUID `json:"household_ref"`
	CooperativeRef   uuid.UUID  `json:"cooperative_ref"`
	CommuneCode      string     `json:"commune_code"`
	SeasonCode       string     `json:"season_code"`
	SeasonYear       int        `json:"season_year"`
	VarietyCode      string     `json:"variety_code"`
	HarvestDate      string     `json:"harvest_date"`
	MassKg           string     `json:"mass_kg"`
	AreaHa           string     `json:"area_ha"`
	YieldTPerHa      string     `json:"yield_t_per_ha"`
	AnomalyKind      *string    `json:"anomaly_kind"`
	AnomalyConfirmed bool       `json:"anomaly_confirmed"`
	Source           string     `json:"source"`
	SupersedesID     *uuid.UUID `json:"supersedes_id"`
	ActorRef         uuid.UUID  `json:"actor_ref"`
	EventTime        string     `json:"event_time"`
}

// CoopSeasonAreaChangedV1 là payload của pf.cropcycle.coop_season_area.changed:
// tổng diện tích vụ của HTX sau thay đổi. pf/mechanization áp qua
// AreaFeed.ApplySyncedArea (CGH FN-07), dùng event_id làm khóa chống trùng.
//
//ag:event pf.cropcycle.coop_season_area.changed v1
type CoopSeasonAreaChangedV1 struct {
	CooperativeRef   uuid.UUID `json:"cooperative_ref"`
	SeasonCode       string    `json:"season_code"`
	SeasonYear       int       `json:"season_year"`
	AreaHa           string    `json:"area_ha"`
	Seasons          int       `json:"seasons"`
	OpenSeasons      int       `json:"open_seasons"`
	CompletedSeasons int       `json:"completed_seasons"`
	SourceAt         string    `json:"source_at"`
}

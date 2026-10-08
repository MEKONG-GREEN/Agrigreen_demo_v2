package port

// Vụ, nhật ký canh tác, SOP, sản lượng (chuyển từ pf/cropcycle ở W3.3, docs/design/cooperative.md mục 6).
// Tên sự kiện v1 GIỮ tiền tố pf.cropcycle.* (D-8): bên tiêu thụ (mechanization, metrics, notify, mrv) và
// contracts/events/*.json không đổi; chỉ chủ khai //ag:event chuyển sang module này.

import (
	"github.com/google/uuid"
)

// Vai nghiệp vụ của module (tên vai trong Keycloak). coop_manager do pf/party khai (lõi), lặp lại ở đây để
// module kiểm vai bằng hằng của mình.
const (
	// RoleFarmer: nông dân — phạm vi là thửa của nông hộ mình (liên kết tài khoản ↔ nông hộ qua
	// FarmerHouseholds). Tự mở vụ, ghi nhật ký, khai sản lượng.
	RoleFarmer = "farmer"
	// RoleSOPAdmin: soạn quy trình SOP (Admin API). Phát hành cần param_approver khác người soạn (R-56).
	RoleSOPAdmin = "sop_admin"
	// RoleCoopManager là vai quản lý HTX: mở vụ hộ, ghi nhật ký hộ, duyệt nhật ký, nhập sản lượng hàng loạt,
	// sửa hồ sơ HTX/hộ — chỉ HTX của mình (Principal.OrgRef).
	RoleCoopManager = "coop_manager"
)

// EntitySOPVersion: chiều xuống phiên bản SOP đã phát hành (docs/dev/sync-contract.md).
const EntitySOPVersion = "sop_version"

// Tham số đọc qua masterdata/port.Params (có phiên bản + phê duyệt). Chưa có phiên bản đã duyệt → dùng mặc
// định trong mã. Khóa giữ tiền tố cropcycle.* (dữ liệu tham số đã có).
const (
	// ParamGPSMatchToleranceM: khoảng cách tối đa (m) từ điểm GPS tới ranh giới thửa để gắn nhãn "khớp"
	// (HTX US-GPS-01 AC-2); mặc định 50.
	ParamGPSMatchToleranceM = "cropcycle.gps_match_tolerance_m"
	// ParamYieldAnomalyMaxTPerHa: ngưỡng năng suất bất thường (t/ha) chung; mặc định 12. Theo giống: khóa
	// ParamYieldAnomalyMaxTPerHa + ":" + mã giống (HTX US-YIELD-03 AC-2).
	ParamYieldAnomalyMaxTPerHa = "cropcycle.yield_anomaly_max_t_per_ha"
)

// GpsLogConsumer là consumer của worker ghi log GPS (pf/geo GpsLog) cho vụ, nhật ký, khai sản lượng SAU khi
// giao dịch nghiệp vụ đã commit (HTX US-GPS-03 AC-3). Tên consumer (khóa hộp thư inbox) giữ nguyên từ
// pf/cropcycle để sự kiện đã xử lý không bị giao lại sau W3.3.
const GpsLogConsumer = "pf.cropcycle.gps_log"

// GpsLogEvents là các sự kiện consumer GpsLogConsumer đăng ký.
var GpsLogEvents = []string{EventSeasonOpened, EventFarmLogRecorded, EventHarvestDeclared}

// Sự kiện vụ/nhật ký/SOP (schema trong contracts/events/, tên chốt ở g2-conventions §4).
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

// SeasonKey là mã vụ / năm của một vụ (Seasons.Codes — van thu gom pf/cropcycle gắn vụ cho bảng kê).
type SeasonKey struct {
	SeasonCode string
	SeasonYear int
}

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

package port

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// DTO chuyển nguyên trạng từ partyport, geoport, ccport (docs/design/cooperative.md mục 4) — cùng
// tên trường, cùng ngữ nghĩa. Không có dữ liệu cá nhân.

// UnitCooperatives là số HTX đang hoạt động và số hộ thành viên đang mở của một đơn vị hành chính
// (xã của HTX). Nguồn: partyport.UnitCooperatives.
type UnitCooperatives struct {
	UnitCode     string
	Cooperatives int64
	Households   int64
}

// CooperativeSite là một HTX đang hoạt động kèm xã, vị trí hồ sơ và số hộ thành viên đang mở.
// Nguồn: partyport.CooperativeSite.
type CooperativeSite struct {
	Ref         uuid.UUID
	Code        string
	CommuneCode string
	Lon, Lat    *decimal.Decimal // nil: hồ sơ HTX chưa có toạ độ
	Members     int64
}

// CoopCentroid là tâm vùng nguyên liệu của HTX (tâm ranh giới HTX đang dùng). Nguồn: geoport.CoopCentroid.
type CoopCentroid struct {
	CooperativeRef uuid.UUID
	Lon, Lat       decimal.Decimal
}

// HouseholdMembership là thuộc tính KHÔNG định danh của nông hộ. Nguồn: partyport.HouseholdMembership.
type HouseholdMembership struct {
	Ref            uuid.UUID
	CooperativeRef uuid.UUID // HTX hiện tại; Nil nếu chưa là thành viên
	CommuneCode    string
	Active         bool
}

// Plot là thông tin tối thiểu của thửa. Nguồn: geoport.Plot.
type Plot struct {
	Ref            uuid.UUID
	Code           string
	CommuneCode    string
	CooperativeRef uuid.UUID // Nil = chưa gán HTX
	HouseholdRef   uuid.UUID // Nil = chưa gán nông hộ
	Status         string    // Plot*
	Version        int64
	AreaHa         decimal.Decimal
}

// PlotStat là số thửa đã số hoá và tổng diện tích của một nhóm (xã hoặc HTX). Nguồn: geoport.PlotStat.
type PlotStat struct {
	CommuneCode    string    // theo xã (DigitizedByCommune)
	CooperativeRef uuid.UUID // theo HTX (DigitizedByCooperative)
	Plots          int64
	AreaHa         decimal.Decimal
}

// CoopActivity là số vụ đang mở và nhật ký chờ duyệt của một HTX. Nguồn: ccport.CoopActivity.
type CoopActivity struct {
	CooperativeRef uuid.UUID
	OpenSeasons    int64
	PendingLogs    int64
}

// CoopSeasonArea là tổng diện tích vụ (đang mở + đã hoàn thành) của HTX theo vụ/năm. Nguồn: ccport.CoopSeasonArea.
type CoopSeasonArea struct {
	CooperativeRef   uuid.UUID
	SeasonCode       string
	SeasonYear       int
	AreaHa           decimal.Decimal
	Seasons          int
	OpenSeasons      int
	CompletedSeasons int
}

// CoopSeasonSupply là sản lượng lúa theo HTX × vụ của một năm. Nguồn: ccport.CoopSeasonSupply.
type CoopSeasonSupply struct {
	CooperativeRef  uuid.UUID
	SeasonCode      string
	SeasonYear      int
	AreaHa          decimal.Decimal
	PaddyKg         decimal.Decimal
	HarvestedAreaHa decimal.Decimal // phần diện tích đã khai sản lượng thực tế
	Seasons         int
}

// ProductionQuery là truy vấn báo cáo sản lượng HTX (HTX US-DASH-02/03). Nguồn: ccport.ProductionQuery.
//   - CoopOnly: quản lý HTX — chỉ HTX CooperativeRef (bắt buộc), bỏ qua Scope;
//   - ngược lại Scope (All = toàn tenant; Units = mã xã/tỉnh; rỗng = không thấy gì) và
//     CooperativeRef (nếu có) là bộ lọc thêm.
type ProductionQuery struct {
	TenantID       uuid.UUID
	Scope          authz.Scope
	CoopOnly       bool
	CooperativeRef uuid.UUID
	SeasonCode     string // rỗng = mọi vụ
	SeasonYear     int    // 0 = mọi năm
	VarietyCode    string // rỗng = mọi giống
	ByCooperative  bool
	BySeason       bool
	ByVariety      bool
}

// ProductionFigures là số liệu dự kiến – thực tế của một nhóm. Nguồn: ccport.ProductionFigures.
type ProductionFigures struct {
	Seasons           int64
	OpenSeasons       int64
	CompletedSeasons  int64
	Households        int64
	AreaHa            decimal.Decimal
	HarvestedAreaHa   decimal.Decimal
	ExpectedKg        mass.Kg
	ActualKg          mass.Kg
	ActualYieldTPerHa *decimal.Decimal // nil = chưa có khai sản lượng
	SOPSteps          int64
	SOPStepsDone      int64
	SOPProgressPct    *decimal.Decimal // nil = không có bước SOP
}

// ProductionRow là một dòng theo HTX × vụ × giống (chiều không chọn để trống).
type ProductionRow struct {
	CooperativeRef uuid.UUID
	SeasonCode     string
	SeasonYear     int
	VarietyCode    string
	ProductionFigures
}

// ProductionData là các dòng và dòng tổng (tính lại trên toàn tập).
type ProductionData struct {
	Rows   []ProductionRow
	Totals ProductionFigures
}

// SeasonHarvestDate là ngày gieo / thu hoạch của một vụ (cropcycle/store.HarvestDates, không có con trỏ đồng bộ).
type SeasonHarvestDate struct {
	SeasonRef           uuid.UUID
	PlotRef             uuid.UUID
	CooperativeRef      uuid.UUID
	CommuneCode         string
	SeasonCode          string
	SeasonYear          int
	SowingDate          time.Time
	ExpectedHarvestDate time.Time
	HarvestOpenDate     time.Time
	Status              string // Season*
	CompletedAt         *time.Time
}

// DiaryEntry là phiên bản hiện hành của một nhật ký canh tác (cột của farm_log_current, không có
// GPS thô, ghi chú hay vật tư).
type DiaryEntry struct {
	ID             uuid.UUID // id phiên bản
	LogRef         uuid.UUID
	Version        int64
	SeasonRef      uuid.UUID
	PlotRef        uuid.UUID
	HouseholdRef   uuid.UUID // Nil = không gắn hộ
	CooperativeRef uuid.UUID
	CommuneCode    string
	ActivityCode   string
	SOPStepSeq     *int
	EventTime      time.Time
	LocationStatus string
	DistanceM      *decimal.Decimal
	AuthorRef      uuid.UUID
	OnBehalf       bool
	BatchRef       uuid.UUID // Nil = không thuộc lô
	Status         string    // Log*
	RecordedAt     time.Time
}

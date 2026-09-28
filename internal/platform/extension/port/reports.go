package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// ParamProgramTargetPrefix: mục tiêu diện tích Đề án 1 triệu ha của một tỉnh (ha),
// khóa đầy đủ "extension.program_target_ha:<mã tỉnh>" (FR-03). Chưa có → không
// tính tỷ lệ hoàn thành mục tiêu.
const ParamProgramTargetPrefix = "extension.program_target_ha"

// ProgramTargetKey dựng khóa tham số mục tiêu của tỉnh.
func ProgramTargetKey(province string) string { return ParamProgramTargetPrefix + ":" + province }

// ReportQuery là tham số đọc dữ liệu báo cáo cho pf/reports.
//
// Cổng tin cậy nội bộ: pf/reports đã kiểm vai của người yêu cầu và giải phạm vi
// (Scope) lúc yêu cầu; extension chỉ lọc theo Scope được truyền, không đọc
// Principal (worker chạy không có token người dùng).
type ReportQuery struct {
	TenantID   uuid.UUID
	Scope      authz.Scope
	Unit       string    // mã tỉnh hoặc xã; rỗng = toàn phạm vi
	Periods    []string  // kỳ tháng YYYY-MM (FR-05: tháng/quý/năm = danh sách tháng)
	SeasonCode string    // FR-03: lọc vụ; rỗng = mọi vụ
	SeasonYear int       // FR-03: 0 = mọi năm
	At         time.Time // thời điểm dữ liệu (nhiệm vụ quá hạn, tham số mục tiêu)
}

// Completeness cho biết số liệu đã đủ chưa (US-DASH-02 AC-1 "Số liệu tạm tính").
type Completeness struct {
	ExpectedCommunes int      // xã có cán bộ phụ trách trong phạm vi
	ReportedCommunes int      // trong đó đã có báo cáo của kỳ
	MissingCommunes  []string // xã chưa báo cáo
	OpenProvinces    []string // tỉnh chưa chốt MỌI kỳ trong truy vấn
	Unapproved       int      // FR-03: số dòng chưa được Admin tỉnh duyệt
	Provisional      bool
}

// SowingRow là diện tích đã sạ (FR-01) theo xã × vụ × giống, phiên bản hiện hành.
type SowingRow struct {
	ProvinceCode string
	CommuneCode  string
	SeasonCode   string
	VarietyCode  string
	SownAreaHa   decimal.Decimal
	Cooperatives int64
	Reports      int64
}

// SowingData là dữ liệu báo cáo gieo sạ.
type SowingData struct {
	Rows         []SowingRow
	Completeness Completeness
}

// PestRow là sâu bệnh (FR-02) theo xã × loại × mức độ.
type PestRow struct {
	ProvinceCode   string
	CommuneCode    string
	PestCode       string
	Severity       string
	AffectedAreaHa decimal.Decimal
	Observations   int64
	Urgent         int64
}

// PestData là dữ liệu báo cáo sinh trưởng & sâu bệnh.
type PestData struct {
	Rows         []PestRow
	Completeness Completeness
}

// ProgramRow là tiến độ Đề án (FR-03) của một HTX × vụ — phiên bản hiện hành.
type ProgramRow struct {
	ProvinceCode   string
	CommuneCode    string
	CooperativeRef uuid.UUID
	SeasonCode     string
	SeasonYear     int
	RegisteredHa   decimal.Decimal
	DeployingHa    decimal.Decimal
	CompletedHa    decimal.Decimal
	Approved       bool
}

// ProgramData là dữ liệu tiến độ Đề án kèm mục tiêu từng tỉnh (nil = chưa đặt).
type ProgramData struct {
	Rows         []ProgramRow
	Targets      map[string]*decimal.Decimal // mã tỉnh → mục tiêu (ha)
	Completeness Completeness
}

// TaskRow là tình hình xử lý nhiệm vụ hỗ trợ của một xã trong kỳ (US-TASK-02).
type TaskRow struct {
	ProvinceCode string
	CommuneCode  string
	Opened       int64 // tạo trong kỳ
	Resolved     int64 // đóng trong kỳ
	OnTime       int64 // trong đó đóng đúng hạn SLA
	Escalated    int64 // leo thang trong kỳ
	Open         int64 // đang mở tại At
	Overdue      int64 // đang mở và quá hạn tại At
}

// TaskData là dữ liệu báo cáo nhiệm vụ.
type TaskData struct {
	Rows []TaskRow
}

// SummaryRow là tổng hợp kỳ (FR-05) của một xã: gom FR-01, FR-02, FR-03 và nhiệm vụ.
type SummaryRow struct {
	ProvinceCode   string
	CommuneCode    string
	SownAreaHa     decimal.Decimal
	AffectedAreaHa decimal.Decimal
	PestReports    int64
	UrgentReports  int64
	RegisteredHa   decimal.Decimal
	CompletedHa    decimal.Decimal
	TasksOpened    int64
	TasksResolved  int64
	TasksOverdue   int64
}

// SummaryData là dữ liệu tổng hợp báo cáo kỳ (FR-05).
type SummaryData struct {
	Rows         []SummaryRow
	Completeness Completeness
}

// ReportData là cổng đọc dữ liệu báo cáo của pf/extension cho pf/reports
// (US-DASH-02, FR-01..FR-05). Chỉ đọc phiên bản hiện hành; không ghi.
type ReportData interface {
	Sowing(ctx context.Context, q ReportQuery) (SowingData, error)
	GrowthPest(ctx context.Context, q ReportQuery) (PestData, error)
	ProgramProgress(ctx context.Context, q ReportQuery) (ProgramData, error)
	TasksSLA(ctx context.Context, q ReportQuery) (TaskData, error)
	PeriodSummary(ctx context.Context, q ReportQuery) (SummaryData, error)
}

// Package extensionsrc là điểm mở rộng của pf/reports (cap/export): nguồn số liệu báo cáo Khuyến nông (FR-03,
// FR-05) và lần tổng hợp báo cáo Chi cục (FN-TI-02.06). pf/extension hiện thực ReportData, Consolidations; pf/reports
// hiện thực DispatchHistory cho pf/extension. Khai ở tầng năng lực để phụ thuộc chỉ đi xuống (R-MOD-05).
package extensionsrc

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

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

// ErrConsolidationNotFound: kỳ chưa có lần tổng hợp nào cho biểu.
var ErrConsolidationNotFound = errors.New("extension: kỳ chưa có dữ liệu tổng hợp")

// PeriodInfo là một kỳ báo cáo.
type PeriodInfo struct {
	ID             uuid.UUID
	BatchID        uuid.UUID
	Label          string
	RangeLabel     string
	SeasonLabel    string
	From, To       time.Time // ngày (00:00 UTC của ngày dương lịch)
	Closed         bool
	ProvinceCode   string
	DepartmentCode string
	ReportType     string
	Slot           int      // kỳ thứ n của đợt
	SlotLabels     []string // nhãn mọi kỳ của đợt theo khe (tiêu đề cột theo kỳ của mẫu nhiều kỳ)
}

// ConsolidationRow là một dòng xã (hoặc xã × chỉ tiêu) của lần tổng hợp.
type ConsolidationRow struct {
	Key         string
	Commune     string
	CommuneName string
	Region      string
	RegionName  string
	Indicator   string
	Cells       map[string]string // chữ cái cột → giá trị (decimal dạng chuỗi)
}

// ConsolidationTotal là dòng Cộng khu vực / Toàn tỉnh.
type ConsolidationTotal struct {
	Key        string
	Kind       string // region | province
	Region     string
	RegionName string
	Indicator  string
	Members    int
	Cells      map[string]string
}

// ConsolidationView là một lần tổng hợp (bất biến) của một biểu trong kỳ.
type ConsolidationView struct {
	RunID           uuid.UUID
	RunNo           int
	TemplateID      uuid.UUID
	VersionID       uuid.UUID
	FormCode        string
	At              time.Time
	Trigger         string
	ActorRef        uuid.UUID
	Provisional     bool
	MissingCommunes []string
	MissingNames    []string // tên xã chưa nộp (cùng thứ tự) — in kèm tệp "Tạm tính"
	Rows            []ConsolidationRow
	Totals          []ConsolidationTotal
}

// Consolidations là cổng đọc lần tổng hợp gần nhất (pf/reports dựng tệp Chi cục; tenant truyền tường minh vì
// gửi chạy ở worker). formCodes rỗng = mọi biểu của kỳ đã tổng hợp.
type Consolidations interface {
	Period(ctx context.Context, tenantID, periodID uuid.UUID) (PeriodInfo, error)
	LatestRuns(ctx context.Context, tenantID, periodID uuid.UUID, formCodes []string) ([]ConsolidationView, error)
}

// DispatchHistory (tùy chọn, pf/reports hiện thực) cho biết báo cáo Chi cục của kỳ đã được gửi — mở lại kỳ đó
// phải cảnh báo "số liệu đã thay đổi" (FN-TI-02.04 luồng phụ).
type DispatchHistory interface {
	SentForPeriod(ctx context.Context, tenantID, periodID uuid.UUID) (bool, error)
}

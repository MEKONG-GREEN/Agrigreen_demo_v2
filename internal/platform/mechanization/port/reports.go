package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// ReportQuery là tham số đọc báo cáo CGH FN-10 cho pf/reports.
//
// Cổng tin cậy nội bộ: pf/reports đã kiểm vai và giải phạm vi lúc yêu cầu xuất;
// mechanization chỉ lọc theo Scope được truyền. MỌI số liệu lấy từ kết quả cân
// đối ĐÃ LƯU (FN-09 BR-05, FN-10 BR-04) — không tính lại từ diện tích/máy hiện tại.
type ReportQuery struct {
	TenantID     uuid.UUID
	Scope        authz.Scope
	RunID        uuid.UUID // chọn đúng lần cân đối; Nil → lần mới nhất của vụ
	SeasonCode   string    // rỗng + RunID Nil → lần mới nhất của mọi vụ
	SeasonYear   int
	ProvinceCode string
	CommuneCode  string
	MachineType  string
	Level        string // province | commune (tab 1, tab 3)
}

// CompareQuery chọn Vụ A (kỳ trước) và Vụ B (kỳ này) cho tab đối chiếu (FN-10 B3).
type CompareQuery struct {
	ReportQuery        // SeasonCode/SeasonYear = Vụ A
	SeasonCodeB string // Vụ B
	SeasonYearB int
}

// RunInfo là lần cân đối đã lưu mà báo cáo đọc.
type RunInfo struct {
	RunID              uuid.UUID
	SeasonCode         string
	SeasonYear         int
	ReferenceDate      time.Time
	ComputedAt         time.Time
	RuleVersion        string
	MissingNorms       []string
	ThresholdsComplete bool
}

// Figures là số liệu của một nhóm: nhu cầu (số máy cần), năng lực (máy Hoạt động),
// mức đáp ứng (%) và trạng thái (sufficient|attention|shortage|surplus|no_data|no_norm|unclassified).
type Figures struct {
	Required      *decimal.Decimal
	RequiredWhole *int
	Capacity      int
	CoveragePct   *decimal.Decimal
	Status        string
	StatusLabel   string
}

// CoverageRow là tab "Tổng hợp theo khu vực": đơn vị × khâu × chủng loại.
type CoverageRow struct {
	ProvinceCode string
	CommuneCode  string // chỉ khi Level = commune
	FarmStage    string
	MachineType  string
	Cooperatives int
	Figures
}

// CoverageData là tab 1.
type CoverageData struct {
	Run  RunInfo
	Rows []CoverageRow
}

// ShortageRow là tab "Thiếu hụt theo HTX": một dòng kết quả đã lưu có năng lực < nhu cầu.
type ShortageRow struct {
	ResultID        uuid.UUID
	CooperativeRef  uuid.UUID
	CooperativeCode string
	CooperativeName string
	ProvinceCode    string
	CommuneCode     string
	FarmStage       string
	MachineType     string
	Capacity        int // số máy hiện có (Hoạt động)
	Required        *decimal.Decimal
	RequiredWhole   *int // nhu cầu (số máy, làm tròn lên)
	Shortage        int  // số lượng thiếu = −gap đã lưu
	CoveragePct     *decimal.Decimal
	Status          string
	AreaSource      string // app_htx | manual — nhãn nguồn (FN-10 BR-05)
}

// ShortageData là tab 2.
type ShortageData struct {
	Run  RunInfo
	Rows []ShortageRow
}

// CompareRow là tab "Đối chiếu giữa các vụ" của một vùng × khâu × chủng loại.
type CompareRow struct {
	ProvinceCode string
	CommuneCode  string
	FarmStage    string
	MachineType  string
	A, B         *Figures         // nil = không có trong vụ đó
	CapacityDiff *int             // năng lực B − A
	PctDiff      *decimal.Decimal // mức đáp ứng B − A (điểm %)
}

// CompareData là tab 3.
type CompareData struct {
	RunA, RunB RunInfo
	Rows       []CompareRow
}

// ReportData là cổng đọc báo cáo FN-10 (3 tab) từ kết quả cân đối đã lưu.
type ReportData interface {
	Coverage(ctx context.Context, q ReportQuery) (CoverageData, error)
	Shortage(ctx context.Context, q ReportQuery) (ShortageData, error)
	SeasonCompare(ctx context.Context, q CompareQuery) (CompareData, error)
}

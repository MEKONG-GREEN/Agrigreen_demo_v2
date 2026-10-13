package port

// Phòng Kế hoạch – Tài chính (W11.61, D4 KH1, BRD KHTC v2.2): danh sách CT/DA chủ và Kế hoạch năm có phiên bản theo
// quyết định. Hiện thực ở extension/khtc; module khác (và gói con khác của extension) chỉ dùng kiểu ở đây.
//
// Quyết định D20 (Q-061 đề xuất của dev): KHTC là CHỦ danh sách CT/DA. CNTS (W11.47) gắn điểm/hộ vào CT/DA bằng
// program_ref; thu – chi lấy từ nhật ký CNTS về sau cũng tham chiếu program_ref. Không ai đọc thẳng bảng pf_extension.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Lỗi của cổng KHTC.
var (
	ErrKHTCProgramNotFound = errors.New("extension: không có CT/DA trong danh sách KHTC")
	ErrKHTCPlanNotFound    = errors.New("extension: chưa có kế hoạch năm hiệu lực")
)

// KHTCProgram là bản hiện hành của một CT/DA trong danh sách chủ.
type KHTCProgram struct {
	Ref             uuid.UUID // định danh bền (tham chiếu từ module khác)
	Code            string
	Name            string
	Kind            string // program | project | central_project | cooperation | research | advisory
	FieldCode       string // lĩnh vực (Danh mục KHTC)
	OwnerDepartment string // phòng chủ trì (tùy chọn)
	StartYear       int    // 0 = không ghi
	EndYear         int
	Active          bool
	Version         int
}

// KHTCProgramFilter lọc danh sách CT/DA.
type KHTCProgramFilter struct {
	Kind           string // rỗng = mọi loại
	IncludeRetired bool
}

// KHTCPrograms là cổng đọc danh sách CT/DA chủ của KHTC (tenant lấy từ ctx).
type KHTCPrograms interface {
	// Program trả bản hiện hành; không có → ErrKHTCProgramNotFound.
	Program(ctx context.Context, ref uuid.UUID) (KHTCProgram, error)
	// Programs trả danh sách theo mã.
	Programs(ctx context.Context, f KHTCProgramFilter) ([]KHTCProgram, error)
}

// KHTCPlanEntry là một dòng kế hoạch năm (CT/DA, nhóm, hạng mục).
type KHTCPlanEntry struct {
	Ref         uuid.UUID
	Tab         string // programs | prev_year | central_project | cooperation | research | advisory
	Kind        string // program | group | item
	ParentRef   *uuid.UUID
	ProgramRef  *uuid.UUID
	Roman       string // số La Mã của CT ở tab "Các chương trình" (BR-DL-21), rỗng với dòng khác
	SortOrder   int
	Code        string
	Name        string
	UnitCode    string
	PlannedQty  *decimal.Decimal
	ScaleNote   string
	Department  string // phòng phụ trách (hạng mục)
	DataSource  string // form | module (hạng mục)
	SourceKey   string // khóa nguồn module (BR-DL-10)
	Tracking    string // tracking | stopped
	StoppedFrom *time.Time
}

// KHTCPlan là một phiên bản kế hoạch năm.
type KHTCPlan struct {
	VersionID     uuid.UUID
	Year          int
	Version       int
	DecisionNo    string
	DecisionDate  *time.Time
	EffectiveFrom time.Time
	Entries       []KHTCPlanEntry
}

// KHTCPlans là cổng đọc Kế hoạch năm cho kỳ KHTC, phiếu các phòng, mẫu báo cáo (W11.62, W11.63, W11.65).
type KHTCPlans interface {
	// PlanAt trả phiên bản dùng cho ngày at (BR-DL-13: ngày hiệu lực ≤ at, gần nhất); CT/DA ngừng trước at không có
	// số La Mã. Chưa có → ErrKHTCPlanNotFound.
	PlanAt(ctx context.Context, year int, at time.Time) (KHTCPlan, error)
}

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
	Ref          uuid.UUID
	Tab          string // programs | prev_year | central_project | cooperation | research | advisory
	Kind         string // program | group | item
	ParentRef    *uuid.UUID
	ProgramRef   *uuid.UUID
	Roman        string // số La Mã của CT ở tab "Các chương trình" (BR-DL-21), rỗng với dòng khác
	SortOrder    int
	Code         string
	Name         string
	UnitCode     string
	PlannedQty   *decimal.Decimal
	ScaleNote    string
	Department   string // phòng phụ trách (hạng mục)
	DataSource   string // form | module (hạng mục)
	SourceKey    string // khóa nguồn module (BR-DL-10)
	Tracking     string // tracking | stopped
	StoppedFrom  *time.Time
	Communes     []string // địa bàn theo kế hoạch, dùng bộ lọc Mẫu 5
	PointsCount  *int
	Budget       *decimal.Decimal
	FundingCode  string
	FieldCode    string
	StopDecision string
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

// KHTCPlanVersions resolves an immutable plan identity recorded by a closed source snapshot.
type KHTCPlanVersions interface {
	PlanVersion(ctx context.Context, year int, versionID uuid.UUID, at time.Time) (KHTCPlan, error)
}

// ---------------------------------------------------------------------------
// W11.62 — Kỳ KHTC (BRD KHTC v2.2 FN-TI-02.03, D4 KH2): cổng mà Kỳ KHTC CẦN từ gói khác. Gốc lắp ráp nối khi gói cài
// xong; chưa nối → Kỳ KHTC vẫn chạy với giả định ghi trong docs/kn/gia-dinh/W11.62.md.
// ---------------------------------------------------------------------------

// KHTCSourceRef là tham chiếu tới MỘT kỳ nguồn của module phòng chuyên môn (Trồng trọt, CNTS, Giá…) mà kỳ KHTC lấy số
// liệu. Kỳ KHTC KHÔNG tự đổi lịch: bộ quy đổi (crosswalk, gói PFPUB — QĐ D7/D8) trả khóa kỳ theo LỊCH CỦA NGUỒN; Kỳ
// KHTC chỉ lưu bộ ba (calendar_id, calendar_version, native_key) kèm phòng ban và mã nguồn.
type KHTCSourceRef struct {
	Department      string // mã phòng ban sở hữu nguồn (vd chan_nuoi_thuy_san)
	Source          string // mã nguồn trong hợp đồng _pub (vd "cnts.bm04", "trong_trot.consolidation", "price.board")
	CalendarID      string // mã lịch kỳ của nguồn (D8: mỗi phòng giữ lịch riêng)
	CalendarVersion string // phiên bản lịch kỳ của nguồn
	NativeKey       string // khóa kỳ theo lịch của nguồn (vd "2026-W41", "2026-10-12")
	Label           string // nhãn hiển thị (tùy chọn)
}

// KHTCSourceState là trạng thái chốt của một kỳ nguồn ("nguồn đã chốt" — QĐ-6, Q-048).
type KHTCSourceState struct {
	Closed   bool
	ClosedAt *time.Time
}

// KHTCSourceCrosswalk là bộ quy đổi kỳ KHTC → kỳ nguồn và trạng thái chốt (hiện thực ở gói PFPUB). Quy tắc quy đổi
// (QĐ CEO 13/10): một tuần nguồn thuộc tuần KHTC chứa NGÀY KẾT THÚC của nó; tháng/quý/năm KHTC gồm các tuần KHTC.
type KHTCSourceCrosswalk interface {
	// Sources trả các kỳ nguồn thuộc kỳ KHTC [from, to] (loại kỳ, khóa kỳ KHTC) cho các phòng ban departments.
	Sources(ctx context.Context, kind, key string, from, to time.Time, departments []string) ([]KHTCSourceRef, error)
	// States trả trạng thái chốt của từng tham chiếu (cùng thứ tự với refs).
	States(ctx context.Context, refs []KHTCSourceRef) ([]KHTCSourceState, error)
}

// KHTCPlanDepartments trả phòng phụ trách các hạng mục của Kế hoạch năm có hiệu lực tại ngày on (W11.61 — danh mục
// CT/DA, Kế hoạch năm). Chưa nối → KHTC chọn phòng tham gia khi mở kỳ (mặc định: mọi phòng ban đang áp dụng trừ KHTC).
type KHTCPlanDepartments interface {
	PlanDepartments(ctx context.Context, year int, on time.Time) ([]string, error)
}

// KHTCReportStates trả trạng thái bản báo cáo của kỳ KHTC (màn soạn — KH6). Chưa nối → mọi kỳ "Chưa soạn" (hủy kỳ
// không bị chặn bởi Bản cuối).
type KHTCReportStates interface {
	HasFinal(ctx context.Context, periodID string) (bool, error)
}

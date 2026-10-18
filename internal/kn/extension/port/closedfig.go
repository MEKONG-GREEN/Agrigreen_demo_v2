package port

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ClosedFigures là bộ đọc "số liệu đã chốt" thống nhất cho KHTC (ADR-0053, gói PFPUB): đọc họ hợp đồng
// closed_figures/v1 — pf_extension_pub.v1_closed_figures (vòng đời báo cáo cũ, Trồng trọt) và
// pf_forms_pub.v1_closed_figures (kỳ native cap/forms, CNTS) — trong MỘT giao dịch chỉ đọc của tenant, bằng vai
// r_extension (thành viên r_extension_pub, r_forms_pub — khai báo readpubview của module). Port mức thấp (chẩn đoán, kiểm thử); KHTC
// dùng KHTCSources. Bên gọi KHÔNG rẽ nhánh theo nguồn (hai nguồn lâu dài — Trồng trọt giữ vòng đời báo cáo).
//
// Không kiểm quyền người dùng: dữ liệu là số đã chốt của tenant; bên gọi kiểm vai (KHTC Cấp Tỉnh) trước khi đọc.
type ClosedFigures interface {
	// ClosedFigures trả kỳ nguồn nguyên dạng (lịch của nguồn — D8).
	ClosedFigures(ctx context.Context, tenantID uuid.UUID, q ClosedFiguresQuery) ([]ClosedFigure, error)
	// Converted quy đổi kỳ nguồn sang kỳ KHTC theo bảng chuyển đổi có phiên bản (ADR-0053 §6): mỗi kỳ nguồn thuộc kỳ
	// đích chứa NGÀY CUỐI của kỳ nguồn; không chia tỷ lệ. q lọc kỳ nguồn (PeriodKeys là khóa GỐC).
	Converted(ctx context.Context, tenantID uuid.UUID, q ClosedFiguresQuery, t CrosswalkTarget) ([]TargetFigure, error)
}

// CrosswalkV1 là phiên bản bảng chuyển đổi duy nhất hiện có: gán theo ngày cuối kỳ nguồn, không chia tỷ lệ; số cộng
// dồn (Sum) cộng chính xác theo decimal, số lũy kế lấy của kỳ nguồn cuối (Last — "lũy kế của nguồn là chuẩn", D18).
const CrosswalkV1 = "kn.crosswalk/v1;attribution=source_end"

// CrosswalkTarget là lịch đích. Rỗng: phiên bản CrosswalkV1, lịch KHTC mặc định (kn.period_rule/v2, tuần Thứ Tư – Thứ
// Ba), loại kỳ tuần.
type CrosswalkTarget struct {
	Version    string // CrosswalkV1
	CalendarID string // mặc định ke_hoach_tai_chinh
	RuleTag    string // thẻ quy tắc của lịch đích — Calendar.Rule.Tag() của (*reportcycle/app.Service).CalendarAt(ctx, KHTC, ngày)
	PeriodType string // week (mặc định) | month | quarter | year | day
}

// SourcePeriodRef là danh tính một kỳ nguồn đã quy vào kỳ đích.
type SourcePeriodRef struct {
	CalendarID      string
	CalendarVersion *int   // D8: 1 v1, 2 v2 mặc định, 3 CNTS…; nil = kỳ không theo lịch tuần (BM-05)
	NativeKey       string // khóa gốc theo lịch nguồn, vd 2026-W45
	PeriodType      string
	From, To        time.Time
	Source          string
	SourceModule    string
	SourcePeriodID  uuid.UUID
	FormCode        string
	Closed          bool // biểu của kỳ nguồn đã chốt VÀ có số đã chốt
	Converted       bool // khoảng ngày của kỳ nguồn khác khoảng của kỳ đích (đã quy đổi)
	// Nguồn gốc cho ảnh chụp "Cập nhật số liệu" (W11.64).
	ClosedAt           *time.Time
	SnapshotNo         *int64
	FiguresHash        *string
	StatusHash         *string
	ConfirmationStatus string
	// Cells: tổng các ô số của MỌI đơn vị của kỳ nguồn này (khóa như Sum/Last; nil = chưa có số đã chốt) — ảnh chụp
	// "Cập nhật số liệu" giữ số từng kỳ nguồn, Mẫu 1 A.III lấy hai ngày bảng giá đã duyệt gần nhất (W11.64).
	Cells map[string]string
}

// TargetFigure là số đã chốt của một (phòng, tỉnh, biểu) quy vào một kỳ đích.
type TargetFigure struct {
	RuleVersion           string // phiên bản bảng chuyển đổi đã dùng (CrosswalkV1)
	DepartmentCode        string
	ProvinceCode          string
	FormCode              string
	TargetCalendarID      string
	TargetCalendarVersion int
	TargetRule            string
	TargetType            string
	TargetKey             string
	TargetFrom, TargetTo  time.Time
	Sources               []SourcePeriodRef // sắp theo ngày cuối
	Converted             bool              // có kỳ nguồn khác khoảng kỳ đích
	// Closed: MỌI kỳ nguồn của biểu quy vào kỳ đích đã chốt; KeyClosed: mọi kỳ nguồn của MỌI biểu của phòng quy vào kỳ
	// đích đã chốt (D17 — "đã chốt" phía KHTC).
	Closed    bool
	KeyClosed bool
	// Sum: tổng chính xác (decimal) các ô số của mọi đơn vị × mọi kỳ nguồn quy vào — dùng cho số phát sinh (vd kỳ ngày
	// BM-01). Last: tổng các ô của kỳ nguồn có ngày cuối muộn nhất — dùng cho số lũy kế (lũy kế của nguồn là chuẩn);
	// số tăng trong tuần KHTC = Last(tuần) − Last(tuần trước) (closedfig.Increment). Khóa ô: "<chỉ tiêu>/<ô>" hoặc "<ô>".
	Sum           map[string]string
	Last          map[string]string
	LastSourceKey string
	// Tách theo trạng thái xác nhận (D2): Sum = SumConfirmed + SumUnconfirmed (tương tự Last). Ô vắng = 0.
	SumConfirmed, SumUnconfirmed   map[string]string
	LastConfirmed, LastUnconfirmed map[string]string
	// FiguresHash/StatusHash: sha256 (hex) gộp băm của các kỳ nguồn (theo thứ tự Sources) — đổi khi số đã chốt của
	// bất kỳ kỳ nguồn nào đổi, hoặc khi tập kỳ nguồn đổi. StatusHash chỉ đổi theo trạng thái xác nhận.
	FiguresHash string
	StatusHash  string
}

// KHTCSources là port DUY NHẤT phía KHTC (W11.61…W11.69, ADR-0053 §7): theo từng kỳ KHTC, số đã chốt của từng phòng
// nguồn (Trồng trọt — vòng đời báo cáo, tuần Thứ Tư – Thứ Ba; CNTS — kỳ native cap/forms, lịch riêng Thứ Ba – Thứ Hai;
// Báo giá — pf_price_pub) đã quy đổi sang lịch KHTC, giữ danh tính kỳ nguồn, cờ chốt và nguồn gốc (băm). KHTC chụp kết
// quả khi cán bộ bấm "Cập nhật số liệu" (bất biến ở phía KHTC) và chỉ so băm để cảnh báo — không ghi đè.
type KHTCSources interface {
	Periods(ctx context.Context, tenantID uuid.UUID, q KHTCQuery) ([]KHTCPeriod, error)
}

// Mã phòng/biểu của nguồn Báo giá trong KHTCSources (bảng giá ngày và số phiếu tuần — pf_price_pub, W11.76).
const (
	PriceFormBoard   = "PRICE-BOARD"   // một kỳ ngày; dòng = mặt hàng; ô price_min, price_max, price_avg, points_reported
	PriceFormTickets = "PRICE-TICKETS" // một tuần báo giá; dòng = xã báo giá; ô tickets_collected, days_scheduled, days_approved
)

// KHTCQuery: tỉnh và khoảng ngày [From, To] — trả mọi kỳ KHTC giao với khoảng. Departments rỗng = mọi phòng có số.
type KHTCQuery struct {
	PriceAt *time.Time // M4: calendar date for approved prices; nil retains the quantity-period reader.

	ConfirmationAt *time.Time // M6: end of the report cutoff day in Vietnam; nil retains current-state callers.
	Province       string
	From, To       time.Time
	Departments    []string
	Target         CrosswalkTarget
}

// KHTCPeriod là một kỳ KHTC với số của từng phòng.
type KHTCPeriod struct {
	// PriceAsOf contains full approved boards covering the last two days per item at PriceAt. Nil is
	// the legacy weekly projection; non-nil (even empty) is authoritative. Each
	// figure retains its actual D8 target, independently of the quantity period.
	PriceAsOf []TargetFigure

	RuleVersion           string
	TargetCalendarID      string
	TargetCalendarVersion int
	TargetRule            string
	TargetType            string
	TargetKey             string
	TargetFrom, TargetTo  time.Time
	Departments           []DepartmentFigures // sắp theo mã phòng
	Closed                bool                // mọi phòng (của Departments hoặc có số) đã chốt cho kỳ (D17)
}

// DepartmentFigures là số của một phòng nguồn trong một kỳ KHTC.
type DepartmentFigures struct {
	DepartmentCode string
	SourceModule   string // pf/extension | pf/forms | pf/price
	Forms          []TargetFigure
	Closed         bool // mọi kỳ nguồn của mọi biểu của phòng quy vào kỳ đã chốt; false nếu Missing/Unavailable
	Missing        bool // phòng được yêu cầu nhưng không có kỳ nguồn nào quy vào kỳ này
	Unavailable    bool // nguồn chưa công bố view (vd pf_price_pub trước W11.76) — không phải "không có số"
	// Fallback (Báo giá, D22): không có ngày duyệt trong kỳ → Last của PRICE-BOARD lấy ngày đã duyệt gần nhất trước kỳ.
	Fallback bool
}

// ClosedFiguresQuery lọc theo cột khóa tra cứu (rỗng = không lọc). Cờ KeyClosedAll luôn tính trên MỌI biểu của khóa kỳ
// ở cả hai nguồn — FormCodes chỉ lọc dòng trả về.
type ClosedFiguresQuery struct {
	Department string
	Province   string
	PeriodType string
	PeriodKeys []string
	// From, To (tùy chọn): chỉ kỳ nguồn có ngày cuối trong [From, To] (theo ngày, giờ VN).
	From, To  *time.Time
	FormCodes []string
}

// Trạng thái xác nhận của đơn vị/dòng (D2: HTX do TTKN tạo "Chưa xác nhận" — số vẫn vào báo cáo, ghi nhãn).
const (
	ConfirmationConfirmed   = "confirmed"
	ConfirmationUnconfirmed = "unconfirmed"
)

// UnitKindCooperative là loại đơn vị của dòng HTX (unit = mã Nút HTX) — trạng thái xác nhận lấy từ HTXConfirmations.
const UnitKindCooperative = "cooperative"

// HTXConfirmations là cổng đọc xác nhận do KN sở hữu; gốc lắp ráp nối publication v1.
// Nút HTX là bí danh của neo đã nhận quyền → confirmed; HTX tạm/chưa neo → unconfirmed (tenant của ctx).
type HTXConfirmations interface {
	Confirmations(context.Context, []uuid.UUID) (map[uuid.UUID]string, error)
}

// Nguồn của dòng (cột source) — chỉ để chẩn đoán, KHTC không rẽ nhánh theo nguồn.
const (
	ClosedSourceReportCycle = "reportcycle"
	ClosedSourceForms       = "forms"
)

// ClosedFigure là một dòng của họ hợp đồng closed_figures/v1 (contracts/pubview/*.closed_figures.v1.json) cộng cờ
// KeyClosedAll của bộ đọc.
type ClosedFigure struct {
	DepartmentCode string
	ProvinceCode   string
	// Danh tính kỳ nguồn (D8): (CalendarID, CalendarVersion, PeriodKey = khóa gốc).
	CalendarID      string
	CalendarVersion *int // nil = kỳ không theo lịch tuần của phòng (tuần làm việc BM-05)
	PeriodType      string
	PeriodKey       string
	WeekYear        *int
	PeriodRule      *string
	PeriodFrom      *time.Time
	PeriodTo        *time.Time
	PeriodLabel     string
	Source          string
	SourceModule    string // pf/extension | pf/forms (| pf/price — KHTCSources)
	SourcePeriodID  uuid.UUID
	FormCode        string
	FormVersionID   uuid.UUID
	FormClosed      bool
	PeriodClosed    bool
	// KeyClosed: mọi biểu của mọi kỳ cùng khóa đã chốt TRONG NGUỒN của dòng; KeyClosedAll: trên cả hai nguồn
	// ("nguồn đã chốt cho tuần X" của phòng — Q-048).
	KeyClosed    bool
	KeyClosedAll bool
	ClosedAt     *time.Time
	// Số đã chốt (nil = chưa chốt lần nào). Rows/Totals: JSON theo hợp đồng (chuỗi thập phân, chỉ ô số).
	SnapshotID    *uuid.UUID
	SnapshotNo    *int64
	DataAt        *time.Time
	Provisional   *bool
	UnitsTotal    *int
	UnitsWithData *int
	MissingUnits  []string
	Rows          json.RawMessage
	Totals        json.RawMessage
	// D2: confirmed | unconfirmed (có đơn vị chưa xác nhận). FiguresHash: băm số đã chốt (không gồm trạng thái xác
	// nhận) — W11.64 so với ảnh chụp để báo "Nguồn đã thay đổi"; StatusHash: băm riêng trạng thái xác nhận.
	ConfirmationStatus string
	FiguresHash        *string
	StatusHash         *string
}

// KHTCMetricName is presentation metadata copied into the report's frozen payload.
type KHTCMetricName struct{ Label, Unit string }

// KHTCMetricNames reads published labels as of the reporting cutoff, never private source tables.
type KHTCMetricNames interface {
	MetricNames(ctx context.Context, tenantID uuid.UUID, cutoff time.Time) (map[string]KHTCMetricName, error)
}

// HTXConfirmationHistory is an optional historical read implemented by the existing data provider.
type HTXConfirmationHistory interface {
	ConfirmationsAt(context.Context, []uuid.UUID, time.Time) (map[uuid.UUID]string, error)
}

package port

// Mẫu báo cáo dạng lưới Excel của KN v7.7 (FN-AD-04.06, FN-XA-03.02; kn-v7.7-plan
// P3). Định nghĩa lưới (FormDefinition) là hợp đồng JSON cho web/app (lược đồ
// contracts/schemas/form-template.v1.json, OpenAPI components FormDefinition) và
// là đầu vào của bộ tính (reports/form). P4 (pf/extension: phát hành, bài nộp,
// tổng hợp) dùng FormEngine — không đọc bảng của pf/reports.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// FormSchema là phiên bản lược đồ định nghĩa lưới.
const FormSchema = "form-template.v1"

// Loại cột (BRD §8.1).
const (
	ColumnFixed      = "fixed"      // Cố định: STT, Khu vực, Xã… — hệ thống điền
	ColumnProvincial = "provincial" // Tỉnh giao: Cấp Tỉnh nhập khi phát hành, Xã chỉ xem
	ColumnInput      = "input"      // Nhập: Cán bộ xã nhập
	ColumnPerPeriod  = "per_period" // Cột theo kỳ: chỉ cột của kỳ đang mở được nhập
	ColumnFormula    = "formula"    // Công thức: tính theo mẫu, không sửa tay
)

// Kiểu dữ liệu của cột.
const (
	DTypeNumber  = "number"  // decimal, chuỗi "123.45"
	DTypeInteger = "integer" // số nguyên
	DTypeText    = "text"
	DTypeDate    = "date"    // "YYYY-MM-DD"
	DTypeCatalog = "catalog" // mã mục danh mục (catalog)
)

// Nguồn giá trị của cột cố định (hệ thống điền).
const (
	FixedRowNo          = "row_no"          // STT
	FixedRegionName     = "region_name"     // Khu vực
	FixedCommuneName    = "commune_name"    // Xã, phường, đặc khu
	FixedIndicatorLabel = "indicator_label" // nhãn dòng chỉ tiêu (đối tượng sâu bệnh…)
	FixedXRefPrefix     = "xref:"           // "xref:T01:M" — giá trị cột M của mẫu T01 cùng xã cùng kỳ
	FixedNone           = "none"            // trống (cột trang trí)
)

// Cách tính dòng tổng (Khu vực, Toàn tỉnh).
const (
	TotalSum     = "sum"     // cộng các dòng thành viên
	TotalFormula = "formula" // tính lại công thức trên dòng tổng (năng suất = sản lượng/diện tích)
	TotalNone    = "none"
)

// Cấp nhập liệu của mẫu.
const (
	LevelCommune  = "commune"
	LevelProvince = "province"
)

// Loại báo cáo của phòng ban (tạm, chờ danh mục report_type của P2).
const (
	ReportWeekly   = "weekly"   // Báo cáo tuần
	ReportMonthly  = "monthly"  // Báo cáo tháng
	ReportSeasonal = "seasonal" // Báo cáo hàng vụ
	ReportAnnual   = "annual"   // Báo cáo năm (Q-07)
	ReportAdhoc    = "adhoc"    // Báo cáo đột xuất
	ReportDispatch = "dispatch" // Báo cáo gửi Chi cục (CC01, Q-08)
)

// ReportTypes là tập loại báo cáo hợp lệ.
var ReportTypes = []string{ReportWeekly, ReportMonthly, ReportSeasonal, ReportAnnual, ReportAdhoc, ReportDispatch}

// Trạng thái phiên bản mẫu.
const (
	FormDraft     = "draft"     // nháp: Admin sửa được
	FormProposed  = "proposed"  // đã đề nghị phát hành: khóa nội dung, chờ người khác duyệt (R-56)
	FormPublished = "published" // đã phát hành: bất biến
	FormRetired   = "retired"   // ngừng dùng (kèm lý do)
)

// Mức của vấn đề khi kiểm lưới.
const (
	SeverityError   = "error"   // chặn gửi
	SeverityWarning = "warning" // chỉ báo
)

// FormDefinition là định nghĩa lưới của một phiên bản mẫu, đúng bố cục sheet Excel.
type FormDefinition struct {
	Schema        string        `json:"schema"`
	Code          string        `json:"code"`
	Title         string        `json:"title"`
	SheetName     string        `json:"sheet_name"`   // vd "PL 01_Tđộ XG TĐ 2026"
	Requirements  string        `json:"requirements"` // "Yêu cầu biểu mẫu" nguyên văn
	Columns       []FormColumn  `json:"columns"`      // theo thứ tự chữ cái, liên tục từ A
	Header        FormHeader    `json:"header"`
	PeriodBlocks  []PeriodBlock `json:"period_blocks"`
	Rows          FormRows      `json:"rows"`
	Totals        []TotalRowDef `json:"totals"`
	Groups        []ColumnGroup `json:"groups"`         // nhóm cột cho "Đi tới"
	FreezeThrough string        `json:"freeze_through"` // cố định tới cột này (vd "J"); rỗng = không
	Rules         []FormRule    `json:"rules"`          // ràng buộc của mẫu
	Pending       []PendingItem `json:"pending"`        // phần chưa biết — "chờ khách Q-01"
	Completeness  string        `json:"completeness"`   // complete | partial | placeholder
	Source        *FormSource   `json:"source,omitempty"`
}

// Mức đầy đủ của định nghĩa.
const (
	CompletenessComplete    = "complete"
	CompletenessPartial     = "partial"
	CompletenessPlaceholder = "placeholder"
)

// FormColumn là một cột Excel.
type FormColumn struct {
	Letter      string  `json:"letter"`
	Key         string  `json:"key,omitempty"` // khóa ổn định (tùy chọn) để đối chiếu giữa phiên bản
	Title       string  `json:"title"`
	Kind        string  `json:"kind"`
	DType       string  `json:"dtype"`
	Unit        string  `json:"unit,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Catalog     string  `json:"catalog,omitempty"`      // mã danh mục khi dtype = catalog
	Formula     string  `json:"formula,omitempty"`      // kind = formula: "SUM(F#:L#)"
	FixedSource string  `json:"fixed_source,omitempty"` // kind = fixed
	PeriodBlock string  `json:"period_block,omitempty"` // kind = per_period: id khối
	PeriodSlot  int     `json:"period_slot,omitempty"`  // kind = per_period: kỳ thứ n (từ 1)
	Total       string  `json:"total,omitempty"`        // sum | formula | none; rỗng = mặc định theo loại
	Min         *string `json:"min,omitempty"`          // giới hạn của mẫu (decimal), nếu mẫu yêu cầu
	Max         *string `json:"max,omitempty"`
	Decimals    *int    `json:"decimals,omitempty"`    // số chữ số hiển thị
	Group       string  `json:"group,omitempty"`       // id nhóm cột
	Placeholder bool    `json:"placeholder,omitempty"` // chưa biết từ mẫu gốc — chờ khách Q-01
	Note        string  `json:"note,omitempty"`
	Inactive    bool    `json:"inactive,omitempty"` // chỉ ở layout: khe kỳ không dùng trong đợt
}

// FormHeader là các dòng tiêu đề nhiều tầng (trên hàng chữ cái), có vùng gộp.
type FormHeader struct {
	Rows   int          `json:"rows"` // số dòng tiêu đề (≥ 1)
	Cells  []HeaderCell `json:"cells"`
	Merges []string     `json:"merges"` // vùng gộp Excel trong khung tiêu đề, dòng tính từ 1: "F1:L1", "A1:A2"
}

// HeaderCell là một ô tiêu đề (ô góc trên trái của vùng gộp mang chữ).
type HeaderCell struct {
	Row         int    `json:"row"`
	Letter      string `json:"letter"`
	Text        string `json:"text"`
	PeriodBlock string `json:"period_block,omitempty"` // ô tiêu đề theo kỳ: chữ đổi theo nhãn kỳ khi phát hành
	PeriodSlot  int    `json:"period_slot,omitempty"`
}

// PeriodBlock là khối cột lặp theo kỳ trên cùng một dòng (PL 01, PL 01B, PL 02, PL 07).
type PeriodBlock struct {
	ID           string `json:"id"`
	FirstLetter  string `json:"first_letter"`
	Width        int    `json:"width"`         // số cột mỗi kỳ (PL 07: 4)
	Slots        int    `json:"slots"`         // số kỳ tối đa của mẫu
	TitlePattern string `json:"title_pattern"` // vd "Tuần {n}"
}

// FormRows là tập dòng dữ liệu của mẫu.
type FormRows struct {
	Kind       string      `json:"kind"`       // per_commune
	Indicators []Indicator `json:"indicators"` // rỗng: một dòng mỗi xã; có: mỗi xã × mỗi chỉ tiêu một dòng
}

// RowsPerCommune: mỗi xã một dòng (hoặc một nhóm dòng chỉ tiêu).
const RowsPerCommune = "per_commune"

// Indicator là một dòng chỉ tiêu (vd một đối tượng sâu bệnh của T05).
type Indicator struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	Catalog     string `json:"catalog,omitempty"`
	Placeholder bool   `json:"placeholder,omitempty"`
}

// TotalRowDef là một loại dòng tổng.
type TotalRowDef struct {
	Kind        string `json:"kind"`         // region | province
	Label       string `json:"label"`        // "Cộng khu vực {region}", "Toàn tỉnh"
	LabelLetter string `json:"label_letter"` // cột mang nhãn
}

// Loại dòng tổng.
const (
	TotalRegion   = "region"
	TotalProvince = "province"
)

// ColumnGroup là nhóm cột cho ô "Đi tới" (vd một nhóm cây của T04).
type ColumnGroup struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// FormRule là ràng buộc của mẫu, tính trên từng dòng dữ liệu (biểu thức logic
// cùng cú pháp công thức; được dùng XREF tham chiếu liên biểu).
type FormRule struct {
	ID       string   `json:"id"`
	Expr     string   `json:"expr"`           // vd "J#=D#"
	When     string   `json:"when,omitempty"` // điều kiện áp dụng, vd "ISNUMBER(D#)"
	Message  string   `json:"message"`
	Severity string   `json:"severity"` // error | warning
	Letters  []string `json:"letters"`  // ô báo lỗi
}

// PendingItem là phần mẫu chưa biết từ tệp gốc.
type PendingItem struct {
	Part     string `json:"part"` // vd "Cột K–HR: danh sách 54 cây"
	Detail   string `json:"detail"`
	Question string `json:"question"` // "Q-01"
}

// FormSource ghi nguồn của định nghĩa (seed trong mã hoặc tệp Excel đã nạp).
type FormSource struct {
	Kind     string `json:"kind"` // seed | import | designer
	File     string `json:"file,omitempty"`
	Sheet    string `json:"sheet,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
	DataRow  int    `json:"data_row,omitempty"` // dòng dữ liệu đầu trong tệp Excel gốc
	SeedNote string `json:"seed_note,omitempty"`
}

// ---------------------------------------------------------------------------
// Lưới bài nộp và kết quả tính (hợp đồng với P4)
// ---------------------------------------------------------------------------

// EvalMode: draft bỏ kiểm bắt buộc; submit kiểm đủ.
const (
	EvalDraft  = "draft"
	EvalSubmit = "submit"
)

// EvalRow là một dòng dữ liệu đưa vào tính.
type EvalRow struct {
	Key       string                       `json:"key"`                 // "<mã xã>" hoặc "<mã xã>/<mã chỉ tiêu>"
	Commune   string                       `json:"commune"`             // mã ĐVHC xã
	Region    string                       `json:"region,omitempty"`    // mã khu vực (cho dòng tổng)
	Indicator string                       `json:"indicator,omitempty"` // mã chỉ tiêu khi mẫu có dòng chỉ tiêu
	Input     map[string]string            `json:"input"`               // ô do người nhập: chữ cái → giá trị
	System    map[string]string            `json:"system,omitempty"`    // ô hệ thống: cố định + Tỉnh giao (P4 điền)
	XRefs     map[string]map[string]string `json:"xrefs,omitempty"`     // mã mẫu → cột → giá trị (cùng xã cùng kỳ)
}

// EvalInput là đầu vào tính một lưới.
type EvalInput struct {
	Mode      string    `json:"mode"`
	OpenSlots []int     `json:"open_slots"` // khe kỳ đang mở; nil = không kiểm (xem trước)
	Rows      []EvalRow `json:"rows"`
}

// EvalRowResult là một dòng đã tính: mọi ô (cố định, Tỉnh giao, nhập, công thức).
type EvalRowResult struct {
	Key       string            `json:"key"`
	Commune   string            `json:"commune"`
	Region    string            `json:"region,omitempty"`
	Indicator string            `json:"indicator,omitempty"`
	Cells     map[string]string `json:"cells"`
}

// CellIssue là một vấn đề tại ô (báo tại ô, không cho gửi nếu mức error).
type CellIssue struct {
	RowKey   string `json:"row_key"`
	Letter   string `json:"letter,omitempty"`
	Code     string `json:"code"` // reports.cell_required, reports.rule_failed…
	Message  string `json:"message"`
	Severity string `json:"severity"`
	RuleID   string `json:"rule_id,omitempty"`
}

// EvalResult là kết quả tính lưới.
type EvalResult struct {
	Valid  bool            `json:"valid"` // không có vấn đề mức error
	Rows   []EvalRowResult `json:"rows"`
	Issues []CellIssue     `json:"issues"`
}

// TotalRowResult là một dòng tổng (Khu vực, Toàn tỉnh).
type TotalRowResult struct {
	Key       string            `json:"key"` // "region:<mã>" hoặc "province" (+ "/<chỉ tiêu>")
	Kind      string            `json:"kind"`
	Region    string            `json:"region,omitempty"`
	Indicator string            `json:"indicator,omitempty"`
	Members   int               `json:"members"` // số dòng thành viên
	Cells     map[string]string `json:"cells"`
}

// FormVersion là một phiên bản mẫu (P4 ghim id phiên bản vào công việc khi phát hành).
type FormVersion struct {
	TemplateID     uuid.UUID      `json:"template_id"`
	VersionID      uuid.UUID      `json:"version_id"`
	Code           string         `json:"code"`
	Name           string         `json:"name"`
	DepartmentCode string         `json:"department_code"`
	EntryLevel     string         `json:"entry_level"`
	Version        int            `json:"version"`
	Status         string         `json:"status"`
	EffectiveFrom  *time.Time     `json:"effective_from"`
	EffectiveLabel *string        `json:"effective_label"`
	ReportTypes    []string       `json:"report_types"`
	Definition     FormDefinition `json:"definition"`
}

// ErrFormNotFound: không có phiên bản mẫu phù hợp.
var ErrFormNotFound = errors.New("reports: không có phiên bản mẫu phù hợp")

// FormEngine là cổng P4 dùng (pf/reports hiện thực; tenant truyền tường minh vì job
// phát hành/tổng hợp chạy ở worker).
type FormEngine interface {
	// FormVersion đọc một phiên bản theo id (mọi trạng thái — công việc đã ghim giữ phiên bản cũ).
	FormVersion(ctx context.Context, tenantID, versionID uuid.UUID) (FormVersion, error)
	// ApplicableVersion trả phiên bản ĐÃ PHÁT HÀNH mới nhất có "áp dụng từ kỳ" ≤ periodStart.
	ApplicableVersion(ctx context.Context, tenantID, templateID uuid.UUID, periodStart time.Time) (FormVersion, error)
	// AssignedForms trả các mẫu đang gán cho (phòng ban, cấp, loại báo cáo) kèm phiên bản áp dụng tại periodStart.
	AssignedForms(ctx context.Context, tenantID uuid.UUID, departmentCode, entryLevel, reportType string, periodStart time.Time) ([]FormVersion, error)
	// Evaluate kiểm và tính một lưới theo định nghĩa (thuần, không DB).
	Evaluate(def FormDefinition, in EvalInput) EvalResult
	// Totals dựng dòng tổng Khu vực/Toàn tỉnh từ các dòng đã tính (thuần, không DB).
	Totals(def FormDefinition, rows []EvalRowResult) []TotalRowResult
}

// Sự kiện mẫu báo cáo.
const EventFormVersionPublished = "pf.reports.form_version.published"

// FormVersionPublishedV1 là payload của pf.reports.form_version.published: một phiên bản
// mẫu vừa phát hành (người duyệt ≠ người đề nghị, R-56). Không PII.
//
//ag:event pf.reports.form_version.published v1
type FormVersionPublishedV1 struct {
	TemplateID     string  `json:"template_id"`
	VersionID      string  `json:"version_id"`
	Code           string  `json:"code"`
	Version        int     `json:"version"`
	DepartmentCode string  `json:"department_code"`
	EntryLevel     string  `json:"entry_level"`
	EffectiveFrom  string  `json:"effective_from"`
	EffectiveLabel *string `json:"effective_label"`
	ProposedBy     string  `json:"proposed_by"`
	PublishedBy    string  `json:"published_by"`
	PublishedAt    string  `json:"published_at"`
}

// FormImportOptions là tham số nạp mẫu từ một sheet Excel (reports/formimport).
type FormImportOptions struct {
	Sheet       string // rỗng = sheet đầu
	Code        string // mã mẫu (vd T01)
	Title       string // rỗng = tên sheet
	HeaderStart int    // dòng tiêu đề đầu (0 = 1)
	DataRow     int    // dòng dữ liệu đầu (0 = tự nhận)
	FileName    string
}

// FormImportReport là kết quả phân tích tệp nạp.
type FormImportReport struct {
	Sheet      string   `json:"sheet"`
	Sheets     []string `json:"sheets"`
	HeaderRows int      `json:"header_rows"`
	DataRow    int      `json:"data_row"`
	Columns    int      `json:"columns"`
	Merges     int      `json:"merges"`
	Formulas   int      `json:"formulas"`
	KindRow    bool     `json:"kind_row"` // tệp có dòng loại cột (tệp do hệ thống xuất)
	Warnings   []string `json:"warnings"`
}

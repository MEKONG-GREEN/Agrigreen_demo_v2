// Package port là cửa công khai DUY NHẤT của module mkg/acctbridge (R-02): dựng bút toán
// TỔNG HỢP theo kỳ từ kết quả đóng kỳ của mkg/costing và đẩy MỘT CHIỀU, tần suất tháng, tới
// phần mềm kế toán pháp định mua ngoài (tài liệu kiến trúc mục 16; docs/design/mkg.md §5.7;
// docs/dev/g4-conventions.md). Không đẩy giao dịch chi tiết.
//
// Luồng: mkg.costing.period.closed → job dựng bút toán (mẫu mục 12, ảnh chụp = PeriodSummary +
// bảng ánh xạ tài khoản có phiên bản) → chief_accountant duyệt (≠ người dựng, R-56) → job đẩy
// qua LedgerAdapter; lỗi tạm thời → thử lại có giãn cách, lỗi vĩnh viễn → DLQ + cảnh báo (R-62);
// thành công → mkg.acctbridge.journal.pushed.
//
// Nhà cung cấp phần mềm kế toán CHƯA CHỐT (câu hỏi khách): G4 có bộ điều hợp xuất tệp (CSV/XML,
// tệp ký SHA-256 ở kho đối tượng trong lãnh thổ — RB-7) và bộ giả cho kiểm thử. Bộ điều hợp của
// nhà cung cấp thật thêm sau, không đổi interface; giấy phép kiểm lúc chọn (RB-4).
//
// ĐÓNG BĂNG ở G4-0: đổi chữ ký / ngữ nghĩa qua người tích hợp; thêm phương thức vào interface MỚI.
// Bút toán KHÔNG BAO GIỜ qua van.
package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	costingport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// DBRole là vai của module ở cụm mkg (schema mkg_acctbridge, mkg 000014/000019).
const DBRole = "acctbridge_app"

// Vai nghiệp vụ (realm Keycloak; tài khoản tenant MKG).
const (
	// RoleAccountingBridgeOperator: vận hành cầu kế toán — yêu cầu dựng bút toán tổng hợp, đề xuất
	// bảng ánh xạ tài khoản, xem lần đẩy, đẩy lại từ DLQ sau khi xử lý nguyên nhân.
	RoleAccountingBridgeOperator = "accounting_bridge_operator"
	// RoleChiefAccountant: duyệt bút toán tổng hợp (≠ người dựng) và bảng ánh xạ tài khoản
	// (≠ người đề xuất). Cùng vai với costing/port.RoleChiefAccountant.
	RoleChiefAccountant = costingport.RoleChiefAccountant
)

// Side là bên Nợ / Có của một dòng bút toán.
type Side string

// Bên.
const (
	Debit  Side = "debit"
	Credit Side = "credit"
)

// JournalStatus: built → approved → pushed; push_failed (lỗi vĩnh viễn, chờ vận hành) → pushed
// sau khi đẩy lại. Bút toán đã duyệt không sửa; sai = bút toán mới supersedes (RB-1).
type JournalStatus string

// Trạng thái bút toán tổng hợp.
const (
	JournalBuilt      JournalStatus = "built"
	JournalApproved   JournalStatus = "approved"
	JournalPushed     JournalStatus = "pushed"
	JournalPushFailed JournalStatus = "push_failed"
)

// ---------------------------------------------------------------------------
// Sự kiện nội bộ (ADR-0018).
// ---------------------------------------------------------------------------

// EventJournalPushed: bút toán tổng hợp của kỳ đã được phần mềm kế toán nhận.
const EventJournalPushed = "mkg.acctbridge.journal.pushed"

// Consumer và job của acctbridge.
const (
	// PeriodClosedConsumer: mkg.costing.period.closed → tạo job dựng bút toán tổng hợp (idempotent
	// theo (period_id, close_run_id)).
	PeriodClosedConsumer = "mkg.acctbridge.period_closed"
	// JobBuildJournal / JobPushJournal: loại job (mẫu mục 12) ở cmd/worker-internal.
	JobBuildJournal = "acctbridge.build_journal"
	JobPushJournal  = "acctbridge.push_journal"
)

// Job theo mẫu mục 12 (module tự phát, tự nhận trên vhost mkg.internal).
const (
	EventJournalBuildRequested = "mkg.acctbridge.journal_build.requested"
	EventJournalPushRequested  = "mkg.acctbridge.journal_push.requested"
	JournalBuildConsumer       = "mkg.acctbridge.journal_build"
	JournalPushConsumer        = "mkg.acctbridge.journal_push"
)

// JournalBuildRequestedV1 là payload yêu cầu dựng bút toán tổng hợp của một kỳ đã đóng. Worker nạp
// ảnh chụp và cấu hình từ bảng job, không từ sự kiện.
//
//ag:internal-event mkg.acctbridge.journal_build.requested v1
type JournalBuildRequestedV1 struct {
	JobID       uuid.UUID `json:"job_id"`
	PeriodID    uuid.UUID `json:"period_id"`
	CloseRunID  uuid.UUID `json:"close_run_id"`
	RequestedBy uuid.UUID `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
}

// JournalPushRequestedV1 là payload yêu cầu đẩy một bút toán ĐÃ DUYỆT (thử lại dùng job mới, cùng
// IdempotencyKey của bút toán).
//
//ag:internal-event mkg.acctbridge.journal_push.requested v1
type JournalPushRequestedV1 struct {
	JobID       uuid.UUID `json:"job_id"`
	JournalID   uuid.UUID `json:"journal_id"`
	Attempt     int       `json:"attempt"`
	RequestedBy uuid.UUID `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
}

// JournalPushedV1 là payload khi đẩy thành công. Người nhận nội bộ: mkg/costing (kỳ → posted,
// khóa điều chỉnh vào kỳ đó).
//
//ag:internal-event mkg.acctbridge.journal.pushed v1
type JournalPushedV1 struct {
	JournalID      uuid.UUID    `json:"journal_id"`
	PeriodID       uuid.UUID    `json:"period_id"`
	PeriodCode     string       `json:"period_code"`
	CloseRunID     uuid.UUID    `json:"close_run_id"`
	SourceHash     string       `json:"source_hash"` // = costing ResultSetSHA256
	Adapter        string       `json:"adapter"`     // vd "file_export.csv.v1"
	ExternalRef    string       `json:"external_ref"`
	LineCount      int          `json:"line_count"`
	TotalDebit     money.Amount `json:"total_debit"`
	TotalCredit    money.Amount `json:"total_credit"`
	Attempt        int          `json:"attempt"`
	MappingVersion int          `json:"mapping_version"`
	PushedAt       time.Time    `json:"pushed_at"`
	EventTime      time.Time    `json:"event_time"`
}

// ---------------------------------------------------------------------------
// Bút toán và bộ điều hợp phần mềm kế toán.
// ---------------------------------------------------------------------------

// JournalLine là một dòng bút toán tổng hợp.
type JournalLine struct {
	LineNo      int
	Account     string // số hiệu tài khoản theo bảng ánh xạ có phiên bản (chế độ kế toán — chờ khách)
	Side        Side
	Amount      money.Amount // > 0
	Category    costingport.CostCategory
	Dimension   string // mã chiều đã ánh xạ (vd mã đối tượng công nợ HTX) — không dữ liệu cá nhân
	Description string // ngắn, không dữ liệu cá nhân
}

// SummaryJournal là bút toán tổng hợp của một kỳ ĐÃ DUYỆT. Bất biến: Σ Nợ = Σ Có (> 0); SourceHash
// = ResultSetSHA256 của kỳ costing; cùng (PeriodID, CloseRunID, MappingVersion) → cùng dòng.
type SummaryJournal struct {
	JournalID      uuid.UUID
	TenantID       uuid.UUID
	PeriodID       uuid.UUID
	PeriodCode     string // YYYY-MM
	CloseRunID     uuid.UUID
	Currency       string // "VND"
	Lines          []JournalLine
	TotalDebit     money.Amount
	TotalCredit    money.Amount
	SourceHash     string
	MappingVersion int
	BuiltBy        uuid.UUID
	ApprovedBy     uuid.UUID
	ApprovedAt     time.Time
	// IdempotencyKey: khóa tất định "<journal_id>:<attempt-độc-lập>" — bộ điều hợp PHẢI dùng để
	// phần mềm kế toán không ghi trùng khi đẩy lại (nếu đích hỗ trợ; tệp xuất: tên tệp tất định).
	IdempotencyKey string
}

// ExternalRef là tham chiếu bên phần mềm kế toán sau khi nhận.
type ExternalRef struct {
	Adapter string // tên + phiên bản bộ điều hợp, vd "file_export.csv.v1", "fake.v1"
	Ref     string // mã chứng từ bên đích, hoặc khóa tệp ở kho đối tượng
	// ArtifactSHA256: băm tệp đã xuất (bộ điều hợp tệp) — rỗng nếu đích là API.
	ArtifactSHA256 string
	AcceptedAt     time.Time
}

// LedgerAdapter đẩy một bút toán tổng hợp tới phần mềm kế toán pháp định. Idempotent theo
// j.IdempotencyKey: đẩy lại bút toán đã nhận trả cùng ExternalRef. Lỗi:
//   - ErrAdapterUnavailable (tạm thời: mạng, bảo trì) → thử lại có giãn cách;
//   - ErrAdapterRejected (vĩnh viễn: tài khoản không tồn tại, kỳ đích đã khóa) → push_failed, DLQ,
//     cảnh báo; không tự thử lại.
//
// Hiện thực G4: bộ xuất tệp (CSV và XML, tất định byte-cho-byte) + bộ giả. Nhà cung cấp chưa chốt.
type LedgerAdapter interface {
	Name() string
	Push(ctx context.Context, j SummaryJournal) (ExternalRef, error)
}

// Lỗi ổn định của port (problem.* mã acctbridge.* — docs/errors.md).
var (
	ErrAdapterUnavailable = errors.New("acctbridge: phần mềm kế toán tạm không nhận")
	ErrAdapterRejected    = errors.New("acctbridge: phần mềm kế toán từ chối bút toán")
	ErrUnbalanced         = errors.New("acctbridge: bút toán không cân (Σ Nợ ≠ Σ Có)")
)

// Tham số / cấu hình (tạm, chờ khách).
const (
	// ParamAccountMapping: bảng ánh xạ (CostCategory, DimensionKind) → tài khoản Nợ/Có, có phiên
	// bản, accounting_bridge_operator đề xuất ≠ chief_accountant duyệt. Chế độ kế toán (TT200/TT133)
	// và hệ thống tài khoản chờ khách.
	ParamAccountMapping = "acctbridge.account_mapping"
	// ParamPushRetryMax: số lần thử lại lỗi tạm thời trước khi vào DLQ (mặc định 8, giãn cách mũ).
	ParamPushRetryMax = "acctbridge.push.retry_max"
)

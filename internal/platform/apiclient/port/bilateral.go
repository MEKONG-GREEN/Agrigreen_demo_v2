package port

// Luồng song phương (W5.6 — ADR-0034, kiến trúc v4.1 §38.2): sổ song phương MKG ↔ HTX ở module chủ pf/cooperative
// (schema pf_coop, chính sách hai bên p_pair). Cổng tích hợp (bên a — đối tác, ở đây MKG) chỉ đi qua cổng hẹp
// BilateralLedger này với tenant của khách hàng API (lấy từ bản ghi khóa, không bao giờ từ request); module chủ cài đặt
// và gốc lắp ráp nối (cmd/integration-gateway/assembly, cmd/worker). Kiểu chỉ thư viện chuẩn + uuid.
//
//   - Nạp theo lô (W5.4): collection_schedule, roll_count_statement (author_side a) → AppendCounterparty.
//   - Feed của cặp GET /v1/feeds/bilateral?pair=<htx_ref>: kho feed W5.3, phân vùng = PairID; job pf/apiclient chép
//     bản ghi của sổ theo seq của cặp (PairRecords).
//   - Tổng kiểm theo ngày bilateral_day.v1 (W5.7, R-INT-10) trên sổ thật (DayRecords).
//
// Bản ghi song phương KHÔNG BAO GIỜ tham gia tổng hợp, chỉ số, view _pub, r_agg, sản phẩm dữ liệu hay feed nào khác
// ngoài feed của cặp (ADR-0034 §1, §5).

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// FeedBilateral là tên feed của cặp (contracts/integration bilateral.pair_feed).
const FeedBilateral = "bilateral"

// Phía tác giả của bản ghi song phương.
const (
	SideA = "a" // đối tác (MKG) qua cổng tích hợp
	SideB = "b" // HTX trong tenant chủ (App HTX)
)

// Kênh ghi của bản ghi song phương.
const (
	ChannelAPI  = "api"
	ChannelFile = "file"
	ChannelApp  = "app"
)

// Mã lỗi của luồng song phương (docs/errors.md mục "Cổng tích hợp — luồng song phương").
var (
	// ErrPairMissing: không có cặp song phương đang hiệu lực giữa tenant bên gọi và HTX (chưa lập / đã chấm dứt).
	ErrPairMissing = problem.New(http.StatusForbidden, "integration.pair_missing", "Không có cặp song phương đang hiệu lực giữa bên gọi và HTX này")
	// ErrBilateralWrongSide: hợp đồng song phương do bên kia ghi (vd htx_roll_confirmation chỉ HTX ghi).
	ErrBilateralWrongSide = problem.New(http.StatusForbidden, "integration.bilateral_wrong_side", "Hợp đồng song phương này do bên kia của cặp ghi")
	// ErrBilateralRecordInvalid: bản ghi không đúng hợp đồng song phương (cùng mã với nạp theo lô).
	ErrBilateralRecordInvalid = problem.New(http.StatusUnprocessableEntity, "integration.record_invalid", "Bản ghi không đúng hợp đồng nạp")
	// ErrBilateralKeyConflict: khóa nghiệp vụ đã ghi trong cặp với nội dung khác (cùng mã với nạp theo lô).
	ErrBilateralKeyConflict = problem.New(http.StatusConflict, "integration.business_key_conflict", "Khóa nghiệp vụ đã nhận với nội dung khác")
	// ErrBilateralUnavailable: sổ song phương chưa nối ở tiến trình này.
	ErrBilateralUnavailable = problem.New(http.StatusServiceUnavailable, "integration.bilateral_unavailable", "Sổ song phương chưa sẵn sàng")
)

// pairNamespace là không gian tên UUIDv5 của mã cặp.
var pairNamespace = uuid.MustParse("0199c8a0-5a6e-7c10-9d2e-1b2c3d4e5f60")

// PairID là mã cặp song phương (counterparty_tenant_id, htx_ref) — UUIDv5 tất định, dùng làm partition_key của feed
// cặp và stream của sổ. Hai bên tự tính được, không phải bí mật (quyền đọc do chính sách p_pair + kiểm cặp ở cổng).
func PairID(counterparty, htxRef uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(pairNamespace, append(counterparty[:], htxRef[:]...))
}

// BilateralPair là trạng thái một cặp song phương.
type BilateralPair struct {
	PairID       uuid.UUID
	OwnerTenant  uuid.UUID // tenant chủ của bên HTX
	HTXRef       uuid.UUID // = htx_node_id (Nút HTX trong cây tổ chức của tenant chủ)
	Counterparty uuid.UUID // tenant đối tác (MKG)
	Active       bool
	Since        time.Time  // lần lập gần nhất
	TerminatedAt *time.Time // lần chấm dứt gần nhất (khi !Active)
}

// BilateralSubmission là một bản ghi song phương đã qua hợp đồng (ValidateRecord ở bên gọi) gửi tới sổ.
type BilateralSubmission struct {
	Contract        string
	ContractVersion int
	EntityKey       string
	BusinessKey     string
	HTXRef          uuid.UUID
	Payload         json.RawMessage
	Channel         string // api | file
	ClientID        uuid.UUID
	BatchID         uuid.UUID
	Index           int
	ReceivedAt      time.Time
}

// BilateralRecord là một bản ghi của sổ song phương.
type BilateralRecord struct {
	ID              uuid.UUID
	PairID          uuid.UUID
	HTXRef          uuid.UUID
	Seq             int64 // thứ tự trong cặp (liên tục từ 1, theo thứ tự commit)
	Contract        string
	ContractVersion int
	EntityKey       string
	BusinessKey     string
	AuthorSide      string
	Channel         string
	Payload         json.RawMessage
	PayloadSHA256   string
	RecordedAt      time.Time // thời điểm sổ nhận — ngày nghiệp vụ của tổng kiểm bilateral_day.v1
}

// BilateralLedger là cổng hẹp của sổ song phương cho phía đối tác (bên a). Mọi lời gọi chạy với app.tenant_id =
// counterparty (nhánh counterparty_tenant_id của p_pair) — không bao giờ thấy cặp của tenant khác.
type BilateralLedger interface {
	// AppendCounterparty ghi bản ghi author_side a vào cặp (counterparty, s.HTXRef) đang hiệu lực. Idempotent theo
	// (cặp, hợp đồng, phiên bản, khóa nghiệp vụ): cùng nội dung → nil; khác nội dung → problem 409
	// integration.business_key_conflict; không có cặp hiệu lực → ErrPairMissing.
	AppendCounterparty(ctx context.Context, counterparty uuid.UUID, s BilateralSubmission) error
	// CounterpartyPair trả cặp (kể cả đã chấm dứt); không có → ErrPairMissing.
	CounterpartyPair(ctx context.Context, counterparty, htxRef uuid.UUID) (BilateralPair, error)
	// CounterpartyPairs liệt kê mọi cặp (kể cả đã chấm dứt) của counterparty.
	CounterpartyPairs(ctx context.Context, counterparty uuid.UUID) ([]BilateralPair, error)
	// PairRecords trả tối đa limit bản ghi của cặp có seq > afterSeq, theo seq.
	PairRecords(ctx context.Context, counterparty, pairID uuid.UUID, afterSeq int64, limit int) ([]BilateralRecord, error)
	// DayRecords trả bản ghi của hợp đồng contract (mọi cặp của counterparty, hoặc chỉ pairID khác Nil) có recorded_at
	// trong [from, to).
	DayRecords(ctx context.Context, counterparty uuid.UUID, contract string, pairID uuid.UUID, from, to time.Time) ([]BilateralRecord, error)
}

package port

// Luồng song phương MKG ↔ HTX qua cổng tích hợp (W5.6 — ADR-0034, ADR-0033 §3; Đề xuất, bật production chờ C-12):
// MKG gửi lịch gom và bảng kê số cuộn bằng nạp theo lô (POST /v1/batches/{hợp đồng}), đọc bản ghi của cặp (cả xác
// nhận / phản đối của HTX) qua feed của cặp (GET /v1/feeds/bilateral?pair=<htx_ref>) và đối soát tổng kiểm theo ngày
// (GET /v1/feeds/{hợp đồng}/checksums). Như mọi đối tác: khóa API, hạn mức, sổ đo lường. Van valve_collection giữ
// nguyên tới W5.10.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// JobBilateral là tên job gửi lô + kéo feed của cặp + đối soát tổng kiểm (AG_MKG_JOBS).
const JobBilateral = "fieldops_bilateral"

// Hợp đồng song phương (contracts/integration/ingest; collection_schedule cùng tên hợp đồng van —
// ContractCollectionSchedule ở port.go).
const (
	ContractRollCountStatement  = "roll_count_statement"
	ContractHTXRollConfirmation = "htx_roll_confirmation"
)

// BilateralContracts là các hợp đồng song phương MKG đối soát, theo thứ tự.
var BilateralContracts = []string{ContractCollectionSchedule, ContractRollCountStatement, ContractHTXRollConfirmation}

// BilateralItem là một phần tử của lô nạp (phong bì ingest_batch.v1).
type BilateralItem struct {
	EntityKey string
	Record    json.RawMessage
}

// BilateralResult là kết quả một bản ghi của lô (GET /v1/batches/{id}).
type BilateralResult struct {
	Index       int
	Outcome     string // accepted | rejected
	ProblemCode string
}

// BilateralBatch là trạng thái lô.
type BilateralBatch struct {
	ID      uuid.UUID
	Status  string // queued | completed | dead_lettered
	Results []BilateralResult
}

// PairFeedItem là một mục feed của cặp.
type PairFeedItem struct {
	Seq             int64
	EventID         uuid.UUID
	Contract        string // tên hợp đồng (không phiên bản)
	ContractVersion int
	EntityKey       string
	Record          json.RawMessage
	RecordedAt      time.Time
}

// PairFeedPage là một trang feed của cặp.
type PairFeedPage struct {
	NextCursor string
	HasMore    bool
	AsOf       time.Time
	Items      []PairFeedItem
}

// DayChecksum là tổng kiểm một ngày (bilateral_day.v1) do cổng trả.
type DayChecksum struct {
	Date        string
	RecordCount int64
	SHA256      string
}

// BilateralGateway là cổng tích hợp nhìn từ MKG (khóa API của khách hàng API tenant MKG). Lỗi:
//   - ErrBilateralRetry (bọc): mạng, 429, 5xx — thử lại lượt sau với CÙNG batch_ref;
//   - ErrPairMissing: 403 integration.pair_missing (cặp chưa lập / đã chấm dứt);
//   - ErrFeedCursorExpired: 410 — đọc lại từ đầu phần còn giữ;
//   - lỗi khác (4xx của lô: hợp đồng không phục vụ, thiếu scope…): không thử lại cùng lô.
type BilateralGateway interface {
	SubmitBatch(ctx context.Context, contract string, version int, batchRef uuid.UUID, items []BilateralItem) (uuid.UUID, error)
	Batch(ctx context.Context, id uuid.UUID) (BilateralBatch, error)
	PairPage(ctx context.Context, htxRef uuid.UUID, cursor string, limit int) (PairFeedPage, error)
	DayChecksums(ctx context.Context, contract string, htxRef uuid.UUID, from, to string) ([]DayChecksum, error)
}

// Lỗi của BilateralGateway.
var (
	ErrBilateralRetry = errors.New("fieldops: cổng tích hợp tạm không nhận — thử lại lượt sau")
	ErrPairMissing    = errors.New("fieldops: không có cặp song phương đang hiệu lực với HTX (integration.pair_missing)")
)

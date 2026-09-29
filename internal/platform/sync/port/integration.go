package port

import (
	"time"

	"github.com/google/uuid"
)

// Giám sát đồng bộ tích hợp (GIS US-SYNC-01/02, CGH FN-07 BR-04): mỗi consumer của worker
// là một luồng đồng bộ theo sự kiện (App HTX → CGH, ERP/MKG → GIS qua van công bố, …). Lần
// xử lý thành công nằm ở shared.inbox; lần thất bại ở pf_sync.integration_failure.

// EventIntegrationDeadLettered: một sự kiện đã hết số lần thử (x-delivery-limit) và vào
// hàng chờ lỗi <consumer>.dlq — pf/notify báo Admin nền tảng (GIS US-SYNC-02 AC-1).
const EventIntegrationDeadLettered = "pf.sync.integration.dead_lettered"

// IntegrationDeadLetteredV1 là payload của pf.sync.integration.dead_lettered, schema_version 1.
// Không có dữ liệu nghiệp vụ hay thông điệp lỗi tự do — chỉ mã lỗi đã chuẩn hóa.
//
//ag:event pf.sync.integration.dead_lettered v1
type IntegrationDeadLetteredV1 struct {
	FailureID     uuid.UUID `json:"failure_id"`
	Consumer      string    `json:"consumer"`
	Source        string    `json:"source"` // nhãn nguồn tích hợp (tiếng Việt)
	EventID       uuid.UUID `json:"event_id"`
	EventType     string    `json:"event_type"`
	ErrorCode     string    `json:"error_code"`
	DeliveryCount int       `json:"delivery_count"`
	RecordedAt    string    `json:"recorded_at"`
}

// ConsumerFailure là một lần xử lý thất bại cần ghi (từ inbox.Failure).
type ConsumerFailure struct {
	TenantID      uuid.UUID
	EventID       uuid.UUID
	Consumer      string
	EventType     string
	ErrorCode     string
	DeliveryCount int
	DeadLettered  bool
	EventTime     time.Time
}

// Trạng thái của một luồng đồng bộ trên màn giám sát.
const (
	IntegrationOK    = "ok"    // lần gần nhất thành công, không có sự kiện trong DLQ
	IntegrationError = "error" // lần gần nhất thất bại, hoặc có sự kiện vào DLQ trong cửa sổ, hoặc DLQ > 0
	IntegrationIdle  = "idle"  // chưa có sự kiện nào trong cửa sổ
)

package apiclient

import (
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/ingest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Nạp theo lô của cổng tích hợp (W5.4, ADR-0033 §3) — gốc lắp ráp (cmd/integration-gateway/assembly) chỉ đi qua
// gốc module này (R-02).
type (
	// IngestService là nạp theo lô (POST/GET /v1/batches, bộ xử lý hàng đợi đối tác).
	IngestService = ingest.Service
	// IngestQueue là hàng đợi riêng từng đối tác.
	IngestQueue = ingest.Queue
	// IngestOwner là port module chủ đã nối cho một hợp đồng nạp.
	IngestOwner = ingest.Owner
	// IngestSink là port module chủ (interface do bộ xử lý lô khai).
	IngestSink = ingest.RecordSink
	// IngestRecord là một bản ghi đã qua hợp đồng gửi tới port module chủ.
	IngestRecord = ingest.Record
	// IngestMemoryQueue là hàng đợi trong tiến trình (dev/test).
	IngestMemoryQueue = ingest.Memory
)

// NewIngestMemoryQueue tạo hàng đợi trong tiến trình (dev/test, một bản sao).
func NewIngestMemoryQueue(maxLen int) *ingest.Memory { return ingest.NewMemory(maxLen) }

// NewIngestRabbitQueue tạo hàng đợi RabbitMQ (quorum queue + DLQ riêng từng đối tác).
func NewIngestRabbitQueue(url string, maxLen int, log *slog.Logger) *ingest.Rabbit {
	return ingest.NewRabbit(url, maxLen, log)
}

// IngestDeps là phụ thuộc của nạp theo lô.
type IngestDeps struct {
	Queue ingest.Queue
	// Production: residency.Production — hợp đồng proposed/draft không bao giờ phục vụ.
	Production bool
	// AllowProposed: cờ dev/test AG_GATEWAY_INGEST_ALLOW_PROPOSED (bị bỏ qua ở production).
	AllowProposed bool
	// Owners: hợp đồng → port module chủ. Nil → chỉ ba hợp đồng song phương vào sổ đệm (StagingOwners).
	Owners         map[string]ingest.Owner
	MaxQueued      int
	RetryAfter     time.Duration
	OperatorTenant uuid.UUID
	Clock          clock.Clock
	Log            *slog.Logger
}

// NewIngest dựng nạp theo lô trên pool của đơn vị (bọc vai r_apiclient).
func NewIngest(db tenant.Beginner, d IngestDeps) *ingest.Service {
	db = forModule(db)
	owners := d.Owners
	if owners == nil {
		owners = StagingOwners(db, d.Clock)
	}
	return &ingest.Service{DB: db, Queue: d.Queue, Gate: ingest.Gate{Production: d.Production, AllowProposed: d.AllowProposed && !d.Production},
		Owners: owners, MaxQueued: d.MaxQueued, RetryAfter: d.RetryAfter, OperatorTenant: d.OperatorTenant, Clock: d.Clock, Log: d.Log}
}

// StagingOwners nối ba hợp đồng song phương (collection_schedule, roll_count_statement, htx_roll_confirmation) vào
// sổ đệm pf_apiclient.ingest_staging — port module chủ CHƯA có (W5.6 thay bằng port thật).
func StagingOwners(db tenant.Beginner, clk clock.Clock) map[string]ingest.Owner {
	db = forModule(db)
	out := map[string]ingest.Owner{}
	for _, c := range ingest.BilateralContracts {
		out[c] = ingest.Owner{Target: ingest.StagingTarget, Sink: ingest.Staging{DB: db, Clock: clk}}
	}
	return out
}

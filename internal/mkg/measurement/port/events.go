package port

import (
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Sự kiện NỘI BỘ MKG (ADR-0018): outbox.Event{Kind: outbox.KindInternal} → vhost
// RabbitMQ mkg.internal, consumer ở cmd/worker-internal. Schema ở
// contracts/events-internal/; contractcheck bắt payload //ag:internal-event không
// bao giờ đi với KindPlatform (vhost nền tảng).
const (
	EventWeighTicketRecorded     = "mkg.measurement.weigh_ticket.recorded"
	EventReconciliationCompleted = "mkg.measurement.reconciliation.completed"
)

// Hợp đồng van (dự thảo, contracts/publication/) — CHỈ trường này của measurement
// được qua van: khối lượng làm tròn theo bậc cho MRV (chờ C-1).
const ContractMRVCollectionEvidence = "mrv_collection_evidence"

// EventMRVCollectionEvidencePublished là sự kiện van (dự thảo) — outbox.KindPlatform.
const EventMRVCollectionEvidencePublished = "mkg.measurement.mrv_collection_evidence.published"

// Publication là payload được phép đi qua van (chỉ struct //ag:publication cài đặt).
type Publication interface {
	PublicationName() string
}

// MRVCollectionEvidencePublishedV1 là payload van mrv_collection_evidence v1 (dự
// thảo, chờ C-1): khối lượng theo BẬC, không bao giờ khối lượng chính xác.
//
//ag:publication mrv_collection_evidence
//ag:event mkg.measurement.mrv_collection_evidence.published v1
type MRVCollectionEvidencePublishedV1 struct {
	EvidenceRef       uuid.UUID  `json:"evidence_ref"`
	PlotRef           uuid.UUID  `json:"plot_ref"`
	SeasonRef         *uuid.UUID `json:"season_ref"`
	CollectedOn       string     `json:"collected_on"`
	CollectedKgBucket string     `json:"collected_kg_bucket"`
	BucketRuleVersion string     `json:"bucket_rule_version"`
	Revision          int        `json:"revision"`
	// MassBasis (thêm ở G3-2, tùy chọn trong hợp đồng — tương thích ngược): HUB_IN, hoặc
	// CUSTOMER_IN_AGGREGATED cho tuyến DIRECT (khối lượng khách cân, gộp, không tách hao hụt).
	MassBasis string `json:"mass_basis"`
}

// PublicationName cài đặt Publication.
func (MRVCollectionEvidencePublishedV1) PublicationName() string { return "mrv_collection_evidence" }

// WeighTicketRecordedV1 là payload khi một phiếu cân được ghi (kể cả phiếu bù).
// Người nhận nội bộ: mkg/hub (khối lượng lô nhận — Q-MKG-04), mkg/costing (G4).
//
//ag:internal-event mkg.measurement.weigh_ticket.recorded v1
type WeighTicketRecordedV1 struct {
	TicketID     uuid.UUID    `json:"ticket_id"`
	LotRef       uuid.UUID    `json:"lot_ref"`
	Point        WeighPoint   `json:"point"`
	Route        Route        `json:"route"`
	Source       TicketSource `json:"source"`
	MassKg       mass.Kg      `json:"mass_kg"`
	MoisturePct  *string      `json:"moisture_pct"`
	EventTime    time.Time    `json:"event_time"`
	RuleVersion  *string      `json:"rule_version"`
	SupersedesID *uuid.UUID   `json:"supersedes_id"`
	// CollectionOrderRef (thêm ở G3-2, tùy chọn — tương thích ngược): lệnh gom liên kết
	// TƯỜNG MINH với lô (sổ lot_collection); null = quy ước đồng nhất (lot_ref là mã lệnh gom).
	CollectionOrderRef *uuid.UUID `json:"collection_order_ref"`
}

// ReconciliationCompletedV1 là payload khi đối soát một lô hoàn tất (RB-6):
// tách hao hụt dự kiến / bất thường (HUB_IN → HUB_OUT, định mức sấy có phiên bản)
// và chênh thương mại (HUB_OUT → CUSTOMER_IN, bảng dung sai có phiên bản — C-4).
//
//ag:internal-event mkg.measurement.reconciliation.completed v1
type ReconciliationCompletedV1 struct {
	ReconciliationID uuid.UUID  `json:"reconciliation_id"`
	LotRef           uuid.UUID  `json:"lot_ref"`
	Route            Route      `json:"route"`
	HubInKg          *mass.Kg   `json:"hub_in_kg"`
	HubOutKg         *mass.Kg   `json:"hub_out_kg"`
	CustomerInKg     *mass.Kg   `json:"customer_in_kg"`
	ExpectedLossKg   *mass.Kg   `json:"expected_loss_kg"`
	AbnormalLossKg   *mass.Kg   `json:"abnormal_loss_kg"`
	CommercialDiffKg *mass.Kg   `json:"commercial_diff_kg"`
	Outcome          string     `json:"outcome"`
	ToleranceVersion *string    `json:"tolerance_version"`
	LossNormVersion  *string    `json:"loss_norm_version"`
	DisputeID        *uuid.UUID `json:"dispute_id"`
	RuleVersion      string     `json:"rule_version"`
	EventTime        time.Time  `json:"event_time"`
}

// Kết quả đối soát.
const (
	OutcomeWithinTolerance = "within_tolerance" // mọi chênh trong ngưỡng
	OutcomeLossAlert       = "loss_alert"       // hao hụt bất thường HUB_IN → HUB_OUT vượt định mức
	OutcomeDisputeOpened   = "dispute_opened"   // chênh HUB_OUT → CUSTOMER_IN vượt dung sai → hồ sơ tranh chấp
	OutcomeDirectAggregate = "direct_aggregate" // RouteDirect: chỉ CUSTOMER_IN, không tách hao hụt
)

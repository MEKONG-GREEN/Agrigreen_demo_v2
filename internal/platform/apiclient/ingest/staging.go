package ingest

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// StagingTarget là tên đích ghi vào kết quả bản ghi khi hợp đồng đi vào sổ đệm.
const StagingTarget = "pf/apiclient.ingest_staging"

// BilateralContracts là ba hợp đồng nạp song phương (ADR-0034, C-12): W5.4 nối chúng vào Staging; từ W5.6 bên a
// (collection_schedule, roll_count_statement) đi vào sổ song phương thật (BilateralOwners — pf/cooperative, pf_coop),
// htx_roll_confirmation do HTX ghi trên App HTX.
var BilateralContracts = []string{"collection_schedule", "roll_count_statement", "htx_roll_confirmation"}

// Staging là RecordSink sổ đệm của W5.4: ghi bản ghi đã qua hợp đồng vào pf_apiclient.ingest_staging (tenant đối tác
// gửi), idempotent theo (hợp đồng, phiên bản, khóa nghiệp vụ): cùng khóa cùng nội dung → accepted (gọi lặp); cùng khóa
// khác nội dung → rejected integration.business_key_conflict. Từ W5.6 KHÔNG nối mặc định (port thật thay) — chỉ còn
// cho kiểm thử cơ chế lô / kênh tệp khi gốc lắp ráp truyền tường minh (StagingOwners).
type Staging struct {
	DB    tenant.Beginner // pool đã bọc vai r_apiclient
	Clock clock.Clock
}

// canonical chuẩn hóa JSON (khóa sắp xếp, số giữ nguyên) để so nội dung không phụ thuộc định dạng.
func canonical(raw []byte) ([]byte, error) { return integration.CanonicalJSON(raw) }

// Ingest ghi một bản ghi vào sổ đệm.
func (s Staging) Ingest(ctx context.Context, r Record) error {
	payload, err := canonical(r.Payload)
	if err != nil {
		return ErrRecordInvalid
	}
	sum := Hash(payload)
	now := r.ReceivedAt
	if s.Clock != nil {
		now = s.Clock.Now()
	}
	row := stagedRow{ID: ids.New(), TenantID: r.PartnerTenant, Contract: r.Contract, ContractVersion: r.ContractVersion, ClientID: r.ClientID,
		BatchID: r.BatchID, Index: r.Index, EntityKey: r.EntityKey, BusinessKey: r.BusinessKey, Payload: payload, PayloadSHA256: sum, At: now}
	err = tenant.WithTenantTx(ctx, s.DB, r.PartnerTenant, func(tx pgx.Tx) error { return insertStaged(ctx, tx, row) })
	if !errors.Is(err, ledger.ErrDuplicate) {
		return err
	}
	var prev string
	if err := tenant.WithTenantTx(ctx, s.DB, r.PartnerTenant, func(tx pgx.Tx) error {
		var err error
		prev, err = stagedHash(ctx, tx, r.Contract, r.ContractVersion, r.BusinessKey)
		return err
	}); err != nil {
		return err
	}
	if prev != sum {
		return ErrBusinessKeyConflict
	}
	return nil
}

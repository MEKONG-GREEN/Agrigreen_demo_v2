package ingest

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// fileRow là một dòng sổ pf_apiclient.ingest_file (pf 000128).
type fileRow struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ClientID        uuid.UUID
	OperatorTenant  uuid.UUID
	Contract        string
	ContractVersion int
	Format          string
	SHA256          string
	Bytes           int64
	Records         int
	ChunkSize       int
	Batches         int
	TraceID         string
	ReceivedAt      time.Time
}

const fileCols = `id, tenant_id, client_id, operator_tenant_id, contract, contract_version, format, file_sha256, file_bytes, record_count,
  chunk_size, batch_count, coalesce(trace_id, ''), event_time`

func scanFile(row pgx.Row) (*fileRow, error) {
	var f fileRow
	err := row.Scan(&f.ID, &f.TenantID, &f.ClientID, &f.OperatorTenant, &f.Contract, &f.ContractVersion, &f.Format, &f.SHA256, &f.Bytes,
		&f.Records, &f.ChunkSize, &f.Batches, &f.TraceID, &f.ReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// fileIdem: một tệp (tổng kiểm) mỗi (khách hàng, hợp đồng, phiên bản).
func fileIdem(clientID uuid.UUID, contractID, sha string) string {
	return "file:" + clientID.String() + ":" + contractID + ":" + sha
}

func insertFile(ctx context.Context, tx pgx.Tx, f fileRow, contractID string) error {
	var trace *string
	if f.TraceID != "" {
		trace = &f.TraceID
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.ingest_file (id, tenant_id, stream_key, client_id, operator_tenant_id, contract,
	    contract_version, format, file_sha256, file_bytes, record_count, chunk_size, batch_count, trace_id, event_time, actor_ref, idem_key,
	    rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::text, $7::int, $8::text, $9::text, $10::bigint, $11::int, $12::int,
	    $13::int, $14::text, $15::timestamptz, $4::uuid, $16::text, $17::text)`,
		f.ID, f.TenantID, "client:"+f.ClientID.String(), f.ClientID, f.OperatorTenant, f.Contract, f.ContractVersion, f.Format, f.SHA256,
		f.Bytes, f.Records, f.ChunkSize, f.Batches, trace, f.ReceivedAt.UTC(), fileIdem(f.ClientID, contractID, f.SHA256), RuleVersion)
	return ledger.Classify(err)
}

func fileByChecksum(ctx context.Context, tx pgx.Tx, clientID uuid.UUID, contract string, version int, sha string) (*fileRow, error) {
	return scanFile(tx.QueryRow(ctx, "SELECT "+fileCols+` FROM pf_apiclient.ingest_file
	  WHERE client_id = $1::uuid AND contract = $2::text AND contract_version = $3::int AND file_sha256 = $4::text
	  ORDER BY recorded_at LIMIT 1`, clientID, contract, version, sha))
}

func fileByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*fileRow, error) {
	return scanFile(tx.QueryRow(ctx, "SELECT "+fileCols+" FROM pf_apiclient.ingest_file WHERE id = $1::uuid LIMIT 1", id))
}

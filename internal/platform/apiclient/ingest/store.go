package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// batchRow là một dòng sổ ingest_batch.
type batchRow struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ClientID        uuid.UUID
	OperatorTenant  uuid.UUID
	Contract        string
	ContractVersion int
	BatchRef        uuid.UUID
	RequestHash     string
	ItemCount       int
	Items           []byte
	SentAt          time.Time
	AsOf            *time.Time
	TraceID         string
	ReceivedAt      time.Time
}

const batchCols = `id, tenant_id, client_id, operator_tenant_id, contract, contract_version, batch_ref, request_hash, item_count, items,
  sent_at, as_of, coalesce(trace_id, ''), event_time`

func scanBatch(row pgx.Row) (*batchRow, error) {
	var b batchRow
	err := row.Scan(&b.ID, &b.TenantID, &b.ClientID, &b.OperatorTenant, &b.Contract, &b.ContractVersion, &b.BatchRef, &b.RequestHash,
		&b.ItemCount, &b.Items, &b.SentAt, &b.AsOf, &b.TraceID, &b.ReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func batchIdem(clientID, batchRef uuid.UUID) string {
	return "batch:" + clientID.String() + ":" + batchRef.String()
}

func insertBatch(ctx context.Context, tx pgx.Tx, b batchRow) error {
	var trace *string
	if b.TraceID != "" {
		trace = &b.TraceID
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.ingest_batch (id, tenant_id, stream_key, client_id, operator_tenant_id, contract,
	    contract_version, batch_ref, request_hash, item_count, items, sent_at, as_of, trace_id, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::text, $7::int, $8::uuid, $9::text, $10::int, $11::jsonb,
	    $12::timestamptz, $13::timestamptz, $14::text, $15::timestamptz, $4::uuid, $16::text, $17::text)`,
		b.ID, b.TenantID, "client:"+b.ClientID.String(), b.ClientID, b.OperatorTenant, b.Contract, b.ContractVersion, b.BatchRef, b.RequestHash,
		b.ItemCount, string(b.Items), b.SentAt.UTC(), utcPtr(b.AsOf), trace, b.ReceivedAt.UTC(), batchIdem(b.ClientID, b.BatchRef), RuleVersion)
	return ledger.Classify(err)
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func batchByRef(ctx context.Context, tx pgx.Tx, clientID, batchRef uuid.UUID) (*batchRow, error) {
	return scanBatch(tx.QueryRow(ctx, "SELECT "+batchCols+` FROM pf_apiclient.ingest_batch
	  WHERE client_id = $1::uuid AND batch_ref = $2::uuid ORDER BY recorded_at LIMIT 1`, clientID, batchRef))
}

func batchByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*batchRow, error) {
	return scanBatch(tx.QueryRow(ctx, "SELECT "+batchCols+" FROM pf_apiclient.ingest_batch WHERE id = $1::uuid LIMIT 1", id))
}

// outcomeRow là kết cục lô (nil = chưa xong).
type outcomeRow struct {
	Outcome       string
	Accepted      int
	Rejected      int
	ProblemCode   string
	DeliveryCount int
	At            time.Time
}

func outcomeOf(ctx context.Context, tx pgx.Tx, batchID uuid.UUID) (*outcomeRow, error) {
	var o outcomeRow
	err := tx.QueryRow(ctx, `SELECT outcome, accepted, rejected, coalesce(problem_code, ''), delivery_count, event_time
	    FROM pf_apiclient.ingest_batch_outcome WHERE batch_id = $1::uuid ORDER BY recorded_at LIMIT 1`, batchID).
		Scan(&o.Outcome, &o.Accepted, &o.Rejected, &o.ProblemCode, &o.DeliveryCount, &o.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func insertOutcome(ctx context.Context, tx pgx.Tx, id, tenantID, clientID, batchID uuid.UUID, o outcomeRow) error {
	var code *string
	if o.ProblemCode != "" {
		code = &o.ProblemCode
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.ingest_batch_outcome (id, tenant_id, stream_key, batch_id, outcome, accepted, rejected,
	    problem_code, delivery_count, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::int, $7::int, $8::text, $9::int, $10::timestamptz, $11::uuid, $12::text,
	    $13::text)`,
		id, tenantID, "batch:"+batchID.String(), batchID, o.Outcome, o.Accepted, o.Rejected, code, o.DeliveryCount, o.At.UTC(), clientID,
		"outcome:"+batchID.String(), RuleVersion)
	return ledger.Classify(err)
}

// Result là kết quả một bản ghi.
type Result struct {
	Index       int    `json:"index"`
	EntityKey   string `json:"entity_key"`
	Outcome     string `json:"outcome"`
	ProblemCode string `json:"problem_code,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// Kết quả một bản ghi.
const (
	OutcomeAccepted = "accepted"
	OutcomeRejected = "rejected"
)

func insertResult(ctx context.Context, tx pgx.Tx, id, tenantID, clientID, batchID uuid.UUID, target string, r Result, at time.Time) error {
	var code, detail *string
	if r.ProblemCode != "" {
		code = &r.ProblemCode
	}
	if r.Detail != "" {
		d := trim(r.Detail)
		detail = &d
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.ingest_record_result (id, tenant_id, stream_key, batch_id, item_index, entity_key,
	    outcome, problem_code, detail, target, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::int, $6::text, $7::text, $8::text, $9::text, $10::text, $11::timestamptz,
	    $12::uuid, $13::text, $14::text)`,
		id, tenantID, "batch:"+batchID.String(), batchID, r.Index, truncKey(r.EntityKey), r.Outcome, code, detail, target, at.UTC(), clientID,
		"result:"+batchID.String()+":"+itoa(r.Index), RuleVersion)
	return ledger.Classify(err)
}

func resultsOf(ctx context.Context, tx pgx.Tx, batchID uuid.UUID) ([]Result, error) {
	rows, err := tx.Query(ctx, `SELECT item_index, entity_key, outcome, coalesce(problem_code, ''), coalesce(detail, '')
	    FROM pf_apiclient.ingest_record_result WHERE batch_id = $1::uuid ORDER BY item_index`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Result{}
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Index, &r.EntityKey, &r.Outcome, &r.ProblemCode, &r.Detail); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// stagedRow là một dòng sổ đệm song phương.
type stagedRow struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Contract        string
	ContractVersion int
	ClientID        uuid.UUID
	BatchID         uuid.UUID
	Index           int
	EntityKey       string
	BusinessKey     string
	Payload         json.RawMessage
	PayloadSHA256   string
	At              time.Time
}

func stageIdem(contract string, version int, businessKey string) string {
	return "stage:" + contract + ":v" + itoa(version) + ":" + businessKey
}

func insertStaged(ctx context.Context, tx pgx.Tx, r stagedRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.ingest_staging (id, tenant_id, stream_key, contract, contract_version, client_id,
	    batch_id, item_index, entity_key, business_key, payload, payload_sha256, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::int, $6::uuid, $7::uuid, $8::int, $9::text, $10::text, $11::jsonb, $12::text,
	    $13::timestamptz, $6::uuid, $14::text, $15::text)`,
		r.ID, r.TenantID, "stage:"+r.Contract, r.Contract, r.ContractVersion, r.ClientID, r.BatchID, r.Index, truncKey(r.EntityKey), r.BusinessKey,
		string(r.Payload), r.PayloadSHA256, r.At.UTC(), stageIdem(r.Contract, r.ContractVersion, r.BusinessKey), RuleVersion)
	return ledger.Classify(err)
}

func stagedHash(ctx context.Context, tx pgx.Tx, contract string, version int, businessKey string) (string, error) {
	var h string
	err := tx.QueryRow(ctx, `SELECT payload_sha256 FROM pf_apiclient.ingest_staging
	    WHERE contract = $1::text AND contract_version = $2::int AND business_key = $3::text ORDER BY recorded_at LIMIT 1`,
		contract, version, businessKey).Scan(&h)
	return h, err
}

// activePartners liệt kê khách hàng API đang active của đơn vị vận hành (tx ở tenant vận hành) — để mở consumer
// hàng đợi riêng của từng đối tác khi khởi động.
func activePartners(ctx context.Context, tx pgx.Tx) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT client_id FROM pf_apiclient.client_current WHERE status = 'active' ORDER BY client_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func truncKey(s string) string {
	if r := []rune(s); len(r) > 300 {
		return string(r[:300])
	}
	return s
}

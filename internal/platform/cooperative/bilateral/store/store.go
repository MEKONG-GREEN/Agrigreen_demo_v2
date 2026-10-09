// Package store: SQL của sổ song phương pf_coop.bilateral_pair_event, pf_coop.bilateral_record (pf 000135, W5.6 —
// ADR-0034). Mọi hàm nhận pgx.Tx đã ở vai r_cooperative + ngữ cảnh (app.tenant_id, app.node_id): chính sách hai bên
// (p_pair) quyết định dòng nào thấy được — hàm KHÔNG tự lọc theo bên gọi ngoài điều kiện nghiệp vụ.
package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// RuleVersion ghim vào rule_version của hai sổ.
const RuleVersion = "cooperative.bilateral.v1"

// PairEvent là một sự kiện vòng đời cặp.
type PairEvent struct {
	ID           uuid.UUID
	TenantID     uuid.UUID // tenant chủ của HTX
	PairID       uuid.UUID
	HTXNodeID    uuid.UUID
	Counterparty uuid.UUID
	AuthorSide   string
	Action       string // established | terminated
	Reason       *string
	EventTime    time.Time
	ActorRef     uuid.UUID
	IdemKey      string
}

// Pair là trạng thái cặp = sự kiện cuối.
type Pair struct {
	PairID       uuid.UUID
	TenantID     uuid.UUID
	HTXNodeID    uuid.UUID
	Counterparty uuid.UUID
	LastAction   string
	LastAt       time.Time
	Events       int64
}

// Active: sự kiện cuối là established.
func (p Pair) Active() bool { return p.LastAction == "established" }

// InsertPairEvent ghi một sự kiện vòng đời cặp (ledger.ErrDuplicate khi idem_key đã ghi).
func InsertPairEvent(ctx context.Context, tx pgx.Tx, e PairEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.bilateral_pair_event (id, tenant_id, stream_key, pair_id, htx_node_id, counterparty_tenant_id,
	    author_side, action, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::uuid, $7::text, $8::text, $9::text, $10::timestamptz, $11::uuid, $12::text, $13::text)`,
		e.ID, e.TenantID, "pair:"+e.PairID.String(), e.PairID, e.HTXNodeID, e.Counterparty, e.AuthorSide, e.Action, e.Reason, e.EventTime.UTC(),
		e.ActorRef, e.IdemKey, RuleVersion)
	return ledger.Classify(err)
}

const pairSelect = `SELECT DISTINCT ON (pair_id) pair_id, tenant_id, htx_node_id, counterparty_tenant_id, action, event_time,
    count(*) OVER (PARTITION BY pair_id)
  FROM pf_coop.bilateral_pair_event`

func scanPairs(rows pgx.Rows, err error) ([]Pair, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pair
	for rows.Next() {
		var p Pair
		if err := rows.Scan(&p.PairID, &p.TenantID, &p.HTXNodeID, &p.Counterparty, &p.LastAction, &p.LastAt, &p.Events); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PairByID trả trạng thái cặp thấy được (nil nếu không có / không thấy).
func PairByID(ctx context.Context, tx pgx.Tx, pairID uuid.UUID) (*Pair, error) {
	ps, err := scanPairs(tx.Query(ctx, pairSelect+` WHERE pair_id = $1::uuid ORDER BY pair_id, seq DESC`, pairID))
	if err != nil || len(ps) == 0 {
		return nil, err
	}
	return &ps[0], nil
}

// PairsByCounterparty liệt kê cặp của tenant đối tác (nhánh counterparty của chính sách).
func PairsByCounterparty(ctx context.Context, tx pgx.Tx, counterparty uuid.UUID) ([]Pair, error) {
	return scanPairs(tx.Query(ctx, pairSelect+` WHERE counterparty_tenant_id = $1::uuid ORDER BY pair_id, seq DESC`, counterparty))
}

// PairsByHTX liệt kê cặp của Nút HTX trong tenant chủ (nhánh HTX của chính sách).
func PairsByHTX(ctx context.Context, tx pgx.Tx, tid, htx uuid.UUID) ([]Pair, error) {
	return scanPairs(tx.Query(ctx, pairSelect+` WHERE tenant_id = $1::uuid AND htx_node_id = $2::uuid ORDER BY pair_id, seq DESC`, tid, htx))
}

// Record là một bản ghi song phương.
type Record struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	PairID          uuid.UUID
	HTXNodeID       uuid.UUID
	Counterparty    uuid.UUID
	Seq             int64
	Contract        string
	ContractVersion int
	RecordRef       string
	BusinessKey     string
	Payload         []byte
	PayloadSHA256   string
	AuthorSide      string
	Channel         string
	ClientID        *uuid.UUID
	BatchID         *uuid.UUID
	ItemIndex       *int
	EventTime       time.Time
	RecordedAt      time.Time
	ActorRef        uuid.UUID
}

// IdemKey là idem_key của bản ghi: một dòng mỗi (cặp, hợp đồng, phiên bản, khóa nghiệp vụ).
func IdemKey(pair uuid.UUID, contract string, version int, businessKey string) string {
	return "rec:" + pair.String() + ":" + contract + ":v" + strconv.Itoa(version) + ":" + businessKey
}

// InsertRecord ghi một bản ghi (ledger.ErrDuplicate khi khóa nghiệp vụ đã ghi trong cặp).
func InsertRecord(ctx context.Context, tx pgx.Tx, r Record) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.bilateral_record (id, tenant_id, stream_key, pair_id, htx_node_id, counterparty_tenant_id,
	    contract, contract_version, record_ref, business_key, payload, payload_sha256, author_side, channel, client_id, batch_id, item_index,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::uuid, $7::text, $8::int, $9::text, $10::text, $11::jsonb, $12::text,
	    $13::text, $14::text, $15::uuid, $16::uuid, $17::int, $18::timestamptz, $19::uuid, $20::text, $21::text)`,
		r.ID, r.TenantID, "pair:"+r.PairID.String(), r.PairID, r.HTXNodeID, r.Counterparty, r.Contract, r.ContractVersion, r.RecordRef,
		r.BusinessKey, string(r.Payload), r.PayloadSHA256, r.AuthorSide, r.Channel, r.ClientID, r.BatchID, r.ItemIndex, r.EventTime.UTC(),
		r.ActorRef, IdemKey(r.PairID, r.Contract, r.ContractVersion, r.BusinessKey), RuleVersion)
	return ledger.Classify(err)
}

// RecordHash trả payload_sha256 của bản ghi đã ghi theo khóa nghiệp vụ trong cặp ("" nếu không thấy).
func RecordHash(ctx context.Context, tx pgx.Tx, pair uuid.UUID, contract string, version int, businessKey string) (string, error) {
	var h string
	err := tx.QueryRow(ctx, `SELECT payload_sha256 FROM pf_coop.bilateral_record
	    WHERE pair_id = $1::uuid AND contract = $2::text AND contract_version = $3::int AND business_key = $4::text
	    ORDER BY recorded_at LIMIT 1`, pair, contract, version, businessKey).Scan(&h)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return h, err
}

const recordSelect = `SELECT id, tenant_id, pair_id, htx_node_id, counterparty_tenant_id, seq, contract, contract_version, record_ref,
    business_key, payload, payload_sha256, author_side, channel, client_id, batch_id, item_index, event_time, recorded_at, actor_ref
  FROM pf_coop.bilateral_record`

func scanRecords(rows pgx.Rows, err error) ([]Record, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.TenantID, &r.PairID, &r.HTXNodeID, &r.Counterparty, &r.Seq, &r.Contract, &r.ContractVersion,
			&r.RecordRef, &r.BusinessKey, &r.Payload, &r.PayloadSHA256, &r.AuthorSide, &r.Channel, &r.ClientID, &r.BatchID, &r.ItemIndex,
			&r.EventTime, &r.RecordedAt, &r.ActorRef); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordsAfter trả tối đa limit bản ghi thấy được của cặp có seq > after, theo seq.
func RecordsAfter(ctx context.Context, tx pgx.Tx, pair uuid.UUID, after int64, limit int) ([]Record, error) {
	return scanRecords(tx.Query(ctx, recordSelect+` WHERE pair_id = $1::uuid AND seq > $2::bigint ORDER BY seq LIMIT $3::int`, pair, after, limit))
}

// RecordByKey trả bản ghi thấy được theo hợp đồng + khóa nghiệp vụ (mọi cặp thấy được), nil nếu không có.
func RecordByKey(ctx context.Context, tx pgx.Tx, contract, businessKey string) (*Record, error) {
	rs, err := scanRecords(tx.Query(ctx, recordSelect+` WHERE contract = $1::text AND business_key = $2::text ORDER BY recorded_at LIMIT 1`,
		contract, businessKey))
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	return &rs[0], nil
}

// CounterpartyDayRecords trả bản ghi của hợp đồng (mọi cặp của counterparty, hoặc chỉ pair khác Nil) có recorded_at
// trong [from, to).
func CounterpartyDayRecords(ctx context.Context, tx pgx.Tx, counterparty uuid.UUID, contract string, pair uuid.UUID, from, to time.Time) ([]Record, error) {
	q := recordSelect + ` WHERE counterparty_tenant_id = $1::uuid AND contract = $2::text AND recorded_at >= $3::timestamptz AND recorded_at < $4::timestamptz`
	args := []any{counterparty, contract, from.UTC(), to.UTC()}
	if pair != uuid.Nil {
		q += ` AND pair_id = $5::uuid`
		args = append(args, pair)
	}
	return scanRecords(tx.Query(ctx, q+` ORDER BY recorded_at, id`, args...))
}

// CooperativeExists: Nút htx là HTX có hồ sơ ở pf_coop.cooperative của tenant hiện hành.
func CooperativeExists(ctx context.Context, tx pgx.Tx, htx uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_coop.cooperative WHERE org_ref = $1::uuid)`, htx).Scan(&ok)
	return ok, err
}

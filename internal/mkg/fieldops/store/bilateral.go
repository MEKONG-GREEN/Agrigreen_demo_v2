package store

// Luồng song phương phía MKG (W5.6 — ADR-0034; mkg 000038): hàng gửi bilateral_send, trạng thái + bản sao feed của
// cặp bilateral_feed_state / bilateral_feed_record. Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant MKG.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ValveSource là một bản ghi van đã xuất chưa có trong hàng gửi song phương.
type ValveSource struct {
	ID              uuid.UUID
	Contract        string // collection_schedule | htx_collection_statement
	ContractVersion int
	OrderID         uuid.UUID
	Payload         json.RawMessage
}

// UnsentValveExports trả tối đa limit bản ghi van chưa chiếu sang hàng gửi song phương, theo thứ tự ghi.
func UnsentValveExports(ctx context.Context, tx pgx.Tx, limit int) ([]ValveSource, error) {
	rows, err := tx.Query(ctx, `SELECT v.id, v.contract, v.contract_version, v.order_id, v.payload FROM mkg_fieldops.valve_export v
	   WHERE NOT EXISTS (SELECT 1 FROM mkg_fieldops.bilateral_send s WHERE s.tenant_id = v.tenant_id AND s.source_id = v.id)
	   ORDER BY v.recorded_at, v.id LIMIT $1::int`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ValveSource
	for rows.Next() {
		var v ValveSource
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Contract, &v.ContractVersion, &v.OrderID, &raw); err != nil {
			return nil, err
		}
		v.Payload = raw
		out = append(out, v)
	}
	return out, rows.Err()
}

// BilateralSend là một dòng hàng gửi.
type BilateralSend struct {
	SourceID        uuid.UUID
	Contract        string
	ContractVersion int
	EntityKey       string
	BusinessKey     string
	HTXRef          uuid.UUID
	Record          json.RawMessage
	PayloadSHA256   string
	Status          string
	BatchRef        *uuid.UUID
	BatchID         *uuid.UUID
	ItemIndex       *int
	ProblemCode     *string
}

// InsertBilateralSend thêm một dòng pending; trùng nguồn hoặc trùng khóa nghiệp vụ → bỏ qua (trả false).
func InsertBilateralSend(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r BilateralSend) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.bilateral_send (tenant_id, source_id, contract, contract_version, entity_key, business_key,
	    htx_ref, record, payload_sha256, status)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::int, $5::text, $6::text, $7::uuid, $8::jsonb, $9::text, 'pending')
	  ON CONFLICT DO NOTHING`, tid, r.SourceID, r.Contract, r.ContractVersion, r.EntityKey, r.BusinessKey, r.HTXRef, string(r.Record), r.PayloadSHA256)
	return tag.RowsAffected() == 1, err
}

// MarkValveSkipped ghi nguồn van không chiếu được (hợp đồng lạ / bản ghi lệch) dưới dạng dòng rejected để không quét lại.
// Dùng khóa nghiệp vụ "skip:<source>" (không bao giờ gửi).
func MarkValveSkipped(ctx context.Context, tx pgx.Tx, tid uuid.UUID, src ValveSource, contract, code string) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.bilateral_send (tenant_id, source_id, contract, contract_version, entity_key, business_key,
	    htx_ref, record, payload_sha256, status, batch_ref, item_index, problem_code)
	  VALUES ($1::uuid, $2::uuid, $3::text, 1, $4::text, $4::text, $5::uuid, '{}'::jsonb, repeat('0', 64), 'rejected', $2::uuid, 0, $6::text)
	  ON CONFLICT DO NOTHING`, tid, src.ID, contract, "skip:"+src.ID.String(), uuid.Nil, code)
	return err
}

// RequeuePairMissing đưa dòng bị từ chối integration.pair_missing cũ hơn before về pending (cặp có thể lập muộn).
func RequeuePairMissing(ctx context.Context, tx pgx.Tx, before time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, `UPDATE mkg_fieldops.bilateral_send SET status = 'pending', batch_ref = NULL, batch_id = NULL, item_index = NULL,
	    problem_code = NULL, updated_at = now()
	  WHERE status = 'rejected' AND problem_code = 'integration.pair_missing' AND updated_at < $1::timestamptz`, before.UTC())
	return tag.RowsAffected(), err
}

const sendSelect = `SELECT source_id, contract, contract_version, entity_key, business_key, htx_ref, record, payload_sha256, status,
    batch_ref, batch_id, item_index, problem_code FROM mkg_fieldops.bilateral_send`

func scanSends(rows pgx.Rows, err error) ([]BilateralSend, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BilateralSend
	for rows.Next() {
		var r BilateralSend
		var raw []byte
		if err := rows.Scan(&r.SourceID, &r.Contract, &r.ContractVersion, &r.EntityKey, &r.BusinessKey, &r.HTXRef, &raw, &r.PayloadSHA256,
			&r.Status, &r.BatchRef, &r.BatchID, &r.ItemIndex, &r.ProblemCode); err != nil {
			return nil, err
		}
		r.Record = raw
		out = append(out, r)
	}
	return out, rows.Err()
}

// PendingSends trả tối đa limit dòng pending của hợp đồng, theo thứ tự đưa vào.
func PendingSends(ctx context.Context, tx pgx.Tx, contract string, limit int) ([]BilateralSend, error) {
	return scanSends(tx.Query(ctx, sendSelect+` WHERE status = 'pending' AND contract = $1::text ORDER BY staged_at, source_id LIMIT $2::int`,
		contract, limit))
}

// UnackedSends trả các dòng đã gán lô nhưng chưa có mã lô của cổng (gửi lại cùng batch_ref), theo lô rồi chỉ số.
func UnackedSends(ctx context.Context, tx pgx.Tx) ([]BilateralSend, error) {
	return scanSends(tx.Query(ctx, sendSelect+` WHERE status = 'submitted' AND batch_id IS NULL ORDER BY batch_ref, item_index`))
}

// MarkSubmitted gán lô batchRef (chỉ số theo thứ tự ids) cho các dòng pending — TRƯỚC khi gọi cổng.
func MarkSubmitted(ctx context.Context, tx pgx.Tx, batchRef uuid.UUID, ids []uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_fieldops.bilateral_send s SET status = 'submitted', batch_ref = $1::uuid, item_index = u.i - 1,
	    attempts = s.attempts + 1, updated_at = now()
	  FROM unnest($2::uuid[]) WITH ORDINALITY AS u(id, i)
	  WHERE s.source_id = u.id AND s.status = 'pending'`, batchRef, uuidTexts(ids))
	return err
}

// SetBatchID ghi mã lô cổng trả (202) cho các dòng của batchRef.
func SetBatchID(ctx context.Context, tx pgx.Tx, batchRef, batchID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_fieldops.bilateral_send SET batch_id = $2::uuid, updated_at = now()
	  WHERE batch_ref = $1::uuid AND status = 'submitted'`, batchRef, batchID)
	return err
}

// ResetBatch đưa các dòng đã gán của batchRef về pending (cổng từ chối cả lô, hoặc lô vào DLQ) và ghi mã lỗi.
func ResetBatch(ctx context.Context, tx pgx.Tx, batchRef uuid.UUID, code string) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_fieldops.bilateral_send SET status = 'pending', batch_ref = NULL, batch_id = NULL, item_index = NULL,
	    problem_code = nullif($2::text, ''), updated_at = now()
	  WHERE batch_ref = $1::uuid AND status = 'submitted'`, batchRef, code)
	return err
}

// OpenBatches trả (batch_id, batch_ref) của các lô đã gửi còn chờ kết quả.
func OpenBatches(ctx context.Context, tx pgx.Tx) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT batch_id, batch_ref FROM mkg_fieldops.bilateral_send WHERE status = 'submitted' AND batch_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var id, ref uuid.UUID
		if err := rows.Scan(&id, &ref); err != nil {
			return nil, err
		}
		out[id] = ref
	}
	return out, rows.Err()
}

// ApplyBatchResult ghi kết quả một bản ghi của lô.
func ApplyBatchResult(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, index int, outcome, code string) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_fieldops.bilateral_send SET status = $3::text, problem_code = nullif($4::text, ''), updated_at = now()
	  WHERE batch_id = $1::uuid AND item_index = $2::int AND status = 'submitted'`, batchID, index, outcome, code)
	return err
}

// SendCounts đếm hàng gửi theo trạng thái.
func SendCounts(ctx context.Context, tx pgx.Tx) (map[string]int, error) {
	rows, err := tx.Query(ctx, `SELECT status, count(*) FROM mkg_fieldops.bilateral_send GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}

// PairRefs trả mọi htx_ref MKG có bản ghi song phương (đã gửi) hoặc đã theo dõi feed của cặp.
func PairRefs(ctx context.Context, tx pgx.Tx) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT htx_ref FROM mkg_fieldops.bilateral_send WHERE htx_ref <> '00000000-0000-0000-0000-000000000000'::uuid
	  UNION SELECT htx_ref FROM mkg_fieldops.bilateral_feed_state ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// PairFeedState là trạng thái feed của một cặp.
type PairFeedState struct {
	HTXRef       uuid.UUID
	Cursor       *string
	CaughtUp     bool
	AsOf         *time.Time
	TerminatedAt *time.Time
	LastError    *string
}

// GetPairFeedState đọc trạng thái (nil nếu chưa có).
func GetPairFeedState(ctx context.Context, tx pgx.Tx, htx uuid.UUID) (*PairFeedState, error) {
	s := PairFeedState{HTXRef: htx}
	err := tx.QueryRow(ctx, `SELECT feed_cursor, caught_up, as_of, terminated_at, last_error FROM mkg_fieldops.bilateral_feed_state
	  WHERE htx_ref = $1::uuid`, htx).Scan(&s.Cursor, &s.CaughtUp, &s.AsOf, &s.TerminatedAt, &s.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// PutPairFeedState ghi trạng thái.
func PutPairFeedState(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s PairFeedState) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.bilateral_feed_state (tenant_id, htx_ref, feed_cursor, caught_up, as_of, terminated_at,
	    last_pulled_at, last_error, updated_at)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::bool, $5::timestamptz, $6::timestamptz, now(), $7::text, now())
	  ON CONFLICT (tenant_id, htx_ref) DO UPDATE SET feed_cursor = EXCLUDED.feed_cursor, caught_up = EXCLUDED.caught_up, as_of = EXCLUDED.as_of,
	    terminated_at = EXCLUDED.terminated_at, last_pulled_at = now(), last_error = EXCLUDED.last_error, updated_at = now()`,
		tid, s.HTXRef, s.Cursor, s.CaughtUp, s.AsOf, s.TerminatedAt, s.LastError)
	return err
}

// PairFeedRecord là một mục feed của cặp đã áp.
type PairFeedRecord struct {
	HTXRef          uuid.UUID
	Seq             int64
	EventID         uuid.UUID
	Contract        string
	ContractVersion int
	EntityKey       string
	BusinessKey     string
	Record          json.RawMessage
	PayloadSHA256   string
	RecordedAt      time.Time
}

// InsertPairFeedRecord áp một mục (áp lại cùng seq / event_id là no-op).
func InsertPairFeedRecord(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r PairFeedRecord) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.bilateral_feed_record (tenant_id, htx_ref, seq, event_id, contract, contract_version, entity_key,
	    business_key, record, payload_sha256, recorded_at)
	  VALUES ($1::uuid, $2::uuid, $3::bigint, $4::uuid, $5::text, $6::int, $7::text, $8::text, $9::jsonb, $10::text, $11::timestamptz)
	  ON CONFLICT DO NOTHING`, tid, r.HTXRef, r.Seq, r.EventID, r.Contract, r.ContractVersion, r.EntityKey, r.BusinessKey, string(r.Record),
		r.PayloadSHA256, r.RecordedAt.UTC())
	return err
}

// PairFeedRecords trả bản sao của hợp đồng (một cặp, hoặc mọi cặp khi htx = Nil) có recorded_at trong [from, to).
func PairFeedRecords(ctx context.Context, tx pgx.Tx, contract string, htx uuid.UUID, from, to time.Time) ([]PairFeedRecord, error) {
	q := `SELECT htx_ref, seq, event_id, contract, contract_version, entity_key, business_key, record, payload_sha256, recorded_at
	  FROM mkg_fieldops.bilateral_feed_record WHERE contract = $1::text AND recorded_at >= $2::timestamptz AND recorded_at < $3::timestamptz`
	args := []any{contract, from.UTC(), to.UTC()}
	if htx != uuid.Nil {
		q += ` AND htx_ref = $4::uuid`
		args = append(args, htx)
	}
	rows, err := tx.Query(ctx, q+` ORDER BY htx_ref, seq`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PairFeedRecord
	for rows.Next() {
		var r PairFeedRecord
		var raw []byte
		if err := rows.Scan(&r.HTXRef, &r.Seq, &r.EventID, &r.Contract, &r.ContractVersion, &r.EntityKey, &r.BusinessKey, &raw, &r.PayloadSHA256,
			&r.RecordedAt); err != nil {
			return nil, err
		}
		r.Record = raw
		out = append(out, r)
	}
	return out, rows.Err()
}

// AcceptedNotInFeed đếm dòng đã được cổng nhận nhưng chưa thấy (cùng khóa nghiệp vụ và băm) trong bản sao feed của cặp.
func AcceptedNotInFeed(ctx context.Context, tx pgx.Tx) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM mkg_fieldops.bilateral_send s
	  WHERE s.status = 'accepted' AND NOT EXISTS (SELECT 1 FROM mkg_fieldops.bilateral_feed_record r
	    WHERE r.tenant_id = s.tenant_id AND r.htx_ref = s.htx_ref AND r.contract = s.contract AND r.contract_version = s.contract_version
	      AND r.business_key = s.business_key AND r.payload_sha256 = s.payload_sha256)`).Scan(&n)
	return n, err
}

func uuidTexts(xs []uuid.UUID) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.String()
	}
	return out
}

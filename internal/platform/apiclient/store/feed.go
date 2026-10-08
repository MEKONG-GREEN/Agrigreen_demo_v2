package store

// Kho feed và tập dữ liệu dựng sẵn (pf 000126–000127, W5.3). Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant đơn vị
// vận hành.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FeedHead là đầu một feed × phân vùng.
type FeedHead struct {
	LastSeq        int64
	PurgedThrough  int64
	LastRecordedAt *time.Time
}

// LockFeedHead tạo (nếu chưa có) và khóa dòng đầu feed — người ghi cùng feed xếp hàng, seq cấp theo thứ tự commit.
func LockFeedHead(ctx context.Context, tx pgx.Tx, tid uuid.UUID, feed, partition string) (FeedHead, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.feed_head (tenant_id, feed, partition_key) VALUES ($1::uuid, $2::text, $3::text)
	  ON CONFLICT (tenant_id, feed, partition_key) DO NOTHING`, tid, feed, partition); err != nil {
		return FeedHead{}, err
	}
	var h FeedHead
	err := tx.QueryRow(ctx, `SELECT last_seq, purged_through, last_recorded_at FROM pf_apiclient.feed_head
	  WHERE tenant_id = $1::uuid AND feed = $2::text AND partition_key = $3::text FOR UPDATE`, tid, feed, partition).
		Scan(&h.LastSeq, &h.PurgedThrough, &h.LastRecordedAt)
	return h, err
}

// GetFeedHead đọc đầu feed (nil nếu feed chưa có mục nào).
func GetFeedHead(ctx context.Context, tx pgx.Tx, feed, partition string) (*FeedHead, error) {
	var h FeedHead
	err := tx.QueryRow(ctx, `SELECT last_seq, purged_through, last_recorded_at FROM pf_apiclient.feed_head
	  WHERE feed = $1::text AND partition_key = $2::text`, feed, partition).Scan(&h.LastSeq, &h.PurgedThrough, &h.LastRecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &h, err
}

// AdvanceFeedHead ghi seq cuối sau khi nối mục.
func AdvanceFeedHead(ctx context.Context, tx pgx.Tx, feed, partition string, lastSeq int64, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_apiclient.feed_head SET last_seq = $3::bigint, last_recorded_at = $4::timestamptz, updated_at = now()
	  WHERE feed = $1::text AND partition_key = $2::text`, feed, partition, lastSeq, at.UTC())
	return err
}

// FeedEntry là một mục feed.
type FeedEntry struct {
	Feed            string
	Partition       string
	Seq             int64
	EventID         uuid.UUID
	EntityKey       string
	Op              string // upsert | retract
	ContractVersion int
	DatasetVersion  *int64
	Record          json.RawMessage // nil khi retract
	RecordedAt      time.Time
}

// InsertFeedEntries nối các mục (seq đã cấp) — một câu INSERT … unnest mỗi lô 1000 (COPY FROM không chạy được
// trên bảng có RLS).
func InsertFeedEntries(ctx context.Context, tx pgx.Tx, tid uuid.UUID, es []FeedEntry) error {
	for len(es) > 0 {
		n := min(len(es), 1000)
		chunk := es[:n]
		es = es[n:]
		var (
			feeds, parts, keys, ops []string
			seqs                    []int64
			evs                     []string
			cvs                     []int32
			dvs                     []*int64
			recs                    []*string
			ats                     []time.Time
		)
		for _, e := range chunk {
			var rec *string
			if e.Record != nil {
				s := string(e.Record)
				rec = &s
			}
			feeds, parts, keys, ops = append(feeds, e.Feed), append(parts, e.Partition), append(keys, e.EntityKey), append(ops, e.Op)
			seqs, evs, cvs = append(seqs, e.Seq), append(evs, e.EventID.String()), append(cvs, int32(e.ContractVersion)) //nolint:gosec // phiên bản hợp đồng nhỏ
			dvs, recs, ats = append(dvs, e.DatasetVersion), append(recs, rec), append(ats, e.RecordedAt.UTC())
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.feed_entry
		    (tenant_id, feed, partition_key, seq, event_id, entity_key, op, contract_version, dataset_version, record, recorded_at)
		  SELECT $1::uuid, f, p, s, ev, k, o, cv, dv, r::jsonb, at
		  FROM unnest($2::text[], $3::text[], $4::bigint[], $5::uuid[], $6::text[], $7::text[], $8::int[], $9::bigint[], $10::text[], $11::timestamptz[])
		    AS u(f, p, s, ev, k, o, cv, dv, r, at)`,
			tid, feeds, parts, seqs, evs, keys, ops, cvs, dvs, recs, ats); err != nil {
			return err
		}
	}
	return nil
}

// FeedEntriesAfter trả tối đa limit mục có seq > after của phiên bản hợp đồng, theo seq.
func FeedEntriesAfter(ctx context.Context, tx pgx.Tx, feed, partition string, contractVersion int, after int64, limit int) ([]FeedEntry, error) {
	rows, err := tx.Query(ctx, `SELECT seq, event_id, entity_key, op, contract_version, dataset_version, record, recorded_at
	  FROM pf_apiclient.feed_entry
	  WHERE feed = $1::text AND partition_key = $2::text AND seq > $3::bigint AND contract_version = $4::int
	  ORDER BY seq LIMIT $5::int`, feed, partition, after, contractVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeedEntry
	for rows.Next() {
		e := FeedEntry{Feed: feed, Partition: partition}
		var rec []byte
		if err := rows.Scan(&e.Seq, &e.EventID, &e.EntityKey, &e.Op, &e.ContractVersion, &e.DatasetVersion, &rec, &e.RecordedAt); err != nil {
			return nil, err
		}
		if rec != nil {
			e.Record = rec
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeExpiredFeedEntries gọi pf_apiclient.purge_feed_entries (mục quá 30 ngày của tenant hiện hành) một lô.
func PurgeExpiredFeedEntries(ctx context.Context, tx pgx.Tx, limit int) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, `SELECT pf_apiclient.purge_feed_entries($1::int)`, limit).Scan(&n)
	return n, err
}

// DatasetRecord là trạng thái đã phát của một entity_key (Record nil = đã retract).
type DatasetRecord struct {
	EntityKey string
	SHA256    string // rỗng khi đã retract
	Record    json.RawMessage
}

// DatasetRecords trả trạng thái đã phát của tập × phân vùng (cả dòng đã retract), khóa → bản ghi.
func DatasetRecords(ctx context.Context, tx pgx.Tx, dataset, partition string) (map[string]DatasetRecord, error) {
	rows, err := tx.Query(ctx, `SELECT entity_key, coalesce(record_sha256, ''), record FROM pf_apiclient.dataset_record
	  WHERE dataset = $1::text AND partition_key = $2::text`, dataset, partition)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DatasetRecord{}
	for rows.Next() {
		var r DatasetRecord
		var rec []byte
		if err := rows.Scan(&r.EntityKey, &r.SHA256, &rec); err != nil {
			return nil, err
		}
		if rec != nil {
			r.Record = rec
		}
		out[r.EntityKey] = r
	}
	return out, rows.Err()
}

// PutDatasetRecord ghi trạng thái đã phát (record nil → retract: record + băm NULL).
func PutDatasetRecord(ctx context.Context, tx pgx.Tx, tid uuid.UUID, dataset, partition, key string, record json.RawMessage, sha string, version *int64) error {
	var rec, s any
	if record != nil {
		rec, s = string(record), sha
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.dataset_record (tenant_id, dataset, partition_key, entity_key, record, record_sha256, version)
	  VALUES ($1::uuid, $2::text, $3::text, $4::text, $5::jsonb, $6::text, $7::bigint)
	  ON CONFLICT (tenant_id, dataset, partition_key, entity_key) DO UPDATE
	  SET record = EXCLUDED.record, record_sha256 = EXCLUDED.record_sha256, version = EXCLUDED.version, updated_at = now()`,
		tid, dataset, partition, key, rec, s, version)
	return err
}

// CellStates trả khóa → đã ẩn (dataset_cell_state) của tập.
func CellStates(ctx context.Context, tx pgx.Tx, dataset string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT entity_key, suppressed FROM pf_apiclient.dataset_cell_state WHERE dataset = $1::text`, dataset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		var s bool
		if err := rows.Scan(&k, &s); err != nil {
			return nil, err
		}
		out[k] = s
	}
	return out, rows.Err()
}

// PutCellState ghi trạng thái ẩn của một ô (reason rỗng = được phát). since_version chỉ đổi khi trạng thái đổi.
func PutCellState(ctx context.Context, tx pgx.Tx, tid uuid.UUID, dataset, key string, suppressed bool, reason string, version int64) error {
	var r any
	if reason != "" {
		r = reason
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.dataset_cell_state (tenant_id, dataset, entity_key, suppressed, reason, since_version)
	  VALUES ($1::uuid, $2::text, $3::text, $4::boolean, $5::text, $6::bigint)
	  ON CONFLICT (tenant_id, dataset, entity_key) DO UPDATE
	  SET since_version = CASE WHEN pf_apiclient.dataset_cell_state.suppressed = EXCLUDED.suppressed
	                           THEN pf_apiclient.dataset_cell_state.since_version ELSE EXCLUDED.since_version END,
	      suppressed = EXCLUDED.suppressed, reason = EXCLUDED.reason, updated_at = now()`,
		tid, dataset, key, suppressed, r, version)
	return err
}

// DatasetVersion là một phiên bản đã phát (bất biến).
type DatasetVersion struct {
	Dataset         string
	Version         int64
	ContractVersion int
	BuiltAt         time.Time
	AsOf            time.Time
	AdminUnitAsOf   *time.Time
	KUsed           *int
	Hysteresis      *int
	Contributors    *int
	RecordCount     int64
	SHA256          string
	SnapshotKey     *string
	SnapshotSize    *int64
	FirstSeq        *int64
	LastSeq         *int64
	Upserts         int
	Retracts        int
	Suppressed      *int
	RuleVersion     string
}

const versionCols = `dataset, version, contract_version, built_at, as_of, admin_unit_as_of, k_used, hysteresis, contributors, record_count,
  sha256, snapshot_key, snapshot_size, first_seq, last_seq, upserts, retracts, suppressed, rule_version`

func scanVersion(row pgx.Row) (*DatasetVersion, error) {
	var v DatasetVersion
	err := row.Scan(&v.Dataset, &v.Version, &v.ContractVersion, &v.BuiltAt, &v.AsOf, &v.AdminUnitAsOf, &v.KUsed, &v.Hysteresis, &v.Contributors,
		&v.RecordCount, &v.SHA256, &v.SnapshotKey, &v.SnapshotSize, &v.FirstSeq, &v.LastSeq, &v.Upserts, &v.Retracts, &v.Suppressed, &v.RuleVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// InsertDatasetVersion ghi một phiên bản (bất biến).
func InsertDatasetVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v DatasetVersion) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.dataset_version (tenant_id, `+versionCols+`)
	  VALUES ($1::uuid, $2::text, $3::bigint, $4::int, $5::timestamptz, $6::timestamptz, $7::date, $8::int, $9::int, $10::int, $11::bigint,
	    $12::text, $13::text, $14::bigint, $15::bigint, $16::bigint, $17::int, $18::int, $19::int, $20::text)`,
		tid, v.Dataset, v.Version, v.ContractVersion, v.BuiltAt.UTC(), v.AsOf.UTC(), v.AdminUnitAsOf, v.KUsed, v.Hysteresis, v.Contributors,
		v.RecordCount, v.SHA256, v.SnapshotKey, v.SnapshotSize, v.FirstSeq, v.LastSeq, v.Upserts, v.Retracts, v.Suppressed, v.RuleVersion)
	return err
}

// LatestDatasetVersion trả phiên bản mới nhất của tập (nil nếu chưa có).
func LatestDatasetVersion(ctx context.Context, tx pgx.Tx, dataset string) (*DatasetVersion, error) {
	return scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_apiclient.dataset_version WHERE dataset = $1::text
	  ORDER BY version DESC LIMIT 1`, dataset))
}

// DatasetVersionByNo trả một phiên bản (nil nếu không có).
func DatasetVersionByNo(ctx context.Context, tx pgx.Tx, dataset string, version int64) (*DatasetVersion, error) {
	return scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_apiclient.dataset_version WHERE dataset = $1::text AND version = $2::bigint`,
		dataset, version))
}

// AdminUnitRow là một phiên bản đơn vị hành chính của bảng chiếu lõi k_ref.admin_unit (W2.5).
type AdminUnitRow struct {
	Code           string
	ValidFrom      time.Time
	ValidTo        *time.Time
	Name           string
	Level          string
	ParentCode     *string
	SuccessorCodes []string
}

// AdminUnitRows trả mọi phiên bản đơn vị hành chính của tenant hiện hành (k_ref đọc được bởi mọi r_*).
func AdminUnitRows(ctx context.Context, tx pgx.Tx) ([]AdminUnitRow, error) {
	rows, err := tx.Query(ctx, `SELECT code, valid_from, valid_to, name, level, parent_code, successor_codes
	  FROM k_ref.admin_unit ORDER BY code, valid_from`) // RLS: tenant hiện hành
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminUnitRow
	for rows.Next() {
		var u AdminUnitRow
		if err := rows.Scan(&u.Code, &u.ValidFrom, &u.ValidTo, &u.Name, &u.Level, &u.ParentCode, &u.SuccessorCodes); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

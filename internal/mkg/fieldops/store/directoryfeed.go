package store

// Bản sao danh bạ HTX/thửa dựng từ feed của cổng tích hợp (W5.5, mkg 000036).

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FeedState là một dòng directory_feed_state.
type FeedState struct {
	Feed            string
	Cursor          string // rỗng = chưa đọc / đọc lại từ đầu
	ContractVersion int
	CaughtUp        bool
	AsOf            *time.Time
	MaxAge          time.Duration // 0 = chưa biết
	RevokedAt       *time.Time
	LastPulledAt    *time.Time
	LastError       string
}

// FeedStates đọc trạng thái mọi feed danh bạ của tenant hiện hành (feed → trạng thái).
func FeedStates(ctx context.Context, tx pgx.Tx) (map[string]FeedState, error) {
	rows, err := tx.Query(ctx, `SELECT feed, coalesce(feed_cursor, ''), coalesce(contract_version, 0), caught_up, as_of,
	    coalesce(max_age_seconds, 0), revoked_at, last_pulled_at, coalesce(last_error, '')
	  FROM mkg_fieldops.directory_feed_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FeedState{}
	for rows.Next() {
		var s FeedState
		var secs int64
		if err := rows.Scan(&s.Feed, &s.Cursor, &s.ContractVersion, &s.CaughtUp, &s.AsOf, &secs, &s.RevokedAt, &s.LastPulledAt, &s.LastError); err != nil {
			return nil, err
		}
		s.MaxAge = time.Duration(secs) * time.Second
		out[s.Feed] = s
	}
	return out, rows.Err()
}

// PutFeedState ghi trạng thái một feed.
func PutFeedState(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s FeedState) error {
	var cur, lastErr *string
	if s.Cursor != "" {
		cur = &s.Cursor
	}
	if s.LastError != "" {
		e := s.LastError
		if len(e) > 300 {
			e = e[:300]
		}
		lastErr = &e
	}
	var cv, maxAge *int64
	if s.ContractVersion > 0 {
		v := int64(s.ContractVersion)
		cv = &v
	}
	if s.MaxAge > 0 {
		v := int64(s.MaxAge / time.Second)
		maxAge = &v
	}
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.directory_feed_state AS c
	    (tenant_id, feed, feed_cursor, contract_version, caught_up, as_of, max_age_seconds, revoked_at, last_pulled_at, last_error)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	  ON CONFLICT (tenant_id, feed) DO UPDATE SET feed_cursor = EXCLUDED.feed_cursor, contract_version = EXCLUDED.contract_version,
	    caught_up = EXCLUDED.caught_up, as_of = EXCLUDED.as_of, max_age_seconds = EXCLUDED.max_age_seconds, revoked_at = EXCLUDED.revoked_at,
	    last_pulled_at = EXCLUDED.last_pulled_at, last_error = EXCLUDED.last_error, updated_at = now()`,
		tid, s.Feed, cur, cv, s.CaughtUp, utcPtr(s.AsOf), maxAge, utcPtr(s.RevokedAt), utcPtr(s.LastPulledAt), lastErr)
	return err
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// PutFeedRecord ghi trạng thái hiện hành của một ref từ mục upsert seq; mục cũ hơn (seq ≤ seq đã áp) là no-op.
func PutFeedRecord(ctx context.Context, tx pgx.Tx, tid uuid.UUID, feed string, ref uuid.UUID, seq int64, eventID uuid.UUID,
	record json.RawMessage, recordedAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.directory_feed_record AS c (tenant_id, feed, ref, seq, event_id, record, recorded_at)
	  VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
	  ON CONFLICT (tenant_id, feed, ref) DO UPDATE SET seq = EXCLUDED.seq, event_id = EXCLUDED.event_id, record = EXCLUDED.record,
	    recorded_at = EXCLUDED.recorded_at, applied_at = now()
	  WHERE c.seq < EXCLUDED.seq`, tid, feed, ref, seq, eventID, string(record), recordedAt.UTC())
	return err
}

// ClearFeedRecord áp mục retract seq: xóa dòng của ref nếu dòng đó cũ hơn seq.
func ClearFeedRecord(ctx context.Context, tx pgx.Tx, feed string, ref uuid.UUID, seq int64) error {
	_, err := tx.Exec(ctx, `DELETE FROM mkg_fieldops.directory_feed_record WHERE feed = $1 AND ref = $2 AND seq < $3`, feed, ref, seq)
	return err
}

// ClearFeedRecords xóa mọi dòng của feed (thu hồi cấp quyền / đồng bộ lại sau cursor quá hạn). Trả số dòng đã xóa.
func ClearFeedRecords(ctx context.Context, tx pgx.Tx, feed string) (int64, error) {
	tag, err := tx.Exec(ctx, `DELETE FROM mkg_fieldops.directory_feed_record WHERE feed = $1`, feed)
	return tag.RowsAffected(), err
}

// FeedRecords đọc bản ghi của các ref trong feed (ref vắng = feed không có). refs nil → mọi dòng của feed.
func FeedRecords(ctx context.Context, tx pgx.Tx, feed string, refs []uuid.UUID) (map[uuid.UUID]json.RawMessage, error) {
	q := `SELECT ref, record FROM mkg_fieldops.directory_feed_record WHERE feed = $1`
	args := []any{feed}
	if refs != nil {
		q += ` AND ref = ANY($2::uuid[])`
		args = append(args, uuidStrings(refs))
	}
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]json.RawMessage{}
	for rows.Next() {
		var ref uuid.UUID
		var rec []byte
		if err := rows.Scan(&ref, &rec); err != nil {
			return nil, err
		}
		out[ref] = rec
	}
	return out, rows.Err()
}

// AllReplicaHTX đọc mọi dòng bản sao HTX cũ (mkg 000033) — đối soát hai nguồn (job fieldops_directory_feed).
func AllReplicaHTX(ctx context.Context, tx pgx.Tx) ([]ReplicaHTX, error) {
	rows, err := tx.Query(ctx, `SELECT htx_ref, present, coalesce(commune_code, ''), active, as_of FROM mkg_fieldops.directory_htx_replica ORDER BY htx_ref`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReplicaHTX, error) {
		var h ReplicaHTX
		err := r.Scan(&h.Ref, &h.Present, &h.CommuneCode, &h.Active, &h.AsOf)
		return h, err
	})
}

// AllReplicaPlots đọc mọi dòng bản sao thửa cũ (mkg 000033).
func AllReplicaPlots(ctx context.Context, tx pgx.Tx) ([]ReplicaPlot, error) {
	rows, err := tx.Query(ctx, `SELECT plot_ref, present, cooperative_ref, coalesce(commune_code, ''), coalesce(status, ''), as_of
	  FROM mkg_fieldops.directory_plot_replica ORDER BY plot_ref`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReplicaPlot, error) {
		var p ReplicaPlot
		err := r.Scan(&p.Ref, &p.Present, &p.CooperativeRef, &p.CommuneCode, &p.Status, &p.AsOf)
		return p, err
	})
}

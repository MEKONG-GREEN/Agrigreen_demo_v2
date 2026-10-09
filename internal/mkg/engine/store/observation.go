// Package store là SQL của mkg/engine (schema mkg_engine, mkg 000037). Mọi hàm chạy trong giao dịch tenant
// (RLS) dưới vai r_engine.
package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/port"
)

// FeedState là một dòng observation_feed_state.
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

// FeedStates đọc trạng thái mọi feed của tenant hiện hành (feed → trạng thái).
func FeedStates(ctx context.Context, tx pgx.Tx) (map[string]FeedState, error) {
	rows, err := tx.Query(ctx, `SELECT feed, coalesce(feed_cursor, ''), coalesce(contract_version, 0), caught_up, as_of,
	    coalesce(max_age_seconds, 0), revoked_at, last_pulled_at, coalesce(last_error, '')
	  FROM mkg_engine.observation_feed_state`)
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

// PutFeedState ghi trạng thái một feed (upsert).
func PutFeedState(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s FeedState) error {
	var cur, lastErr *string
	if s.Cursor != "" {
		cur = &s.Cursor
	}
	if s.LastError != "" {
		e := []rune(s.LastError)
		if len(e) > 300 {
			e = e[:300]
		}
		v := string(e)
		lastErr = &v
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
	_, err := tx.Exec(ctx, `INSERT INTO mkg_engine.observation_feed_state AS c
	    (tenant_id, feed, feed_cursor, contract_version, caught_up, as_of, max_age_seconds, revoked_at, last_pulled_at, last_error)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	  ON CONFLICT (tenant_id, feed) DO UPDATE SET feed_cursor = EXCLUDED.feed_cursor, contract_version = EXCLUDED.contract_version,
	    caught_up = EXCLUDED.caught_up, as_of = EXCLUDED.as_of, max_age_seconds = EXCLUDED.max_age_seconds, revoked_at = EXCLUDED.revoked_at,
	    last_pulled_at = EXCLUDED.last_pulled_at, last_error = EXCLUDED.last_error, updated_at = now()`,
		tid, s.Feed, cur, cv, s.CaughtUp, utcPtr(s.AsOf), maxAge, utcPtr(s.RevokedAt), utcPtr(s.LastPulledAt), lastErr)
	return err
}

// Row là một quan sát cần ghi.
type Row struct {
	ID              uuid.UUID
	StreamKey       string
	IdemKey         string
	Kind            string
	EntityKey       string
	Source          string
	Feed            string
	ContractVersion int
	FeedSeq         int64
	EventID         uuid.UUID
	FeedCursor      string
	AsOf            time.Time
	MaxAge          time.Duration
	Retracted       bool
	Value           *decimal.Decimal
	Unit            string
	Uncertainty     *decimal.Decimal
	Basis           []string
	Payload         json.RawMessage // nil → JSON null
	MapperVersion   string
}

// ExistingIdem trả các idem_key đã có trong sổ (tenant hiện hành) — áp lại mục đã ghi là no-op.
func ExistingIdem(ctx context.Context, tx pgx.Tx, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT idem_key FROM mkg_engine.observation WHERE idem_key = ANY($1::text[])`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// Insert ghi một quan sát vào sổ (seq, prev_hash, row_hash, recorded_at do trigger đặt).
func Insert(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r Row) error {
	payload := []byte("null")
	if len(r.Payload) > 0 {
		payload = r.Payload
	}
	basis := r.Basis
	if basis == nil {
		basis = []string{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO mkg_engine.observation
	    (id, tenant_id, stream_key, seq, idem_key, kind, entity_key, source, feed, contract_version, feed_seq, event_id, feed_cursor,
	     as_of, max_age_seconds, retracted, value, unit, uncertainty, uncertainty_basis, payload, mapper_version, prev_hash, row_hash)
	  VALUES ($1, $2, $3, 0, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::numeric, $17, $18::numeric, $19::text[], $20::jsonb, $21,
	     '\x00'::bytea, '\x00'::bytea)`,
		r.ID, tid, r.StreamKey, r.IdemKey, r.Kind, r.EntityKey, r.Source, nilStr(r.Feed), nilInt(int64(r.ContractVersion)),
		nilInt(r.FeedSeq), nilUUID(r.EventID), nilStr(r.FeedCursor), r.AsOf.UTC(), int64(r.MaxAge/time.Second), r.Retracted,
		decStr(r.Value), nilStr(r.Unit), decStr(r.Uncertainty), basis, string(payload), r.MapperVersion)
	return err
}

// Entity là cặp (kind, entity_key).
type Entity struct {
	Kind      string
	EntityKey string
}

// LiveFromFeed trả các entity mà quan sát MỚI NHẤT (mọi nguồn) đến từ feed và chưa rút — dùng khi bên cấp thu hồi
// cấp quyền (ghi quan sát rút cho từng entity).
func LiveFromFeed(ctx context.Context, tx pgx.Tx, feed string) ([]Entity, error) {
	rows, err := tx.Query(ctx, `SELECT kind, entity_key FROM (
	    SELECT DISTINCT ON (o.kind, o.entity_key) o.kind, o.entity_key, o.feed, o.retracted
	      FROM mkg_engine.observation o
	     WHERE o.kind IN (SELECT DISTINCT kind FROM mkg_engine.observation WHERE feed = $1)
	     ORDER BY o.kind, o.entity_key, o.as_of DESC, o.recorded_at DESC, o.seq DESC) l
	  WHERE l.feed = $1 AND NOT l.retracted
	  ORDER BY kind, entity_key`, feed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.Kind, &e.EntityKey); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Latest đọc quan sát mới nhất mỗi entity của kind (keys nil = mọi entity), kèm as_of của lần đọc tới đầu feed nguồn
// (nil khi feed chưa đọc tới đầu, đã thu hồi, hoặc quan sát không đến từ feed).
func Latest(ctx context.Context, tx pgx.Tx, kind string, keys []string) ([]port.Observation, []*time.Time, error) {
	var keyArg any
	if keys != nil {
		keyArg = keys
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (o.entity_key) o.id, o.kind, o.entity_key, o.source, coalesce(o.feed, ''),
	    coalesce(o.contract_version, 0), coalesce(o.feed_seq, 0), o.event_id, coalesce(o.feed_cursor, ''), o.as_of, o.max_age_seconds,
	    o.retracted, o.value::text, coalesce(o.unit, ''), o.uncertainty::text, o.uncertainty_basis, o.payload::text, o.mapper_version,
	    o.recorded_at, CASE WHEN s.caught_up AND s.revoked_at IS NULL THEN s.as_of END
	  FROM mkg_engine.observation o
	  LEFT JOIN mkg_engine.observation_feed_state s ON s.tenant_id = o.tenant_id AND s.feed = o.feed
	  WHERE o.kind = $1 AND ($2::text[] IS NULL OR o.entity_key = ANY($2::text[]))
	  ORDER BY o.entity_key, o.as_of DESC, o.recorded_at DESC, o.seq DESC`, kind, keyArg)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []port.Observation
	var feedAsOf []*time.Time
	for rows.Next() {
		var o port.Observation
		var ev *uuid.UUID
		var secs int64
		var val, unc *string
		var payload string
		var fa *time.Time
		if err := rows.Scan(&o.ID, &o.Kind, &o.EntityKey, &o.Source, &o.Feed, &o.ContractVersion, &o.FeedSeq, &ev, &o.FeedCursor,
			&o.AsOf, &secs, &o.Retracted, &val, &o.Unit, &unc, &o.UncertaintyBasis, &payload, &o.MapperVersion, &o.RecordedAt, &fa); err != nil {
			return nil, nil, err
		}
		if ev != nil {
			o.EventID = *ev
		}
		o.MaxAge = time.Duration(secs) * time.Second
		if o.Value, err = parseDec(val); err != nil {
			return nil, nil, err
		}
		if o.Uncertainty, err = parseDec(unc); err != nil {
			return nil, nil, err
		}
		if payload != "null" {
			o.Payload = json.RawMessage(payload)
		}
		o.AsOf, o.RecordedAt = o.AsOf.UTC(), o.RecordedAt.UTC()
		out = append(out, o)
		feedAsOf = append(feedAsOf, utcPtr(fa))
	}
	return out, feedAsOf, rows.Err()
}

func parseDec(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func decStr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	// Giữ số chữ số lẻ của nguồn ("812.40" không thành "812.4"): numeric giữ scale của chuỗi nhập.
	s := d.String()
	if e := d.Exponent(); e < 0 {
		s = d.StringFixed(-e)
	}
	return &s
}

func nilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nilInt(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}

func nilUUID(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

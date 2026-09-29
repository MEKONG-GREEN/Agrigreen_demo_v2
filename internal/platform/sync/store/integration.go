package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// IntegrationFailure là một dòng pf_sync.integration_failure.
type IntegrationFailure struct {
	ID            uuid.UUID
	Consumer      string
	EventID       uuid.UUID
	EventType     string
	ErrorCode     string
	DeliveryCount int
	DeadLettered  bool
	EventTime     time.Time
	RecordedAt    time.Time
}

// InsertIntegrationFailure ghi một lần thất bại. Dòng dead_lettered trùng (sự kiện,
// consumer) bị bỏ qua (chỉ mục duy nhất một phần) → inserted = false.
func InsertIntegrationFailure(ctx context.Context, tx pgx.Tx, tid uuid.UUID, f *IntegrationFailure) (bool, error) {
	err := tx.QueryRow(ctx, `INSERT INTO pf_sync.integration_failure
	    (tenant_id, id, consumer, event_id, event_type, error_code, delivery_count, dead_lettered, event_time)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	  ON CONFLICT (tenant_id, event_id, consumer) WHERE dead_lettered DO NOTHING
	  RETURNING recorded_at`,
		tid, f.ID, f.Consumer, f.EventID, f.EventType, f.ErrorCode, f.DeliveryCount, f.DeadLettered, f.EventTime.UTC()).Scan(&f.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ConsumerOK là tóm tắt lần xử lý thành công của một consumer (shared.inbox).
type ConsumerOK struct {
	Consumer      string
	Processed     int64 // trong cửa sổ
	LastProcessed *time.Time
	LastEventType *string
}

// ConsumerErr là tóm tắt lần thất bại của một consumer trong cửa sổ.
type ConsumerErr struct {
	Consumer      string
	Failures      int64
	DeadLettered  int64
	LastFailure   *time.Time
	LastErrorCode *string
}

// InboxSummary gộp shared.inbox theo consumer của tenant hiện tại (RLS): số sự kiện xử
// lý từ since, lần gần nhất (mọi thời điểm) và loại sự kiện gần nhất.
func InboxSummary(ctx context.Context, tx pgx.Tx, since time.Time) ([]ConsumerOK, error) {
	rows, err := tx.Query(ctx, `SELECT consumer, count(*) FILTER (WHERE processed_at >= $1), max(processed_at),
	    (array_agg(event_type ORDER BY processed_at DESC))[1]
	  FROM shared.inbox GROUP BY consumer ORDER BY consumer`, since.UTC())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConsumerOK, error) {
		var c ConsumerOK
		err := r.Scan(&c.Consumer, &c.Processed, &c.LastProcessed, &c.LastEventType)
		return c, err
	})
}

// FailureSummary gộp pf_sync.integration_failure theo consumer từ since.
func FailureSummary(ctx context.Context, tx pgx.Tx, since time.Time) ([]ConsumerErr, error) {
	rows, err := tx.Query(ctx, `SELECT consumer, count(*), count(*) FILTER (WHERE dead_lettered), max(recorded_at),
	    (array_agg(error_code ORDER BY recorded_at DESC))[1]
	  FROM pf_sync.integration_failure WHERE recorded_at >= $1 GROUP BY consumer ORDER BY consumer`, since.UTC())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConsumerErr, error) {
		var c ConsumerErr
		err := r.Scan(&c.Consumer, &c.Failures, &c.DeadLettered, &c.LastFailure, &c.LastErrorCode)
		return c, err
	})
}

// IntegrationLogRow là một dòng nhật ký đồng bộ (thành công từ inbox hoặc thất bại).
type IntegrationLogRow struct {
	At            time.Time
	Consumer      string
	EventType     string
	EventID       uuid.UUID
	Status        string // ok | error
	ErrorCode     *string
	DeliveryCount int
	DeadLettered  bool
}

// LogFilter là bộ lọc nhật ký: consumer, trạng thái, khoảng thời gian [From, To), con trỏ
// (At, EventID) giảm dần.
type LogFilter struct {
	Consumer   string
	Status     string
	From, To   time.Time
	AfterAt    *time.Time
	AfterEvent uuid.UUID
	Limit      int
}

// IntegrationLog trả nhật ký mới nhất trước (GIS US-SYNC-01 AC-1: thời điểm, nguồn, trạng thái, mã lỗi).
func IntegrationLog(ctx context.Context, tx pgx.Tx, f LogFilter) ([]IntegrationLogRow, error) {
	rows, err := tx.Query(ctx, `SELECT at, consumer, event_type, event_id, status, error_code, delivery_count, dead_lettered FROM (
	    SELECT processed_at AS at, consumer, event_type, event_id, 'ok' AS status, NULL::text AS error_code,
	           0 AS delivery_count, false AS dead_lettered
	      FROM shared.inbox
	     WHERE ($1 = '' OR consumer = $1) AND $2 IN ('', 'ok') AND processed_at >= $3 AND processed_at < $4
	    UNION ALL
	    SELECT recorded_at, consumer, event_type, event_id, 'error', error_code, delivery_count, dead_lettered
	      FROM pf_sync.integration_failure
	     WHERE ($1 = '' OR consumer = $1) AND $2 IN ('', 'error') AND recorded_at >= $3 AND recorded_at < $4
	  ) x
	  WHERE $5::timestamptz IS NULL OR (at, event_id) < ($5::timestamptz, $6::uuid)
	  ORDER BY at DESC, event_id DESC
	  LIMIT $7`, f.Consumer, f.Status, f.From.UTC(), f.To.UTC(), f.AfterAt, f.AfterEvent, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (IntegrationLogRow, error) {
		var l IntegrationLogRow
		err := r.Scan(&l.At, &l.Consumer, &l.EventType, &l.EventID, &l.Status, &l.ErrorCode, &l.DeliveryCount, &l.DeadLettered)
		return l, err
	})
}

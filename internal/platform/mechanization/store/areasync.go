package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
)

// Kết quả áp một bản diện tích vụ (nhật ký đồng bộ CGH FN-07 B5).
const (
	SyncApplied      = "applied"
	SyncKeptLocked   = "kept_locked"
	SyncStaleIgnored = "stale_ignored"
)

// AreaSyncLog là một dòng pf_mechanization.area_sync_log.
type AreaSyncLog struct {
	ID             uuid.UUID
	CooperativeRef uuid.UUID
	Season         domain.Season
	Source         string
	Outcome        string
	AreaHa         decimal.Decimal
	SourceAt       time.Time
	EventID        *uuid.UUID
	ActorRef       uuid.UUID
	RecordedAt     time.Time
	Count          int64 // số lần (chỉ ở bản tóm tắt theo HTX × vụ)
}

// InsertAreaSyncLog ghi một dòng nhật ký (cùng giao dịch với sổ diện tích vụ / inbox).
func InsertAreaSyncLog(ctx context.Context, tx pgx.Tx, tid uuid.UUID, l *AreaSyncLog) error {
	err := tx.QueryRow(ctx, `INSERT INTO pf_mechanization.area_sync_log
	    (tenant_id, id, cooperative_ref, season_code, season_year, source, outcome, area_ha, source_at, event_id, actor_ref, recorded_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9, $10, $11, $12)
	  ON CONFLICT (tenant_id, event_id) WHERE event_id IS NOT NULL DO NOTHING
	  RETURNING recorded_at`,
		tid, l.ID, l.CooperativeRef, l.Season.Code, l.Season.Year, l.Source, l.Outcome, l.AreaHa.String(), l.SourceAt.UTC(),
		l.EventID, l.ActorRef, l.RecordedAt.UTC()).Scan(&l.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // sự kiện đã ghi nhật ký (giao lặp)
	}
	return err
}

const syncLogCols = `id, cooperative_ref, season_code, season_year, source, outcome, area_ha::text, source_at, event_id, actor_ref, recorded_at`

func scanSyncLog(r pgx.Row, withCount bool) (AreaSyncLog, error) {
	var l AreaSyncLog
	var area string
	dst := []any{&l.ID, &l.CooperativeRef, &l.Season.Code, &l.Season.Year, &l.Source, &l.Outcome, &area, &l.SourceAt, &l.EventID,
		&l.ActorRef, &l.RecordedAt}
	if withCount {
		dst = append(dst, &l.Count)
	}
	if err := r.Scan(dst...); err != nil {
		return l, err
	}
	d, err := decimal.NewFromString(area)
	l.AreaHa = d
	return l, err
}

// SyncLogFilter lọc nhật ký: HTX, vụ (Code rỗng = mọi vụ), nguồn, con trỏ (RecordedAt, ID) giảm dần.
type SyncLogFilter struct {
	CooperativeRef uuid.UUID
	Season         domain.Season
	Source         string
	AfterAt        *time.Time
	AfterID        uuid.UUID
	Limit          int
}

// AreaSyncLogs trả nhật ký mới nhất trước.
func AreaSyncLogs(ctx context.Context, tx pgx.Tx, f SyncLogFilter) ([]AreaSyncLog, error) {
	rows, err := tx.Query(ctx, `SELECT `+syncLogCols+` FROM pf_mechanization.area_sync_log
	  WHERE ($1::uuid = '00000000-0000-0000-0000-000000000000' OR cooperative_ref = $1)
	    AND ($2 = '' OR (season_code = $2 AND season_year = $3))
	    AND ($4 = '' OR source = $4)
	    AND ($5::timestamptz IS NULL OR (recorded_at, id) < ($5::timestamptz, $6::uuid))
	  ORDER BY recorded_at DESC, id DESC LIMIT $7`,
		f.CooperativeRef, f.Season.Code, f.Season.Year, f.Source, f.AfterAt, f.AfterID, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AreaSyncLog, error) { return scanSyncLog(r, false) })
}

// LatestSyncPerCoop trả dòng nhật ký mới nhất của mỗi HTX trong một vụ, kèm số lần.
func LatestSyncPerCoop(ctx context.Context, tx pgx.Tx, s domain.Season) ([]AreaSyncLog, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (cooperative_ref) `+syncLogCols+`,
	    count(*) OVER (PARTITION BY cooperative_ref)
	  FROM pf_mechanization.area_sync_log WHERE season_code = $1 AND season_year = $2
	  ORDER BY cooperative_ref, recorded_at DESC, id DESC`, s.Code, s.Year)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AreaSyncLog, error) { return scanSyncLog(r, true) })
}

// LastAppHTXSync trả lần nhận dữ liệu App HTX gần nhất của tenant (mọi vụ); nil = chưa từng.
func LastAppHTXSync(ctx context.Context, tx pgx.Tx) (*time.Time, error) {
	var t *time.Time
	err := tx.QueryRow(ctx, `SELECT max(recorded_at) FROM pf_mechanization.area_sync_log WHERE source = 'app_htx'`).Scan(&t)
	return t, err
}

// InsertFeedAlert ghi cảnh báo độ tươi của ngày; đã có → false (không phát lại).
func InsertFeedAlert(ctx context.Context, tx pgx.Tx, tid uuid.UUID, day time.Time, last time.Time, threshold decimal.Decimal) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_mechanization.area_feed_alert (tenant_id, alert_date, last_sync_at, threshold_hours)
	  VALUES ($1, $2, $3, $4::numeric) ON CONFLICT DO NOTHING`, tid, day, last.UTC(), threshold.String())
	return tag.RowsAffected() == 1, err
}

// FeedTenants tìm tenant đã từng nhận dữ liệu App HTX (pool vai ag_ops_jobs, xuyên tenant).
func FeedTenants(ctx context.Context, pool *pgxpool.Pool) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, "SELECT DISTINCT tenant_id FROM pf_mechanization.area_sync_log WHERE source = 'app_htx'")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

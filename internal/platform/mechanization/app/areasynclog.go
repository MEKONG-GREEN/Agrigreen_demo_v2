package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DefaultAreaFeedStaleHours là ngưỡng độ tươi khi chưa có tham số đã duyệt (CGH FN-07 AC-03).
var DefaultAreaFeedStaleHours = decimal.NewFromInt(48)

// logSync ghi một dòng nhật ký đồng bộ/nhập tay diện tích vụ (CGH FN-07 B5) trong giao dịch
// đang mở (cùng sổ diện tích vụ hoặc cùng dòng inbox).
func logSync(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, a domain.SeasonArea, source, outcome string, eventID *uuid.UUID, now time.Time) error {
	l := store.AreaSyncLog{ID: ids.New(), CooperativeRef: a.CooperativeRef, Season: a.Season, Source: source, Outcome: outcome,
		AreaHa: a.AreaHa, SourceAt: a.SourceAt, EventID: eventID, ActorRef: actor, RecordedAt: now}
	return store.InsertAreaSyncLog(ctx, tx, tid, &l)
}

// staleHours đọc cgh.area_feed_stale_hours tại at (thiếu → mặc định 48 giờ).
func (s *Service) staleHours(ctx context.Context, at time.Time) (decimal.Decimal, error) {
	if s.Params == nil {
		return DefaultAreaFeedStaleHours, nil
	}
	p, err := s.paramAt(ctx, mdport.ParamCGHAreaFeedStaleHours, at)
	if err != nil {
		return decimal.Zero, err
	}
	if p == nil || !p.Value.IsPositive() {
		return DefaultAreaFeedStaleHours, nil
	}
	return p.Value, nil
}

func isStale(last *time.Time, now time.Time, hours decimal.Decimal) bool {
	if last == nil {
		return false
	}
	limit := time.Duration(hours.Mul(decimal.NewFromInt(int64(time.Hour))).IntPart())
	return now.Sub(*last) > limit
}

// FeedStatus là độ tươi của nguồn App HTX và dòng nhật ký mới nhất theo HTX của một vụ.
type FeedStatus struct {
	LastAppHTXAt   *time.Time
	ThresholdHours decimal.Decimal
	// Stale: lần nhận App HTX gần nhất cũ hơn ngưỡng — màn CGH báo đang dùng dữ liệu nhập tay
	// dự phòng (FN-07 AC-03, luồng ngoại lệ "đang dùng dữ liệu nhập tay dự phòng").
	Stale bool
	Items []CoopSyncStatus
}

// CoopSyncStatus là lần đồng bộ/nhập gần nhất của một HTX trong vụ.
type CoopSyncStatus struct {
	store.AreaSyncLog
	// Stale: bản App HTX của HTX này cũ hơn ngưỡng độ tươi (nhập tay/khóa để dùng dữ liệu dự phòng).
	Stale bool
}

// AreaSyncStatus trả độ tươi nguồn App HTX và lần đồng bộ gần nhất theo HTX của vụ (CGH FN-07
// B5, AC-03) — Cục KTHT và Admin đọc (chỉ báo độ tươi trên màn CGH).
func (s *Service) AreaSyncStatus(ctx context.Context, sn domain.Season) (FeedStatus, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return FeedStatus{}, err
	}
	if _, err := s.season(ctx, sn); err != nil {
		return FeedStatus{}, err
	}
	now := s.now()
	hours, err := s.staleHours(ctx, now)
	if err != nil {
		return FeedStatus{}, err
	}
	var last *time.Time
	var rows []store.AreaSyncLog
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if last, e = store.LastAppHTXSync(ctx, tx); e != nil {
			return e
		}
		rows, e = store.LatestSyncPerCoop(ctx, tx, sn)
		return e
	})
	if err != nil {
		return FeedStatus{}, err
	}
	out := FeedStatus{LastAppHTXAt: last, ThresholdHours: hours, Stale: isStale(last, now, hours), Items: []CoopSyncStatus{}}
	for _, r := range rows {
		if s.coopVisible(ctx, sc, r.CooperativeRef) != nil {
			continue
		}
		at := r.RecordedAt
		out.Items = append(out.Items, CoopSyncStatus{AreaSyncLog: r, Stale: r.Source == port.SourceAppHTX && isStale(&at, now, hours)})
	}
	return out, nil
}

// SyncLogQuery là bộ lọc nhật ký đồng bộ diện tích vụ.
type SyncLogQuery struct {
	CooperativeRef uuid.UUID
	Season         domain.Season // Code rỗng = mọi vụ
	Source         string
	AfterAt        *time.Time
	AfterID        uuid.UUID
	Limit          int
}

// AreaSyncLog trả nhật ký đồng bộ và cập nhật thủ công diện tích vụ (thời điểm, nguồn App
// HTX/Nhập tay, HTX × vụ, kết quả QT-03), mới nhất trước — Admin CGH (FN-07 B5, BR-04).
func (s *Service) AreaSyncLog(ctx context.Context, q SyncLogQuery) ([]store.AreaSyncLog, bool, error) {
	p, sc, err := s.caller(ctx, []string{port.RoleCGHAdmin, authz.RolePlatformAdmin})
	if err != nil {
		return nil, false, err
	}
	switch q.Source {
	case "", port.SourceAppHTX, port.SourceManual:
	default:
		return nil, false, problem.Validation.WithField("source", "phải là app_htx hoặc manual")
	}
	if q.Season.Code != "" {
		if _, err := s.season(ctx, q.Season); err != nil {
			return nil, false, err
		}
	}
	if q.CooperativeRef != uuid.Nil {
		if err := s.coopVisible(ctx, sc, q.CooperativeRef); err != nil {
			return nil, false, err
		}
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	var rows []store.AreaSyncLog
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.AreaSyncLogs(ctx, tx, store.SyncLogFilter{CooperativeRef: q.CooperativeRef, Season: q.Season, Source: q.Source,
			AfterAt: q.AfterAt, AfterID: q.AfterID, Limit: q.Limit + 1})
		return e
	})
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > q.Limit
	if more {
		rows = rows[:q.Limit]
	}
	out := rows[:0]
	for _, r := range rows {
		if sc.All || s.coopVisible(ctx, sc, r.CooperativeRef) == nil {
			out = append(out, r)
		}
	}
	return out, more, nil
}

// TenantLister trả danh sách tenant cần chạy job.
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// RunAreaFeedCheck là thân job port.JobAreaFeedCheck (worker, chỉ leader): với mỗi tenant đã
// từng nhận dữ liệu App HTX, nếu lần nhận gần nhất cũ hơn cgh.area_feed_stale_hours thì ghi
// một cảnh báo cho ngày nghiệp vụ và phát pf.mechanization.area_feed.stale (pf/notify báo Admin
// CGH). Idempotent theo (tenant, ngày): chạy lại cùng ngày không báo lần hai (CGH FN-07 AC-02/AC-03).
func (s *Service) RunAreaFeedCheck(ctx context.Context, tenants TenantLister) (int, error) {
	tids, err := tenants(ctx)
	if err != nil {
		return 0, err
	}
	now := s.now()
	day := dayOf(now)
	n := 0
	for _, tid := range tids {
		tctx := authz.WithPrincipal(ctx, authz.Principal{TenantID: tid, ActorRef: port.SyncActorRef, Roles: []string{port.RoleCGHSync}})
		hours, err := s.staleHours(tenant.WithID(tctx, tid), now)
		if err != nil {
			return n, err
		}
		err = s.tx(ctx, tid, func(tx pgx.Tx) error {
			last, err := store.LastAppHTXSync(ctx, tx)
			if err != nil || !isStale(last, now, hours) {
				return err
			}
			ok, err := store.InsertFeedAlert(ctx, tx, tid, day, *last, hours)
			if err != nil || !ok {
				return err
			}
			n++
			return outbox.Enqueue(ctx, tx, outbox.Event{
				ID: ids.New(), TenantID: tid, Type: port.EventAreaFeedStale, SchemaVersion: 1, OccurredAt: now,
				TraceID: telemetry.TraceID(ctx), Producer: "pf/mechanization",
				Payload: port.AreaFeedStaleV1{LastSyncAt: last.UTC().Format(time.RFC3339), ThresholdHours: hours.String(),
					AlertDate: day.Format(time.DateOnly), DetectedAt: now.Format(time.RFC3339)},
			})
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

package ingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// Session là một phiên cân ĐÃ ĐÓNG ở điểm cân HUB_IN/HUB_OUT: mốc mở/đóng cửa sổ
// theo BỘ ĐẾM do mkg/measurement (hoặc fieldops/hub qua measurement) ghi khi người
// vận hành mở/đóng lần cân (ADR-0019 §4).
type Session struct {
	ID          uuid.UUID // định danh phiên cân — khóa idempotency của phiếu
	TenantID    uuid.UUID
	LotRef      uuid.UUID
	Point       port.WeighPoint
	Route       port.Route
	Window      port.Window
	MoisturePct string    // tùy chọn, "12.50"; trạng thái ẩm khi cân chờ C-4
	ActorRef    uuid.UUID // người đóng phiên
	ClosedAt    time.Time // giờ hệ lúc đóng phiên — đo độ trễ tích phân
}

// SessionSource liệt kê các phiên cân đã đóng mà CHƯA có phiếu nguồn edge, cũ
// trước, trên mọi tenant (ingest là tiến trình hệ thống). Hiện thực thuộc
// mkg/measurement (vai ingest_mkg). Phiên đã có phiếu (TicketWriter.Record thành
// công) không được trả lại nữa; trả lại thì Record idempotent theo IdemKey.
type SessionSource interface {
	PendingSessions(ctx context.Context, limit int) ([]Session, error)
}

// WindowIntegrator là phần Integrator mà vòng lặp cần (Integrator cài đặt).
type WindowIntegrator interface {
	IntegrateDetailed(ctx context.Context, tenantID uuid.UUID, w port.Window) (Result, error)
}

// DefaultStuckAfter là ngưỡng báo động phiên kẹt mặc định (tạm): agent đệm ≥ 72 giờ
// nên thiếu lô ngắn hạn là bình thường; quá 30 phút sau khi đóng phiên thì cần người xem.
const DefaultStuckAfter = 30 * time.Minute

// IdemKey là khóa idempotency của phiếu tích phân từ một phiên theo một rule_version.
func IdemKey(sessionID uuid.UUID, ruleVersion string) string {
	return "ingest:" + sessionID.String() + ":" + ruleVersion
}

// Loop tích phân phiên cân thành phiếu (R-70).
type Loop struct {
	Sessions   SessionSource
	Integrator WindowIntegrator
	Tickets    port.TicketWriter
	Clock      clock.Clock
	Log        *slog.Logger
	Metrics    *Metrics
	BatchSize  int           // ≤ 0 → 100
	StuckAfter time.Duration // ≤ 0 → DefaultStuckAfter

	mu      sync.Mutex
	failing map[uuid.UUID]*failure
}

type failure struct {
	first   time.Time
	alerted bool
}

// Stats là kết quả một lượt.
type Stats struct {
	Recorded int
	Retrying int
	Alerted  int
}

// ErrorCode ánh xạ lỗi tích phân / ghi phiếu sang mã ổn định (nhãn metric, log).
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, port.ErrWindowIncomplete):
		return "ingest.window_incomplete"
	case errors.Is(err, ErrNoStableReadings):
		return "ingest.no_stable_readings"
	case errors.Is(err, ErrNonPositiveNet):
		return "ingest.non_positive_net"
	case errors.Is(err, ErrWindowTooLarge):
		return "ingest.window_too_large"
	case errors.Is(err, ErrWindowCorrupt):
		return "ingest.window_corrupt"
	case errors.Is(err, port.ErrInvalidTicket):
		return "measurement.invalid_ticket"
	}
	return "internal_error"
}

// Run chạy RunOnce mỗi interval tới khi ctx hủy.
func (l *Loop) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := l.RunOnce(ctx); err != nil && ctx.Err() == nil {
			l.Log.Error("lượt tích phân lỗi", "event", "ingest.integrate", "outcome", "error", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunOnce xử lý một trang phiên chờ. Lỗi của một phiên không chặn phiên khác: phiên
// lỗi được thử lại ở lượt sau, và báo động MỘT lần khi kẹt quá StuckAfter.
func (l *Loop) RunOnce(ctx context.Context) (Stats, error) {
	var st Stats
	n := l.BatchSize
	if n <= 0 {
		n = 100
	}
	sessions, err := l.Sessions.PendingSessions(ctx, n)
	if err != nil {
		return st, err
	}
	now := l.Clock.Now()
	var oldest time.Duration
	seen := map[uuid.UUID]bool{}
	for _, s := range sessions {
		seen[s.ID] = true
		if age := now.Sub(s.ClosedAt); age > oldest {
			oldest = age
		}
		if err := l.one(ctx, s); err != nil {
			st.Retrying++
			if l.noteFailure(ctx, s, err, now) {
				st.Alerted++
			}
			continue
		}
		l.clearFailure(s.ID)
		st.Recorded++
	}
	l.prune(seen)
	l.Metrics.oldest.Record(ctx, int64(oldest/time.Second))
	return st, nil
}

func (l *Loop) one(ctx context.Context, s Session) error {
	res, err := l.Integrator.IntegrateDetailed(ctx, s.TenantID, s.Window)
	if err != nil {
		return err
	}
	integ := res.Integration
	t, err := l.Tickets.Record(ctx, s.TenantID, port.TicketDraft{
		ID:          ids.New(),
		IdemKey:     IdemKey(s.ID, integ.RuleVersion),
		LotRef:      s.LotRef,
		Point:       s.Point,
		Route:       s.Route,
		Source:      port.SourceEdge,
		MassKg:      integ.NetKg,
		MoisturePct: s.MoisturePct,
		EventTime:   res.LastReceivedAt,
		Integration: &integ,
		ActorRef:    s.ActorRef,
	})
	if err != nil {
		return err
	}
	l.Metrics.integration(ctx, "recorded", "")
	l.Metrics.integrationLag(ctx, l.Clock.Now().Sub(s.ClosedAt))
	l.Log.Info("ghi phiếu cân từ luồng thô", "event", "ingest.integrate", "outcome", "recorded",
		"tenant_id", s.TenantID.String(), "session_id", s.ID.String(), "ticket_id", t.ID.String(),
		"device_id", s.Window.DeviceID, "epoch", s.Window.Epoch, "counter_from", s.Window.CounterFrom,
		"counter_to", s.Window.CounterTo, "rule_version", integ.RuleVersion)
	return nil
}

// noteFailure ghi nhận lỗi; true khi vừa báo động (một lần mỗi phiên).
func (l *Loop) noteFailure(ctx context.Context, s Session, err error, now time.Time) bool {
	code := ErrorCode(err)
	l.Metrics.integration(ctx, "retry", code)
	l.mu.Lock()
	if l.failing == nil {
		l.failing = map[uuid.UUID]*failure{}
	}
	f, ok := l.failing[s.ID]
	if !ok {
		f = &failure{first: now}
		l.failing[s.ID] = f
	}
	limit := l.StuckAfter
	if limit <= 0 {
		limit = DefaultStuckAfter
	}
	since := s.ClosedAt
	if since.IsZero() || f.first.Before(since) {
		since = f.first
	}
	alert := !f.alerted && now.Sub(since) >= limit
	if alert {
		f.alerted = true
	}
	l.mu.Unlock()
	attrs := []any{"event", "ingest.integrate", "code", code, "tenant_id", s.TenantID.String(), "session_id", s.ID.String(),
		"device_id", s.Window.DeviceID, "epoch", s.Window.Epoch, "counter_from", s.Window.CounterFrom, "counter_to", s.Window.CounterTo}
	if code == "internal_error" {
		attrs = append(attrs, "error", err.Error())
	}
	if alert {
		l.Metrics.stuck.Add(ctx, 1)
		l.Log.Error("phiên cân kẹt quá ngưỡng — cần người xử lý (nhập tay có bằng chứng nếu cân hỏng)", append(attrs, "outcome", "alarm")...)
		return true
	}
	l.Log.Warn("chưa tích phân được phiên cân — thử lại lượt sau", append(attrs, "outcome", "retry")...)
	return false
}

func (l *Loop) clearFailure(id uuid.UUID) {
	l.mu.Lock()
	delete(l.failing, id)
	l.mu.Unlock()
}

// prune bỏ theo dõi phiên không còn chờ (đã có phiếu từ nơi khác, ví dụ nhập tay).
func (l *Loop) prune(seen map[uuid.UUID]bool) {
	l.mu.Lock()
	for id := range l.failing {
		if !seen[id] {
			delete(l.failing, id)
		}
	}
	l.mu.Unlock()
}

// Package schedule là bộ lập lịch trong tiến trình cho job vận hành, tính
// theo giờ nghiệp vụ Asia/Ho_Chi_Minh (shared/clock, R-12).
//
// Chỉ chạy ở leader (shared/leader) — package này không tự chống chạy trùng
// giữa các tiến trình. Mỗi job chạy tuần tự với chính nó; job phải idempotent
// vì có thể chạy lại khi leader đổi hoặc khi khởi động (RunOnStart).
package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Spec cho biết lần chạy kế tiếp sau một thời điểm.
type Spec interface {
	Next(after time.Time) time.Time
	String() string
}

// Daily chạy mỗi ngày lúc Hour:Minute giờ Việt Nam.
type Daily struct{ Hour, Minute int }

// Next trả lần chạy kế tiếp (UTC) sau after.
func (d Daily) Next(after time.Time) time.Time {
	t := after.In(clock.Zone())
	c := time.Date(t.Year(), t.Month(), t.Day(), d.Hour, d.Minute, 0, 0, clock.Zone())
	if !c.After(t) {
		c = time.Date(t.Year(), t.Month(), t.Day()+1, d.Hour, d.Minute, 0, 0, clock.Zone())
	}
	return c.UTC()
}

func (d Daily) String() string {
	return fmt.Sprintf("hằng ngày %02d:%02d %s", d.Hour, d.Minute, clock.BusinessZone)
}

// Every chạy theo chu kỳ, canh theo mốc tròn (vd mỗi giờ đúng phút 00 UTC —
// Việt Nam lệch UTC số giờ nguyên nên cũng là phút 00 giờ Việt Nam).
type Every struct{ Interval time.Duration }

// Next trả mốc tròn kế tiếp sau after.
func (e Every) Next(after time.Time) time.Time {
	return after.UTC().Truncate(e.Interval).Add(e.Interval)
}

func (e Every) String() string { return "mỗi " + e.Interval.String() }

// Job là một việc định kỳ.
type Job struct {
	Name       string
	Spec       Spec
	RunOnStart bool // chạy một lần ngay khi khởi động (bắt kịp lần lỡ); job phải idempotent
	Timeout    time.Duration
	Run        func(ctx context.Context) error
}

// Scheduler chạy các Job tới khi ctx bị hủy.
type Scheduler struct {
	Clock clock.Clock
	Log   *slog.Logger
	Jobs  []Job
}

// Run chạy mọi job, trả khi ctx bị hủy.
func (s *Scheduler) Run(ctx context.Context) error {
	if len(s.Jobs) == 0 {
		return errors.New("schedule: không có job")
	}
	clk := s.Clock
	if clk == nil {
		clk = clock.System{}
	}
	var wg sync.WaitGroup
	for _, j := range s.Jobs {
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			s.loop(ctx, clk, j)
		}(j)
	}
	wg.Wait()
	return ctx.Err()
}

func (s *Scheduler) loop(ctx context.Context, clk clock.Clock, j Job) {
	if j.RunOnStart {
		s.runOnce(ctx, j)
	}
	for {
		next := j.Spec.Next(clk.Now())
		s.Log.Info("lịch job", "event", "job.scheduled", "job", j.Name, "next_run", next.Format(time.RFC3339), "outcome", "ok")
		t := time.NewTimer(next.Sub(clk.Now()))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		s.runOnce(ctx, j)
	}
}

func (s *Scheduler) runOnce(ctx context.Context, j Job) {
	if ctx.Err() != nil {
		return
	}
	timeout := j.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	jctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	jctx, span := telemetry.Tracer().Start(jctx, "job "+j.Name, trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("job", j.Name)))
	defer span.End()
	start := time.Now()
	err := func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("schedule: job %s panic: %v", j.Name, p)
			}
		}()
		return j.Run(jctx)
	}()
	outcome := "ok"
	if err != nil {
		outcome = "error"
		span.SetStatus(codes.Error, "job failed")
		s.Log.Error("job lỗi", "event", "job.run", "job", j.Name, "trace_id", telemetry.TraceID(jctx),
			"duration_ms", time.Since(start).Milliseconds(), "outcome", outcome, "error", err.Error())
	} else {
		s.Log.Info("job xong", "event", "job.run", "job", j.Name, "trace_id", telemetry.TraceID(jctx),
			"duration_ms", time.Since(start).Milliseconds(), "outcome", outcome)
	}
	telemetry.RecordJobRun(ctx, j.Name, outcome)
}

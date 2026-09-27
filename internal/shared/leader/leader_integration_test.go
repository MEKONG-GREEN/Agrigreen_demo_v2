package leader_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/leader"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const lockID = 727199

// Chỉ một worker giữ quyền chạy lịch.
func TestLeaderExclusive(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Get(t).DirectPool(t, migrations.PF, dbadmin.OutboxPublisher)
	a, err := leader.TryAcquire(ctx, pool, lockID, leader.Options{})
	if err != nil || a == nil {
		t.Fatalf("lần đầu phải lấy được khóa: %v", err)
	}
	b, err := leader.TryAcquire(ctx, pool, lockID, leader.Options{})
	if err != nil || b != nil {
		t.Fatalf("tiến trình thứ hai không được lấy khóa: %v %v", b, err)
	}
	if err := a.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Release(ctx); err != nil {
		t.Fatalf("Release lặp phải an toàn: %v", err)
	}
	b, err = leader.TryAcquire(ctx, pool, lockID, leader.Options{})
	if err != nil || b == nil {
		t.Fatalf("sau khi nhả, người khác phải lấy được: %v", err)
	}
	_ = b.Release(ctx)
}

// Phiên giữ khóa chết → Lost đóng, và khóa được nhả cho người khác.
func TestLeaderLostWhenSessionKilled(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	pool := env.DirectPool(t, migrations.PF, dbadmin.OutboxPublisher)
	a, err := leader.TryAcquire(ctx, pool, lockID+1, leader.Options{Heartbeat: 50 * time.Millisecond})
	if err != nil || a == nil {
		t.Fatal(err)
	}
	defer a.Release(ctx) //nolint:errcheck
	if _, err := env.AdminPool(t, migrations.PF).Exec(ctx, "SELECT pg_terminate_backend($1)", int64(a.PID())); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.Lost():
	case <-time.After(5 * time.Second):
		t.Fatal("Lost phải đóng khi phiên giữ khóa chết")
	}
	b, err := leader.TryAcquire(ctx, pool, lockID+1, leader.Options{})
	if err != nil || b == nil {
		t.Fatalf("khóa phải được nhả khi phiên chết: %v", err)
	}
	_ = b.Release(ctx)
}

// Hai vòng Run song song: tại mỗi thời điểm chỉ một vòng chạy fn.
func TestRunSingleLeader(t *testing.T) {
	pool := pgtest.Get(t).DirectPool(t, migrations.PF, dbadmin.OutboxPublisher)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	var running, maxRunning, runs atomic.Int32
	fn := func(ctx context.Context) error {
		runs.Add(1)
		n := running.Add(1)
		for {
			m := maxRunning.Load()
			if n <= m || maxRunning.CompareAndSwap(m, n) {
				break
			}
		}
		<-ctx.Done()
		running.Add(-1)
		return ctx.Err()
	}
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_ = leader.Run(ctx, pool, lockID+2, 20*time.Millisecond, leader.Options{}, log, fn)
			done <- struct{}{}
		}()
	}
	<-done
	<-done
	if maxRunning.Load() != 1 || runs.Load() != 1 {
		t.Fatalf("chỉ một leader được chạy: max=%d runs=%d", maxRunning.Load(), runs.Load())
	}
}

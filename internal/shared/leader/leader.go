// Package leader bầu một tiến trình duy nhất chạy lịch vận hành (neo chuỗi,
// dọn idempotency) khi có nhiều bản worker.
//
// Cơ chế: pg_try_advisory_lock mức PHIÊN trên một kết nối riêng tách khỏi
// pool (Hijack). Mất kết nối = mất khóa: PostgreSQL tự nhả khi phiên chết, và
// Lease.Lost() báo cho người giữ dừng việc.
//
// Kết nối PHẢI đi thẳng PostgreSQL. Qua PgBouncer transaction mode khóa phiên
// không giữ được — hai worker có thể cùng tưởng mình là leader.
package leader

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LockSchedules là khóa tư vấn của lịch vận hành trong cmd/worker.
// Dải 7271xx dành cho job vận hành (727001 là khóa bootstrap của dbadmin).
const LockSchedules int64 = 727101

// Options điều chỉnh nhịp kiểm kết nối.
type Options struct {
	// Heartbeat là chu kỳ ping kết nối giữ khóa, mặc định 10 giây.
	Heartbeat time.Duration
}

// Lease là quyền leader đang giữ.
type Lease struct {
	conn     *pgx.Conn
	id       int64
	lost     chan struct{}
	stop     chan struct{}
	done     chan struct{}
	lostOnce sync.Once
	relOnce  sync.Once
}

// TryAcquire thử lấy khóa lockID. Trả (nil, nil) nếu tiến trình khác đang giữ.
func TryAcquire(ctx context.Context, pool *pgxpool.Pool, lockID int64, o Options) (*Lease, error) {
	pc, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	conn := pc.Hijack() // tách khỏi pool: kết nối này chỉ để giữ khóa
	var ok bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&ok); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	if !ok {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, nil
	}
	hb := o.Heartbeat
	if hb <= 0 {
		hb = 10 * time.Second
	}
	l := &Lease{conn: conn, id: lockID, lost: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{})}
	go l.heartbeat(context.WithoutCancel(ctx), hb)
	return l, nil
}

func (l *Lease) heartbeat(base context.Context, every time.Duration) {
	defer close(l.done)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(base, every)
			err := l.conn.Ping(ctx)
			cancel()
			if err != nil {
				l.markLost()
				return
			}
		}
	}
}

func (l *Lease) markLost() { l.lostOnce.Do(func() { close(l.lost) }) }

// Lost đóng khi kết nối giữ khóa hỏng — người giữ phải dừng việc ngay.
func (l *Lease) Lost() <-chan struct{} { return l.lost }

// PID trả pid phiên PostgreSQL giữ khóa (chẩn đoán, kiểm thử).
func (l *Lease) PID() uint32 { return l.conn.PgConn().PID() }

// Release nhả khóa và đóng kết nối. Gọi lặp an toàn.
func (l *Lease) Release(ctx context.Context) error {
	var err error
	l.relOnce.Do(func() {
		close(l.stop)
		<-l.done
		ctx := context.WithoutCancel(ctx)
		select {
		case <-l.lost:
		default:
			_, err = l.conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", l.id)
		}
		l.markLost()
		err = errors.Join(err, l.conn.Close(ctx))
	})
	return err
}

// Run lặp: chờ tới khi thành leader, chạy fn với ctx bị hủy khi mất khóa, nhả
// khóa khi fn trả về, rồi thử lại sau retry. Trả khi ctx bị hủy.
func Run(ctx context.Context, pool *pgxpool.Pool, lockID int64, retry time.Duration, o Options, log *slog.Logger, fn func(context.Context) error) error {
	if retry <= 0 {
		retry = 15 * time.Second
	}
	for {
		lease, err := TryAcquire(ctx, pool, lockID, o)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Warn("không lấy được khóa leader", "event", "leader.acquire", "outcome", "error", "error", err.Error())
		case lease != nil:
			log.Info("đã thành leader", "event", "leader.acquire", "outcome", "ok")
			lctx, cancel := context.WithCancel(ctx)
			go func() {
				select {
				case <-lease.Lost():
					log.Warn("mất khóa leader", "event", "leader.lost", "outcome", "error")
					cancel()
				case <-lctx.Done():
				}
			}()
			ferr := fn(lctx)
			cancel()
			_ = lease.Release(ctx)
			if ferr != nil && !errors.Is(ferr, context.Canceled) {
				log.Warn("việc của leader dừng do lỗi", "event", "leader.run", "outcome", "error", "error", ferr.Error())
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retry):
		}
	}
}

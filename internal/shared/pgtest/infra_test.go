package pgtest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsInfra_WHB(t *testing.T) {
	infra := []error{
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		fmt.Errorf("bọc: %w", &pgconn.PgError{Code: "57P03", Message: "the database system is starting up"}),
		&pgconn.PgError{Code: "57P01"},
		&pgconn.PgError{Code: "08006"},
		&pgconn.PgError{Code: "53300"},
		errInRecovery,
		context.DeadlineExceeded,
		errors.New("failed to connect to `user=postgres database=postgres`: dial tcp [::1]:5432: connectex: No connection could be made because the target machine actively refused it."),
		errors.New("unexpected EOF: conn closed"),
	}
	for _, e := range infra {
		if !IsInfra(e) {
			t.Errorf("phải là lỗi hạ tầng: %v", e)
		}
	}
	notInfra := []error{
		nil,
		&pgconn.PgError{Code: "28P01", Message: "password authentication failed"},
		&pgconn.PgError{Code: "42P01", Message: "relation does not exist"},
		&pgconn.PgError{Code: "23505"},
		errors.New("migration pf/000001: syntax error"),
	}
	for _, e := range notInfra {
		if IsInfra(e) {
			t.Errorf("không phải lỗi hạ tầng: %v", e)
		}
	}
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time        { return c.now }
func (c *fakeClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }
func (c *fakeClock) logf(string, ...any)   {}
func newClock() *fakeClock                 { return &fakeClock{now: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)} }
func refused() error                       { return &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED} }
func probeSeq(errs ...error) func(context.Context) error {
	i := 0
	return func(context.Context) error {
		if i >= len(errs) {
			return errs[len(errs)-1]
		}
		i++
		return errs[i-1]
	}
}

// Chờ: hồi phục trong hạn → nil; hết hạn → một *InfraError có nhãn INFRA; lỗi không phải hạ tầng → trả ngay.
func TestWaitReady_WHB(t *testing.T) {
	ctx := context.Background()

	c := newClock()
	if err := waitReady(ctx, 30*time.Second, probeSeq(refused(), &pgconn.PgError{Code: "57P03"}, errInRecovery, nil), c.Sleep, c.Now, c.logf); err != nil {
		t.Fatalf("hồi phục trong hạn: %v", err)
	}
	if got := c.now.Sub(newClock().now); got != 3*pollEvery {
		t.Errorf("chờ %s, muốn %s", got, 3*pollEvery)
	}

	c = newClock()
	err := waitReady(ctx, 10*time.Second, probeSeq(refused()), c.Sleep, c.Now, c.logf)
	var ie *InfraError
	if !errors.As(err, &ie) || !IsInfra(err) || !strings.HasPrefix(err.Error(), "INFRA: PostgreSQL unavailable") {
		t.Fatalf("hết hạn phải là InfraError: %v", err)
	}
	if ie.Waited != 10*time.Second {
		t.Errorf("đã chờ %s", ie.Waited)
	}

	c = newClock()
	if err := waitReady(ctx, 0, probeSeq(refused()), c.Sleep, c.Now, c.logf); !IsInfra(err) || c.now != newClock().now {
		t.Errorf("AG_TEST_PG_WAIT=0: hỏng ngay, không chờ: %v", err)
	}

	c = newClock()
	auth := &pgconn.PgError{Code: "28P01"}
	if err := waitReady(ctx, time.Minute, probeSeq(auth), c.Sleep, c.Now, c.logf); !errors.Is(err, auth) || IsInfra(err) {
		t.Errorf("sai mật khẩu không phải hạ tầng, không chờ: %v", err)
	}
}

func TestInfraMessageHasNoPassword_WHB(t *testing.T) {
	m := downMarker("postgres://postgres:secret-pw@localhost:5432/postgres")
	if strings.Contains(m, "secret") {
		t.Errorf("tên dấu sập lộ mật khẩu: %s", m)
	}
	if downMarker("postgres://a:b@localhost:5432/x") != downMarker("postgres://c:d@localhost:5432/y") {
		t.Error("dấu sập theo host:port")
	}
}

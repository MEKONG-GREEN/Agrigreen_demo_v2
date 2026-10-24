package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStatementTimeoutFromEnv(t *testing.T) {
	cases := []struct {
		val     string
		want    time.Duration
		wantErr bool
	}{
		{"", DefaultStatementTimeout, false},
		{"0", 0, false},
		{"30s", 30 * time.Second, false},
		{" 5m ", 5 * time.Minute, false},
		{"30", 0, true},
		{"-1s", 0, true},
		{"hai phút", 0, true},
	}
	for _, c := range cases {
		got, err := statementTimeoutFromEnv(c.val)
		if (err != nil) != c.wantErr || got != c.want {
			t.Fatalf("statementTimeoutFromEnv(%q) = %v, %v; muốn %v, lỗi=%v", c.val, got, err, c.want, c.wantErr)
		}
	}
}

// Biến sai cú pháp → Open trả lỗi nêu tên biến trước khi chạm DSN/mạng.
func TestOpenRejectsInvalidStatementTimeoutEnv(t *testing.T) {
	t.Setenv("AG_DB_STATEMENT_TIMEOUT", "2 phút")
	o := OptionsFromEnv("test")
	_, err := Open(context.Background(), "postgres://127.0.0.1:1/x", o)
	if err == nil || !strings.Contains(err.Error(), "AG_DB_STATEMENT_TIMEOUT") {
		t.Fatalf("Open với biến sai: lỗi = %v", err)
	}
}

// Kết nối thẳng: statement_timeout là tham số khởi động của phiên; mặc định Options (0) không đặt.
func TestOpenSetsStatementTimeoutDirect(t *testing.T) {
	dsn := os.Getenv("AG_TEST_PG_ADMIN_URL")
	if dsn == "" {
		t.Skip("cần AG_TEST_PG_ADMIN_URL")
	}
	ctx := context.Background()
	for _, c := range []struct {
		o    Options
		want string
	}{
		{Options{AppName: "db-timeout-test", MaxConns: 1, StatementTimeout: 1500 * time.Millisecond}, "1500ms"},
		{Options{AppName: "db-timeout-test", MaxConns: 1}, "0"},
	} {
		p, err := Open(ctx, dsn, c.o)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		err = p.QueryRow(ctx, "SHOW statement_timeout").Scan(&got)
		p.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Fatalf("statement_timeout = %q, muốn %q (opts %+v)", got, c.want, c.o)
		}
	}
}

func TestOptionsFromEnvStatementTimeoutDefault(t *testing.T) {
	t.Setenv("AG_DB_STATEMENT_TIMEOUT", "")
	if o := OptionsFromEnv("test"); o.StatementTimeout != DefaultStatementTimeout || o.envErr != nil {
		t.Fatalf("mặc định: %v, %v", o.StatementTimeout, o.envErr)
	}
}

func TestAcquireTimeoutFromEnv(t *testing.T) {
	cases := []struct {
		val     string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"0", -1, false},
		{"10s", 10 * time.Second, false},
		{" 1m ", time.Minute, false},
		{"10", 0, true},
		{"-1s", 0, true},
		{"mười giây", 0, true},
	}
	for _, c := range cases {
		got, err := acquireTimeoutFromEnv(c.val)
		if (err != nil) != c.wantErr || got != c.want {
			t.Fatalf("acquireTimeoutFromEnv(%q) = %v, %v; muốn %v, lỗi=%v", c.val, got, err, c.want, c.wantErr)
		}
	}
	t.Setenv("AG_DB_ACQUIRE_TIMEOUT", "mười giây")
	_, err := Open(context.Background(), "postgres://127.0.0.1:1/x", OptionsFromEnv("test"))
	if err == nil || !strings.Contains(err.Error(), "AG_DB_ACQUIRE_TIMEOUT") {
		t.Fatalf("Open với biến sai: lỗi = %v", err)
	}
}

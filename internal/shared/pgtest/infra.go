package pgtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Phân biệt lỗi hạ tầng (PostgreSQL không chạy, đang khôi phục sau sự cố, mất kết nối) với lỗi của kiểm thử (WH-B).
// Trước đây PostgreSQL dùng chung trên máy dev sập → mỗi kiểm thử của gói HỎNG 0.00s với lỗi kết nối, không phân
// biệt được với lỗi thật. Nay:
//
//   - trước khi dựng database, pgtest chờ PostgreSQL sẵn sàng tối đa AG_TEST_PG_WAIT giây (mặc định 120; 0 = không
//     chờ). Hết hạn → gói HỎNG MỘT LẦN với dòng "INFRA: PostgreSQL unavailable …", không chạy kiểm thử nào;
//   - đang chạy mà PostgreSQL sập: Get thăm dò (tối đa mỗi probeEvery), chờ lại; hồi phục → chạy tiếp (database
//     kiểm thử còn nguyên sau khôi phục); không hồi phục → kiểm thử đó HỎNG với "INFRA: …", các kiểm thử sau BỎ QUA
//     với cùng nhãn (gói vẫn HỎNG — không che lỗi trong CI);
//   - gói sau (go test -p 1 ./...) thấy dấu "vừa sập" (< downMarkerTTL) chỉ chờ downRetryWait thay vì cả hạn.

// EnvWait là biến chọn số giây chờ PostgreSQL sẵn sàng.
const EnvWait = "AG_TEST_PG_WAIT"

const (
	defaultWait   = 120 * time.Second
	probeEvery    = 5 * time.Second
	pollEvery     = 2 * time.Second
	probeTimeout  = 5 * time.Second
	downMarkerTTL = 10 * time.Minute
	downRetryWait = 10 * time.Second
)

// InfraError là lỗi hạ tầng PostgreSQL — không phải lỗi của kiểm thử.
type InfraError struct {
	Waited time.Duration
	Err    error
}

func (e *InfraError) Error() string {
	return fmt.Sprintf("INFRA: PostgreSQL unavailable (đã chờ %s): %v — không phải lỗi kiểm thử; kiểm pg_isready, hoặc chạy trên cụm tạm: scripts/pg-ephemeral.ps1 (docs/dev/parallel-packages.md)", e.Waited.Round(time.Second), e.Err)
}

func (e *InfraError) Unwrap() error { return e.Err }

var (
	errInRecovery = errors.New("PostgreSQL đang ở chế độ khôi phục (pg_is_in_recovery)")
	errSkipInfra  = errors.New("pgtest: PostgreSQL đã được báo sập trong gói này")
)

// IsInfra cho biết err là lỗi hạ tầng: không kết nối được, PostgreSQL đang khởi động/khôi phục/tắt, mất kết nối giữa
// chừng, quá số kết nối. Sai mật khẩu, database không tồn tại, lỗi SQL — không phải.
func IsInfra(err error) bool {
	if err == nil {
		return false
	}
	var ie *InfraError
	if errors.As(err, &ie) || errors.Is(err, errInRecovery) {
		return true
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch {
		case strings.HasPrefix(pe.Code, "08"), // connection_exception
			pe.Code == "57P01", pe.Code == "57P02", pe.Code == "57P03", // admin/crash shutdown, cannot_connect_now
			pe.Code == "53300",               // too_many_connections
			strings.HasPrefix(pe.Code, "58"): // system_error (I/O)
			return true
		}
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection refused", "connection reset", "conn closed", "the database system is", "server closed the connection", "no connection could be made", "forcibly closed"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// probe kiểm PostgreSQL nhận kết nối và không ở chế độ khôi phục.
func probe(ctx context.Context, admin string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	var rec bool
	if err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&rec); err != nil {
		return err
	}
	if rec {
		return errInRecovery
	}
	return nil
}

// waitReady thăm dò tới khi probe thành công, gặp lỗi KHÔNG phải hạ tầng (trả ngay), hoặc hết budget (*InfraError).
func waitReady(ctx context.Context, budget time.Duration, probe func(context.Context) error, sleep func(time.Duration), clock func() time.Time, logf func(string, ...any)) error {
	start := clock()
	logged := false
	for {
		err := probe(ctx)
		if err == nil {
			if logged {
				logf("pgtest: PostgreSQL sẵn sàng sau %s", clock().Sub(start).Round(time.Second))
			}
			return nil
		}
		if !IsInfra(err) {
			return err
		}
		waited := clock().Sub(start)
		if waited >= budget {
			return &InfraError{Waited: waited, Err: err}
		}
		if !logged {
			logf("pgtest: PostgreSQL chưa sẵn sàng (%v) — chờ tối đa %s (%s)", err, budget, EnvWait)
			logged = true
		}
		sleep(min(pollEvery, budget-waited))
	}
}

// waitBudget đọc AG_TEST_PG_WAIT (giây).
func waitBudget() time.Duration {
	v := strings.TrimSpace(os.Getenv(EnvWait))
	if v == "" {
		return defaultWait
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "pgtest: %s=%q không hợp lệ — dùng %s\n", EnvWait, v, defaultWait)
		return defaultWait
	}
	return time.Duration(n) * time.Second
}

// downMarker là tệp tạm đánh dấu "PostgreSQL ở địa chỉ này vừa sập" để các gói sau không chờ cả hạn. Tên băm từ
// host:port (không chứa mật khẩu).
func downMarker(admin string) string {
	host := "unknown"
	if u, err := url.Parse(admin); err == nil {
		host = u.Host
	}
	sum := sha256.Sum256([]byte(host))
	return filepath.Join(os.TempDir(), "agrigreen-pgtest-down-"+hex.EncodeToString(sum[:6]))
}

func recentlyDown(admin string) bool {
	st, err := os.Stat(downMarker(admin))
	return err == nil && time.Since(st.ModTime()) < downMarkerTTL
}

func markDown(admin string, down bool) {
	p := downMarker(admin)
	if !down {
		_ = os.Remove(p)
		return
	}
	_ = os.WriteFile(p, []byte(time.Now().UTC().Format(time.RFC3339)), 0o600)
}

func stderrf(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }

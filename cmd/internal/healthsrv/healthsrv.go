// Package healthsrv là máy chủ sức khỏe dùng chung của các lệnh nền không có HTTP nghiệp vụ (worker,
// outbox-publisher — như worker-internal): /healthz (tiến trình sống) và /readyz (mọi phụ thuộc đã đăng ký
// sẵn sàng: pool DB ping được, kết nối RabbitMQ còn mở). Địa chỉ đọc từ AG_HEALTH_ADDR — chỉ trong mạng nội bộ
// (probe của kubelet, healthcheck Docker qua `<lệnh> -healthcheck <url>`). Không có dữ liệu nghiệp vụ.
package healthsrv

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvAddr là biến địa chỉ lắng nghe.
const EnvAddr = "AG_HEALTH_ADDR"

// Check là một kiểm tra sẵn sàng; lỗi → /readyz 503 với status "<tên>_unavailable".
type Check func(ctx context.Context) error

type named struct {
	name  string
	check Check
}

// Server gom các kiểm tra sẵn sàng.
type Server struct {
	zone   string
	mu     sync.Mutex
	checks []named
	info   map[string]any
}

// New tạo máy chủ sức khỏe của một vùng (pf | mkg).
func New(zone string) *Server { return &Server{zone: zone, info: map[string]any{}} }

// Add đăng ký một kiểm tra sẵn sàng.
func (s *Server) Add(name string, c Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks = append(s.checks, named{name: name, check: c})
}

// Info gắn thêm trường mô tả vào phản hồi /readyz (vd số consumer).
func (s *Server) Info(k string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info[k] = v
}

// Pools kiểm mọi pool ping được.
func Pools(pools func() []*pgxpool.Pool) Check {
	return func(ctx context.Context) error {
		for _, p := range pools() {
			if err := p.Ping(ctx); err != nil {
				return err
			}
		}
		return nil
	}
}

// Closed kiểm một kết nối (vd amqp.Connection.IsClosed) còn mở.
func Closed(isClosed func() bool) Check {
	return func(context.Context) error {
		if isClosed() {
			return errors.New("closed")
		}
		return nil
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Handler trả bộ định tuyến /healthz, /readyz.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "zone": s.zone})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		s.mu.Lock()
		checks := append([]named(nil), s.checks...)
		body := map[string]any{"status": "ready", "zone": s.zone}
		for k, v := range s.info {
			body[k] = v
		}
		s.mu.Unlock()
		for _, c := range checks {
			if err := c.check(ctx); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": c.name + "_unavailable", "zone": s.zone})
				return
			}
		}
		writeJSON(w, http.StatusOK, body)
	})
	return mux
}

// Start lắng nghe addr (rỗng → AG_HEALTH_ADDR, rồi def) ở nền; lỗi lắng nghe gửi vào errc. Trả hàm dừng.
func (s *Server) Start(addr, def string, log *slog.Logger, errc chan<- error) func() {
	if addr == "" {
		addr = os.Getenv(EnvAddr)
	}
	if addr == "" {
		addr = def
	}
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	if log != nil {
		log.Info("máy chủ sức khỏe", "event", "startup", "addr", addr, "outcome", "ok")
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// Healthcheck gọi url (địa chỉ loopback trong cấu hình healthcheck Docker distroless) và trả mã thoát 0 khi 200.
func Healthcheck(url string) int {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url) //nolint:gosec,noctx // URL cố định trong cấu hình healthcheck
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

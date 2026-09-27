// Package telemetry: log có cấu trúc và trace_id xuyên suốt (mục 19).
//
// Trường log chuẩn (R-91): ts, level, service, trace_id, tenant_id, actor_ref,
// event, outcome. KHÔNG có trường chứa dữ liệu cá nhân (R-14) — chỉ mã tham
// chiếu. Không log bí mật, token, URL có chữ ký, nội dung ảnh (R-92).
//
// trace_id theo W3C traceparent, log JSON qua slog. Từ G1: OpenTelemetry xuất
// OTLP (trace + metric) khi đặt AG_OTLP_ENDPOINT — xem otel.go và docs/adr/0013.
package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

type traceKey struct{}

// NewLogger tạo logger JSON với trường service và khóa thời gian "ts".
func NewLogger(service string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Key = "ts"
			}
			return a
		},
	})
	return slog.New(h).With("service", service)
}

var traceparentRe = regexp.MustCompile(`^[0-9a-f]{2}-([0-9a-f]{32})-[0-9a-f]{16}-[0-9a-f]{2}$`)

// TraceIDFromHeader lấy trace_id từ header traceparent (W3C), rỗng nếu không hợp lệ.
func TraceIDFromHeader(traceparent string) string {
	m := traceparentRe.FindStringSubmatch(strings.TrimSpace(traceparent))
	if m == nil || m[1] == strings.Repeat("0", 32) {
		return ""
	}
	return m[1]
}

// NewTraceID sinh trace_id 16 byte hex.
func NewTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WithTraceID gắn trace_id vào context.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

// TraceID lấy trace_id từ context; chưa gắn thì lấy của span OTel đang hoạt động (nếu có).
func TraceID(ctx context.Context) string {
	if s, _ := ctx.Value(traceKey{}).(string); s != "" {
		return s
	}
	return spanTraceID(ctx)
}

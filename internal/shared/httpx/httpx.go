// Package httpx là middleware HTTP dùng chung cho mọi bề mặt API (mục 11).
package httpx

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// TraceHeader trả trace_id về client ở mọi phản hồi (R-63).
const TraceHeader = "X-Trace-Id"

// NewEngine tạo Gin engine với middleware chuẩn: trace, log, recovery, giới hạn thân.
func NewEngine(log *slog.Logger, maxBody int64) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(Trace(), AccessLog(log), Recover(log), BodyLimit(maxBody))
	r.NoRoute(func(c *gin.Context) { Fail(c, problem.NotFound) })
	r.NoMethod(func(c *gin.Context) {
		Fail(c, problem.New(http.StatusMethodNotAllowed, "method_not_allowed", "Phương thức không được hỗ trợ"))
	})
	return r
}

// Trace đọc traceparent hoặc sinh trace_id mới.
//
// Khi OpenTelemetry bật (telemetry.Enabled), mở span máy chủ là con của
// traceparent gửi tới (W3C), và trace_id lấy từ span đó — cùng giá trị ở
// header X-Trace-Id, trong problem+json và trong log. Khi tắt: như G0.
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		if telemetry.Enabled() {
			traceOTel(c)
			return
		}
		id := telemetry.TraceIDFromHeader(c.GetHeader("traceparent"))
		if id == "" {
			id = telemetry.NewTraceID()
		}
		c.Request = c.Request.WithContext(telemetry.WithTraceID(c.Request.Context(), id))
		c.Header(TraceHeader, id)
		c.Next()
	}
}

func traceOTel(c *gin.Context) {
	ctx := telemetry.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
	route := c.FullPath()
	name := c.Request.Method + " " + route
	if route == "" {
		name = c.Request.Method
	}
	ctx, span := telemetry.Tracer().Start(ctx, name, trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("http.request.method", c.Request.Method), attribute.String("http.route", route)))
	defer span.End()
	id := span.SpanContext().TraceID().String()
	if !span.SpanContext().IsValid() {
		id = telemetry.NewTraceID()
	}
	c.Request = c.Request.WithContext(telemetry.WithTraceID(ctx, id))
	c.Header(TraceHeader, id)
	c.Next()
	status := c.Writer.Status()
	span.SetAttributes(attribute.Int("http.response.status_code", status))
	if status >= 500 {
		span.SetStatus(codes.Error, http.StatusText(status))
	}
}

// AccessLog ghi một dòng cho mỗi request với trường chuẩn R-91. Không ghi
// query string, header hay thân (có thể chứa dữ liệu cá nhân hoặc token).
func AccessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		ctx := c.Request.Context()
		attrs := []any{
			"trace_id", telemetry.TraceID(ctx), "event", "http.request",
			"method", c.Request.Method, "route", c.FullPath(), "status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(), "outcome", outcome(c.Writer.Status()),
		}
		telemetry.RecordHTTP(ctx, c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(start))
		if p, ok := authz.FromContext(ctx); ok {
			attrs = append(attrs, "tenant_id", p.TenantID, "actor_ref", p.ActorRef)
		}
		log.Info("request", attrs...)
	}
}

func outcome(status int) string {
	switch {
	case status >= 500:
		return "error"
	case status >= 400:
		return "rejected"
	default:
		return "ok"
	}
}

// Recover biến panic thành 500 problem, không lộ chi tiết.
func Recover(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic", "trace_id", telemetry.TraceID(c.Request.Context()), "route", c.FullPath(), "event", "http.panic")
				Fail(c, problem.Internal)
			}
		}()
		c.Next()
	}
}

// BodyLimit giới hạn kích thước thân request.
func BodyLimit(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if n > 0 && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		}
		c.Next()
	}
}

// Authenticate xác minh Bearer token, gắn Principal và tenant vào context.
// Tenant CHỈ lấy từ token (R-16).
func Authenticate(v *authz.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		raw, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || raw == "" {
			c.Header("WWW-Authenticate", `Bearer realm="agrigreen"`)
			Fail(c, problem.Unauthenticated)
			return
		}
		p, err := v.Verify(c.Request.Context(), raw)
		if err != nil {
			c.Header("WWW-Authenticate", `Bearer realm="agrigreen", error="invalid_token"`)
			Fail(c, problem.Unauthenticated.WithDetail("Token không hợp lệ hoặc đã hết hạn"))
			return
		}
		ctx := authz.WithPrincipal(c.Request.Context(), p)
		ctx = tenant.WithID(ctx, p.TenantID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequireMFA chặn người gọi chưa qua yếu tố thứ hai — bắt buộc ở bề mặt Admin (mục 11).
func RequireMFA() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		if !ok {
			Fail(c, problem.Unauthenticated)
			return
		}
		if !p.MFA {
			Fail(c, problem.MFARequired)
			return
		}
		c.Next()
	}
}

// Fail ghi problem và dừng chuỗi handler. Lỗi 5xx ghi nguyên nhân vào log qua c.Error.
func Fail(c *gin.Context, err error) {
	p := problem.From(err)
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		p = problem.PayloadTooLarge
	}
	if p.Status >= 500 {
		_ = c.Error(err)
	}
	problem.Write(c.Writer, c.Request, p, telemetry.TraceID(c.Request.Context()))
	c.Abort()
}

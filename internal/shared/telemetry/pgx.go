package telemetry

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// PgxTracer trả pgx.QueryTracer tạo span con cho mỗi câu lệnh SQL.
//
// Tự viết thay cho thư viện bên thứ ba để kiểm soát chặt thuộc tính (R-14):
//   - KHÔNG ghi tham số ($1, $2, ...) và KHÔNG ghi văn bản SQL — chỉ tên thao
//     tác (SELECT, INSERT, ...), tên database, và SQLSTATE khi lỗi.
//   - Không ghi thông điệp lỗi của PostgreSQL: Detail/Message có thể chứa giá trị dòng.
//   - Chỉ tạo span khi đã có span cha (request, consumer, job), để vòng quét nền
//     như publisher không sinh hàng nghìn trace gốc.
func PgxTracer() pgx.QueryTracer { return pgxTracer{} }

type pgxTracer struct{}

func (pgxTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}
	op := SQLOperation(data.SQL)
	attrs := []attribute.KeyValue{
		attribute.String("db.system.name", "postgresql"),
		attribute.String("db.operation.name", op),
	}
	if conn != nil && conn.Config() != nil {
		attrs = append(attrs, attribute.String("db.namespace", conn.Config().Database))
	}
	ctx, _ = Tracer().Start(ctx, "db "+op, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return ctx
}

func (pgxTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if data.Err != nil {
		code := "error"
		var pg *pgconn.PgError
		if errors.As(data.Err, &pg) {
			code = pg.Code
		}
		span.SetAttributes(attribute.String("db.response.status_code", code))
		span.SetStatus(codes.Error, code)
	}
	span.End()
}

// SQLOperation trả từ khóa đầu tiên (viết hoa) của câu lệnh sau khi bỏ chú
// thích đầu câu; từ khóa ngoài danh sách → OTHER. Chỉ dùng làm nhãn cardinality thấp.
func SQLOperation(sql string) string {
	s := strings.TrimSpace(sql)
	for strings.HasPrefix(s, "--") || strings.HasPrefix(s, "/*") {
		if strings.HasPrefix(s, "--") {
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return "UNKNOWN"
			}
			s = strings.TrimSpace(s[i+1:])
		} else {
			i := strings.Index(s, "*/")
			if i < 0 {
				return "UNKNOWN"
			}
			s = strings.TrimSpace(s[i+2:])
		}
	}
	end := strings.IndexFunc(s, func(r rune) bool { return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') })
	if end < 0 {
		end = len(s)
	}
	op := strings.ToUpper(s[:end])
	switch op {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH", "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE",
		"SET", "RESET", "CREATE", "ALTER", "DROP", "GRANT", "REVOKE", "COPY", "LOCK", "VALUES", "SHOW", "CALL", "DO", "EXPLAIN":
		return op
	}
	return "OTHER"
}

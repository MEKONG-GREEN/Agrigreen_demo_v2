// Package outbox ghi sự kiện vào bảng shared.outbox trong CÙNG giao dịch với
// thay đổi nghiệp vụ, và phát chúng lên hàng đợi (mục 12).
//
// Quy tắc: chỉ đánh dấu published_at SAU khi broker xác nhận (publisher
// confirm). Phát lặp là có thể — consumer phải idempotent theo event_id (R-61).
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Envelope là phong bì sự kiện trên dây (mục 12).
type Envelope struct {
	EventID       uuid.UUID       `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int             `json:"schema_version"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	RecordedAt    time.Time       `json:"recorded_at"`
	TraceID       string          `json:"trace_id,omitempty"`
	Producer      string          `json:"producer"`
	Payload       json.RawMessage `json:"payload"`

	// TraceParent là header W3C traceparent lúc ghi (cột trace_parent). Không
	// thuộc phong bì trên dây: publisher đưa nó vào header thông điệp (R-63, R-90).
	TraceParent string `json:"-"`
}

// R-64: <vùng>.<module>.<thực thể>.<động từ quá khứ>
var eventTypeRe = regexp.MustCompile(`^(pf|mkg|vault|edge)\.[a-z_]+\.[a-z_]+\.[a-z_]+$`)

// ValidateType kiểm tên sự kiện.
func ValidateType(t string) error {
	if !eventTypeRe.MatchString(t) {
		return fmt.Errorf("outbox: tên sự kiện %q sai quy ước <vùng>.<module>.<thực thể>.<động từ>", t)
	}
	return nil
}

// Kind là loại sự kiện trên outbox, quyết định vhost RabbitMQ mà outbox-publisher
// phát lên (ADR-0018, cột shared.outbox.kind — migration shared/000004).
type Kind string

const (
	// KindPlatform (mặc định, giá trị rỗng cũng là platform): exchange ag.events của
	// vhost nền tảng — mọi consumer nền tảng bind được. Từ vùng mkg chỉ được là
	// payload van công bố (contractcheck).
	KindPlatform Kind = "platform"
	// KindInternal: sự kiện nội bộ MKG, phát lên vhost mkg.internal (tài khoản nền
	// tảng không có quyền). Chỉ tên mkg.*, chỉ phát từ internal/mkg/**, payload
	// phải là struct //ag:internal-event (contractcheck).
	KindInternal Kind = "internal"
)

func (k Kind) orDefault() Kind {
	if k == "" {
		return KindPlatform
	}
	return k
}

// Event là sự kiện cần ghi. ID là UUIDv7, cũng là event_id.
type Event struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Type          string
	SchemaVersion int
	OccurredAt    time.Time
	TraceID       string
	Producer      string
	Payload       any
	// Kind rỗng = KindPlatform.
	Kind Kind
}

// Enqueue ghi sự kiện trong giao dịch nghiệp vụ đang mở. Ngữ cảnh trace của
// ctx (span OTel đang hoạt động) được lưu kèm để nối trace qua hàng đợi;
// TraceID rỗng thì lấy từ ctx.
func Enqueue(ctx context.Context, tx pgx.Tx, e Event) error {
	if err := ValidateType(e.Type); err != nil {
		return err
	}
	kind := e.Kind.orDefault()
	switch kind {
	case KindPlatform:
	case KindInternal:
		if !strings.HasPrefix(e.Type, "mkg.") {
			return fmt.Errorf("outbox: sự kiện nội bộ %q phải thuộc vùng mkg (ADR-0018)", e.Type)
		}
	default:
		return fmt.Errorf("outbox: loại sự kiện %q không hợp lệ", e.Kind)
	}
	if e.ID == uuid.Nil || e.TenantID == uuid.Nil || e.SchemaVersion < 1 || e.Producer == "" || e.OccurredAt.IsZero() {
		return errors.New("outbox: thiếu trường bắt buộc của sự kiện")
	}
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("outbox: payload: %w", err)
	}
	if e.TraceID == "" {
		e.TraceID = telemetry.TraceID(ctx)
	}
	var traceID, parent *string
	if e.TraceID != "" {
		traceID = &e.TraceID
	}
	if tp := telemetry.Traceparent(ctx); tp != "" {
		parent = &tp
	}
	_, err = tx.Exec(ctx, `INSERT INTO shared.outbox
	  (id, tenant_id, event_type, schema_version, occurred_at, trace_id, producer, payload, trace_parent, kind)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)`,
		// payload gửi dạng chuỗi: ở QueryExecModeExec (PgBouncer) []byte bị mã hóa thành bytea.
		e.ID, e.TenantID, e.Type, e.SchemaVersion, e.OccurredAt.UTC(), traceID, e.Producer, string(payload), parent, string(kind))
	return err
}

// Broker phát một phong bì và chỉ trả nil khi broker đã xác nhận bền vững.
type Broker interface {
	Publish(ctx context.Context, env Envelope) error
}

// Publisher quét outbox và phát. Pool phải dùng vai ag_outbox_publisher (BYPASSRLS).
// Mỗi Publisher chỉ quét MỘT loại (Kind rỗng = KindPlatform) và phát qua đúng
// Broker của vhost loại đó — sự kiện nội bộ MKG không bao giờ lên vhost nền tảng.
type Publisher struct {
	Pool   *pgxpool.Pool
	Broker Broker
	Batch  int
	Kind   Kind
	// PublishTimeout là hạn chờ MỘT lần phát (gửi + confirm); 0 = DefaultPublishTimeout. Broker treo
	// (connection.blocked, mạng nửa vời) không giữ giao dịch outbox và kết nối DB vô hạn.
	PublishTimeout time.Duration
}

// DefaultPublishTimeout là hạn chờ mặc định của một lần phát.
const DefaultPublishTimeout = 10 * time.Second

// idleInTxTimeout chặn giao dịch outbox treo idle-in-transaction (giữ khóa hàng, giữ kết nối PgBouncer) khi
// tiến trình kẹt ngoài DB. Hạn tính từ lệnh SQL cuối; mỗi sự kiện phát xong đều có một UPDATE, nên chỉ cần lớn
// hơn PublishTimeout.
const idleInTxTimeout = "60s"

// batchLockKey là khóa tư vấn (theo giao dịch) giữ thứ tự khi nhiều bản publisher chạy song song: chỉ một
// bản phát một loại (Kind) của một cụm tại một thời điểm. SKIP LOCKED một mình không đủ — bản B có thể bỏ
// qua sự kiện X đang bị bản A giữ (lỗi, đang thử lại) rồi phát X+1 trước X.
func batchLockKey(k Kind) string { return "ag.outbox.publish:" + string(k) }

// ParkAfterRejections: số lần broker từ chối (ErrRejected) một sự kiện trước khi publisher gác nó (parked_at,
// shared 000005) để sự kiện sau không bị chặn mãi. Lỗi kết nối không bao giờ dẫn tới gác.
const ParkAfterRejections = 5

// PublishBatch phát tối đa Batch sự kiện chưa phát, theo thứ tự id (UUIDv7 ~
// thứ tự ghi). Dừng ở lỗi đầu tiên để giữ thứ tự — trừ sự kiện bị broker từ chối
// đủ ParkAfterRejections lần: gác rồi đi tiếp. Trả số sự kiện đã phát.
func (p *Publisher) PublishBatch(ctx context.Context) (int, error) {
	batch := p.Batch
	if batch <= 0 {
		batch = 100
	}
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, "SELECT set_config('idle_in_transaction_session_timeout', $1, true)", idleInTxTimeout); err != nil {
		return 0, err
	}
	var leader bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", batchLockKey(p.Kind.orDefault())).Scan(&leader); err != nil {
		return 0, err
	}
	if !leader {
		return 0, nil // bản khác đang phát loại này: chờ lượt sau
	}

	rows, err := tx.Query(ctx, `SELECT id, tenant_id, event_type, schema_version, occurred_at, recorded_at,
	    coalesce(trace_id, ''), producer, payload, coalesce(trace_parent, ''), attempts
	  FROM shared.outbox WHERE published_at IS NULL AND parked_at IS NULL AND kind = $2
	  ORDER BY id FOR UPDATE SKIP LOCKED LIMIT $1`, batch, string(p.Kind.orDefault()))
	if err != nil {
		return 0, err
	}
	var envs []Envelope
	var attempts []int
	for rows.Next() {
		var e Envelope
		var n int
		if err := rows.Scan(&e.EventID, &e.TenantID, &e.EventType, &e.SchemaVersion, &e.OccurredAt, &e.RecordedAt,
			&e.TraceID, &e.Producer, &e.Payload, &e.TraceParent, &n); err != nil {
			rows.Close()
			return 0, err
		}
		envs = append(envs, e)
		attempts = append(attempts, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	sent := 0
	var pubErr error
	for i, e := range envs {
		if err := p.publishOne(ctx, e); err != nil {
			pubErr = err
			msg := err.Error()
			if len(msg) > 500 {
				msg = msg[:500]
			}
			if errors.Is(err, ErrRejected) && attempts[i]+1 >= ParkAfterRejections {
				// Sự kiện độc: gác để không chặn sự kiện sau (mất thứ tự với riêng sự kiện này — chấp nhận, nó không
				// bao giờ phát được). Metric ag.outbox.parked báo để người vận hành xử lý.
				if _, uerr := tx.Exec(ctx, "UPDATE shared.outbox SET attempts = attempts + 1, last_error = $2, parked_at = now() WHERE id = $1",
					e.EventID, msg); uerr != nil {
					return sent, uerr
				}
				pubErr = fmt.Errorf("outbox: gác sự kiện %s (%s) sau %d lần broker từ chối: %w", e.EventID, e.EventType, attempts[i]+1, err)
				continue
			}
			// attempts chỉ đếm lần broker TỪ CHỐI (quyết định gác): lỗi kết nối/hết hạn chờ không đẩy sự kiện tới gác.
			if _, uerr := tx.Exec(ctx, `UPDATE shared.outbox SET attempts = attempts + CASE WHEN $3 THEN 1 ELSE 0 END,
			    last_error = $2 WHERE id = $1`, e.EventID, msg, errors.Is(err, ErrRejected)); uerr != nil {
				return sent, uerr
			}
			break
		}
		// CHỈ sau confirm.
		if _, err := tx.Exec(ctx, "UPDATE shared.outbox SET published_at = now(), attempts = attempts + 1, last_error = NULL WHERE id = $1", e.EventID); err != nil {
			return sent, err
		}
		sent++
	}
	if err := tx.Commit(ctx); err != nil {
		return sent, err
	}
	return sent, pubErr
}

// publishOne phát trong span producer là con của trace lúc ghi outbox. OTel
// tắt → gọi thẳng broker như G0.
func (p *Publisher) publishOne(ctx context.Context, e Envelope) error {
	timeout := p.PublishTimeout
	if timeout <= 0 {
		timeout = DefaultPublishTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if !telemetry.Enabled() {
		return p.Broker.Publish(ctx, e)
	}
	pctx := telemetry.ContextWithParent(ctx, e.TraceParent, e.TraceID)
	pctx, span := telemetry.Tracer().Start(pctx, "publish "+e.EventType, trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination.name", Exchange),
			attribute.String("messaging.message.id", e.EventID.String()),
		))
	defer span.End()
	err := p.Broker.Publish(pctx, e)
	if err != nil {
		span.SetStatus(codes.Error, "publish failed")
	}
	return err
}

// Run lặp PublishBatch cho tới khi ctx hủy.
func (p *Publisher) Run(ctx context.Context, idle time.Duration, onErr func(error)) {
	for {
		n, err := p.PublishBatch(ctx)
		if err != nil && onErr != nil && !errors.Is(err, context.Canceled) {
			onErr(err)
		}
		if n > 0 && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(idle):
		}
	}
}

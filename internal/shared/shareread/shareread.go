// Package shareread ghi nhật ký đọc tại chỗ qua cấp phép chia sẻ (W4.2, ADR-0030 mục 4, kiến trúc v4.1 §17.3).
//
// Đơn vị xem đọc dữ liệu của đơn vị chủ qua CÙNG port/endpoint của module chủ; RLS (p_share, pf 000136) chỉ mở nhánh
// chia sẻ khi giao dịch có app.purpose (mục đích xử lý) khớp một cấp phép intra_platform đang hiệu lực. Vì vậy:
//
//   - Middleware đọc mục đích từ header X-Purpose (R-42); mã đúng dạng (như purpose_code của cấp phép) được gắn vào
//     context (authz.WithPurpose → db đặt app.purpose). Không có mục đích (hoặc sai dạng — để module tự xử lý như
//     trước) → nhánh chia sẻ không thể trả dòng nào → middleware đi thẳng.
//   - Đệm phản hồi CHỈ khi lúc handler bắt đầu ghi phản hồi đã có bản ghi qua cấp phép (port gọi Note khi trả dữ
//     liệu, trước khi handler ghi): phản hồi khác — kể cả tải tệp có X-Purpose của pf/sync — đi thẳng, không đệm.
//   - Port của module chủ đánh dấu bản ghi có tenant ≠ tenant phiên (Via) — cờ via_share_grant — và gọi Note.
//   - Cuối yêu cầu, nếu có bản ghi qua cấp phép, middleware ghi MỘT sự kiện gộp (đơn vị xem, mục đích, route, theo
//     bảng × đơn vị chủ: số bản ghi) qua Sink, và tăng chỉ số "đọc qua cấp phép theo đơn vị" (telemetry.MetricShareReads).
//   - Đóng khi lỗi (giữ nguyên tắc ADR-0020): Sink lỗi → 503 share.read_log_unavailable, phản hồi đã đệm bị bỏ —
//     không dữ liệu đọc qua cấp phép nào ra khỏi máy chủ khi không ghi được nhật ký.
//
// Gói này không biết module nào (internal/shared): Sink do pf/party cài (sự kiện pf.party.share_read.recorded).
package shareread

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// HeaderPurpose là header mang mã mục đích xử lý (R-42) — cùng tên với các module đã đọc nó (sync, vault).
const HeaderPurpose = "X-Purpose"

// rePurpose: dạng mã mục đích, khớp CHECK purpose_code của pf_party.share_grant_terms (pf 000120).
var rePurpose = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}(\.[a-z][a-z0-9-]{0,39}){0,3}$`)

// ErrLogUnavailable: không ghi được nhật ký đọc qua cấp phép → không trả dữ liệu (đóng khi lỗi).
var ErrLogUnavailable = problem.New(http.StatusServiceUnavailable, "share.read_log_unavailable",
	"Không ghi được nhật ký đọc dữ liệu chia sẻ — thử lại sau")

// Read là số bản ghi một yêu cầu đã đọc qua cấp phép ở một bảng (khóa share_scope, vd pf_coop.plot) của một
// đơn vị chủ.
type Read struct {
	ModuleTable string    `json:"module_table"`
	OwnerTenant uuid.UUID `json:"owner_tenant_id"`
	Rows        int       `json:"rows"`
}

// Event là sự kiện gộp của một yêu cầu (một sự kiện mỗi yêu cầu có đọc qua cấp phép).
type Event struct {
	ViewerTenant uuid.UUID
	ActorRef     uuid.UUID
	Purpose      string
	Method       string
	Route        string // mẫu route (gin FullPath), không có tham số thật
	Reads        []Read // sắp theo (ModuleTable, OwnerTenant)
}

// Sink ghi bền sự kiện gộp (pf/party: outbox pf.party.share_read.recorded). Lỗi → 503.
type Sink func(ctx context.Context, e Event) error

type collector struct {
	mu    sync.Mutex
	reads map[Read]int // khóa Read{ModuleTable, OwnerTenant} (Rows = 0)
}

type ctxKey struct{}

// Via cho biết bản ghi của tenant rowTenant đến từ nhánh chia sẻ: khác tenant của phiên (tenant.FromContext).
// Không có tenant phiên (consumer, job) hoặc rowTenant rỗng → false.
func Via(ctx context.Context, rowTenant uuid.UUID) bool {
	tid, err := tenant.FromContext(ctx)
	return err == nil && rowTenant != uuid.Nil && rowTenant != tid
}

// Note cộng n bản ghi đọc qua cấp phép ở bảng moduleTable của đơn vị chủ owner vào sự kiện gộp của yêu cầu. Ngoài
// Middleware (consumer, job, kiểm thử) → không làm gì.
func Note(ctx context.Context, moduleTable string, owner uuid.UUID, n int) {
	c, ok := ctx.Value(ctxKey{}).(*collector)
	if !ok || n <= 0 || owner == uuid.Nil {
		return
	}
	c.mu.Lock()
	c.reads[Read{ModuleTable: moduleTable, OwnerTenant: owner}] += n
	c.mu.Unlock()
}

// Mark là tiện ích cho port: với mỗi tenant bản ghi (owners[i]), trả via[i] = Via(ctx, owners[i]) và Note gộp
// theo đơn vị chủ.
func Mark(ctx context.Context, moduleTable string, owners []uuid.UUID) []bool {
	via := make([]bool, len(owners))
	count := map[uuid.UUID]int{}
	for i, o := range owners {
		if Via(ctx, o) {
			via[i] = true
			count[o]++
		}
	}
	for o, n := range count {
		Note(ctx, moduleTable, o, n)
	}
	return via
}

// WithCollector gắn bộ gom vào ctx (Middleware dùng; kiểm thử port dùng trực tiếp) và trả hàm đọc kết quả.
func WithCollector(ctx context.Context) (context.Context, func() []Read) {
	c := &collector{reads: map[Read]int{}}
	return context.WithValue(ctx, ctxKey{}, c), c.list
}

func (c *collector) list() []Read {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Read, 0, len(c.reads))
	for k, n := range c.reads {
		k.Rows = n
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b Read) int {
		if c := strings.Compare(a.ModuleTable, b.ModuleTable); c != 0 {
			return c
		}
		return strings.Compare(a.OwnerTenant.String(), b.OwnerTenant.String())
	})
	return out
}

// Middleware gắn mục đích + bộ gom, đệm phản hồi khi yêu cầu có mục đích, và ghi một sự kiện gộp mỗi yêu cầu có
// đọc qua cấp phép (đóng khi lỗi). Đặt SAU xác thực (cần tenant, Principal). sink nil → mọi yêu cầu có đọc qua
// cấp phép bị từ chối 503 (chưa nối nhật ký thì không mở chia sẻ).
func Middleware(sink Sink) gin.HandlerFunc {
	return func(c *gin.Context) {
		purpose := strings.TrimSpace(c.GetHeader(HeaderPurpose))
		if purpose == "" || !rePurpose.MatchString(purpose) {
			c.Next()
			return
		}
		ctx, reads := WithCollector(authz.WithPurpose(c.Request.Context(), purpose))
		c.Request = c.Request.WithContext(ctx)
		orig := c.Writer
		buf := &bufWriter{ResponseWriter: orig, shared: func() bool { return len(reads()) > 0 }}
		c.Writer = buf
		c.Next()
		c.Writer = orig
		rs := reads()
		if len(rs) == 0 {
			buf.flush()
			return
		}
		if buf.mode == modeDirect {
			// Port gọi Note SAU khi handler đã ghi phản hồi (vi phạm quy ước) — không còn đóng được; vẫn ghi nhật ký.
			_ = c.Error(errors.New("shareread: Note sau khi phản hồi đã ghi — không đệm được (port phải Note trước khi trả)"))
			if sink != nil {
				if err := sink(ctx, Event{Purpose: purpose, Method: c.Request.Method, Route: c.FullPath(), Reads: rs,
					ViewerTenant: viewerOf(ctx), ActorRef: actorOf(ctx)}); err != nil {
					_ = c.Error(err)
				}
			}
			return
		}
		e := Event{Purpose: purpose, Method: c.Request.Method, Route: c.FullPath(), Reads: rs, ViewerTenant: viewerOf(ctx), ActorRef: actorOf(ctx)}
		var err error
		if sink == nil {
			err = ErrLogUnavailable
		} else {
			err = sink(ctx, e)
		}
		if err != nil {
			_ = c.Error(err)
			h := orig.Header()
			for k := range h {
				delete(h, k)
			}
			problem.Write(orig, c.Request, ErrLogUnavailable, telemetry.TraceID(ctx))
			return
		}
		record(ctx, e)
		buf.flush()
	}
}

func viewerOf(ctx context.Context) uuid.UUID {
	tid, _ := tenant.FromContext(ctx)
	return tid
}

func actorOf(ctx context.Context) uuid.UUID {
	if p, ok := authz.FromContext(ctx); ok {
		return p.ActorRef
	}
	return uuid.Nil
}

const (
	modeUndecided = iota
	modeBuffer    // đã có đọc qua cấp phép khi bắt đầu ghi → giữ tới khi nhật ký ghi xong
	modeDirect    // không có → ghi thẳng
)

// bufWriter quyết định ở lần ghi đầu tiên: có bản ghi qua cấp phép → giữ phản hồi trong bộ nhớ tới khi nhật ký ghi
// được; không → ghi thẳng writer gốc (không tốn bộ nhớ cho tải tệp, phản hồi lớn). Header ghi thẳng vào writer gốc.
type bufWriter struct {
	gin.ResponseWriter
	shared func() bool
	mode   int
	status int
	buf    bytes.Buffer
}

func (w *bufWriter) decide() int {
	if w.mode == modeUndecided {
		w.mode = modeDirect
		if w.shared() {
			w.mode = modeBuffer
		}
	}
	return w.mode
}

func (w *bufWriter) WriteHeader(code int) {
	if w.decide() == modeDirect {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.status == 0 {
		w.status = code
	}
}
func (w *bufWriter) WriteHeaderNow() {
	if w.decide() == modeDirect {
		w.ResponseWriter.WriteHeaderNow()
		return
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
}
func (w *bufWriter) Write(b []byte) (int, error) {
	if w.decide() == modeDirect {
		return w.ResponseWriter.Write(b)
	}
	w.WriteHeaderNow()
	return w.buf.Write(b)
}
func (w *bufWriter) WriteString(s string) (int, error) {
	if w.decide() == modeDirect {
		return w.ResponseWriter.WriteString(s)
	}
	w.WriteHeaderNow()
	return w.buf.WriteString(s)
}
func (w *bufWriter) Status() int {
	if w.mode != modeBuffer {
		return w.ResponseWriter.Status()
	}
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
func (w *bufWriter) Size() int {
	if w.mode != modeBuffer {
		return w.ResponseWriter.Size()
	}
	if w.status == 0 {
		return -1
	}
	return w.buf.Len()
}
func (w *bufWriter) Written() bool {
	if w.mode != modeBuffer {
		return w.ResponseWriter.Written()
	}
	return w.status != 0
}
func (w *bufWriter) Flush() {
	if w.mode == modeDirect {
		w.ResponseWriter.Flush()
	}
}

// flush đẩy phản hồi đã giữ ra writer gốc (chế độ ghi thẳng: không còn gì để đẩy).
func (w *bufWriter) flush() {
	if w.mode != modeBuffer || w.status == 0 {
		return
	}
	w.ResponseWriter.WriteHeader(w.status)
	if w.buf.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.buf.Bytes())
	}
}

var (
	readsOnce    sync.Once
	readsCounter metric.Int64Counter
)

// record tăng chỉ số đọc qua cấp phép theo đơn vị xem × bảng (telemetry.MetricShareReads). No-op khi OTel tắt.
func record(ctx context.Context, e Event) {
	if !telemetry.Enabled() {
		return
	}
	readsOnce.Do(func() {
		readsCounter, _ = telemetry.Meter().Int64Counter(telemetry.MetricShareReads, metric.WithUnit("{record}"),
			metric.WithDescription("Bản ghi đọc tại chỗ qua cấp phép chia sẻ theo đơn vị xem và bảng (ADR-0030 mục 4)"))
	})
	if readsCounter == nil {
		return
	}
	for _, r := range e.Reads {
		readsCounter.Add(ctx, int64(r.Rows), metric.WithAttributes(attribute.String("viewer_tenant", e.ViewerTenant.String()),
			attribute.String("module_table", r.ModuleTable)))
	}
}

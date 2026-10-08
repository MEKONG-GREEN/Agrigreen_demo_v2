// Package public là bề mặt Public API cho bên thứ ba (partner.<domain>, tiến trình cmd/api-public —
// ADR-0022; contracts/openapi/public.yaml). Mỗi lời gọi:
//
//  1. X-API-Key → port.Authenticator → port.Caller trong context (thất bại → 401, KHÔNG ghi sổ; chỉ
//     log key_prefix + metric ag.public.unauthenticated);
//  2. giới hạn tần suất theo khách hàng (RateLimiter) → 429 apiclient.rate_limited + Retry-After;
//  3. handler dựng phản hồi vào BỘ ĐỆM: scope ∧ cấp quyền dp_<sản phẩm> (party/port.GrantLookup) ∧
//     sản phẩm phục vụ được (draft không phục vụ ở production) — thiếu → 403 apiclient.forbidden; hạn
//     mức sớm qua metering/port.Quotas; mỗi phần tử qua hợp đồng contracts/dataproduct (mặc định từ
//     chối);
//  4. metering/port.Ledger.Record ghi MỘT dòng sổ sử dụng (+ egress) TRƯỚC khi gửi byte nào: lỗi →
//     503 metering.unavailable, không có dữ liệu; vượt hạn mức → 429 metering.quota_exceeded;
//  5. gửi header (AG-Data-Contract, RateLimit-*, AG-Quota-Remaining, AG-Request-Id) + thân.
//
// Tenant của bên gọi luôn lấy từ bản ghi khóa (Caller.Client.ClientTenantID), không bao giờ từ
// path/query/body; tham số grantor chỉ CHỌN bên cấp.
package public

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/ingest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// GrantorLister (tùy chọn) liệt kê mọi bên cấp đang có cấp quyền loại kind cho grantee — chỉ để
// điền danh mục /v1/data-products. Không có → dùng GrantLookup.ActiveGrant(grantor = Nil).
type GrantorLister interface {
	ActiveGrantors(ctx context.Context, grantee uuid.UUID, kind string, at time.Time) ([]uuid.UUID, error)
}

// Deps là phụ thuộc của Public API. Thiếu Ledger / Grants → mọi lời gọi đã xác thực trả 503 / 403
// (đóng khi lỗi, mặc định từ chối).
type Deps struct {
	Svc     *app.Service // pf/apiclient: việc xuất, tệp
	Auth    port.Authenticator
	Plans   port.Plans
	Limiter RateLimiter

	Ledger meter.Ledger
	Quotas meter.Quotas
	Usage  meter.Usage
	Grants partyport.GrantLookup
	// Reads (K-05, KN US-MRV-05): sổ đọc partner_read_log ở tenant bên cấp cho mỗi lần đọc / tải dữ
	// liệu 2xx — ghi TRƯỚC sổ đo lường; lỗi → 503 party.partner_audit_unavailable, không có dữ liệu.
	// Nil → không ghi sổ đọc (hành vi trước tích hợp E2E G5).
	Reads  partyport.APIReadLog
	Params params.Params // publicdata.k_min (nil → k_default của hợp đồng)

	Collection port.CollectionVolumeSource
	Hubs       port.HubCapacitySource
	MRV        port.MRVPackageSource

	// Ingest: nạp theo lô (W5.4, POST/GET /v1/batches). Nil → 503 apiclient.dependency_unavailable.
	Ingest *ingest.Service

	OperatorTenant uuid.UUID
	Production     bool // residency.Production: hợp đồng draft không phục vụ
	Clock          clock.Clock
	Log            *slog.Logger
	// Ready là các kiểm tra sẵn sàng của /readyz (ping pool).
	Ready []func(context.Context) error
}

// Server là Public API đã nối.
type Server struct {
	d      Deps
	unauth metric.Int64Counter
	rated  metric.Int64Counter
}

// maxBody giới hạn thân request (gateway cũng giới hạn — R-55).
const maxBody = 1 << 20

// Metric (cardinality thấp — R-91).
const (
	MetricUnauthenticated = "ag.public.unauthenticated" // → ag_public_unauthenticated_total
	MetricRateLimited     = "ag.public.rate_limited"
)

// NewRouter dựng Gin engine của Public API (chỉ route Public — ADR-0022 mục 1).
func NewRouter(d Deps) *gin.Engine {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Clock == nil {
		d.Clock = clock.System{}
	}
	if d.Limiter == nil {
		d.Limiter = NewMemory(1)
	}
	s := &Server{d: d}
	s.unauth, _ = telemetry.Meter().Int64Counter(MetricUnauthenticated, metric.WithDescription("Lời gọi Public API không xác thực được"))
	s.rated, _ = telemetry.Meter().Int64Counter(MetricRateLimited, metric.WithDescription("Lời gọi Public API vượt giới hạn tần suất"))

	r := httpx.NewEngine(d.Log, maxBody)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		for _, f := range d.Ready {
			if err := f(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	v1 := r.Group("/v1", s.authenticate)
	v1.GET("/data-products", s.handle(spec{kind: meter.CallMeta}, s.catalog))
	v1.GET("/data/collection-volumes", s.handle(spec{kind: meter.CallRead, product: port.ProductCollectionVolume, action: port.ActionRead}, s.collection))
	v1.POST("/data/collection-volumes/exports", s.handle(spec{kind: meter.CallExportRequest, product: port.ProductCollectionVolume, action: port.ActionExport}, s.exportCollection))
	v1.GET("/data/hub-capacities", s.handle(spec{kind: meter.CallRead, product: port.ProductHubCapacity, action: port.ActionRead}, s.hubs))
	v1.GET("/data/mrv-packages", s.handle(spec{kind: meter.CallRead, product: port.ProductMRVPackageStatus, action: port.ActionRead}, s.mrvPackages))
	v1.GET("/data/mrv-packages/:export_id", s.handle(spec{kind: meter.CallRead, product: port.ProductMRVPackageStatus, action: port.ActionRead}, s.mrvPackage))
	v1.GET("/jobs/:id", s.handle(spec{kind: meter.CallJobStatus}, s.job))
	v1.GET("/jobs/:id/file", s.handle(spec{kind: meter.CallFileDownload, action: port.ActionExport}, s.jobFile))
	v1.GET("/usage", s.handle(spec{kind: meter.CallMeta, scope: port.ScopeUsageRead}, s.usage))
	registerBatches(s, v1) // W5.4
	return r
}

// ---------------------------------------------------------------------------
// Xác thực
// ---------------------------------------------------------------------------

func (s *Server) authenticate(c *gin.Context) {
	ctx := c.Request.Context()
	presented := c.GetHeader(port.KeyHeader)
	caller, err := s.d.Auth.Authenticate(ctx, presented, s.d.Clock.Now())
	if err != nil {
		p := problem.From(err)
		if p.Code == port.ErrUnauthenticated.Code {
			s.unauth.Add(ctx, 1)
			attrs := []any{"event", "public.unauthenticated", "trace_id", telemetry.TraceID(ctx), "outcome", "rejected"}
			if prefix, ok := port.ParseKey(presented); ok {
				attrs = append(attrs, "key_prefix", prefix) // KHÔNG BAO GIỜ log cả khóa
			}
			s.d.Log.WarnContext(ctx, "Public API: khóa không hợp lệ", attrs...)
			c.Header("WWW-Authenticate", `ApiKey realm="agrigreen-partner", header="X-API-Key"`)
			httpx.Fail(c, port.ErrUnauthenticated)
			return
		}
		s.d.Log.ErrorContext(ctx, "Public API: lỗi xác thực", "event", "public.auth_error", "trace_id", telemetry.TraceID(ctx), "outcome", "error", "code", p.Code)
		if p.Status < 500 {
			p = port.ErrUnauthenticated
		}
		httpx.Fail(c, p)
		return
	}
	ctx = port.WithCaller(ctx, caller)
	ctx = tenant.WithID(ctx, caller.Client.ClientTenantID)
	c.Request = c.Request.WithContext(ctx)
	c.Header(port.HeaderRequestID, caller.RequestID.String())
	c.Next()
}

// ---------------------------------------------------------------------------
// Đường ống: phản hồi đệm → sổ đo lường → gửi
// ---------------------------------------------------------------------------

type spec struct {
	kind    meter.CallKind
	product string
	action  string
	scope   string
}

type result struct {
	status          int
	header          http.Header
	body            []byte
	stream          io.ReadCloser // tệp xuất (thay body); size đã biết trước
	size            int64
	rows            int64
	dataTenant      uuid.UUID             // bên cấp có dữ liệu được phục vụ (2xx)
	grant           *partyport.ShareGrant // cấp quyền dp_* đã kiểm cho dữ liệu được phục vụ (sổ đọc K-05)
	product         string
	contractVersion int
	replayed        bool
	jobID           uuid.UUID
	problemCode     string
}

func newResult(status int) *result { return &result{status: status, header: http.Header{}} }

func jsonResult(status int, v any) (*result, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	r := newResult(status)
	r.body = append(b, '\n')
	r.header.Set("Content-Type", "application/json; charset=utf-8")
	return r, nil
}

type bufWriter struct {
	h      http.Header
	status int
	buf    bytes.Buffer
}

func (b *bufWriter) Header() http.Header         { return b.h }
func (b *bufWriter) Write(p []byte) (int, error) { return b.buf.Write(p) }
func (b *bufWriter) WriteHeader(s int)           { b.status = s }

// failResult dựng phản hồi lỗi RFC 9457 vào bộ đệm (lỗi 5xx: ghi nguyên nhân vào log, không ra ngoài).
func (s *Server) failResult(c *gin.Context, err error) *result {
	p := problem.From(err)
	if p.Status >= 500 {
		s.d.Log.ErrorContext(c.Request.Context(), "Public API: lỗi", "event", "public.error", "route", c.FullPath(), "code", p.Code,
			"trace_id", telemetry.TraceID(c.Request.Context()), "error", err.Error(), "outcome", "error")
	}
	w := &bufWriter{h: http.Header{}}
	problem.Write(w, c.Request, p, telemetry.TraceID(c.Request.Context()))
	r := newResult(p.Status)
	r.body = w.buf.Bytes()
	r.header.Set("Content-Type", problem.ContentType)
	r.problemCode = p.Code
	return r
}

func routeTemplate(c *gin.Context) string {
	parts := strings.Split(c.FullPath(), "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return c.Request.Method + " " + strings.Join(parts, "/")
}

func seconds(d time.Duration) string {
	if d <= 0 {
		return "1"
	}
	return strconv.FormatInt(int64(math.Ceil(d.Seconds())), 10)
}

// nextBusinessDay là đầu ngày nghiệp vụ kế tiếp (giờ VN) — cửa sổ hạn mức ngày.
func nextBusinessDay(t time.Time) time.Time {
	return clock.StartOfBusinessDate(clock.BusinessDate(t.Add(24 * time.Hour)))
}

func isCode(err error, code string) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Code == code
}

type handlerFn func(c *gin.Context, caller port.Caller, plan port.Plan) (*result, error)

func (s *Server) handle(sp spec, fn handlerFn) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		caller, ok := port.CallerFrom(ctx)
		if !ok {
			httpx.Fail(c, port.ErrUnauthenticated)
			return
		}
		var (
			res  *result
			rate RateDecision
			err  error
		)
		plan, perr := s.d.Plans.PlanFor(ctx, s.d.OperatorTenant, caller.Client.ID, caller.ReceivedAt)
		switch {
		case perr != nil:
			res = s.failResult(c, perr)
		default:
			rate = s.d.Limiter.Allow(caller.Client.ID, plan.RequestsPerMinute, caller.ReceivedAt)
			if !rate.Allowed {
				s.rated.Add(ctx, 1, metric.WithAttributes(attribute.String("route", c.FullPath())))
				res = s.failResult(c, port.ErrRateLimited)
				res.header.Set("Retry-After", seconds(rate.Reset))
			} else if res, err = fn(c, caller, plan); err != nil {
				res = s.failResult(c, err)
			}
		}
		if res.problemCode == meter.ErrQuotaExceeded.Code && res.header.Get("Retry-After") == "" {
			res.header.Set("Retry-After", seconds(nextBusinessDay(caller.ReceivedAt).Sub(caller.ReceivedAt)))
		}
		if res.product == "" {
			res.product = sp.product
		}
		// Sổ đọc của bên cấp (K-05) TRƯỚC sổ đo lường: lỗi → 503, không có dữ liệu; sổ đo lường ghi
		// lời gọi với status 503, 0 đơn vị.
		if res.grant != nil && res.status >= 200 && res.status <= 299 && s.d.Reads != nil {
			q := c.Request.URL.RawQuery
			if rerr := s.d.Reads.RecordRead(ctx, *res.grant, partyport.PartnerRead{Kind: partyport.DataProductKind(res.product),
				Endpoint: routeTemplate(c), Query: q, Items: int(res.rows)}); rerr != nil {
				if res.stream != nil {
					_ = res.stream.Close()
				}
				prod := res.product
				res = s.failResult(c, partyport.ErrPartnerAuditUnavailable.WithCause(rerr))
				res.product = prod
			}
		}
		scope := sp.scope
		if scope == "" && res.product != "" && sp.action != "" {
			scope = port.ProductScope(res.product, sp.action)
		}
		call := meter.Call{RequestID: caller.RequestID, ReceivedAt: caller.ReceivedAt, ClientID: caller.Client.ID,
			ClientTenantID: caller.Client.ClientTenantID, KeyPrefix: caller.KeyPrefix, AuthMethod: string(caller.AuthMethod), Kind: sp.kind,
			Product: res.product, ContractVersion: res.contractVersion, Scope: scope, RouteTemplate: routeTemplate(c), Status: res.status,
			ProblemCode: res.problemCode, Replayed: res.replayed, JobID: res.jobID, TraceID: telemetry.TraceID(ctx)}
		if res.status >= 200 && res.status <= 299 {
			call.DataTenantID, call.Rows = res.dataTenant, res.rows
		}
		call.Bytes = int64(len(res.body))
		if res.stream != nil {
			call.Bytes = res.size
		}
		call.Latency = s.d.Clock.Now().Sub(caller.ReceivedAt)

		// Sổ đo lường TRƯỚC khi gửi byte nào (ADR-0023 mục 1) — đóng khi lỗi.
		var rec meter.Recorded
		if s.d.Ledger == nil {
			err = errors.New("apiclient: chưa nối metering/port.Ledger")
		} else {
			rec, err = s.d.Ledger.Record(ctx, s.d.OperatorTenant, call)
		}
		if err != nil {
			if res.stream != nil {
				_ = res.stream.Close()
			}
			if !isCode(err, meter.ErrQuotaExceeded.Code) {
				s.d.Log.ErrorContext(ctx, "Public API: không ghi được sổ đo lường — không trả dữ liệu", "event", "public.metering_unavailable",
					"request_id", caller.RequestID, "trace_id", telemetry.TraceID(ctx), "outcome", "error", "error", err.Error())
				httpx.Fail(c, meter.ErrUnavailable)
				return
			}
			// Dòng sổ đã ghi với status 429, units 0 (metering/port.Ledger).
			res = s.failResult(c, meter.ErrQuotaExceeded)
			reset := rec.WindowReset
			if reset.IsZero() {
				reset = nextBusinessDay(caller.ReceivedAt)
			}
			res.header.Set("Retry-After", seconds(reset.Sub(caller.ReceivedAt)))
		}
		h := c.Writer.Header()
		for k, vs := range res.header {
			for _, v := range vs {
				h.Add(k, v)
			}
		}
		if rate.Limit > 0 {
			h.Set(port.HeaderRateLimitLimit, strconv.FormatInt(rate.Limit, 10))
			h.Set(port.HeaderRateLimitRemaining, strconv.FormatInt(rate.Remaining, 10))
			h.Set(port.HeaderRateLimitReset, seconds(rate.Reset))
		}
		if err == nil {
			h.Set(port.HeaderQuotaRemaining, strconv.FormatInt(rec.MonthlyRemaining, 10))
		}
		if res.stream != nil && err == nil {
			defer func() { _ = res.stream.Close() }()
			h.Set("Content-Length", strconv.FormatInt(res.size, 10))
			c.Status(res.status)
			if _, cerr := io.Copy(c.Writer, res.stream); cerr != nil {
				s.d.Log.ErrorContext(ctx, "Public API: lỗi phát luồng tệp", "event", "public.stream_error", "request_id", caller.RequestID,
					"outcome", "error", "error", cerr.Error())
			}
			return
		}
		c.Status(res.status)
		_, _ = c.Writer.Write(res.body)
	}
}

// ---------------------------------------------------------------------------
// Kiểm truy cập hiệu lực (ADR-0022 mục 6)
// ---------------------------------------------------------------------------

type access struct {
	product *dataproduct.Product
	grant   partyport.ShareGrant
}

// authorize kiểm đủ bốn điều kiện cho (sản phẩm, hành động). grantor = Nil → chỉ nhận khi đúng một
// bên cấp. Thiếu điều kiện → port.ErrForbidden (không nói thiếu điều kiện nào).
func (s *Server) authorize(ctx context.Context, caller port.Caller, name, action string, grantor uuid.UUID) (access, error) {
	p, err := dataproduct.Get(name)
	if err != nil {
		return access{}, port.ErrUnknownProduct
	}
	if !p.Servable(s.d.Production) || !caller.Client.HasScope(port.ProductScope(name, action)) {
		return access{}, port.ErrForbidden
	}
	if action == port.ActionExport && (p.Export == nil || !p.Export.Enabled) {
		return access{}, port.ErrForbidden
	}
	if s.d.Grants == nil {
		return access{}, port.ErrForbidden
	}
	g, err := s.d.Grants.ActiveGrant(ctx, caller.Client.ClientTenantID, grantor, partyport.DataProductKind(name), caller.ReceivedAt)
	if err != nil {
		if isCode(err, partyport.ErrShareGrantMissing.Code) {
			return access{}, port.ErrForbidden
		}
		return access{}, err
	}
	if g.Grantee != caller.Client.ClientTenantID || !g.Covers(partyport.DataProductKind(name)) {
		return access{}, port.ErrForbidden
	}
	return access{product: p, grant: g}, nil
}

func grantorParam(c *gin.Context) (uuid.UUID, error) {
	return partyport.ParseGrantor(c.Query("grantor"))
}

// precheck kiểm hạn mức sớm (đọc bộ đếm đã commit). Thẩm quyền cuối là Ledger.Record.
func (s *Server) precheck(ctx context.Context, caller port.Caller, product string) error {
	if s.d.Quotas == nil {
		return meter.ErrUnavailable
	}
	rem, err := s.d.Quotas.Remaining(ctx, s.d.OperatorTenant, caller.Client.ID, product, caller.ReceivedAt)
	if err != nil {
		if isCode(err, meter.ErrQuotaExceeded.Code) {
			return err
		}
		return meter.ErrUnavailable.WithCause(err)
	}
	if rem.DailyUnits <= 0 || rem.MonthlyUnits <= 0 {
		return meter.ErrQuotaExceeded
	}
	return nil
}

// kFor đọc ngưỡng k (publicdata.k_min) của bên cấp tại at; chưa đặt → k_default của hợp đồng.
func (s *Server) kFor(ctx context.Context, p *dataproduct.Product, grantor uuid.UUID, at time.Time) int64 {
	k := int64(2)
	if p.Aggregation != nil && p.Aggregation.KDefault > 2 {
		k = int64(p.Aggregation.KDefault)
	}
	if s.d.Params == nil || p.Aggregation == nil {
		return k
	}
	v, err := s.d.Params.At(tenant.WithID(ctx, grantor), p.Aggregation.KParam, at)
	if err != nil {
		if !errors.Is(err, params.ErrParamNotFound) {
			s.d.Log.WarnContext(ctx, "Public API: không đọc được tham số k — dùng k mặc định của hợp đồng", "event", "public.k_param", "outcome", "error")
		}
		return k
	}
	if n := v.Value.IntPart(); n >= 2 {
		return n
	}
	return k
}

// dataResult dựng phản hồi một trang sản phẩm dữ liệu.
func dataResult(a access, items []json.RawMessage, next string, meta any) (*result, error) {
	var nc *string
	if next != "" {
		nc = &next
	}
	body := map[string]any{"items": items, "next_cursor": nc}
	if meta != nil {
		body["meta"] = meta
	}
	r, err := jsonResult(http.StatusOK, body)
	if err != nil {
		return nil, err
	}
	r.header.Set(port.HeaderDataContract, a.product.ContractHeader())
	r.rows, r.dataTenant, r.product, r.contractVersion = int64(len(items)), a.grant.Grantor, a.product.Name, a.product.Version
	r.grant = &a.grant
	return r, nil
}

func limitParam(c *gin.Context) (int, error) {
	v := c.Query("limit")
	if v == "" {
		return 100, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 500 {
		return 0, problem.Validation.WithField("limit", "1–500")
	}
	return n, nil
}

func cursorParam(c *gin.Context) (string, error) {
	v := c.Query("cursor")
	if len(v) > 512 {
		return "", problem.Validation.WithField("cursor", "con trỏ không hợp lệ")
	}
	return v, nil
}

func hashHex(b ...[]byte) string {
	h := sha256New()
	for _, x := range b {
		h.Write(x)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func digestHeader(shaHex string) string {
	raw, err := hex.DecodeString(shaHex)
	if err != nil {
		return ""
	}
	return "sha-256=" + base64.StdEncoding.EncodeToString(raw)
}

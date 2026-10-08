package public

// Feed theo cursor và ảnh chụp của cổng tích hợp (W5.3 — R-INT-05, R-INT-06; ADR-0033 §2; contracts/openapi/
// public.yaml tag integration_feeds). Đi qua cùng đường ống handle (khóa API HMAC, giới hạn tần suất, sổ đo lường
// TRƯỚC byte, điểm kiểm soát xuất) như sản phẩm dữ liệu:
//
//	GET /v1/feeds/{feed}?cursor=&limit=&contract_version=   một trang phong bì feed_page.v1
//	GET /v1/snapshots/{dataset}/{version}                   URL ký ≤ 15 phút của ảnh chụp (cấp lúc yêu cầu)
//
// Truy cập = scope feed:<tên>:read (đăng ký sản phẩm tên = tên feed, bốn mắt) ∧ hợp đồng phục vụ được: approved
// mọi nơi; proposed/draft CHỈ ngoài production và khi AG_GATEWAY_FEED_ALLOW_PROPOSED=1 (Deps.AllowUnapproved).
// MỌI bản ghi của trang qua ValidateRecord của hợp đồng và cả trang qua phong bì feed_page TRƯỚC khi trả (mặc định
// từ chối: lệch → 500 apiclient.contract_violation, không byte dữ liệu).
// Feed W5.3 là tập chung của đơn vị vận hành (tổng hợp ≥ k, tham chiếu): không lọc từng hàng theo đối tác.
// Feed cấp phép theo bên cấp (grant_class partner, W5.5) và song phương (W5.6) chưa phục vụ ở đây.

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// HeaderContractStatus báo trạng thái hợp đồng khi cổng phục vụ hợp đồng chưa approved (ngoài production).
const HeaderContractStatus = "AG-Contract-Status"

var feedNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,47}$`)

func (s *Server) registerFeeds(v1 *gin.RouterGroup) {
	v1.GET("/feeds/:feed", s.handle(spec{kind: meter.CallRead}, s.feed))
	v1.GET("/snapshots/:dataset/:version", s.handle(spec{kind: meter.CallFileDownload}, s.snapshot))
}

// feedAccess kiểm hợp đồng phục vụ được, scope và lớp cấp phép của feed name.
func (s *Server) feedAccess(c *gin.Context, caller port.Caller, name string, version int) (*integration.Contract, error) {
	if !feedNameRe.MatchString(name) {
		return nil, port.ErrUnknownFeed
	}
	ct, err := app.FeedContract(name, version)
	if err != nil {
		return nil, err
	}
	switch {
	case ct.Servable(true):
	case s.d.Production || !s.d.AllowUnapproved || !ct.Servable(false):
		return nil, port.ErrContractNotServable.WithDetail("Hợp đồng " + ct.ID() + " ở trạng thái " + ct.Status)
	default:
		c.Header(HeaderContractStatus, ct.Status)
	}
	if !caller.Client.HasScope(port.FeedScope(name)) {
		return nil, port.ErrForbidden
	}
	if ct.GrantClass != integration.GrantAggregate && ct.GrantClass != integration.GrantReference {
		return nil, port.ErrContractNotServable.WithDetail("Feed lớp " + ct.GrantClass + " chưa phục vụ ở cổng (W5.5/W5.6)")
	}
	return ct, nil
}

func (s *Server) feed(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	version := 0
	if v := c.Query("contract_version"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, problem.Validation.WithField("contract_version", "số nguyên ≥ 1")
		}
		version = n
	}
	limit := port.DefaultFeedPageLimit
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > port.MaxFeedPageLimit {
			return nil, problem.Validation.WithField("limit", "1–1000")
		}
		limit = n
	}
	var after *int64
	cursor := c.Query("cursor")
	if cursor != "" {
		n, err := domain.DecodeCursor(cursor)
		if err != nil {
			return nil, port.ErrInvalidCursor
		}
		after = &n
	}
	name := c.Param("feed")
	ct, err := s.feedAccess(c, caller, name, version)
	if err != nil {
		return nil, err
	}
	if err := s.precheck(ctx, caller, name); err != nil {
		return nil, err
	}
	if s.d.Svc == nil {
		return nil, app.ErrDependencyUnavailable
	}
	page, err := s.d.Svc.ReadFeed(ctx, ct, "", after, limit)
	if err != nil {
		return nil, err
	}
	if page.Expired {
		p := port.ErrCursorExpired.WithDetail("Đồng bộ lại từ ảnh chụp rồi đọc tiếp từ cursor đầu trang (bỏ tham số cursor)")
		if snap := snapshotRef(ct, page.Latest); snap != nil {
			p = p.With("snapshot", snap)
		}
		res := s.failResult(c, p)
		res.product, res.contractVersion = name, ct.Version
		return res, nil
	}
	items := make([]map[string]any, 0, len(page.Entries))
	for _, e := range page.Entries {
		if e.Op == "upsert" {
			if err := ct.ValidateRecord(e.Record); err != nil {
				return nil, s.violation(c, ct, e, err)
			}
			if key, err := ct.EntityKeyOf(e.Record); err != nil || key != e.EntityKey {
				return nil, s.violation(c, ct, e, err)
			}
		}
		var rec any
		if e.Record != nil {
			rec = e.Record
		}
		items = append(items, map[string]any{"seq": e.Seq, "event_id": e.EventID.String(), "entity_key": e.EntityKey, "op": e.Op,
			"recorded_at": e.RecordedAt.UTC().Format(time.RFC3339Nano), "record": rec})
	}
	var cur any
	if cursor != "" {
		cur = cursor
	}
	maxAge := ""
	if ct.Freshness != nil {
		maxAge = ct.Freshness.MaxAge
	}
	body := map[string]any{"feed": ct.Name, "contract_version": ct.Version, "cursor": cur, "next_cursor": domain.EncodeCursor(page.Next),
		"has_more": page.HasMore, "as_of": page.AsOf.UTC().Format(time.RFC3339Nano), "max_age": maxAge, "dataset": datasetDTO(page.Dataset),
		"snapshot": snapshotRef(ct, page.Dataset), "items": items}
	r, err := jsonResult(http.StatusOK, body)
	if err != nil {
		return nil, err
	}
	env, err := integration.EnvelopeFor(integration.KindFeed)
	if err != nil {
		return nil, err
	}
	if err := env.Validate(r.body); err != nil {
		s.d.Log.ErrorContext(ctx, "feed: trang không khớp phong bì feed_page — không trả", "event", "integration.envelope_violation",
			"feed", ct.ID(), "trace_id", telemetry.TraceID(ctx), "outcome", "error", "error", err.Error())
		return nil, port.ErrContractViolation.WithCause(err)
	}
	r.header.Set(port.HeaderDataContract, ct.ID())
	r.header.Set("Cache-Control", "no-store")
	r.rows, r.dataTenant, r.product, r.contractVersion, r.scope = int64(len(items)), s.d.OperatorTenant, ct.Name, ct.Version, port.FeedScope(ct.Name)
	return r, nil
}

func (s *Server) violation(c *gin.Context, ct *integration.Contract, e store.FeedEntry, err error) error {
	msg := "entity_key lệch"
	if err != nil {
		msg = err.Error()
	}
	s.d.Log.ErrorContext(c.Request.Context(), "feed: bản ghi không khớp hợp đồng — không trả trang", "event", "integration.record_rejected",
		"feed", ct.ID(), "seq", e.Seq, "trace_id", telemetry.TraceID(c.Request.Context()), "outcome", "error", "error", msg)
	return port.ErrContractViolation
}

func datasetDTO(v *store.DatasetVersion) any {
	if v == nil {
		return nil
	}
	var asOf any
	if v.AdminUnitAsOf != nil {
		asOf = v.AdminUnitAsOf.Format(time.DateOnly)
	}
	return map[string]any{"version": v.Version, "built_at": v.BuiltAt.UTC().Format(time.RFC3339Nano), "admin_unit_as_of": asOf,
		"k_used": v.KUsed, "contributors": v.Contributors, "checksum": map[string]any{"record_count": v.RecordCount, "sha256": v.SHA256}}
}

// snapshotRef là liên kết ảnh chụp trong trang feed: CHỈ đường dẫn tới cổng, không bao giờ URL ký.
func snapshotRef(ct *integration.Contract, v *store.DatasetVersion) map[string]any {
	if v == nil || v.SnapshotKey == nil {
		return nil
	}
	return map[string]any{"dataset": ct.Name, "version": v.Version, "href": "/v1/snapshots/" + ct.Name + "/" + strconv.FormatInt(v.Version, 10)}
}

func (s *Server) snapshot(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	version, err := strconv.ParseInt(c.Param("version"), 10, 64)
	if err != nil || version < 1 {
		return nil, port.ErrSnapshotNotFound
	}
	name := c.Param("dataset")
	ct, err := s.feedAccess(c, caller, name, 0)
	if err != nil {
		return nil, err
	}
	if err := s.precheck(ctx, caller, name); err != nil {
		return nil, err
	}
	if s.d.Svc == nil {
		return nil, app.ErrDependencyUnavailable
	}
	link, err := s.d.Svc.SnapshotURL(ctx, name, version)
	if err != nil {
		return nil, err
	}
	v := link.Version
	r, err := jsonResult(http.StatusOK, map[string]any{"dataset": name, "version": v.Version, "contract_version": v.ContractVersion,
		"url": link.URL, "expires_at": link.ExpiresAt.UTC().Format(time.RFC3339), "content_type": "application/x-ndjson",
		"size": v.SnapshotSize, "checksum": map[string]any{"record_count": v.RecordCount, "sha256": v.SHA256}})
	if err != nil {
		return nil, err
	}
	r.header.Set(port.HeaderDataContract, name+".v"+strconv.Itoa(v.ContractVersion))
	r.header.Set("Cache-Control", "no-store")
	// Đo theo kích thước ảnh chụp (ADR-0033 §2): URL chỉ cấp SAU khi sổ đo lường ghi xong (đường ống handle).
	r.rows, r.dataTenant, r.product, r.contractVersion, r.scope = v.RecordCount, s.d.OperatorTenant, ct.Name, v.ContractVersion, port.FeedScope(ct.Name)
	if v.SnapshotSize != nil {
		r.meteredBytes = *v.SnapshotSize
	}
	return r, nil
}

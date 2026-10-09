package public

// Feed của cặp song phương (W5.6 — ADR-0034 §4; R-INT-05 ngoại lệ "lọc theo cặp"): GET /v1/feeds/bilateral?pair=<htx_ref>
// &cursor=&limit=. Cùng đường ống handle (khóa API, hạn mức, sổ đo lường TRƯỚC byte, điểm kiểm soát xuất) và phong bì
// feed_page.v1 (feed = "bilateral", items[].contract = "<tên>.v<N>").
//
//   - Cặp = (tenant khách hàng — từ bản ghi khóa, không bao giờ từ request; pair = htx_ref): phải ĐANG HIỆU LỰC ở sổ
//     song phương (port.BilateralLedger, kiểm mỗi lời gọi) — chưa lập / đã chấm dứt → 403 integration.pair_missing.
//     Phân vùng kho feed = port.PairID(tenant khách hàng, htx_ref): tenant khác / HTX khác không bao giờ đọc được.
//   - Hợp đồng: mục chỉ được trả khi hợp đồng của mục phục vụ được (approved; proposed chỉ ngoài production với
//     AG_GATEWAY_FEED_ALLOW_PROPOSED=1) VÀ khách hàng có scope ingest:<hợp đồng>:write hoặc feed:<hợp đồng>:read; mục khác
//     bị bỏ qua (cursor vẫn tiến). Không hợp đồng song phương nào phục vụ được → 403 integration.contract_not_servable;
//     không scope nào → 403 apiclient.forbidden.
//   - Mọi bản ghi qua ValidateRecord của hợp đồng của mục, cả trang qua phong bì feed_page trước khi trả.

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// PairFeedMaxAge là độ tươi cam kết của feed cặp (bản ghi bên a PT15M; xác nhận của HTX PT1H — lấy chặt nhất).
const PairFeedMaxAge = "PT15M"

// pairContracts trả hợp đồng song phương (phiên bản theo tên) mà khách hàng đọc được qua feed cặp.
func (s *Server) pairContracts(c *gin.Context, caller port.Caller) (map[string]*integration.Contract, error) {
	all, err := integration.All()
	if err != nil {
		return nil, err
	}
	out := map[string]*integration.Contract{}
	servable := false
	for _, ct := range all {
		if ct.Kind != integration.KindIngest || ct.Flow != integration.FlowBilateral {
			continue
		}
		switch {
		case ct.Servable(true):
		case !s.d.Production && s.d.AllowUnapproved && ct.Servable(false):
			c.Header(HeaderContractStatus, ct.Status)
		default:
			continue
		}
		servable = true
		if caller.Client.HasScope(port.IngestScope(ct.Name)) || caller.Client.HasScope(port.FeedScope(ct.Name)) {
			out[ct.ID()] = ct
		}
	}
	switch {
	case !servable:
		return nil, port.ErrContractNotServable.WithDetail("Hợp đồng song phương chưa phục vụ ở môi trường này (C-12)")
	case len(out) == 0:
		return nil, port.ErrForbidden
	}
	return out, nil
}

func (s *Server) pairFeed(c *gin.Context, caller port.Caller, after *int64, cursor string, limit int) (*result, error) {
	ctx := c.Request.Context()
	cts, err := s.pairContracts(c, caller)
	if err != nil {
		return nil, err
	}
	htx, err := uuid.Parse(c.Query("pair"))
	if err != nil {
		return nil, problem.Validation.WithField("pair", "htx_ref (UUID) của cặp")
	}
	if s.d.Bilateral == nil || s.d.Svc == nil {
		return nil, port.ErrBilateralUnavailable
	}
	pair, err := s.d.Bilateral.CounterpartyPair(ctx, caller.Client.ClientTenantID, htx)
	if err != nil {
		return nil, err
	}
	if !pair.Active || pair.Counterparty != caller.Client.ClientTenantID {
		return nil, port.ErrPairMissing
	}
	if err := s.precheck(ctx, caller, port.FeedBilateral); err != nil {
		return nil, err
	}
	page, err := s.d.Svc.ReadPairFeed(ctx, pair.PairID, after, limit)
	if err != nil {
		return nil, err
	}
	if page.Expired {
		res := s.failResult(c, port.ErrCursorExpired.WithDetail("Đọc lại từ đầu phần còn giữ (bỏ tham số cursor); bản ghi cũ hơn 30 ngày đối soát qua tổng kiểm ngày"))
		res.product = port.FeedBilateral
		return res, nil
	}
	items := make([]map[string]any, 0, len(page.Entries))
	for _, e := range page.Entries {
		ct := cts[e.Contract+".v"+strconv.Itoa(e.ContractVersion)]
		if ct == nil {
			continue // hợp đồng không phục vụ / không có scope: bỏ qua, cursor vẫn tiến
		}
		if err := ct.ValidateRecord(e.Record); err != nil {
			return nil, s.violation(c, ct, e, err)
		}
		if key, err := ct.EntityKeyOf(e.Record); err != nil || key != e.EntityKey {
			return nil, s.violation(c, ct, e, err)
		}
		items = append(items, map[string]any{"seq": e.Seq, "event_id": e.EventID.String(), "entity_key": e.EntityKey, "op": e.Op,
			"contract": ct.ID(), "recorded_at": e.RecordedAt.UTC().Format(time.RFC3339Nano), "record": e.Record})
	}
	var cur any
	if cursor != "" {
		cur = cursor
	}
	body := map[string]any{"feed": port.FeedBilateral, "contract_version": 1, "cursor": cur, "next_cursor": domain.EncodeCursor(page.Next),
		"has_more": page.HasMore, "as_of": page.AsOf.UTC().Format(time.RFC3339Nano), "max_age": PairFeedMaxAge, "dataset": nil,
		"snapshot": nil, "items": items}
	r, err := jsonResult(http.StatusOK, body)
	if err != nil {
		return nil, err
	}
	env, err := integration.EnvelopeFor(integration.KindFeed)
	if err != nil {
		return nil, err
	}
	if err := env.Validate(r.body); err != nil {
		s.d.Log.ErrorContext(ctx, "feed cặp: trang không khớp phong bì feed_page — không trả", "event", "integration.envelope_violation",
			"feed", port.FeedBilateral, "trace_id", telemetry.TraceID(ctx), "outcome", "error", "error", err.Error())
		return nil, port.ErrContractViolation.WithCause(err)
	}
	r.header.Set("Cache-Control", "no-store")
	r.rows, r.dataTenant, r.product, r.contractVersion, r.scope = int64(len(items)), pair.OwnerTenant, port.FeedBilateral, 1, port.FeedScope(port.FeedBilateral)
	return r, nil
}

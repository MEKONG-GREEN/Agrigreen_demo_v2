package public

// Đối soát (W5.7 — R-INT-10; ADR-0033 §4, ADR-0034): GET /v1/feeds/{feed}/checksums
//
//   - feed tập dữ liệu (tổng hợp, tham chiếu — cùng điều kiện truy cập như GET /v1/feeds/{feed}): mỗi phiên bản tập
//     dữ liệu một dòng {version, record_count, sha256} = tổng kiểm ghi lúc dựng (SHA-256 của ảnh chụp NDJSON, bất
//     biến); ?from_version=&limit= (1–100).
//   - hợp đồng song phương (collection_schedule, roll_count_statement, htx_roll_confirmation): mỗi NGÀY nghiệp vụ một
//     dòng {date, record_count, sha256} theo công thức ingest.ChecksumAlgorithm trên bản ghi bên gọi đã gửi;
//     ?from=&to= (YYYY-MM-DD, tối đa 31 ngày; mặc định 7 ngày tới hôm nay). Scope ingest:<hợp đồng>:write hoặc
//     feed:<hợp đồng>:read. Tạm trên sổ đệm tới khi W5.6 có sổ song phương + feed của cặp.

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/ingest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Kiểu tổng kiểm trong phản hồi.
const (
	ChecksumKindDataset   = "dataset_version"
	ChecksumKindBilateral = "bilateral_day"
)

func (s *Server) checksums(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	name := c.Param("feed")
	if !feedNameRe.MatchString(name) {
		return nil, port.ErrUnknownFeed
	}
	if s.d.Svc == nil {
		return nil, app.ErrDependencyUnavailable
	}
	if slices.Contains(ingest.BilateralContracts, name) {
		return s.pairChecksums(c, caller, name)
	}
	from := int64(1)
	if v := c.Query("from_version"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			return nil, problem.Validation.WithField("from_version", "số nguyên ≥ 1")
		}
		from = n
	}
	limit := 100
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return nil, problem.Validation.WithField("limit", "1–100")
		}
		limit = n
	}
	ct, err := s.feedAccess(c, caller, name, 0)
	if err != nil {
		return nil, err
	}
	page, err := s.d.Svc.DatasetChecksums(c.Request.Context(), ct.Name, from, limit)
	if err != nil {
		return nil, err
	}
	vs := make([]map[string]any, 0, len(page.Versions))
	for _, v := range page.Versions {
		vs = append(vs, map[string]any{"version": v.Version, "contract_version": v.ContractVersion, "built_at": v.BuiltAt.UTC().Format(time.RFC3339Nano),
			"as_of": v.AsOf.UTC().Format(time.RFC3339Nano), "record_count": v.RecordCount, "sha256": v.SHA256, "snapshot": snapshotRef(ct, &v)})
	}
	var next any
	if page.Next > 0 {
		next = page.Next
	}
	r, err := jsonResult(http.StatusOK, map[string]any{"feed": ct.Name, "kind": ChecksumKindDataset, "versions": vs, "next_from_version": next})
	if err != nil {
		return nil, err
	}
	r.header.Set("Cache-Control", "no-store")
	r.rows, r.product, r.contractVersion, r.scope = int64(len(vs)), ct.Name, ct.Version, port.FeedScope(ct.Name)
	return r, nil
}

func parseDay(v string) (clock.Date, bool) {
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return clock.Date{}, false
	}
	return clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, true
}

func (s *Server) pairChecksums(c *gin.Context, caller port.Caller, name string) (*result, error) {
	scope := port.IngestScope(name)
	if !caller.Client.HasScope(scope) {
		scope = port.FeedScope(name)
		if !caller.Client.HasScope(scope) {
			return nil, port.ErrForbidden
		}
	}
	to := clock.BusinessDate(caller.ReceivedAt)
	if v := c.Query("to"); v != "" {
		d, ok := parseDay(v)
		if !ok {
			return nil, problem.Validation.WithField("to", "YYYY-MM-DD")
		}
		to = d
	}
	from := clock.BusinessDate(clock.StartOfBusinessDate(to).Add(-6 * 24 * time.Hour))
	if v := c.Query("from"); v != "" {
		d, ok := parseDay(v)
		if !ok {
			return nil, problem.Validation.WithField("from", "YYYY-MM-DD")
		}
		from = d
	}
	span := clock.StartOfBusinessDate(to).Sub(clock.StartOfBusinessDate(from))
	if span < 0 || span >= ingest.MaxChecksumDays*24*time.Hour {
		return nil, problem.Validation.WithField("from", "from ≤ to, tối đa 31 ngày")
	}
	days, err := ingest.DailyChecksums(c.Request.Context(), s.d.Svc.DB, caller.Client.ClientTenantID, name, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(days))
	var total int64
	for _, d := range days {
		total += d.RecordCount
		out = append(out, map[string]any{"date": d.Date, "record_count": d.RecordCount, "sha256": d.SHA256})
	}
	r, err := jsonResult(http.StatusOK, map[string]any{"feed": name, "kind": ChecksumKindBilateral, "algorithm": ingest.ChecksumAlgorithm,
		"from": from.String(), "to": to.String(), "days": out})
	if err != nil {
		return nil, err
	}
	r.header.Set("Cache-Control", "no-store")
	r.rows, r.product, r.scope = total, name, scope
	return r, nil
}

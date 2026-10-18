package assembly

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Cửa sổ ngừng dùng /v1/partner/* (ADR-0033 §7.2, R-API-01): mọi phản hồi của bề mặt đối tác cũ mang
//
//	Deprecation: @<giây Unix>                         (RFC 9745 — mốc W5.5, PartnerDeprecatedAt)
//	Sunset: <HTTP-date>                               (RFC 8594 — chỉ khi đặt AG_PARTNER_SUNSET)
//	Link: <…/v1/feeds/<feed>>; rel="successor-version" (route có feed thay thế trên cổng tích hợp)
//
// và mỗi lời gọi tăng metric ag.partner.legacy_calls{route, client} (Prometheus
// ag_partner_legacy_calls_total — "partner_legacy_calls_total" của ADR) — điều kiện gỡ W5.10 (§7.3) là
// metric bằng 0 trong 14 ngày liên tiếp.
const (
	partnerPrefix = "/v1/partner/"
	// MetricPartnerLegacyCalls đếm lời gọi /v1/partner/* (kể cả bị từ chối 401/403 — mọi bên còn gọi đều chặn gỡ).
	MetricPartnerLegacyCalls = "ag.partner.legacy_calls"
	// EnvPartnerSunset là ngày gỡ dự kiến (YYYY-MM-DD hoặc RFC 3339, UTC). Không đặt → không có header Sunset.
	EnvPartnerSunset = "AG_PARTNER_SUNSET"
	// EnvPartnerSuccessorBase là gốc URL công khai của cổng tích hợp (vd https://partner.example.vn) cho Link.
	// Không đặt → Link mang đường dẫn tương đối gốc (/v1/feeds/…) — đường dẫn trên cổng, không phải api-platform.
	EnvPartnerSuccessorBase = "AG_PARTNER_SUCCESSOR_BASE_URL"
)

// PartnerDeprecatedAt là mốc ngừng dùng /v1/partner/*: W5.5 (MKG đọc danh bạ qua feed, 09/10/2026).
var PartnerDeprecatedAt = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)

// PartnerSuccessors ánh xạ route /v1/partner/* (đường dẫn mẫu của gin) → đường dẫn kế nhiệm trên cổng tích hợp
// (ADR-0033 §2 bảng feed). Chuỗi rỗng = chưa có kế nhiệm (không phát Link). Mọi route /v1/partner/* PHẢI có
// mặt ở đây (kiểm ở cmd/api-platform) — route mới phải quyết định kế nhiệm của nó.
var PartnerSuccessors = map[string]string{
	"/v1/partner/cooperatives":             "/v1/feeds/cooperative_directory",
	"/v1/partner/cooperatives/:ref":        "/v1/feeds/cooperative_directory",
	"/v1/partner/plots":                    "/v1/feeds/plot_directory",
	"/v1/partner/plots/:ref":               "/v1/feeds/plot_directory",
	"/v1/partner/season-harvest-dates":     "/v1/feeds/season_harvest_dates",
	"/v1/partner/waterway/route":           "/v1/feeds/waterway_routes",
	"/v1/partner/htx-collection/responses": "/v1/feeds/bilateral",
	// Danh sách cấp quyền của chính bên nhận: quyền feed kiểm ở cổng mỗi lời gọi (403 party.share_grant_missing),
	// không có feed thay thế.
	"/v1/partner/grants": "",
}

// PartnerLegacy là cấu hình header ngừng dùng của /v1/partner/*. Giá trị 0 = không Sunset, Link tương đối.
type PartnerLegacy struct {
	Sunset        time.Time // 0 = không phát Sunset
	SuccessorBase string    // gốc URL cổng, không có "/" cuối; rỗng = Link tương đối
}

// PartnerLegacyFromEnv đọc AG_PARTNER_SUNSET và AG_PARTNER_SUCCESSOR_BASE_URL. Sunset KHÔNG bắt buộc ở production:
// điều kiện gỡ (§7.3) là metric bằng 0 + một kỳ phát hành, không phải một ngày định trước — đặt ngày khi đã
// chốt lịch W5.10. Đặt thì phải sau PartnerDeprecatedAt; production đòi gốc URL https.
func PartnerLegacyFromEnv(getenv func(string) string) (PartnerLegacy, error) {
	var p PartnerLegacy
	if s := strings.TrimSpace(getenv(EnvPartnerSunset)); s != "" {
		t, err := parseSunset(s)
		if err != nil {
			return p, fmt.Errorf("%s=%q: cần YYYY-MM-DD hoặc RFC 3339", EnvPartnerSunset, s)
		}
		if !t.After(PartnerDeprecatedAt) {
			return p, fmt.Errorf("%s=%q phải sau mốc ngừng dùng %s", EnvPartnerSunset, s, PartnerDeprecatedAt.Format(time.DateOnly))
		}
		p.Sunset = t
	}
	if b := strings.TrimSpace(getenv(EnvPartnerSuccessorBase)); b != "" {
		u, err := url.Parse(b)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return p, fmt.Errorf("%s=%q: cần URL tuyệt đối http(s) không query/fragment", EnvPartnerSuccessorBase, b)
		}
		if u.Scheme != "https" && residency.Production(getenv) {
			return p, fmt.Errorf("%s phải là https ở production", EnvPartnerSuccessorBase)
		}
		p.SuccessorBase = strings.TrimRight(b, "/")
	}
	return p, nil
}

func parseSunset(s string) (time.Time, error) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	return t.UTC(), err
}

// partnerLegacyMiddleware gắn header ngừng dùng và đếm lời gọi cho route /v1/partner/* đã khớp (route lạ → 404
// của gin, không đếm). Đặt ở engine, trước xác thực: header có cả trên 401/403. Nhãn client = tenant của token
// (bên nhận — số ít) hoặc "anonymous" khi chưa xác thực; không có mã người dùng (R-91).
func partnerLegacyMiddleware(cfg PartnerLegacy, count func(ctx context.Context, route, client string)) gin.HandlerFunc {
	deprecation := "@" + strconv.FormatInt(PartnerDeprecatedAt.Unix(), 10)
	var sunset string
	if !cfg.Sunset.IsZero() {
		sunset = cfg.Sunset.UTC().Format(http.TimeFormat)
	}
	return func(c *gin.Context) {
		route := c.FullPath()
		if !strings.HasPrefix(route, partnerPrefix) {
			c.Next()
			return
		}
		h := c.Writer.Header()
		h.Set("Deprecation", deprecation)
		if sunset != "" {
			h.Set("Sunset", sunset)
		}
		if succ := PartnerSuccessors[route]; succ != "" {
			h.Add("Link", "<"+cfg.SuccessorBase+succ+`>; rel="successor-version"`)
		}
		c.Next()
		client := "anonymous"
		if p, ok := authz.FromContext(c.Request.Context()); ok {
			client = p.TenantID.String()
		}
		count(c.Request.Context(), route, client)
	}
}

// otelPartnerCounter là bộ đếm thật (OpenTelemetry → Prometheus).
func otelPartnerCounter() func(ctx context.Context, route, client string) {
	ctr, _ := telemetry.Meter().Int64Counter(MetricPartnerLegacyCalls,
		metric.WithDescription("Lời gọi bề mặt đối tác cũ /v1/partner/* (ADR-0033 §7.2)"))
	return func(ctx context.Context, route, client string) {
		if ctr != nil {
			ctr.Add(ctx, 1, metric.WithAttributes(attribute.String("route", route), attribute.String("client", client)))
		}
	}
}

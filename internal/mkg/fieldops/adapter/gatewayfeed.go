package adapter

// GatewayFeed là port.DirectoryFeed qua cổng tích hợp (W5.5 — ADR-0033 §1, §2, §5): GET /v1/feeds/{feed}?cursor=
// với khóa API của khách hàng API tenant MKG (header X-API-Key — HMAC + pepper phía cổng; bí mật chỉ ở cấu hình, không
// log). Chỉ thư viện chuẩn HTTP — không import vùng nền tảng (R-03). mTLS (§41.2) khi cổng bật cho MKG: Client tùy
// chỉnh có chứng chỉ (GatewayConfig.Client).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

// GatewayConfig là cấu hình GatewayFeed.
type GatewayConfig struct {
	BaseURL string // gốc cổng tích hợp, vd https://partner.agrigreen.vn
	APIKey  string //nolint:gosec // cấu hình, không log
	// GrantorTenant: tenant bên cấp (tham số grantor); rỗng → cổng chọn khi chỉ có đúng một bên cấp.
	GrantorTenant string
	Timeout       time.Duration
	// Client: HTTP client tùy chỉnh (mTLS); nil → client mặc định với Timeout.
	Client *http.Client
}

// GatewayFeed đọc feed danh bạ qua cổng tích hợp.
type GatewayFeed struct {
	cfg    GatewayConfig
	client *http.Client
}

var _ port.DirectoryFeed = (*GatewayFeed)(nil)

// NewGatewayFeed tạo client feed.
func NewGatewayFeed(cfg GatewayConfig) *GatewayFeed {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	c := cfg.Client
	if c == nil {
		c = &http.Client{Timeout: cfg.Timeout}
	}
	return &GatewayFeed{cfg: cfg, client: c}
}

// feedPage là phong bì feed_page.v1 (contracts/integration/envelope) — chỉ trường bộ điều hợp dùng.
type feedPage struct {
	Feed            string `json:"feed"`
	ContractVersion int    `json:"contract_version"`
	NextCursor      string `json:"next_cursor"`
	HasMore         bool   `json:"has_more"`
	AsOf            string `json:"as_of"`
	MaxAge          string `json:"max_age"`
	Items           []struct {
		Seq        int64           `json:"seq"`
		EventID    uuid.UUID       `json:"event_id"`
		EntityKey  string          `json:"entity_key"`
		Op         string          `json:"op"`
		RecordedAt string          `json:"recorded_at"`
		Record     json.RawMessage `json:"record"`
	} `json:"items"`
}

// Page cài đặt port.DirectoryFeed.
func (g *GatewayFeed) Page(ctx context.Context, feed, cursor string, limit int) (port.DirectoryFeedPage, error) {
	q := url.Values{}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if g.cfg.GrantorTenant != "" {
		q.Set("grantor", g.cfg.GrantorTenant)
	}
	u := strings.TrimRight(g.cfg.BaseURL, "/") + "/v1/feeds/" + url.PathEscape(feed)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return port.DirectoryFeedPage{}, unavailable("%v", err)
	}
	req.Header.Set("X-API-Key", g.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return port.DirectoryFeedPage{}, unavailable("%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return port.DirectoryFeedPage{}, unavailable("đọc thân: %v", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusGone:
		return port.DirectoryFeedPage{}, port.ErrFeedCursorExpired
	case http.StatusForbidden:
		var p struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(body, &p) == nil && p.Code == "party.share_grant_missing" {
			return port.DirectoryFeedPage{}, port.ErrFeedGrantMissing
		}
		return port.DirectoryFeedPage{}, unavailable("HTTP 403 %s", problemCode(body))
	default:
		return port.DirectoryFeedPage{}, unavailable("HTTP %d %s", resp.StatusCode, problemCode(body))
	}
	var fp feedPage
	if err := json.Unmarshal(body, &fp); err != nil {
		return port.DirectoryFeedPage{}, unavailable("phong bì feed không hợp lệ: %v", err)
	}
	if fp.Feed != feed || fp.ContractVersion < 1 || fp.NextCursor == "" {
		return port.DirectoryFeedPage{}, unavailable("phong bì feed thiếu feed/contract_version/next_cursor")
	}
	out := port.DirectoryFeedPage{Feed: fp.Feed, ContractVersion: fp.ContractVersion, NextCursor: fp.NextCursor, HasMore: fp.HasMore}
	if out.AsOf, err = time.Parse(time.RFC3339Nano, fp.AsOf); err != nil {
		return port.DirectoryFeedPage{}, unavailable("as_of không hợp lệ")
	}
	if fp.MaxAge != "" {
		if out.MaxAge, err = ParseISODuration(fp.MaxAge); err != nil {
			return port.DirectoryFeedPage{}, unavailable("max_age không hợp lệ: %v", err)
		}
	}
	for _, it := range fp.Items {
		at, err := time.Parse(time.RFC3339Nano, it.RecordedAt)
		if err != nil || it.Seq < 1 || it.EventID == uuid.Nil || it.EntityKey == "" || (it.Op != "upsert" && it.Op != "retract") {
			return port.DirectoryFeedPage{}, unavailable("mục feed không hợp lệ (seq %d)", it.Seq)
		}
		rec := it.Record
		if it.Op == "retract" || string(rec) == "null" {
			rec = nil
		}
		if it.Op == "upsert" && rec == nil {
			return port.DirectoryFeedPage{}, unavailable("mục upsert thiếu record (seq %d)", it.Seq)
		}
		out.Items = append(out.Items, port.DirectoryFeedItem{Seq: it.Seq, EventID: it.EventID, EntityKey: it.EntityKey, Op: it.Op, Record: rec, RecordedAt: at})
	}
	return out, nil
}

// problemCode trích mã problem (RFC 9457) để ghi lỗi — không bao giờ cả thân.
func problemCode(body []byte) string {
	var p struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &p) == nil && len(p.Code) <= 100 {
		return p.Code
	}
	return ""
}

var isoDuration = regexp.MustCompile(`^P(?:([0-9]+)D)?(?:T(?:([0-9]+)H)?(?:([0-9]+)M)?(?:([0-9]+)S)?)?$`)

// ParseISODuration đọc thời lượng ISO 8601 dạng hợp đồng tích hợp dùng (PnD, PTnHnMnS — không năm/tháng).
func ParseISODuration(s string) (time.Duration, error) {
	m := isoDuration.FindStringSubmatch(s)
	if m == nil || s == "P" || s == "PT" {
		return 0, fmt.Errorf("thời lượng ISO 8601 %q không hỗ trợ", s)
	}
	var d time.Duration
	for i, unit := range []time.Duration{24 * time.Hour, time.Hour, time.Minute, time.Second} {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.ParseInt(m[i+1], 10, 64)
		if err != nil || n > 1<<20 {
			return 0, fmt.Errorf("thời lượng ISO 8601 %q quá lớn", s)
		}
		d += time.Duration(n) * unit
	}
	return d, nil
}

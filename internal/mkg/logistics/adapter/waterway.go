// Package adapter: hiện thực các cổng ngoài của mkg/logistics.
//
//   - WaterwayHTTP: port.WaterwayRoutes qua bề mặt đối tác GET/POST /v1/partner/waterway/route của
//     Application API CÔNG KHAI của nền tảng (ADR-0020, cấp quyền waterway_routes), xác thực bằng tài
//     khoản dịch vụ tenant MKG (OIDC client credentials, cùng biến AG_MKG_SA_* / AG_PARTNER_GRANTOR_TENANT
//     với mkg/fieldops). Chỉ thư viện chuẩn HTTP — không import vùng nền tảng (R-03). Thân phản hồi được
//     trả NGUYÊN VĂN (RouteResult.Raw + SHA-256) để app lưu vào route_snapshot (mục 16).
//   - HaversineRoad: port.RoadRoutes = Haversine × hệ số #10 (1,3) tới khi có OSRM trong lãnh thổ (RB-7).
//   - FakeWaterway: bản giả trong bộ nhớ (kiểm thử, dữ liệu tổng hợp R-95).
//   - Unconfigured: đóng khi chưa cấu hình (mọi lời gọi → ErrRouteUnavailable).
//
// Không float (agnofloat phủ toàn module): số JSON của phản hồi đọc thẳng vào decimal.
package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
)

// PlatformConfig là cấu hình tài khoản dịch vụ MKG gọi Application API (cùng các biến của fieldops).
type PlatformConfig struct {
	BaseURL      string // gốc Application API, vd https://api.agrigreen.vn
	TokenURL     string // endpoint token OIDC của realm
	ClientID     string // tài khoản dịch vụ thuộc tenant MKG (vai mkg_sync)
	ClientSecret string //nolint:gosec // cấu hình, không log
	// GrantorTenant: tenant bên cấp của cấp quyền waterway_routes; rỗng → nền tảng tự chọn khi chỉ có một.
	GrantorTenant string
	Timeout       time.Duration
}

// WaterwayHTTP là port.WaterwayRoutes qua bề mặt đối tác của nền tảng.
type WaterwayHTTP struct {
	cfg    PlatformConfig
	client *http.Client
	now    func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewWaterwayHTTP tạo client; now dùng cho hạn token và FetchedAt (clock của lắp ráp).
func NewWaterwayHTTP(cfg PlatformConfig, now func() time.Time) *WaterwayHTTP {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &WaterwayHTTP{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}, now: now}
}

var _ port.WaterwayRoutes = (*WaterwayHTTP)(nil)

func unavailable(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{port.ErrRouteUnavailable}, a...)...)
}

func (c *WaterwayHTTP) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Before(c.expiry) {
		return c.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {c.cfg.ClientID}, "client_secret": {c.cfg.ClientSecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", unavailable("token: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", unavailable("token: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", unavailable("token: HTTP %d", resp.StatusCode)
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil || t.AccessToken == "" {
		return "", unavailable("token: phản hồi không hợp lệ")
	}
	life := time.Duration(t.ExpiresIn) * time.Second
	if life <= 0 {
		life = time.Minute
	}
	c.token, c.expiry = t.AccessToken, c.now().Add(life*9/10)
	return c.token, nil
}

// wirePoint là điểm trên dây: số JSON (không bao giờ float — json.Number từ decimal).
type wirePoint struct {
	Lat json.Number `json:"lat"`
	Lng json.Number `json:"lng"`
}

type wireRequest struct {
	Points      []wirePoint `json:"points"`
	MinLoadTons *string     `json:"min_load_tons,omitempty"`
	Version     *int        `json:"version,omitempty"`
}

// Route cài đặt port.WaterwayRoutes: 2 điểm → GET, nhiều điểm → POST (Idempotency-Key = SHA-256 thân).
func (c *WaterwayHTTP) Route(ctx context.Context, _ uuid.UUID, req port.RouteRequest) (port.RouteResult, error) {
	if len(req.Points) < 2 || len(req.Points) > 25 {
		return port.RouteResult{}, unavailable("cần 2..25 điểm")
	}
	tok, err := c.accessToken(ctx)
	if err != nil {
		return port.RouteResult{}, err
	}
	base := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/partner/waterway/route"
	q := url.Values{}
	if c.cfg.GrantorTenant != "" {
		q.Set("grantor", c.cfg.GrantorTenant)
	}
	var hreq *http.Request
	if len(req.Points) == 2 {
		q.Set("from", req.Points[0].Lat.String()+","+req.Points[0].Lng.String())
		q.Set("to", req.Points[1].Lat.String()+","+req.Points[1].Lng.String())
		if req.MinLoadTons.Sign() > 0 {
			q.Set("min_load_tons", req.MinLoadTons.String())
		}
		if req.NetworkVersion > 0 {
			q.Set("version", fmt.Sprint(req.NetworkVersion))
		}
		hreq, err = http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	} else {
		body := wireRequest{}
		for _, p := range req.Points {
			body.Points = append(body.Points, wirePoint{Lat: json.Number(p.Lat.String()), Lng: json.Number(p.Lng.String())})
		}
		if req.MinLoadTons.Sign() > 0 {
			s := req.MinLoadTons.String()
			body.MinLoadTons = &s
		}
		if req.NetworkVersion > 0 {
			v := req.NetworkVersion
			body.Version = &v
		}
		raw, _ := json.Marshal(body)
		sum := sha256.Sum256(raw)
		u := base
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		hreq, err = http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
		if err == nil {
			hreq.Header.Set("Content-Type", "application/json")
			hreq.Header.Set("Idempotency-Key", "lgr-"+hex.EncodeToString(sum[:]))
		}
	}
	if err != nil {
		return port.RouteResult{}, unavailable("%v", err)
	}
	hreq.Header.Set("Authorization", "Bearer "+tok)
	hreq.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(hreq)
	if err != nil {
		return port.RouteResult{}, unavailable("%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return port.RouteResult{}, unavailable("đọc phản hồi: %v", err)
	}
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		return port.RouteResult{}, port.ErrRouteGrantMissing // ADR-0020: chưa cấp / thu hồi / hết hạn
	case resp.StatusCode == http.StatusUnauthorized:
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return port.RouteResult{}, unavailable("HTTP 401")
	case resp.StatusCode != http.StatusOK:
		return port.RouteResult{}, unavailable("HTTP %d", resp.StatusCode)
	}
	res, err := ParseWaterwayRoute(raw)
	if err != nil {
		return port.RouteResult{}, unavailable("phản hồi không hợp lệ: %v", err)
	}
	res.FetchedAt = c.now()
	return res, nil
}

type wireLimiting struct {
	MaxLoadTons *string `json:"max_load_tons"`
}

type wireLine struct {
	Coordinates [][]decimal.Decimal `json:"coordinates"`
}

type wireLeg struct {
	From        struct{ Lat, Lng decimal.Decimal } `json:"from"`
	To          struct{ Lat, Lng decimal.Decimal } `json:"to"`
	Found       bool                               `json:"found"`
	DistanceKm  string                             `json:"distance_km"`
	AccessKm    string                             `json:"access_km"`
	Source      string                             `json:"source"`
	Geometry    *wireLine                          `json:"geometry"`
	SegmentRefs []uuid.UUID                        `json:"segment_refs"`
	Reason      *string                            `json:"reason"`
}

type wireRoute struct {
	GrantorTenantID uuid.UUID     `json:"grantor_tenant_id"`
	NetworkVersion  *int          `json:"network_version"`
	Found           bool          `json:"found"`
	DistanceKm      string        `json:"distance_km"`
	Source          string        `json:"source"`
	Limiting        *wireLimiting `json:"limiting"`
	Legs            []wireLeg     `json:"legs"`
}

func source(s string) (port.RouteSource, error) {
	switch port.RouteSource(s) {
	case port.RouteRouted, port.RouteDigitised, port.RouteHaversine:
		return port.RouteSource(s), nil
	}
	return "", fmt.Errorf("source %q lạ", s)
}

// ParseWaterwayRoute đọc thân PartnerWaterwayRoute (contracts/openapi/application.yaml) thành RouteResult,
// giữ nguyên văn thân + SHA-256. Dùng lại khi tái hiện chặng cũ từ route_snapshot (không gọi nền tảng).
func ParseWaterwayRoute(raw []byte) (port.RouteResult, error) {
	var w wireRoute
	if err := json.Unmarshal(raw, &w); err != nil {
		return port.RouteResult{}, err
	}
	dist, err := decimal.NewFromString(w.DistanceKm)
	if err != nil {
		return port.RouteResult{}, fmt.Errorf("distance_km: %w", err)
	}
	src, err := source(w.Source)
	if err != nil {
		return port.RouteResult{}, err
	}
	sum := sha256.Sum256(raw)
	res := port.RouteResult{NetworkVersion: w.NetworkVersion, Found: w.Found, DistanceKm: dist, Source: src,
		Raw: append([]byte(nil), raw...), RawSHA256: hex.EncodeToString(sum[:])}
	if w.GrantorTenantID != uuid.Nil {
		g := w.GrantorTenantID
		res.GrantorTenant = &g
	}
	if w.Limiting != nil && w.Limiting.MaxLoadTons != nil {
		if d, err := decimal.NewFromString(*w.Limiting.MaxLoadTons); err == nil {
			res.MaxLoadTons = &d
		}
	}
	for i, l := range w.Legs {
		km, err := decimal.NewFromString(l.DistanceKm)
		if err != nil {
			return port.RouteResult{}, fmt.Errorf("legs[%d].distance_km: %w", i, err)
		}
		acc := decimal.Zero
		if l.AccessKm != "" {
			if acc, err = decimal.NewFromString(l.AccessKm); err != nil {
				return port.RouteResult{}, fmt.Errorf("legs[%d].access_km: %w", i, err)
			}
		}
		ls, err := source(l.Source)
		if err != nil {
			return port.RouteResult{}, err
		}
		leg := port.RouteLeg{From: port.GeoPoint{Lat: l.From.Lat, Lng: l.From.Lng}, To: port.GeoPoint{Lat: l.To.Lat, Lng: l.To.Lng},
			Found: l.Found, DistanceKm: km, AccessKm: acc, Source: ls, SegmentRefs: l.SegmentRefs}
		if l.Geometry != nil {
			for _, c := range l.Geometry.Coordinates {
				if len(c) == 2 {
					leg.Path = append(leg.Path, port.GeoPoint{Lat: c[1], Lng: c[0]})
				}
			}
		}
		if l.Reason != nil {
			leg.Reason = *l.Reason
		}
		res.Legs = append(res.Legs, leg)
	}
	return res, nil
}

// Unconfigured là WaterwayRoutes khi chưa cấu hình tài khoản dịch vụ: đóng (→ dự phòng Haversine / km tay).
type Unconfigured struct{}

// Route cài đặt port.WaterwayRoutes.
func (Unconfigured) Route(context.Context, uuid.UUID, port.RouteRequest) (port.RouteResult, error) {
	return port.RouteResult{}, unavailable("chưa cấu hình AG_PLATFORM_API_URL / tài khoản dịch vụ MKG")
}

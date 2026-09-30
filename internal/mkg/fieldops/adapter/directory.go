// Package adapter: hiện thực các cổng ngoài của mkg/fieldops.
//
//   - PlatformHTTP: port.PlatformDirectory qua bề mặt đối tác /v1/partner/* của
//     Application API CÔNG KHAI của nền tảng, xác thực bằng tài khoản dịch vụ tenant
//     MKG (OIDC client credentials). Chỉ thư viện chuẩn HTTP — không import vùng nền
//     tảng (R-03), không kết nối DB pf.
//   - FakeDirectory: bản giả trong bộ nhớ cho kiểm thử / dữ liệu tổng hợp (R-95).
//   - Unconfigured: đóng khi chưa cấu hình (mọi lời gọi → ErrDirectoryUnavailable).
//   - FakeWeighed, LotMassWeighed: port.WeighedMass (giả / qua measurement/port.LotMass).
//
// ADR-0020 (RB-2, Q-MKG-12 — chủ sản phẩm chốt 29–30/09/2026): MKG là tenant RIÊNG; thửa/HTX
// thuộc tenant chương trình (bên cấp) và chỉ đọc được khi bên cấp đã duyệt một cấp quyền chia sẻ
// (cooperative_directory, plot_directory, season_harvest_dates, htx_collection_responses) cho
// tenant MKG. PlatformConfig.GrantorTenant (AG_PARTNER_GRANTOR_TENANT) chọn bên cấp; rỗng → nền
// tảng tự chọn khi chỉ có đúng một bên cấp. Nền tảng trả 403 party.share_grant_missing khi chưa
// cấp / đã thu hồi / hết hạn: PlatformHTTP quy 403/404 → ErrDirectoryNotFound, nên lệnh gom bị
// từ chối fieldops.htx_not_found / fieldops.plot_not_found thay vì âm thầm nhận ref lạ.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

// PlatformConfig là cấu hình PlatformHTTP.
type PlatformConfig struct {
	BaseURL      string // gốc Application API, vd https://api.agrigreen.vn
	TokenURL     string // endpoint token OIDC của realm
	ClientID     string // tài khoản dịch vụ thuộc tenant MKG
	ClientSecret string //nolint:gosec // cấu hình, không log
	// GrantorTenant là tenant bên cấp (tenant chương trình) của cấp quyền chia sẻ — tham số
	// grantor của /v1/partner/*. Rỗng → nền tảng tự chọn khi chỉ có một bên cấp (ADR-0020).
	GrantorTenant string
	Timeout       time.Duration
}

// PlatformHTTP là port.PlatformDirectory qua Application API công khai.
type PlatformHTTP struct {
	cfg    PlatformConfig
	client *http.Client
	now    func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewPlatformHTTP tạo client. now dùng cho hạn token (clock của lắp ráp).
func NewPlatformHTTP(cfg PlatformConfig, now func() time.Time) *PlatformHTTP {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &PlatformHTTP{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}, now: now}
}

var _ port.PlatformDirectory = (*PlatformHTTP)(nil)

func unavailable(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{port.ErrDirectoryUnavailable}, a...)...)
}

// accessToken lấy (và cache tới gần hết hạn) token client credentials.
func (c *PlatformHTTP) accessToken(ctx context.Context) (string, error) {
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

// partnerPath thêm tham số grantor (nếu cấu hình) vào đường dẫn bề mặt đối tác.
func (c *PlatformHTTP) partnerPath(path string) string {
	if c.cfg.GrantorTenant == "" {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "grantor=" + url.QueryEscape(c.cfg.GrantorTenant)
}

func (c *PlatformHTTP) get(ctx context.Context, path string, v any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.cfg.BaseURL, "/")+path, nil)
	if err != nil {
		return unavailable("%v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return unavailable("%v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden:
		return port.ErrDirectoryNotFound // ADR-0020: chưa cấp quyền / đã thu hồi / không có — trông như không có
	case resp.StatusCode == http.StatusUnauthorized:
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return unavailable("HTTP 401")
	case resp.StatusCode != http.StatusOK:
		return unavailable("HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v); err != nil {
		return unavailable("phản hồi không hợp lệ")
	}
	return nil
}

// Cooperative gọi GET /v1/partner/cooperatives/{ref} (schema PartnerCooperative, cấp quyền
// cooperative_directory — ADR-0020).
func (c *PlatformHTTP) Cooperative(ctx context.Context, ref uuid.UUID) (port.HTX, error) {
	var body struct {
		Ref         uuid.UUID `json:"ref"`
		CommuneCode *string   `json:"commune_code"`
		Active      bool      `json:"active"`
	}
	if err := c.get(ctx, c.partnerPath("/v1/partner/cooperatives/"+ref.String()), &body); err != nil {
		return port.HTX{}, err
	}
	h := port.HTX{Ref: body.Ref, Active: body.Active}
	if body.CommuneCode != nil {
		h.CommuneCode = *body.CommuneCode
	}
	return h, nil
}

// Plot gọi GET /v1/partner/plots/{ref} (schema PartnerPlot, cấp quyền plot_directory — ADR-0020).
func (c *PlatformHTTP) Plot(ctx context.Context, ref uuid.UUID) (port.Plot, error) {
	var body struct {
		Ref            uuid.UUID  `json:"ref"`
		CommuneCode    string     `json:"commune_code"`
		CooperativeRef *uuid.UUID `json:"cooperative_ref"`
		Status         string     `json:"status"`
	}
	if err := c.get(ctx, c.partnerPath("/v1/partner/plots/"+ref.String()), &body); err != nil {
		return port.Plot{}, err
	}
	return port.Plot{Ref: body.Ref, CooperativeRef: body.CooperativeRef, CommuneCode: body.CommuneCode, Status: body.Status}, nil
}

var _ port.HarvestCalendar = (*PlatformHTTP)(nil)

// PlotSeasons gọi GET /v1/partner/season-harvest-dates?plot_ref= (cấp quyền season_harvest_dates —
// ADR-0020; proto FM-01/02). 403/404 → ErrDirectoryNotFound.
func (c *PlatformHTTP) PlotSeasons(ctx context.Context, plot uuid.UUID) ([]port.SeasonDates, error) {
	var out []port.SeasonDates
	after := int64(0)
	for range 20 { // ≤ 20 trang × 200 vụ cho một thửa là quá đủ
		var body struct {
			Items []struct {
				SeasonRef           uuid.UUID `json:"season_ref"`
				PlotRef             uuid.UUID `json:"plot_ref"`
				SeasonCode          string    `json:"season_code"`
				SeasonYear          int       `json:"season_year"`
				SowingDate          string    `json:"sowing_date"`
				ExpectedHarvestDate string    `json:"expected_harvest_date"`
				HarvestOpenDate     string    `json:"harvest_open_date"`
				Status              string    `json:"status"`
			} `json:"items"`
			NextCursor int64 `json:"next_cursor"`
		}
		path := c.partnerPath("/v1/partner/season-harvest-dates?plot_ref=" + plot.String() + "&after=" + strconv.FormatInt(after, 10))
		if err := c.get(ctx, path, &body); err != nil {
			return nil, err
		}
		for _, it := range body.Items {
			out = append(out, port.SeasonDates{SeasonRef: it.SeasonRef, PlotRef: it.PlotRef, SeasonCode: it.SeasonCode, SeasonYear: it.SeasonYear,
				SowingDate: it.SowingDate, ExpectedHarvestDate: it.ExpectedHarvestDate, HarvestOpenDate: it.HarvestOpenDate, Status: it.Status})
		}
		if len(body.Items) == 0 || body.NextCursor <= after {
			break
		}
		after = body.NextCursor
	}
	return out, nil
}

// FakeDirectory là PlatformDirectory trong bộ nhớ (kiểm thử, dữ liệu tổng hợp R-95).
type FakeDirectory struct {
	mu    sync.RWMutex
	htx   map[uuid.UUID]port.HTX
	plots map[uuid.UUID]port.Plot
	// Down: mô phỏng nền tảng không gọi được.
	Down bool
}

// NewFakeDirectory tạo bản giả rỗng.
func NewFakeDirectory() *FakeDirectory {
	return &FakeDirectory{htx: map[uuid.UUID]port.HTX{}, plots: map[uuid.UUID]port.Plot{}}
}

// AddHTX thêm một HTX đang hoạt động.
func (f *FakeDirectory) AddHTX(ref uuid.UUID, commune string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.htx[ref] = port.HTX{Ref: ref, CommuneCode: commune, Active: true}
}

// AddPlot thêm một thửa active thuộc HTX.
func (f *FakeDirectory) AddPlot(ref, htx uuid.UUID, commune string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := htx
	f.plots[ref] = port.Plot{Ref: ref, CooperativeRef: &h, CommuneCode: commune, Status: "active"}
}

// Cooperative cài đặt PlatformDirectory.
func (f *FakeDirectory) Cooperative(_ context.Context, ref uuid.UUID) (port.HTX, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.Down {
		return port.HTX{}, port.ErrDirectoryUnavailable
	}
	h, ok := f.htx[ref]
	if !ok {
		return port.HTX{}, port.ErrDirectoryNotFound
	}
	return h, nil
}

// Plot cài đặt PlatformDirectory.
func (f *FakeDirectory) Plot(_ context.Context, ref uuid.UUID) (port.Plot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.Down {
		return port.Plot{}, port.ErrDirectoryUnavailable
	}
	p, ok := f.plots[ref]
	if !ok {
		return port.Plot{}, port.ErrDirectoryNotFound
	}
	return p, nil
}

// Unconfigured là PlatformDirectory khi chưa cấu hình tài khoản dịch vụ: đóng (fail closed).
type Unconfigured struct{}

// Cooperative cài đặt PlatformDirectory.
func (Unconfigured) Cooperative(context.Context, uuid.UUID) (port.HTX, error) {
	return port.HTX{}, unavailable("chưa cấu hình AG_PLATFORM_API_URL / tài khoản dịch vụ MKG")
}

// Plot cài đặt PlatformDirectory.
func (Unconfigured) Plot(context.Context, uuid.UUID) (port.Plot, error) {
	return port.Plot{}, unavailable("chưa cấu hình AG_PLATFORM_API_URL / tài khoản dịch vụ MKG")
}

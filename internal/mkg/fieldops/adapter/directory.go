// Package adapter: hiện thực các cổng ngoài của mkg/fieldops.
//
//   - PlatformHTTP: port.PlatformDirectory qua Application API CÔNG KHAI của
//     nền tảng, xác thực bằng tài khoản dịch vụ tenant MKG (OIDC client
//     credentials). Chỉ thư viện chuẩn HTTP — không import vùng nền tảng (R-03),
//     không kết nối DB pf.
//   - FakeDirectory: bản giả trong bộ nhớ cho kiểm thử / dữ liệu tổng hợp (R-95).
//   - Unconfigured: đóng khi chưa cấu hình (mọi lời gọi → ErrDirectoryUnavailable).
//   - FakeWeighed, LotMassWeighed: port.WeighedMass (giả / qua measurement/port.LotMass).
//
// Câu hỏi RB-2 (g3-conventions §5): thửa/HTX thuộc tenant chương trình, token
// tenant MKG sẽ nhận 404 do RLS cho tới khi có cấp quyền chia sẻ tường minh.
// PlatformHTTP quy 404 → ErrDirectoryNotFound, nên lệnh gom bị từ chối
// fieldops.htx_not_found / fieldops.plot_not_found thay vì âm thầm nhận ref lạ.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	Timeout      time.Duration
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
		return port.ErrDirectoryNotFound // RB-2: ngoài phạm vi / chưa chia sẻ trông như không có
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

// Cooperative gọi GET /v1/cooperatives/{ref} (schema Cooperative của application.yaml).
func (c *PlatformHTTP) Cooperative(ctx context.Context, ref uuid.UUID) (port.HTX, error) {
	var body struct {
		Ref           uuid.UUID `json:"ref"`
		AdminUnitCode *string   `json:"admin_unit_code"`
		Active        bool      `json:"active"`
	}
	if err := c.get(ctx, "/v1/cooperatives/"+ref.String(), &body); err != nil {
		return port.HTX{}, err
	}
	h := port.HTX{Ref: body.Ref, Active: body.Active}
	if body.AdminUnitCode != nil {
		h.CommuneCode = *body.AdminUnitCode
	}
	return h, nil
}

// Plot gọi GET /v1/plots/{ref} (plot.properties của PlotFeature).
func (c *PlatformHTTP) Plot(ctx context.Context, ref uuid.UUID) (port.Plot, error) {
	var body struct {
		Plot struct {
			Properties struct {
				Ref            uuid.UUID  `json:"ref"`
				CommuneCode    string     `json:"commune_code"`
				CooperativeRef *uuid.UUID `json:"cooperative_ref"`
				Status         string     `json:"status"`
			} `json:"properties"`
		} `json:"plot"`
	}
	if err := c.get(ctx, "/v1/plots/"+ref.String(), &body); err != nil {
		return port.Plot{}, err
	}
	pr := body.Plot.Properties
	return port.Plot{Ref: pr.Ref, CooperativeRef: pr.CooperativeRef, CommuneCode: pr.CommuneCode, Status: pr.Status}, nil
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

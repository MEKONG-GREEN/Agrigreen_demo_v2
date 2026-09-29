package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

// Application API công khai + tài khoản dịch vụ MKG (client credentials): token
// được cache; 404/403 (RB-2: chưa chia sẻ) → ErrDirectoryNotFound; lỗi khác → Unavailable.
func TestPlatformHTTPClientCredentials(t *testing.T) {
	htx, plot := uuid.New(), uuid.New()
	var tokens atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "mkg-fieldops" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			tokens.Add(1)
			_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":300}`))
		case r.Header.Get("Authorization") != "Bearer tok":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/v1/cooperatives/"+htx.String():
			_, _ = w.Write([]byte(`{"ref":"` + htx.String() + `","admin_unit_code":"31117","active":true,"name":"x"}`))
		case r.URL.Path == "/v1/plots/"+plot.String():
			_, _ = w.Write([]byte(`{"plot":{"type":"Feature","properties":{"ref":"` + plot.String() + `","commune_code":"31117","cooperative_ref":"` +
				htx.String() + `","status":"active"}},"versions":[],"conflicts":[]}`))
		case strings.HasPrefix(r.URL.Path, "/v1/cooperatives/"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	c := NewPlatformHTTP(PlatformConfig{BaseURL: srv.URL, TokenURL: srv.URL + "/token", ClientID: "mkg-fieldops", ClientSecret: "s"},
		func() time.Time { return now })
	ctx := context.Background()
	h, err := c.Cooperative(ctx, htx)
	if err != nil || h.CommuneCode != "31117" || !h.Active {
		t.Fatalf("HTX: %+v %v", h, err)
	}
	p, err := c.Plot(ctx, plot)
	if err != nil || p.CooperativeRef == nil || *p.CooperativeRef != htx || p.Status != "active" {
		t.Fatalf("thửa: %+v %v", p, err)
	}
	if tokens.Load() != 1 {
		t.Fatalf("token phải được cache: %d lần", tokens.Load())
	}
	if _, err := c.Cooperative(ctx, uuid.New()); !errors.Is(err, port.ErrDirectoryNotFound) {
		t.Fatalf("404 → NotFound: %v", err)
	}
	if _, err := c.Plot(ctx, uuid.New()); !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("502 → Unavailable: %v", err)
	}
	if _, err := (Unconfigured{}).Plot(ctx, plot); !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("chưa cấu hình → Unavailable: %v", err)
	}
}

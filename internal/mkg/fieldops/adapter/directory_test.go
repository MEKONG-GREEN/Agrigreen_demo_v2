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

// Bề mặt đối tác /v1/partner/* của Application API công khai + tài khoản dịch vụ MKG (client
// credentials): token được cache; tham số grantor = AG_PARTNER_GRANTOR_TENANT (ADR-0020); 404/403
// (chưa cấp quyền / đã thu hồi) → ErrDirectoryNotFound; lỗi khác → Unavailable.
func TestPlatformHTTPClientCredentials(t *testing.T) {
	htx, plot, grantor := uuid.New(), uuid.New(), uuid.New()
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
		case r.URL.Query().Get("grantor") != grantor.String():
			w.WriteHeader(http.StatusForbidden) // party.share_grant_missing
		case r.URL.Path == "/v1/partner/cooperatives/"+htx.String():
			_, _ = w.Write([]byte(`{"ref":"` + htx.String() + `","commune_code":"31117","active":true,"name":"x","code":"HTX1"}`))
		case r.URL.Path == "/v1/partner/plots/"+plot.String():
			_, _ = w.Write([]byte(`{"ref":"` + plot.String() + `","commune_code":"31117","cooperative_ref":"` +
				htx.String() + `","status":"active","area_ha":"1.5000","geometry":{"type":"MultiPolygon","coordinates":[]}}`))
		case r.URL.Path == "/v1/partner/season-harvest-dates" && r.URL.Query().Get("plot_ref") == plot.String():
			if r.URL.Query().Get("after") == "0" {
				_, _ = w.Write([]byte(`{"items":[{"season_ref":"` + htx.String() + `","plot_ref":"` + plot.String() +
					`","season_code":"DX","season_year":2026,"sowing_date":"2026-09-01","expected_harvest_date":"2026-12-05","harvest_open_date":"2026-12-05","status":"open","cursor":7}],"next_cursor":7}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[],"next_cursor":7}`))
		case strings.HasPrefix(r.URL.Path, "/v1/partner/cooperatives/"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	c := NewPlatformHTTP(PlatformConfig{BaseURL: srv.URL, TokenURL: srv.URL + "/token", ClientID: "mkg-fieldops", ClientSecret: "s",
		GrantorTenant: grantor.String()}, func() time.Time { return now })
	ctx := context.Background()
	h, err := c.Cooperative(ctx, htx)
	if err != nil || h.CommuneCode != "31117" || !h.Active {
		t.Fatalf("HTX: %+v %v", h, err)
	}
	p, err := c.Plot(ctx, plot)
	if err != nil || p.CooperativeRef == nil || *p.CooperativeRef != htx || p.Status != "active" {
		t.Fatalf("thửa: %+v %v", p, err)
	}
	ss, err := c.PlotSeasons(ctx, plot)
	if err != nil || len(ss) != 1 || ss[0].ExpectedHarvestDate != "2026-12-05" || ss[0].PlotRef != plot {
		t.Fatalf("ngày thu hoạch của thửa (proto FM-01/02): %+v %v", ss, err)
	}
	if tokens.Load() != 1 {
		t.Fatalf("token phải được cache: %d lần", tokens.Load())
	}
	// Bên cấp khác (không có cấp quyền) → 403 → NotFound.
	other := NewPlatformHTTP(PlatformConfig{BaseURL: srv.URL, TokenURL: srv.URL + "/token", ClientID: "mkg-fieldops", ClientSecret: "s",
		GrantorTenant: uuid.New().String()}, func() time.Time { return now })
	if _, err := other.Cooperative(ctx, htx); !errors.Is(err, port.ErrDirectoryNotFound) {
		t.Fatalf("403 (chưa cấp quyền) → NotFound: %v", err)
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

package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

// proto FM-01/02: ngày gặt qua bề mặt partner của nền tảng (ADR-0020) bằng tài khoản dịch vụ
// MKG; 404 (chưa chia sẻ, RB-2) → NotFound; 5xx → Unavailable; dòng hỏng bị bỏ qua.
func TestPlatformHTTPSeasonHarvestDatesFM01(t *testing.T) {
	plot, season, missing := uuid.New(), uuid.New(), uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":300}`))
		case r.Header.Get("Authorization") != "Bearer tok":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/v1/partner/season-harvest-dates" && r.URL.Query().Get("plot_ref") == plot.String():
			_, _ = w.Write([]byte(`{"items":[
			  {"plot_ref":"` + plot.String() + `","season_ref":"` + season.String() + `","harvest_date":"2026-10-05","kind":"expected"},
			  {"plot_ref":"` + plot.String() + `","season_ref":null,"harvest_date":"05/10/2026","kind":"expected"},
			  {"plot_ref":"` + plot.String() + `","season_ref":null,"harvest_date":"2026-03-01","kind":"other"},
			  {"plot_ref":"` + uuid.NewString() + `","season_ref":null,"harvest_date":"2026-03-01","kind":"actual"}]}`))
		case r.URL.Path == "/v1/partner/season-harvest-dates" && r.URL.Query().Get("plot_ref") == missing.String():
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	c := NewPlatformHTTP(PlatformConfig{BaseURL: srv.URL, TokenURL: srv.URL + "/token", ClientID: "mkg", ClientSecret: "s"},
		func() time.Time { return time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC) })
	ctx := context.Background()
	xs, err := c.SeasonHarvestDates(ctx, plot)
	if err != nil || len(xs) != 1 || xs[0].SeasonRef == nil || *xs[0].SeasonRef != season || xs[0].Kind != port.HarvestExpected ||
		xs[0].HarvestDate.Format(time.DateOnly) != "2026-10-05" {
		t.Fatalf("ngày gặt: %+v %v", xs, err)
	}
	if _, err := c.SeasonHarvestDates(ctx, missing); !errors.Is(err, port.ErrDirectoryNotFound) {
		t.Fatalf("404 → NotFound: %v", err)
	}
	if _, err := c.SeasonHarvestDates(ctx, uuid.New()); !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("503 → Unavailable: %v", err)
	}
	if _, err := (Unconfigured{}).SeasonHarvestDates(ctx, plot); !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("chưa cấu hình → Unavailable: %v", err)
	}
}

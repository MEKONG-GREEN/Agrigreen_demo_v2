package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

func TestParseISODuration(t *testing.T) {
	for in, want := range map[string]time.Duration{"PT6H": 6 * time.Hour, "P1D": 24 * time.Hour, "PT15M": 15 * time.Minute,
		"P1DT2H30M5S": 26*time.Hour + 30*time.Minute + 5*time.Second} {
		if got, err := ParseISODuration(in); err != nil || got != want {
			t.Errorf("%s → %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "P", "PT", "6h", "P1Y", "P1M", "PT-1H"} {
		if _, err := ParseISODuration(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
}

// Trang mẫu của hợp đồng (contracts/integration/examples) giải mã đúng; khóa API ở header, grantor ở query.
func TestGatewayFeedPage(t *testing.T) {
	example, err := os.ReadFile("../../../../contracts/integration/examples/cooperative_directory.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var gotKey, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotQuery = r.Header.Get("X-API-Key"), r.URL.RawQuery
		switch r.URL.Path {
		case "/v1/feeds/cooperative_directory":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(example)
		case "/v1/feeds/revoked":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"party.share_grant_missing"}`))
		case "/v1/feeds/scope":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"apiclient.forbidden"}`))
		case "/v1/feeds/expired":
			w.WriteHeader(http.StatusGone)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	g := NewGatewayFeed(GatewayConfig{BaseURL: srv.URL + "/", APIKey: "agk_test_secret", GrantorTenant: "0199c8a0-0000-7000-8000-000000000001"})
	ctx := context.Background()
	pg, err := g.Page(ctx, port.FeedCooperativeDirectory, "c2VxOjE", 500)
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "agk_test_secret" || gotQuery != "cursor=c2VxOjE&grantor=0199c8a0-0000-7000-8000-000000000001&limit=500" {
		t.Fatalf("khóa/query: %q %q", gotKey, gotQuery)
	}
	if pg.ContractVersion != 1 || pg.NextCursor != "c2VxOjI" || pg.HasMore || pg.MaxAge != 6*time.Hour || pg.AsOf.IsZero() || len(pg.Items) != 2 ||
		pg.Items[0].Op != "upsert" || pg.Items[0].Record == nil || pg.Items[1].Op != "retract" || pg.Items[1].Record != nil {
		t.Fatalf("trang: %+v", pg)
	}
	if _, err := g.Page(ctx, "revoked", "", 0); !errors.Is(err, port.ErrFeedGrantMissing) {
		t.Fatalf("403 party.share_grant_missing → ErrFeedGrantMissing: %v", err)
	}
	if _, err := g.Page(ctx, "scope", "", 0); !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("403 khác → unavailable (không xóa bản sao): %v", err)
	}
	if _, err := g.Page(ctx, "expired", "x", 0); !errors.Is(err, port.ErrFeedCursorExpired) {
		t.Fatalf("410 → ErrFeedCursorExpired: %v", err)
	}
	if _, err := g.Page(ctx, "down", "", 0); !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("503 → unavailable: %v", err)
	}
	// Trang của feed khác tên → phong bì sai.
	if _, err := g.Page(ctx, port.FeedPlotDirectory, "", 0); !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("feed lạ: %v", err)
	}
}

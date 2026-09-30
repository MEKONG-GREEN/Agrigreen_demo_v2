package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

// RB-6 chiều mua (G3-2): luồng phản hồi HTX qua Application API công khai bằng tài khoản dịch vụ
// (client credentials); 403 (chưa cấp vai mkg_sync, RB-2) → Unavailable để job không tiến con trỏ.
func TestPlatformHTTPHTXResponses_RB6(t *testing.T) {
	forbid := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":300}`))
		case forbid:
			w.WriteHeader(http.StatusForbidden)
		case r.URL.Path == "/v1/cropcycle/htx-collection/responses" && r.URL.Query().Get("after") == "5":
			_, _ = w.Write([]byte(`{"items":[{"response_id":"01a0ef9b-06f2-7cb7-82da-47322010d95b","cursor":7,
			  "statement_ref":"01a0ef9b-0636-75ba-bc0b-050263dc5060","statement_revision":2,"htx_ref":"01a0ef9b-0632-7d35-8331-b7a9529a6fea",
			  "plot_ref":"01a0ef9b-0632-7d3c-94e1-e407905ddc59","mkg_rolls":40,"outcome":"disputed","htx_rolls":38,
			  "reason":"Đếm lại tại ruộng","responded_at":"2026-09-29T03:00:00Z","supersedes_response_id":null}],"next_cursor":7}`))
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	c := NewPlatformHTTP(PlatformConfig{BaseURL: srv.URL, TokenURL: srv.URL + "/token", ClientID: "agrigreen-mkg-sync", ClientSecret: "s"},
		func() time.Time { return time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC) })
	pg, err := c.HTXResponses(context.Background(), 5, 200)
	if err != nil || pg.Next != 7 || len(pg.Items) != 1 || pg.Items[0].HTXRolls != 38 || pg.Items[0].StatementRevision != 2 {
		t.Fatalf("trang: %+v %v", pg, err)
	}
	forbid = true
	if _, err := c.HTXResponses(context.Background(), 7, 200); !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("403 → Unavailable: %v", err)
	}
}

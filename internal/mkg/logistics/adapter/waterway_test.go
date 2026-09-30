package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
)

const partnerBody = `{"grantor_tenant_id":"0190b3c4-0000-7000-8000-000000000001","network_version":7,"found":true,"distance_km":"42.500",` +
	`"source":"routed","limiting":{"max_load_tons":"120.000","certain":true,"reason":"cống","segment_refs":null},"legs":[{"from":{"lat":10.0452,"lng":105.7469},` +
	`"to":{"lat":10.386,"lng":105.4352},"found":true,"distance_km":"42.500","access_km":"0.800","source":"routed",` +
	`"geometry":{"type":"LineString","coordinates":[[105.7469,10.0452],[105.4352,10.386]]},"segment_refs":["0190b3c4-0000-7000-8000-0000000000aa"],` +
	`"limiting":null,"reason":null}]}`

// L-03 / L-04: client bề mặt đối tác — GET 2 điểm, POST nhiều điểm có Idempotency-Key tất định, trả nguyên
// văn + SHA-256; 403 → ErrRouteGrantMissing; 5xx → ErrRouteUnavailable.
func TestLogisticsWaterwayHTTPPartnerClient(t *testing.T) {
	var gotAuth, gotQuery, gotIdem, gotMethod string
	status := http.StatusOK
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"tok-1","expires_in":300}`)
	})
	mux.HandleFunc("/v1/partner/waterway/route", func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotQuery, gotIdem, gotMethod = r.Header.Get("Authorization"), r.URL.RawQuery, r.Header.Get("Idempotency-Key"), r.Method
		w.WriteHeader(status)
		_, _ = io.WriteString(w, partnerBody)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewWaterwayHTTP(PlatformConfig{BaseURL: srv.URL, TokenURL: srv.URL + "/token", ClientID: "mkg", ClientSecret: "s",
		GrantorTenant: "0190b3c4-0000-7000-8000-000000000001"}, time.Now)
	pt := func(a, b string) port.GeoPoint {
		return port.GeoPoint{Lat: decimal.RequireFromString(a), Lng: decimal.RequireFromString(b)}
	}
	req := port.RouteRequest{Points: []port.GeoPoint{pt("10.0452", "105.7469"), pt("10.386", "105.4352")}, MinLoadTons: decimal.NewFromInt(90)}
	res, err := c.Route(context.Background(), uuid.Nil, req)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(partnerBody))
	if gotMethod != http.MethodGet || gotAuth != "Bearer tok-1" || !strings.Contains(gotQuery, "grantor=") ||
		!strings.Contains(gotQuery, "min_load_tons=90") || string(res.Raw) != partnerBody || res.RawSHA256 != hex.EncodeToString(sum[:]) ||
		!res.Found || res.DistanceKm.String() != "42.5" || res.NetworkVersion == nil || *res.NetworkVersion != 7 || len(res.Legs) != 1 ||
		len(res.Legs[0].Path) != 2 || res.Legs[0].Path[0].Lat.String() != "10.0452" || res.MaxLoadTons == nil || res.GrantorTenant == nil {
		t.Fatalf("GET: %s %s %+v", gotMethod, gotQuery, res)
	}
	req.Points = append(req.Points, pt("10.5", "105.3"))
	if _, err := c.Route(context.Background(), uuid.Nil, req); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || !strings.HasPrefix(gotIdem, "lgr-") {
		t.Fatalf("POST: %s idem %q", gotMethod, gotIdem)
	}
	status = http.StatusForbidden
	if _, err := c.Route(context.Background(), uuid.Nil, req); !errors.Is(err, port.ErrRouteGrantMissing) {
		t.Fatalf("403 → ErrRouteGrantMissing, nhận %v", err)
	}
	status = http.StatusServiceUnavailable
	if _, err := c.Route(context.Background(), uuid.Nil, req); !errors.Is(err, port.ErrRouteUnavailable) {
		t.Fatalf("503 → ErrRouteUnavailable, nhận %v", err)
	}
	if _, err := (Unconfigured{}).Route(context.Background(), uuid.Nil, req); !errors.Is(err, port.ErrRouteUnavailable) {
		t.Fatal("chưa cấu hình → ErrRouteUnavailable")
	}
}

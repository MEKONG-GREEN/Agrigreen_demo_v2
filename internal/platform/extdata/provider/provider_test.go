package provider_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/provider"
)

var cell = port.Cell{Lat: decimal.RequireFromString("10.05"), Lon: decimal.RequireFromString("105.75"), SizeDeg: decimal.RequireFromString("0.1")}

// ADR-0007: yêu cầu đi ra không mang định danh — chỉ tâm ô lưới trong URL, header cố định,
// không Authorization/Cookie/X-Forwarded-For/X-Device-Id.
func TestHTTPSendsNoIdentifiers_ADR0007(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"temp_c":"28.5"}`))
	}))
	defer srv.Close()
	p := provider.NewHTTP(srv.URL+"/forecast?lat={lat}&lon={lon}&cell={cell}&key=khoa-api-mkg", nil)
	data, err := p.Point(context.Background(), port.PointQuery{Source: "wx", Cell: cell})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"temp_c":"28.5"}` {
		t.Fatalf("thân = %s", data)
	}
	q := got.URL.Query()
	if q.Get("lat") != "10.05" || q.Get("lon") != "105.75" || q.Get("cell") != "0.1" || len(q) != 4 {
		t.Fatalf("truy vấn đi ra = %v", q)
	}
	for k := range got.Header {
		switch http.CanonicalHeaderKey(k) {
		case "User-Agent", "Accept", "Accept-Encoding":
		default:
			t.Errorf("header lạ đi ra nhà cung cấp: %s", k)
		}
	}
	if got.Header.Get("User-Agent") != provider.UserAgent {
		t.Errorf("User-Agent = %q", got.Header.Get("User-Agent"))
	}
}

// ADR-0007: không theo chuyển hướng; thân không phải JSON, quá lớn,
// hay loại tile không cho phép → lỗi nhà cung cấp.
func TestHTTPRejectsRedirectAndBadBodies_ADR0007(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+"/x", http.StatusFound)
		case "/html":
			_, _ = w.Write([]byte("<html>"))
		case "/big":
			_, _ = w.Write([]byte(`"` + strings.Repeat("a", provider.MaxPointBytes+10) + `"`))
		case "/tile.html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		case "/500":
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	for _, path := range []string{"/redirect", "/html", "/big", "/500"} {
		_, err := provider.NewHTTP(srv.URL+path, nil).Point(context.Background(), port.PointQuery{Cell: cell})
		if !errors.Is(err, provider.ErrUpstream) {
			t.Errorf("%s: muốn ErrUpstream, được %v", path, err)
		}
		if err != nil && strings.Contains(err.Error(), srv.URL) {
			t.Errorf("%s: lỗi lộ URL (có thể chứa khóa API): %v", path, err)
		}
	}
	if _, err := provider.NewHTTP(srv.URL+"/tile.html", nil).Tile(context.Background(), port.TileQuery{Z: 1}); !errors.Is(err, provider.ErrUpstream) {
		t.Errorf("tile text/html phải bị từ chối: %v", err)
	}
}

func TestHTTPTileTemplate(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(provider.PNG1x1)
	}))
	defer srv.Close()
	tl, err := provider.NewHTTP(srv.URL+"/sat/{z}/{x}/{y}.png", nil).Tile(context.Background(), port.TileQuery{Z: 10, X: 812, Y: 483})
	if err != nil || tl.ContentType != "image/png" || path != "/sat/10/812/483.png" {
		t.Fatalf("tile: %v %q %q", err, tl.ContentType, path)
	}
}

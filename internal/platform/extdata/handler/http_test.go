package handler_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

func router(t *testing.T) *gin.Engine {
	t.Helper()
	m, err := extdata.FromEnv(func(k string) string {
		return map[string]string{
			"AG_EXTDATA_SOURCES": "wx,sat", "AG_EXTDATA_WX_URL": "fake", "AG_EXTDATA_WX_KIND": "weather",
			"AG_EXTDATA_SAT_URL": "fake", "AG_EXTDATA_SAT_KIND": "tile", "AG_EXTDATA_SAT_MAX_ZOOM": "15",
		}[k]
	}, clock.NewFixed(time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	r := httpx.NewEngine(slog.New(slog.DiscardHandler), 1<<20)
	v1 := r.Group("/v1", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "" {
			ctx := authz.WithPrincipal(c.Request.Context(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), Roles: []string{"farmer"}})
			c.Request = c.Request.WithContext(ctx)
		}
	})
	m.RegisterApp(v1)
	return r
}

func do(r *gin.Engine, path string, auth bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if auth {
		req.Header.Set("Authorization", "Bearer x")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ADR-0007: API proxy — ô lưới thô trong phản hồi, danh mục nguồn không lộ URL, tile có
// giới hạn zoom, mặc định chặn khi chưa xác thực.
func TestExtDataHTTP_ADR0007(t *testing.T) {
	r := router(t)
	w := do(r, "/v1/extdata/wx/point?lat=10.0452&lon=105.7701", true)
	if w.Code != http.StatusOK {
		t.Fatalf("point: %d %s", w.Code, w.Body)
	}
	var body struct {
		Cell struct {
			Lat     string `json:"lat"`
			Lon     string `json:"lon"`
			SizeDeg string `json:"size_deg"`
		} `json:"cell"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Cell.Lat != "10.05" || body.Cell.Lon != "105.75" || body.Data["lat"] != "10.05" {
		t.Fatalf("ô = %+v data = %v", body.Cell, body.Data)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "private, max-age=1800" {
		t.Errorf("Cache-Control = %q", cc)
	}

	w = do(r, "/v1/extdata/sources", true)
	if w.Code != http.StatusOK || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("sources: %d", w.Code)
	}
	var src struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &src)
	if len(src.Items) != 2 || src.Items[0]["id"] != "sat" || src.Items[0]["max_zoom"] != float64(15) || src.Items[1]["cell_deg"] != "0.1" {
		t.Fatalf("sources = %v", src.Items)
	}
	for _, it := range src.Items {
		if _, leak := it["url"]; leak {
			t.Fatal("danh mục nguồn không được trả URL (có thể chứa khóa API)")
		}
	}

	w = do(r, "/v1/extdata/sat/tiles/10/812/483.png", true)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("tile: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	for path, want := range map[string]int{
		"/v1/extdata/sat/tiles/16/52000/30900":  http.StatusUnprocessableEntity, // zoom > 15
		"/v1/extdata/sat/tiles/a/b/c":           http.StatusUnprocessableEntity,
		"/v1/extdata/wx/point?lat=10":           http.StatusUnprocessableEntity,
		"/v1/extdata/owm/point?lat=10&lon=105":  http.StatusNotFound,
		"/v1/extdata/wx/point?lat=40&lon=-74.0": http.StatusUnprocessableEntity,
	} {
		if w := do(r, path, true); w.Code != want {
			t.Errorf("%s: %d, muốn %d (%s)", path, w.Code, want, w.Body)
		}
	}
	if w := do(r, "/v1/extdata/wx/point?lat=10&lon=105", false); w.Code != http.StatusUnauthorized {
		t.Errorf("chưa xác thực: %d", w.Code)
	}
}

package shareread

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func engine(viewer uuid.UUID, sink Sink, h gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { // thay xác thực: tenant phiên
		c.Request = c.Request.WithContext(tenant.WithID(c.Request.Context(), viewer))
	}, Middleware(sink))
	r.GET("/v1/plots/:id", h)
	return r
}

func do(r *gin.Engine, purpose string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/plots/x", nil)
	if purpose != "" {
		req.Header.Set(HeaderPurpose, purpose)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// W4.2 (ADR-0030 mục 4): một sự kiện gộp mỗi yêu cầu có đọc qua cấp phép; không mục đích → không đệm, không ghi;
// Sink lỗi → 503, không lộ thân phản hồi đã đệm; mục đích sai dạng → 400; mục đích được gắn vào context.
func TestMiddlewareAggregatesShareReads_W42(t *testing.T) {
	viewer, ownerA, ownerB := uuid.New(), uuid.New(), uuid.New()
	var events []Event
	sink := func(_ context.Context, e Event) error { events = append(events, e); return nil }
	var seenPurpose string
	h := func(c *gin.Context) {
		ctx := c.Request.Context()
		seenPurpose = authz.PurposeFrom(ctx)
		via := Mark(ctx, "pf_coop.plot", []uuid.UUID{viewer, ownerA, ownerA, ownerB})
		if via[0] || !via[1] || !via[2] || !via[3] {
			t.Errorf("Mark: %v", via)
		}
		Note(ctx, "pf_mechanization.machine", ownerA, 2)
		c.JSON(http.StatusOK, gin.H{"rows": 4, "secret": "dữ-liệu-chia-sẻ"})
	}
	r := engine(viewer, sink, h)

	w := do(r, "khuyen-nong")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "dữ-liệu-chia-sẻ") || seenPurpose != "khuyen-nong" {
		t.Fatalf("có mục đích: %d %s (mục đích %q)", w.Code, w.Body.String(), seenPurpose)
	}
	if len(events) != 1 {
		t.Fatalf("muốn đúng một sự kiện gộp, có %d", len(events))
	}
	e := events[0]
	if e.ViewerTenant != viewer || e.Purpose != "khuyen-nong" || e.Route != "/v1/plots/:id" || e.Method != http.MethodGet || len(e.Reads) != 3 {
		t.Fatalf("sự kiện: %+v", e)
	}
	total := map[string]int{}
	for _, rd := range e.Reads {
		total[rd.ModuleTable+"|"+rd.OwnerTenant.String()] = rd.Rows
	}
	if total["pf_coop.plot|"+ownerA.String()] != 2 || total["pf_coop.plot|"+ownerB.String()] != 1 ||
		total["pf_mechanization.machine|"+ownerA.String()] != 2 {
		t.Fatalf("gộp: %v", total)
	}

	// Không mục đích: đi thẳng, không sự kiện (Note ngoài bộ gom không làm gì).
	events = nil
	if w := do(r, ""); w.Code != http.StatusOK || len(events) != 0 || seenPurpose != "" {
		t.Fatalf("không mục đích: %d, %d sự kiện, mục đích %q", w.Code, len(events), seenPurpose)
	}
	// Mục đích sai dạng → không gắn mục đích (module tự xử lý header như trước), không bộ gom, không sự kiện.
	if w := do(r, "Khuyến Nông!"); w.Code != http.StatusOK || len(events) != 0 || seenPurpose != "" {
		t.Fatalf("mục đích sai dạng: %d, %d sự kiện, mục đích %q", w.Code, len(events), seenPurpose)
	}
	// Port Note sau khi handler đã ghi (sai quy ước): phản hồi đã đi thẳng, vẫn ghi sự kiện.
	late := engine(viewer, sink, func(c *gin.Context) {
		c.String(http.StatusOK, "đã-ghi")
		Note(c.Request.Context(), "pf_coop.plot", ownerA, 1)
	})
	if w := do(late, "khuyen-nong"); w.Code != http.StatusOK || w.Body.String() != "đã-ghi" || len(events) != 1 {
		t.Fatalf("Note muộn: %d %q, %d sự kiện", w.Code, w.Body.String(), len(events))
	}
	events = nil
	// Có mục đích nhưng không đọc qua cấp phép → không sự kiện.
	own := engine(viewer, sink, func(c *gin.Context) {
		Mark(c.Request.Context(), "pf_coop.plot", []uuid.UUID{viewer})
		c.String(http.StatusCreated, "ok")
	})
	if w := do(own, "khuyen-nong"); w.Code != http.StatusCreated || w.Body.String() != "ok" || len(events) != 0 {
		t.Fatalf("không đọc chéo: %d %q, %d sự kiện", w.Code, w.Body.String(), len(events))
	}
	// Sink lỗi → 503, thân đã đệm không ra ngoài.
	bad := engine(viewer, func(context.Context, Event) error { return errors.New("outbox down") }, h)
	if w := do(bad, "khuyen-nong"); w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "dữ-liệu-chia-sẻ") ||
		!strings.Contains(w.Body.String(), "share.read_log_unavailable") {
		t.Fatalf("sink lỗi: %d %s", w.Code, w.Body.String())
	}
	// Chưa nối Sink → đóng.
	if w := do(engine(viewer, nil, h), "khuyen-nong"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("sink nil: %d", w.Code)
	}
}

func TestVia_W42(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	ctx := tenant.WithID(context.Background(), a)
	if Via(ctx, a) || !Via(ctx, b) || Via(ctx, uuid.Nil) || Via(context.Background(), b) {
		t.Fatal("Via sai")
	}
}

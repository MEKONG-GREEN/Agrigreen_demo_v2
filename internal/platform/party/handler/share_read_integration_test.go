package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/shareread"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W4.2 (ADR-0030 mục 4): middleware của pf/party ghi MỘT sự kiện gộp pf.party.share_read.recorded (tenant phong bì
// = đơn vị xem) cho yêu cầu có đọc qua cấp phép; yêu cầu không mục đích hoặc không đọc chéo không ghi gì.
func TestShareReadRecorded_W42(t *testing.T) {
	f := newFixture(t)
	viewer, owner := ids.New(), ids.New()
	gin.SetMode(gin.TestMode)
	eng := gin.New()
	eng.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(tenant.WithID(c.Request.Context(), viewer)) }, f.mod.Middleware())
	eng.GET("/v1/w42/plots", func(c *gin.Context) {
		shareread.Mark(c.Request.Context(), "pf_coop.plot", []uuid.UUID{owner, owner, viewer})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	call := func(purpose string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/w42/plots", nil)
		if purpose != "" {
			req.Header.Set(shareread.HeaderPurpose, purpose)
		}
		w := httptest.NewRecorder()
		eng.ServeHTTP(w, req)
		return w.Code
	}
	if c := call("khuyen-nong"); c != http.StatusOK {
		t.Fatalf("đọc qua cấp phép: %d", c)
	}
	if c := call(""); c != http.StatusOK {
		t.Fatalf("không mục đích: %d", c)
	}
	pub := f.env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	rows, err := pub.Query(context.Background(), `SELECT payload FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2`,
		viewer, port.EventShareReadRecorded)
	if err != nil {
		t.Fatal(err)
	}
	var payloads []port.ShareReadRecordedV1
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var p port.ShareReadRecordedV1
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, p)
	}
	rows.Close()
	if len(payloads) != 1 {
		t.Fatalf("muốn đúng một sự kiện gộp, có %d", len(payloads))
	}
	p := payloads[0]
	if p.PurposeCode != "khuyen-nong" || p.Route != "/v1/w42/plots" || len(p.Reads) != 1 || p.Reads[0].OwnerTenant != owner ||
		p.Reads[0].Rows != 2 || p.Reads[0].ModuleTable != "pf_coop.plot" {
		t.Fatalf("payload: %+v", p)
	}
}

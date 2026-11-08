package price

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// FN-TI-02.13: an authorized province officer opens the first draft of both programs.
// The pre-implementation base returns 404: this is the native behavior baseline.
func TestWeeklyProgramModule284(t *testing.T) {
	env := pgtest.Get(t)
	prof := port.Profile{Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA", "kn_tw": "TW"}, ManageBy: []string{"admin"}}
	mod, err := New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)), []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	p := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{"kn_tinh"}}
	r.Use(func(c *gin.Context) {
		actor := p
		if c.GetHeader("X-Test-Level") == "xa" {
			actor.Roles = []string{"kn_xa"}
		}
		c.Request = c.Request.WithContext(authz.WithPrincipal(context.Background(), actor))
	})
	r.Use(mod.Middleware())
	r.Use(idempotency.Middleware(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Ops)))
	mod.RegisterApp(r.Group("/v1"))
	for _, program := range []string{"information", "training"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/price/weekly-programs/"+program, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			_, cause := mod.weekly.Read(authz.WithPrincipal(context.Background(), p), "kn", program, "")
			t.Logf("weekly service failure: %v", cause)
			t.Fatalf("FN-TI-02.13 %s first draft: HTTP %d want 200: %s", program, w.Code, w.Body.String())
		}
	}
	v, err := mod.weekly.Read(authz.WithPrincipal(context.Background(), p), "kn", "information", "")
	if err != nil {
		t.Fatal(err)
	}
	progress := "1"
	input := app.WeeklyCommand{Version: v.Version, WeekID: v.Report.ID, Action: "save", Rows: []domain.WeeklyPatch{{ID: v.Report.Rows[1].ID, Progress: &progress}}}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	key := ids.New().String()
	call := func(level string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/price/weekly-programs/information/actions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("X-Test-Level", level)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	first := call("tinh")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	denied := call("xa")
	if denied.Code != 403 || denied.Header().Get("Idempotent-Replayed") != "" {
		t.Fatal("current auth must run before cached metadata", denied.Code, denied.Body.String())
	}
	replay := call("tinh")
	if replay.Code != 200 || !bytes.Equal(first.Body.Bytes(), replay.Body.Bytes()) || replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal("shared replay must match exact response", replay.Code, replay.Body.String())
	}

}

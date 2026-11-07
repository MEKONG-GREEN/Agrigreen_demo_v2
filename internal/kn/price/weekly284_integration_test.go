package price

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
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
	r.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(authz.WithPrincipal(context.Background(), p)) })
	mod.RegisterApp(r.Group("/v1"))
	for _, program := range []string{"information", "training"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/price/weekly-programs/"+program, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("FN-TI-02.13 %s first draft: HTTP %d want 200: %s", program, w.Code, w.Body.String())
		}
	}
}

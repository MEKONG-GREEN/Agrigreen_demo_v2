package kn77_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Own composition root: no changes to another author's native fixture or WIP.
// HTTP missing-port probe; deep consumer replay is tested in own app_test.
type deadlineFx209 struct {
	*khtcFx
	fm    *forms.Module
	kn    *extension.Module
	pool  *pgxpool.Pool
	actor uuid.UUID
}

func newDeadlineFx209(t *testing.T) *deadlineFx209 {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	fm, err := forms.New(pool, clk, forms.Deps{Profiles: []formsport.Profile{platformasm.KNKHTCProfile}})
	if err != nil {
		t.Fatal(err)
	}
	kn := extension.New(pool, clk, extension.Deps{})
	cw := &crosswalk{closed: true}
	kn.UseKHTCPeriods(extension.KHTCDeps{Forms: fm.Publisher(), Control: fm.Control(), Crosswalk: cw, MaxSubmissions: 3})
	base := &khtcFx{t: t, clk: clk, cw: cw, tid: ids.New(), node: ids.New()}
	base.eng = httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 8<<20)
	g := base.eng.Group("/v1", func(c *gin.Context) {
		var k authz.KNContext
		_ = json.Unmarshal([]byte(c.GetHeader("X-Test-KN")), &k)
		actor, _ := uuid.Parse(c.GetHeader("X-Test-Actor"))
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindKNTestSession(t, authz.Principal{ActorRef: actor, TenantID: base.tid, KN: &k})))
	}, idempotency.Middleware(ops))
	fm.RegisterApp(g)
	kn.RegisterApp(g)
	f := &deadlineFx209{khtcFx: base, fm: fm, kn: kn, pool: pool, actor: ids.New()}
	code, body := f.do(http.MethodPost, "/v1/extension/khtc/form-template/install", authz.KNAdmin, "", uuid.Nil, "install209", nil)
	f.ok(code, body, nil)
	return f
}

func (f *deadlineFx209) open209() khtcPeriod {
	var p khtcPeriod
	code, body := f.khtc(http.MethodPost, "/periods", f.actor, "open-209", map[string]any{"report_type": "week", "year": 2026, "no": 41, "departments": []string{"trong_trot"}})
	f.ok(code, body, &p)
	return p
}

func TestDeadline209MissingConditionalPortFailsClosed(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	code, body := f.khtc(http.MethodPost, "/periods/"+p.ID+"/deadline", f.actor, "due-209x", map[string]any{"due_at": p.DueAt.Add(time.Hour)})
	if code != http.StatusServiceUnavailable {
		t.Fatalf("missing explicit conditional port: HTTP %d, want503 before any write: %s", code, body)
	}

}

package kn77_test

// #49 KN-GRANT-SCOPE + #71 KN-GRANT-OWNER (BRD CNTS v3.7 FN-14 AC-01…AC-03, BR-02): pf/forms THẬT (kỳ native kn_cnts BM-01)
// + pf/extension THẬT với phép chiếu phiên chủ của module (Module.SessionProjection — như assembly API). Quyền FN-14 trong
// sổ cnts_grant giới hạn công việc/nộp BM-01 theo cơ sở được cấp; thu hồi kèm người nhận chuyển điều kiện công việc chưa nộp
// cho người nhận ngay request kế tiếp, người bị thu hồi mất ngay; bài đã nộp giữ nguyên người nộp lịch sử.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type grantFx struct {
	t    *testing.T
	eng  *gin.Engine
	tid  uuid.UUID
	node uuid.UUID
	clk  *clock.Fixed
	ext  tenant.Beginner
	adm  func(sql string, args ...any) int
}

func newGrantFx(t *testing.T, facs facilityDir) *grantFx {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	adm := env.AdminPool(t, migrations.PF)
	clk := clock.NewFixed(time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)) // 10:00 07/10/2026 giờ VN
	fm, err := forms.New(pool, clk, forms.Deps{Profiles: []formsport.Profile{platformasm.KNCNTSProfile}, Facilities: facs})
	if err != nil {
		t.Fatal(err)
	}
	kn := extension.New(pool, clk, extension.Deps{Orgs: provinceOrgs{}})
	kn.UseFormsPublisher(fm.Publisher())
	fm.UseReviewGuard(platformasm.KNCNTSProfile.Name, kn.CNTSReviewGuard()) // như assembly (#175: chỉ người phụ trách biểu rà soát)
	registry, err := authz.NewSessionRegistry(kn.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	f := &grantFx{t: t, tid: ids.New(), node: ids.New(), clk: clk, ext: shareddb.ForModule(pool, extension.DBModule)}
	f.adm = func(sql string, args ...any) int {
		var n int
		if err := adm.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	f.eng = httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 8<<20)
	g := f.eng.Group("/v1", func(c *gin.Context) {
		var k authz.KNContext
		_ = json.Unmarshal([]byte(c.GetHeader("X-Test-KN")), &k)
		actor, _ := uuid.Parse(c.GetHeader("X-Test-Actor"))
		k.ID = ids.New()
		p := authz.Principal{ActorRef: actor, TenantID: f.tid, KN: &k}
		s, err := registry.ResolveSelected(c.Request.Context(), p)
		if err != nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		p.SelectedSession = &s
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
	}, idempotency.Middleware(ops))
	fm.RegisterApp(g)
	kn.RegisterApp(g)
	return f
}

func (f *grantFx) call(method, path string, actor uuid.UUID, role string, body any) (int, []byte) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	k := authz.KNContext{Role: role, Department: aggDept, OrgRef: f.node, Communes: []string{"X1", "X2"}, Scope: authz.Scope{Units: []string{"X1", "X2"}}}
	if role == authz.KNAdmin {
		k = authz.KNContext{Role: role, Scope: authz.Scope{All: true}}
	}
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	req.Header.Set("X-Test-Actor", actor.String())
	if method == http.MethodPost {
		req.Header.Set(idempotency.Header, "t-"+ids.New().String())
	}
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

// grantRow là một phiên bản sổ cnts_grant (KN) — test gốc lắp ráp không import nội bộ cntsmodel.
type grantRow struct {
	ref, grantee         uuid.UUID
	event, reason        string
	facilities           []string
	successor, succGrant *uuid.UUID
}

// grant ghi một phiên bản sổ cnts_grant như use case FN-14 (granted/revoked) qua vai module — HTTP cấp quyền đã kiểm ở
// cntsmodel (TestCNTSModel_W1147, TestCNTSGrantOwnerSession_71).
func (f *grantFx) grant(g grantRow) {
	f.t.Helper()
	var reason *string
	if g.reason != "" {
		reason = &g.reason
	}
	if err := tenant.WithTenantTx(context.Background(), f.ext, f.tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_extension.cnts_grant (id, tenant_id, stream_key, grant_ref, event,
		    grantee_ref, role, province_code, communes, facility_refs, forms, valid_from, successor_ref, successor_grant_ref, reason,
		    event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, $5, $6, 'ksgm', '89', ARRAY['X1'], $7::text[]::uuid[], ARRAY['BM-01'], DATE '2026-10-07', $8, $9, $10,
		    $11, $12, $13, 'test')`, ids.New(), f.tid, "cgrant:"+g.ref.String(), g.ref, g.event, g.grantee, g.facilities,
			g.successor, g.succGrant, reason, f.clk.Now(), ids.New(), "seed-"+ids.New().String())
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *grantFx) tasks(actor uuid.UUID) map[string]bool {
	f.t.Helper()
	c, b := f.call(http.MethodGet, "/v1/forms/tasks?profile=kn_cnts", actor, authz.KNXa, nil)
	if c != http.StatusOK {
		f.t.Fatalf("tasks: %d %s", c, b)
	}
	var r struct {
		Items []struct {
			Unit string `json:"unit"`
		} `json:"items"`
	}
	_ = json.Unmarshal(b, &r)
	out := map[string]bool{}
	for _, it := range r.Items {
		out[it.Unit] = true
	}
	return out
}

func (f *grantFx) submit(actor uuid.UUID, pid, unit string) int {
	f.t.Helper()
	c, _ := f.call(http.MethodPost, "/v1/forms/periods/"+pid+"/submissions?profile=kn_cnts", actor, authz.KNXa, map[string]any{"unit": unit,
		"values": map[string]string{"trau_bo": "1", "heo": "2", "gia_cam": "3", "de": "0"}, "explanation": "Gửi bổ sung sau hạn do mất mạng"})
	return c
}

func TestCNTSGrantScopeForms_49_71(t *testing.T) {
	facA, facB := ids.New(), ids.New()
	f := newGrantFx(t, facilityDir{facA: "X1", facB: "X1"})
	admin, tinh := ids.New(), ids.New()
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", admin, authz.KNAdmin, nil)
	if c >= 300 {
		t.Fatalf("cài biểu: %d %s", c, b)
	}
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/period-series", tinh, authz.KNTinh, map[string]any{"code": "CNTS-BM01",
		"from_date": "2026-10-01", "to_date": "2026-10-01", "units": []string{facA.String(), facB.String()}})
	var s series
	if c >= 300 || json.Unmarshal(b, &s) != nil || len(s.Periods) != 1 {
		t.Fatalf("phát hành BM-01: %d %s", c, b)
	}
	pid := s.Periods[0].ID
	officer, heir := ids.New(), ids.New()

	// Chưa từng được cấp quyền FN-14 → hành vi W11.07 (không giới hạn cơ sở) giữ nguyên.
	if ts := f.tasks(officer); !ts[facA.String()] || !ts[facB.String()] {
		t.Fatalf("legacy: %v", ts)
	}

	// AC-01/AC-03, BR-02: viên chức KSGM chỉ thấy, chỉ nộp cơ sở được cấp — ngay request kế tiếp.
	ref := ids.New()
	f.grant(grantRow{ref: ref, grantee: officer, event: "granted", facilities: []string{facA.String()}})
	if ts := f.tasks(officer); !ts[facA.String()] || ts[facB.String()] {
		t.Fatalf("AC-03: %v", ts)
	}
	if c := f.submit(officer, pid, facB.String()); c < 400 {
		t.Fatalf("nộp cơ sở chưa cấp: %d", c)
	}
	if c := f.submit(officer, pid, facA.String()); c >= 300 {
		t.Fatalf("nộp cơ sở được cấp: %d", c)
	}
	if ts := f.tasks(heir); !ts[facA.String()] {
		t.Fatalf("người chưa từng được cấp giữ legacy: %v", ts)
	}

	// AC-02: thu hồi kèm người nhận (như RevokeGrant: quyền mới cùng phạm vi + phiên bản revoked).
	succ := ids.New()
	f.grant(grantRow{ref: succ, grantee: heir, event: "granted", reason: "Nhận việc khi thu hồi quyền " + ref.String(),
		facilities: []string{facA.String()}})
	f.grant(grantRow{ref: ref, grantee: officer, event: "revoked", reason: "Chuyển công tác", facilities: []string{facA.String()},
		successor: &heir, succGrant: &succ})
	if ts := f.tasks(officer); len(ts) != 0 {
		t.Fatalf("người bị thu hồi còn công việc: %v", ts)
	}
	if c := f.submit(officer, pid, facA.String()); c < 400 {
		t.Fatalf("người bị thu hồi vẫn nộp được: %d", c)
	}
	if ts := f.tasks(heir); !ts[facA.String()] || ts[facB.String()] {
		t.Fatalf("người nhận chưa nhận việc: %v", ts)
	}
	if c := f.submit(heir, pid, facA.String()); c >= 300 {
		t.Fatalf("người nhận sửa và gửi lại: %d", c)
	}
	// Lịch sử bài nộp không bị viết lại: bản 1 vẫn của người cũ, bản 2 của người nhận.
	if n := f.adm(`SELECT count(*) FROM pf_forms.submission WHERE tenant_id = $1 AND unit_code = $2 AND
	    ((seq = 1 AND submitter_ref = $3) OR (seq = 2 AND submitter_ref = $4))`, f.tid, facA.String(), officer, heir); n != 2 {
		t.Fatalf("người nộp lịch sử: %d", n)
	}
}

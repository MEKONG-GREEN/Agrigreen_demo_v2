package knaudit_test

// AUDIT-LOG-KN (#73): nhật ký hệ thống trên PostgreSQL thật qua HTTP GET /v1/accounts/audit — lõi truy vấn dùng chung #70
// (pf/accounts) + nguồn gốc account_event + nguồn KN, quyền xem và sổ lượt xem của KN (extension.Module.AuditSources/
// AuditAccess), nối như gốc lắp ráp cmd/api-platform. Principal (kèm phiên KN do chủ KN chiếu) do middleware kiểm thử
// đặt — thay cho middleware xác thực + X-AG-Context đã có kiểm thử riêng.
// Kiểm: chỉ Admin KN; mỗi trang (kể cả rỗng) ghi bền một lượt xem có bộ lọc + vị trí dòng trả; lần bị từ chối được
// ghi; bộ ghi lỗi → 503 không dữ liệu; cursor; bộ lọc; tenant; không lộ văn bản tự do; sổ lượt xem không xóa/sửa/cắt được.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const secretName, secretAddr, secretReason = "HTX Bí Mật Riêng", "Số 9 Đường Riêng Tư", "Lý do mở lại riêng tư không được lộ"

// Khóa nguồn/module KN (internal/kn/extension/knaudit) — tests/ chỉ được import gốc module hoặc port/ (archlint R-02).
const (
	srcAuditView, srcProducer, srcPeriod, srcGrant = "kn.audit_view", "kn.producer", "kn.period_event", "kn.cnts_grant"
	modAudit, modCNTS                              = "kn.audit", "kn.cnts"
)

type item struct {
	Source     string    `json:"source"`
	Module     string    `json:"module"`
	EventRef   uuid.UUID `json:"event_ref"`
	ActorRef   uuid.UUID `json:"actor_ref"`
	Department string    `json:"department"`
	Action     string    `json:"action"`
	Outcome    string    `json:"outcome"`
}
type page struct {
	Items      []item   `json:"items"`
	Sources    []string `json:"sources"`
	NextCursor *string  `json:"next_cursor"`
	raw        string
}

type fix struct {
	t      *testing.T
	ctx    context.Context
	pool   tenant.Beginner
	ext    tenant.Beginner // vai r_extension
	kn     *extension.Module
	eng    http.Handler
	who    map[string]authz.Principal
	tenant uuid.UUID
	actor  uuid.UUID // người thao tác trong các sổ KN
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	f := &fix{t: t, ctx: context.Background(), pool: pool, ext: shareddb.ForModule(pool, extension.DBModule), who: map[string]authz.Principal{},
		tenant: ids.New(), actor: ids.New()}
	f.kn = extension.New(pool, clock.System{}, extension.Deps{})
	f.wire(f.kn.AuditAccess())
	return f
}

// wire dựng pf/accounts + đăng ký nguồn như cmd/api-platform/assembly, với cặp quyền xem/bộ ghi cho trước.
func (f *fix) wire(access accport.AuditAccess) {
	acc := accounts.New(f.pool, clock.System{}, accounts.Deps{})
	reg, err := accport.NewAuditRegistry(append([]accport.AuditSource{acc.NativeAuditSource()}, f.kn.AuditSources()...)...)
	if err != nil {
		f.t.Fatal(err)
	}
	acc.UseAudit(reg, access)
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	acc.RegisterAdmin(eng.Group("/v1", func(c *gin.Context) {
		if p, ok := f.who[c.GetHeader("X-Test-Who")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	}))
	f.eng = eng
}

// as đăng ký một người xem (Principal có MFA, kèm phiên KN nếu k khác nil) và trả khóa gọi cùng mã người xem.
func (f *fix) as(tid uuid.UUID, k *knport.SessionContext, roles ...string) (string, uuid.UUID) {
	f.t.Helper()
	p := authz.Principal{ActorRef: ids.New(), TenantID: tid, MFA: true, Roles: roles}
	if k != nil {
		s, err := knport.NewSessionContext(*k)
		if err != nil {
			f.t.Fatal(err)
		}
		p.SelectedSession = &s
	}
	key := p.ActorRef.String()
	f.who[key] = p
	return key, p.ActorRef
}

func (f *fix) get(who string, q url.Values) (int, page) {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/accounts/audit?"+q.Encode(), nil)
	req.Header.Set("X-Test-Who", who)
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	var p page
	p.raw = w.Body.String()
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			f.t.Fatalf("giải JSON: %v %s", err, p.raw)
		}
	}
	return w.Code, p
}

func window() url.Values {
	now := time.Now().UTC()
	return url.Values{"from": {now.Add(-time.Hour).Format(time.RFC3339Nano)}, "to": {now.Add(time.Hour).Format(time.RFC3339Nano)}}
}

func with(q url.Values, kv ...string) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out.Set(kv[i], kv[i+1])
	}
	return out
}

func (f *fix) exec(tid uuid.UUID, sql string, args ...any) {
	f.t.Helper()
	if err := tenant.WithTenantTx(f.ctx, f.ext, tid, func(tx pgx.Tx) error { _, err := tx.Exec(f.ctx, sql, args...); return err }); err != nil {
		f.t.Fatalf("%s: %v", sql[:40], err)
	}
}

// seed ghi 3 dòng sổ KN (hồ sơ, cấp quyền CNTS, mở lại kỳ báo cáo) theo thứ tự thời gian, kèm văn bản tự do bí mật.
func (f *fix) seed(tid uuid.UUID) {
	now := time.Now().UTC()
	producer := ids.New()
	f.exec(tid, `INSERT INTO pf_extension.kn_producer (id, tenant_id, stream_key, producer_ref, kind, event, commune_code, name, person_ref,
	  address, event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, 'kn_producer:' || $3::uuid::text, $3::uuid, 'cooperative', 'created', '30001',
	  $4, $5, $6, $7, $8, $9, 't')`, ids.New(), tid, producer, secretName, ids.New(), secretAddr, now, f.actor, "p:"+producer.String())
	grant := ids.New()
	f.exec(tid, `INSERT INTO pf_extension.cnts_grant (id, tenant_id, stream_key, grant_ref, event, grantee_ref, role, province_code, communes,
	  valid_from, event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, 'cgrant:' || $3::uuid::text, $3::uuid, 'granted', $4, 'ksgm', '89',
	  '{30001}', '2026-10-01', $5, $6, $7, 't')`, ids.New(), tid, grant, ids.New(), now, f.actor, "g:"+grant.String())
	period := ids.New()
	f.exec(tid, `INSERT INTO pf_extension.period_event (id, tenant_id, stream_key, period_id, batch_id, action, reason, reopen_until,
	  department_code, event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, 'period:' || $3::uuid::text, $3::uuid, $4, 'reopened', $5, $6,
	  'trong_trot', $7, $8, $9, 't')`, ids.New(), tid, period, ids.New(), secretReason, now.Add(48*time.Hour), now, f.actor, "pe:"+period.String())
}

func admin() *knport.SessionContext {
	return &knport.SessionContext{ID: ids.New(), Role: knport.SessionRoleAdmin}
}

type viewRow struct {
	Outcome, Denial, Hash, Module, Dept, Action string
	FilterActor                                 *uuid.UUID
	Limit                                       *int
	Sources, RetSources                         []string
	RetRefs                                     []string
	Session                                     *uuid.UUID
}

func (f *fix) views(tid, reader uuid.UUID) []viewRow {
	f.t.Helper()
	var out []viewRow
	err := tenant.WithTenantTx(f.ctx, f.ext, tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(f.ctx, `SELECT outcome, coalesce(denial_code, ''), coalesce(filter_hash, ''), coalesce(filter_module, ''),
		  coalesce(filter_department, ''), coalesce(filter_action, ''), filter_actor_ref, page_limit, sources, returned_sources, returned_refs::text[], session_ref
		  FROM pf_extension.kn_audit_view WHERE actor_ref = $1 ORDER BY recorded_at, seq`, reader)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v viewRow
			if err := rows.Scan(&v.Outcome, &v.Denial, &v.Hash, &v.Module, &v.Dept, &v.Action, &v.FilterActor, &v.Limit, &v.Sources, &v.RetSources, &v.RetRefs, &v.Session); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func TestAdminReadsKNSourcesAndEveryPageIsRecorded_AUDIT73(t *testing.T) {
	f := newFix(t)
	f.seed(f.tenant)
	sess := admin()
	who, reader := f.as(f.tenant, sess)

	in := with(window(), "actor_ref", f.actor.String())
	code, p := f.get(who, in)
	if code != http.StatusOK {
		t.Fatalf("Admin KN đọc nhật ký: %d %s", code, p.raw)
	}
	got := []string{}
	for _, it := range p.Items {
		got = append(got, it.Source+"/"+it.Action+"/"+it.Outcome)
	}
	want := []string{srcPeriod + "/period.reopened/succeeded", srcGrant + "/cnts_grant.granted/succeeded", srcProducer + "/producer.created/succeeded"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("thứ tự/nội dung:\n có  %v\n muốn %v", got, want)
	}
	if p.Items[0].Department != "trong_trot" {
		t.Errorf("kỳ báo cáo phải mang phòng ban: %+v", p.Items[0])
	}
	for _, s := range []string{"accounts.account_event", srcAuditView, srcProducer, srcGrant} {
		if !strings.Contains(strings.Join(p.Sources, ","), s) {
			t.Errorf("độ phủ nguồn thiếu %s: %v", s, p.Sources)
		}
	}
	// Không lộ văn bản tự do (tên HTX, địa chỉ, lý do, mã xã) dù có trong sổ nguồn.
	for _, s := range []string{secretName, secretAddr, secretReason, "30001"} {
		if strings.Contains(p.raw, s) {
			t.Errorf("trang nhật ký lộ %q: %s", s, p.raw)
		}
	}
	// Lượt xem đã ghi bền trước khi trả: đúng một dòng, bộ lọc và vị trí các dòng đã trả.
	v := f.views(f.tenant, reader)
	if len(v) != 1 || v[0].Outcome != "succeeded" || len(v[0].Hash) != 64 || v[0].FilterActor == nil || *v[0].FilterActor != f.actor ||
		v[0].Limit == nil || *v[0].Limit != 50 || len(v[0].RetRefs) != 3 || v[0].RetRefs[0] != p.Items[0].EventRef.String() ||
		v[0].RetSources[0] != srcPeriod || v[0].Session == nil || *v[0].Session != sess.ID {
		t.Fatalf("sổ lượt xem: %+v", v)
	}

	// Trang rỗng cũng ghi một lượt xem.
	now := time.Now().UTC()
	empty := url.Values{"from": {now.Add(-90 * 24 * time.Hour).Format(time.RFC3339)}, "to": {now.Add(-80 * 24 * time.Hour).Format(time.RFC3339)}, "module": {modCNTS}}
	if code, p2 := f.get(who, empty); code != http.StatusOK || len(p2.Items) != 0 {
		t.Fatalf("trang rỗng: %d %s", code, p2.raw)
	}
	v = f.views(f.tenant, reader)
	if len(v) != 2 || v[1].Module != modCNTS || len(v[1].RetRefs) != 0 || strings.Join(v[1].Sources, ",") != srcGrant {
		t.Fatalf("lượt xem trang rỗng: %+v", v)
	}

	// Lượt xem hiện lại như một nguồn nhật ký (module kn.audit) cho Admin.
	code, views := f.get(who, with(window(), "actor_ref", reader.String(), "module", modAudit))
	if code != http.StatusOK || len(views.Items) != 2 || views.Items[0].Action != "audit.viewed" || views.Items[0].Outcome != "succeeded" {
		t.Fatalf("nguồn sổ lượt xem: %d %s", code, views.raw)
	}
}

func TestFiltersAndCursorPaging_AUDIT73(t *testing.T) {
	f := newFix(t)
	f.seed(f.tenant)
	who, reader := f.as(f.tenant, admin())
	base := with(window(), "actor_ref", f.actor.String())

	for name, c := range map[string]struct {
		kv   []string
		want string
	}{
		"phòng ban":  {[]string{"department", "trong_trot"}, "period.reopened"},
		"module":     {[]string{"module", modCNTS}, "cnts_grant.granted"},
		"thao tác":   {[]string{"action", "producer.created"}, "producer.created"},
		"mã lạ":      {[]string{"action", "producer.deleted"}, ""},
		"phòng khác": {[]string{"department", "chan_nuoi_thuy_san"}, ""},
	} {
		code, p := f.get(who, with(base, c.kv...))
		if code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, code, p.raw)
		}
		got := ""
		for _, it := range p.Items {
			got += it.Action
		}
		if got != c.want {
			t.Errorf("%s: có %q, muốn %q", name, got, c.want)
		}
	}

	// Cursor: limit 1 → 3 trang, không trùng, giảm dần; trang cuối không có next_cursor.
	in := with(base, "limit", "1")
	seen := []string{}
	for i := 0; i < 4; i++ {
		code, p := f.get(who, in)
		if code != http.StatusOK {
			t.Fatalf("trang %d: %d %s", i+1, code, p.raw)
		}
		for _, it := range p.Items {
			seen = append(seen, it.Action)
		}
		if p.NextCursor == nil {
			break
		}
		in = with(in, "cursor", *p.NextCursor)
	}
	if strings.Join(seen, ",") != "period.reopened,cnts_grant.granted,producer.created" {
		t.Fatalf("phân trang: %v", seen)
	}
	// Cursor gắn bộ lọc: đổi bộ lọc giữa chừng bị từ chối.
	_, first := f.get(who, with(base, "limit", "1"))
	if code, p := f.get(who, with(base, "limit", "1", "module", modCNTS, "cursor", *first.NextCursor)); code != http.StatusUnprocessableEntity {
		t.Fatalf("cursor với bộ lọc khác: muốn 422, có %d %s", code, p.raw)
	}
	// Khoảng > 93 ngày, khóa lạ (tenant) bị từ chối.
	now := time.Now().UTC()
	if code, _ := f.get(who, url.Values{"from": {now.Add(-94 * 24 * time.Hour).Format(time.RFC3339)}, "to": {now.Format(time.RFC3339)}}); code != http.StatusUnprocessableEntity {
		t.Fatalf("khoảng 94 ngày: muốn 422, có %d", code)
	}
	if code, _ := f.get(who, with(base, "tenant_id", ids.New().String())); code != http.StatusUnprocessableEntity {
		t.Fatalf("tham số tenant: muốn 422, có %d", code)
	}
	// Mỗi trang thành công có đúng một lượt xem (5 bộ lọc + 3 trang + 1 trang đầu của phép thử cursor).
	if n := len(f.views(f.tenant, reader)); n != 9 {
		t.Fatalf("số lượt xem = %d, muốn 9", n)
	}
}

func TestOnlyAdminAndDeniedReadsAreRecorded_AUDIT73(t *testing.T) {
	f := newFix(t)
	f.seed(f.tenant)
	for name, c := range map[string]struct {
		k      *knport.SessionContext
		roles  []string
		denial string
	}{
		"Cấp Tỉnh":              {&knport.SessionContext{ID: ids.New(), Role: knport.SessionRoleProvince, Department: "trong_trot", OrgRef: ids.New()}, nil, "not_admin"},
		"Admin đang hỗ trợ":     {&knport.SessionContext{ID: ids.New(), Role: knport.SessionRoleAdmin, Support: &knport.SessionSupport{SessionID: ids.New()}}, nil, "support_mode"},
		"vai realm, chưa phiên": {nil, []string{"platform_admin", "account_admin"}, "no_session"},
	} {
		who, reader := f.as(f.tenant, c.k, c.roles...)
		code, p := f.get(who, window())
		if code < 400 || code >= 500 || strings.Contains(p.raw, "\"items\"") {
			t.Fatalf("%s: muốn 4xx không dữ liệu, có %d %s", name, code, p.raw)
		}
		v := f.views(f.tenant, reader)
		if len(v) != 1 || v[0].Outcome != "denied" || v[0].Denial != c.denial || v[0].Hash != "" || len(v[0].RetRefs) != 0 {
			t.Fatalf("%s: lượt bị từ chối: %+v", name, v)
		}
	}
	// Thiếu MFA: lõi #70 từ chối trước chủ KN (chưa ghi — câu hỏi PO 4).
	who, _ := f.as(f.tenant, admin())
	p := f.who[who]
	p.MFA = false
	f.who[who] = p
	if code, _ := f.get(who, window()); code != http.StatusForbidden {
		t.Fatalf("thiếu MFA: muốn 403, có %d", code)
	}
}

func TestRecorderFailureReturnsNoData_AUDIT73(t *testing.T) {
	f := newFix(t)
	f.seed(f.tenant)
	f.wire(accport.AuditAccess{Authorize: f.kn.AuditAccess().Authorize, RecordRead: func(context.Context, accport.AuditReadReceipt) error {
		return errors.New("sổ lượt xem không ghi được")
	}})
	who, reader := f.as(f.tenant, admin())
	code, p := f.get(who, window())
	if code != http.StatusServiceUnavailable || strings.Contains(p.raw, "\"items\"") {
		t.Fatalf("bộ ghi lỗi: muốn 503 không dữ liệu, có %d %s", code, p.raw)
	}
	if v := f.views(f.tenant, reader); len(v) != 0 {
		t.Fatalf("không được có lượt xem khi bộ ghi lỗi: %+v", v)
	}
}

func TestTenantIsolation_AUDIT73(t *testing.T) {
	f := newFix(t)
	f.seed(f.tenant)
	whoA, readerA := f.as(f.tenant, admin())
	if code, p := f.get(whoA, window()); code != http.StatusOK {
		t.Fatalf("%d %s", code, p.raw)
	}
	other := ids.New()
	whoB, _ := f.as(other, admin())
	if code, p := f.get(whoB, with(window(), "actor_ref", f.actor.String())); code != http.StatusOK || len(p.Items) != 0 {
		t.Fatalf("tenant B thấy dòng của tenant A: %d %s", code, p.raw)
	}
	if code, p := f.get(whoB, with(window(), "actor_ref", readerA.String())); code != http.StatusOK || len(p.Items) != 0 {
		t.Fatalf("tenant B thấy lượt xem của tenant A: %d %s", code, p.raw)
	}
	if v := f.views(other, readerA); len(v) != 0 {
		t.Fatalf("RLS: lượt xem tenant A đọc được dưới tenant B: %+v", v)
	}
}

// Lưu giữ ≥ 3 tháng: không vai nào (kể cả quản trị DB) xóa, sửa, cắt được sổ lượt xem; sổ đủ trigger bất biến; không
// hàm DB nào nhắc bảng (không có thủ tục dọn).
func TestAuditViewRetention_AUDIT73(t *testing.T) {
	f := newFix(t)
	who, reader := f.as(f.tenant, admin())
	if code, p := f.get(who, window()); code != http.StatusOK {
		t.Fatalf("%d %s", code, p.raw)
	}
	if len(f.views(f.tenant, reader)) != 1 {
		t.Fatal("chưa có lượt xem để thử")
	}
	for _, sql := range []string{
		`UPDATE pf_extension.kn_audit_view SET outcome = 'denied' WHERE actor_ref = $1`,
		`DELETE FROM pf_extension.kn_audit_view WHERE actor_ref = $1`,
	} {
		err := tenant.WithTenantTx(f.ctx, f.ext, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(f.ctx, sql, reader); return err })
		if err == nil {
			t.Errorf("vai r_extension chạy được: %s", sql)
		}
	}
	env := pgtest.Get(t)
	adminPool := env.AdminPool(t, migrations.PF)
	for _, sql := range []string{
		`DELETE FROM pf_extension.kn_audit_view WHERE actor_ref = '` + reader.String() + `'`,
		`UPDATE pf_extension.kn_audit_view SET outcome = 'denied' WHERE actor_ref = '` + reader.String() + `'`,
		`TRUNCATE pf_extension.kn_audit_view`,
	} {
		if _, err := adminPool.Exec(f.ctx, sql); err == nil {
			t.Errorf("quản trị DB chạy được: %s", sql)
		}
	}
	if len(f.views(f.tenant, reader)) != 1 {
		t.Fatal("lượt xem bị mất")
	}
	var missing, funcs, parts int
	if err := adminPool.QueryRow(f.ctx, `SELECT count(*) FROM shared.v_missing_immutability WHERE table_name LIKE 'kn_audit_view%'`).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if err := adminPool.QueryRow(f.ctx, `SELECT count(*) FROM pg_proc WHERE prosrc ILIKE '%kn_audit_view%'`).Scan(&funcs); err != nil {
		t.Fatal(err)
	}
	if err := adminPool.QueryRow(f.ctx, `SELECT count(*) FROM pg_inherits WHERE inhparent = 'pf_extension.kn_audit_view'::regclass`).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if missing != 0 || funcs != 0 || parts < 4 {
		t.Fatalf("thiếu bất biến=%d, hàm nhắc bảng=%d, phân vùng=%d", missing, funcs, parts)
	}
}

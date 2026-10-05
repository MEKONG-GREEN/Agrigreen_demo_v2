package handler_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/pdftest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// Đơn vị công tác dùng chung trong các kiểm thử.
var (
	orgNational = uuid.MustParse("0191e5a0-0000-7000-8000-00000000a001") // TTKN QG — toàn quốc
	orgP89      = uuid.MustParse("0191e5a0-0000-7000-8000-00000000a089") // TTKN tỉnh 89
	orgP92      = uuid.MustParse("0191e5a0-0000-7000-8000-00000000a092") // TTKN tỉnh 92
	orgC30001   = uuid.MustParse("0191e5a0-0000-7000-8000-00000000c001") // tổ KN xã 30001
)

type fixture struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	app    *httptest.Server
	admin  *httptest.Server
	mod    *content.Module
	pool   *pgxpool.Pool
	tenant uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, content.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	sc := newFakeScope()
	sc.set(orgNational, authz.Scope{All: true})
	sc.set(orgP89, authz.Scope{Units: []string{"89", "30001", "30002"}})
	sc.set(orgP92, authz.Scope{Units: []string{"92", "31001"}})
	sc.set(orgC30001, authz.Scope{Units: []string{"30001"}})
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{PresignSecret: []byte("khoa-kiem-thu"), BaseURL: "http://files.test/_objstore"})
	if err != nil {
		t.Fatal(err)
	}
	// ADR-0028 (W2.3): module chạy trên vai đăng nhập app_platform (NOINHERIT, không quyền bảng) — mọi giao dịch
	// phải SET LOCAL ROLE r_content mới chạm được pf_content. pool (content_app) chỉ dùng để kiểm trực tiếp.
	mod := content.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, content.Deps{Scope: sc, Catalogs: newFakeCatalogs(), AdminUnits: newFakeUnits(), Store: st})
	log := telemetry.NewLogger("test", io.Discard)

	// Giới hạn thân chung 1 MB như api-platform; đường tải tệp có giới hạn riêng (httpx.AllowBody).
	appEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	appSrv, adminSrv := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(appSrv.Close)
	t.Cleanup(adminSrv.Close)
	return &fixture{t: t, iss: iss, app: appSrv, admin: adminSrv, mod: mod, pool: pool, tenant: ids.New()}
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	actor uuid.UUID
	token string
}

func (f *fixture) as(org uuid.UUID, roles ...string) caller {
	return f.asTenant(f.tenant, org, roles...)
}

func (f *fixture) asTenant(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})
	return caller{f: f, tid: tid, org: org, actor: actor, token: tok}
}

type resp struct {
	status int
	hdr    http.Header
	body   map[string]any
}

func (r resp) code() string { s, _ := r.body["code"].(string); return s }

func (c caller) raw(base, method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return resp{status: r.StatusCode, hdr: r.Header, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) do(base, method, path string, body any) resp {
	c.f.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	idem := ""
	if method == http.MethodPost {
		idem = key()
	}
	return c.raw(base, method, path, idem, "application/json", b)
}

func (c caller) appGet(path string) resp { return c.do(c.f.app.URL, http.MethodGet, path, nil) }
func (c caller) adminGet(path string) resp {
	return c.do(c.f.admin.URL, http.MethodGet, path, nil)
}
func (c caller) adminPost(path string, body any) resp {
	return c.do(c.f.admin.URL, http.MethodPost, path, body)
}

func (c caller) upload(path, name string, data []byte, sha string) resp {
	c.f.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if sha != "" {
		_ = mw.WriteField("sha256", sha)
	}
	w, _ := mw.CreateFormFile("file", name)
	_, _ = w.Write(data)
	_ = mw.Close()
	return c.raw(c.f.admin.URL, http.MethodPost, path, key(), mw.FormDataContentType(), buf.Bytes())
}

func get(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func str(m map[string]any, path ...string) string { s, _ := get(m, path...).(string); return s }

func items(r resp) []map[string]any {
	raw, _ := r.body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

func refsOf(r resp) []string {
	var out []string
	for _, it := range items(r) {
		out = append(out, it["ref"].(string))
	}
	return out
}

func expect(t *testing.T, r resp, status int, code string) {
	t.Helper()
	if r.status != status || (code != "" && r.code() != code) {
		t.Fatalf("muốn %d %s, nhận %d %v", status, code, r.status, r.body)
	}
}

// Người soạn/duyệt dùng chung.
type actors struct {
	natEditor, natReviewer, natReviewer2, p89Editor, p89Reviewer, p92Editor, p92Reviewer caller
	readerNat, reader89, reader30001, reader92                                           caller
}

func (f *fixture) actors() actors {
	return actors{
		natEditor:    f.as(orgNational, port.RoleEditorNational),
		natReviewer:  f.as(orgNational, port.RoleReviewer),
		natReviewer2: f.as(orgNational, port.RoleReviewer),
		p89Editor:    f.as(orgP89, port.RoleEditorProvince),
		p89Reviewer:  f.as(orgP89, port.RoleReviewer),
		p92Editor:    f.as(orgP92, port.RoleEditorProvince),
		p92Reviewer:  f.as(orgP92, port.RoleReviewer),
		readerNat:    f.as(orgNational),
		reader89:     f.as(orgP89),
		reader30001:  f.as(orgC30001),
		reader92:     f.as(orgP92),
	}
}

type doc struct {
	ref string
	no  int
}

func createDoc(t *testing.T, ed caller, body map[string]any) doc {
	t.Helper()
	r := ed.adminPost("/v1/documents", body)
	expect(t, r, http.StatusCreated, "")
	return doc{ref: str(r.body, "document", "ref"), no: int(get(r.body, "version", "version_no").(float64))}
}

// publish gửi duyệt (người soạn) rồi xuất bản (người duyệt).
func publish(t *testing.T, ed, rv caller, d doc) resp {
	t.Helper()
	expect(t, ed.adminPost("/v1/documents/"+d.ref+"/submit", map[string]any{"version_no": d.no}), http.StatusOK, "")
	r := rv.adminPost("/v1/documents/"+d.ref+"/publish", map[string]any{"version_no": d.no})
	expect(t, r, http.StatusOK, "")
	return r
}

func procedureBody(title string) map[string]any {
	return map[string]any{
		"kind": "procedure", "audience": map[string]any{"level": "national"},
		"title": title, "summary": "Quy trình chuẩn quốc gia", "body": "# " + title + "\n\nBước 1: làm đất.",
		"tags": []string{"rice_variety:OM18", "farm_stage:sowing"},
	}
}

func (f *fixture) outboxCount(tid uuid.UUID, eventType, docRef string) int {
	f.t.Helper()
	var n int
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND payload->>'document_ref' = $2`,
			eventType, docRef).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// ---------------------------------------------------------------------------

// KN FR-12/US-LIB-01, RB-1: xuất bản phiên bản mới không ghi đè bản cũ; chuỗi băm xác minh được.
func TestPublishNewVersionKeepsOld(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	d := createDoc(t, a.natEditor, procedureBody("Quy trình canh tác lúa giảm phát thải"))
	pub := publish(t, a.natEditor, a.natReviewer, d)
	if str(pub.body, "document", "status") != "published" || str(pub.body, "version", "status") != "published" {
		t.Fatalf("xuất bản: %v", pub.body)
	}

	// Bản 2: sửa nội dung, bản 1 vẫn hiển thị cho tới khi bản 2 được duyệt.
	r := a.natEditor.adminPost("/v1/documents/"+d.ref+"/versions", map[string]any{
		"title": "Quy trình canh tác lúa giảm phát thải (sửa đổi 2026)", "body": "Bước 1: làm đất kỹ.", "tags": []string{"rice_variety:OM18"},
	})
	expect(t, r, http.StatusCreated, "")
	v2 := doc{ref: d.ref, no: 2}
	if got := str(a.reader30001.appGet("/v1/documents/"+d.ref).body, "version", "title"); got != "Quy trình canh tác lúa giảm phát thải" {
		t.Fatalf("người đọc phải thấy bản 1 khi bản 2 chưa duyệt: %q", got)
	}
	publish(t, a.natEditor, a.natReviewer2, v2)

	g := a.reader30001.appGet("/v1/documents/" + d.ref)
	expect(t, g, http.StatusOK, "")
	if str(g.body, "version", "title") != "Quy trình canh tác lúa giảm phát thải (sửa đổi 2026)" || get(g.body, "document", "published_version_no").(float64) != 2 {
		t.Fatalf("bản hiện hành sai: %v", g.body)
	}
	hs := items(a.reader30001.appGet("/v1/documents/" + d.ref + "/versions"))
	if len(hs) != 2 || hs[0]["status"] != "superseded" || hs[1]["status"] != "published" {
		t.Fatalf("lịch sử phiên bản: %v", hs)
	}
	// Bản 1 vẫn đọc nguyên văn trên bề mặt quản trị.
	v1 := a.natEditor.adminGet("/v1/documents/" + d.ref + "/versions/1")
	if str(v1.body, "version", "title") != "Quy trình canh tác lúa giảm phát thải" || str(v1.body, "version", "status") != "superseded" {
		t.Fatalf("bản 1: %v", v1.body)
	}

	ref := uuid.MustParse(d.ref)
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		for table, rows := range map[string]int64{"pf_content.document_version": 2, "pf_content.document_event": 4} {
			res, err := ledger.Verify(context.Background(), tx, table, f.tenant, "doc:"+ref.String())
			if err != nil {
				return err
			}
			if !res.OK || res.RowsChecked != rows {
				return fmt.Errorf("%s: %+v", table, res)
			}
		}
		// Sổ chỉ ghi thêm: vai ứng dụng không sửa được phiên bản cũ (chạy trong savepoint).
		sp, err := tx.Begin(context.Background())
		if err != nil {
			return err
		}
		defer func() { _ = sp.Rollback(context.Background()) }()
		if _, err := sp.Exec(context.Background(), "UPDATE pf_content.document_version SET title = 'x' WHERE document_ref = $1", ref); err == nil {
			return fmt.Errorf("UPDATE sổ phải bị chặn")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := f.outboxCount(f.tenant, port.EventDocumentPublished, d.ref); n != 2 {
		t.Fatalf("muốn 2 sự kiện document.published, có %d", n)
	}
}

// KN BR-06, ADR-0009: TTKN tỉnh không soạn phạm vi toàn quốc, không soạn quy
// trình chuẩn, không soạn cho tỉnh khác; người duyệt tỉnh không xuất bản tài liệu toàn quốc.
func TestProvinceEditorScope(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	guide := func(level string, units ...string) map[string]any {
		return map[string]any{"kind": "guide", "audience": map[string]any{"level": level, "units": units}, "title": "Sổ tay bón phân", "body": "Nội dung"}
	}
	expect(t, a.p89Editor.adminPost("/v1/documents", guide("national")), http.StatusForbidden, "content.out_of_scope")
	expect(t, a.p89Editor.adminPost("/v1/documents", procedureBody("Quy trình tỉnh")), http.StatusForbidden, "content.kind_not_allowed")
	expect(t, a.p89Editor.adminPost("/v1/documents", guide("province", "92")), http.StatusForbidden, "content.out_of_scope")
	expect(t, a.p89Editor.adminPost("/v1/documents", guide("commune", "30001", "31001")), http.StatusForbidden, "content.out_of_scope")
	expect(t, a.p89Editor.adminPost("/v1/documents", guide("province", "30001")), http.StatusUnprocessableEntity, "content.audience_invalid")
	d := createDoc(t, a.p89Editor, guide("commune", "30001", "30002"))
	g := a.p89Editor.adminGet("/v1/documents/" + d.ref)
	if str(g.body, "document", "audience", "province_code") != "89" {
		t.Fatalf("tỉnh sở hữu của tài liệu cấp xã: %v", g.body)
	}

	// Tài liệu toàn quốc: người duyệt tỉnh bị chặn, người duyệt TW xuất bản được.
	nd := createDoc(t, a.natEditor, guide("national"))
	expect(t, a.natEditor.adminPost("/v1/documents/"+nd.ref+"/submit", map[string]any{"version_no": 1}), http.StatusOK, "")
	expect(t, a.p89Reviewer.adminPost("/v1/documents/"+nd.ref+"/publish", map[string]any{"version_no": 1}), http.StatusForbidden, "content.out_of_scope")
	expect(t, a.natReviewer.adminPost("/v1/documents/"+nd.ref+"/publish", map[string]any{"version_no": 1}), http.StatusOK, "")

	// Tài liệu tỉnh 89: người soạn/duyệt tỉnh 92 không thấy (404), người duyệt 89 xuất bản.
	expect(t, a.p92Editor.adminPost("/v1/documents/"+d.ref+"/submit", map[string]any{"version_no": 1}), http.StatusNotFound, "content.document_not_found")
	expect(t, a.p89Editor.adminPost("/v1/documents/"+d.ref+"/submit", map[string]any{"version_no": 1}), http.StatusOK, "")
	expect(t, a.p92Reviewer.adminPost("/v1/documents/"+d.ref+"/publish", map[string]any{"version_no": 1}), http.StatusNotFound, "content.document_not_found")
	expect(t, a.p89Reviewer.adminPost("/v1/documents/"+d.ref+"/publish", map[string]any{"version_no": 1}), http.StatusOK, "")

	// Người đọc không có vai soạn không dùng được Admin API.
	expect(t, a.reader89.adminPost("/v1/documents", guide("province", "89")), http.StatusForbidden, "forbidden")
}

// R-56: người duyệt không được xuất bản phiên bản do chính mình soạn.
func TestAuthorCannotSelfApprove(t *testing.T) {
	f := newFixture(t)
	both := f.as(orgNational, port.RoleEditorNational, port.RoleReviewer)
	d := createDoc(t, both, procedureBody("Quy trình tưới ngập khô xen kẽ"))
	expect(t, both.adminPost("/v1/documents/"+d.ref+"/submit", map[string]any{"version_no": 1}), http.StatusOK, "")
	expect(t, both.adminPost("/v1/documents/"+d.ref+"/publish", map[string]any{"version_no": 1}), http.StatusForbidden, "self_approval_forbidden")
	// Người duyệt khác trả lại (có lý do), rồi người khác xuất bản bản mới.
	rv := f.as(orgNational, port.RoleReviewer)
	expect(t, rv.adminPost("/v1/documents/"+d.ref+"/return", map[string]any{"version_no": 1, "reason": "ngắn"}), http.StatusUnprocessableEntity, "validation_failed")
	expect(t, rv.adminPost("/v1/documents/"+d.ref+"/return", map[string]any{"version_no": 1, "reason": "Cần bổ sung liều lượng phân bón"}), http.StatusOK, "")
	expect(t, rv.adminPost("/v1/documents/"+d.ref+"/publish", map[string]any{"version_no": 1}), http.StatusConflict, "content.invalid_transition")
	expect(t, both.adminPost("/v1/documents/"+d.ref+"/submit", map[string]any{"version_no": 1}), http.StatusOK, "")
	expect(t, rv.adminPost("/v1/documents/"+d.ref+"/publish", map[string]any{"version_no": 1}), http.StatusOK, "")
}

// KN FR-13/US-LIB-02: hướng dẫn đặc thù tỉnh gắn quy trình TW, chỉ hiển thị trong tỉnh đó.
func TestLocalSupplementScope(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	draft := createDoc(t, a.natEditor, procedureBody("Quy trình chưa xuất bản"))
	sup := func(parent string) map[string]any {
		return map[string]any{"kind": "supplement", "audience": map[string]any{"level": "province", "units": []string{"89"}}, "parent_ref": parent,
			"title": "Bổ sung An Giang: đất phèn", "body": "Bón vôi trước khi sạ."}
	}
	expect(t, a.p89Editor.adminPost("/v1/documents", sup(draft.ref)), http.StatusUnprocessableEntity, "content.parent_invalid")

	proc := createDoc(t, a.natEditor, procedureBody("Quy trình sản xuất lúa chất lượng cao"))
	publish(t, a.natEditor, a.natReviewer, proc)
	s := createDoc(t, a.p89Editor, sup(proc.ref))
	publish(t, a.p89Editor, a.p89Reviewer, s)

	// Xã 30001 (tỉnh 89) thấy bản bổ sung — trong danh sách và trong chi tiết quy trình gốc.
	if refs := refsOf(a.reader30001.appGet("/v1/documents?kind=supplement")); !slices.Contains(refs, s.ref) {
		t.Fatalf("xã 30001 phải thấy bản bổ sung: %v", refs)
	}
	g := a.reader30001.appGet("/v1/documents/" + proc.ref)
	sups, _ := g.body["supplements"].([]any)
	if len(sups) != 1 || sups[0].(map[string]any)["ref"] != s.ref || sups[0].(map[string]any)["province_code"] != "89" {
		t.Fatalf("chi tiết quy trình gốc phải kèm bản bổ sung tỉnh 89: %v", g.body)
	}
	// Tỉnh 92 không thấy.
	if refs := refsOf(a.reader92.appGet("/v1/documents")); slices.Contains(refs, s.ref) || !slices.Contains(refs, proc.ref) {
		t.Fatalf("tỉnh 92: phải thấy quy trình gốc, không thấy bản bổ sung 89: %v", refs)
	}
	expect(t, a.reader92.appGet("/v1/documents/"+s.ref), http.StatusNotFound, "content.document_not_found")
	if sups, _ := a.reader92.appGet("/v1/documents/" + proc.ref).body["supplements"].([]any); len(sups) != 0 {
		t.Fatalf("tỉnh 92 không được thấy bản bổ sung 89: %v", sups)
	}
	// Bản nháp không hiển thị cho người đọc.
	expect(t, a.readerNat.appGet("/v1/documents/"+draft.ref), http.StatusNotFound, "content.document_not_found")
}

// US-LIB-04: tìm không dấu ("lua" tìm được "lúa"), lọc theo loại và nhãn.
func TestSearchAccentInsensitive(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	d := createDoc(t, a.natEditor, procedureBody("Quy trình canh tác Lúa Đông Xuân"))
	publish(t, a.natEditor, a.natReviewer, d)
	news := createDoc(t, a.natEditor, map[string]any{"kind": "news", "audience": map[string]any{"level": "national"}, "category": "pest_warning",
		"title": "Rầy nâu gây hại", "body": "Theo dõi mật độ rầy trên ruộng.", "tags": []string{"pest:ray_nau"}})
	publish(t, a.natEditor, a.natReviewer, news)

	for q, want := range map[string]bool{"lua": true, "LÚA đông": true, "dong xuan": true, "canh ta": true, "ca phe": false} {
		refs := refsOf(a.reader30001.appGet("/v1/documents?q=" + url.QueryEscape(q)))
		if slices.Contains(refs, d.ref) != want {
			t.Errorf("q=%q: %v (muốn thấy = %v)", q, refs, want)
		}
	}
	if refs := refsOf(a.reader30001.appGet("/v1/documents?q=ray")); !slices.Equal(refs, []string{news.ref}) {
		t.Errorf("q=ray: %v", refs)
	}
	if refs := refsOf(a.reader30001.appGet("/v1/documents?kind=news&category=pest_warning")); !slices.Equal(refs, []string{news.ref}) {
		t.Errorf("lọc chuyên mục: %v", refs)
	}
	if refs := refsOf(a.reader30001.appGet("/v1/documents?tag=farm_stage:sowing")); !slices.Equal(refs, []string{d.ref}) {
		t.Errorf("lọc nhãn: %v", refs)
	}
	// Phân trang cursor.
	p1 := a.reader30001.appGet("/v1/documents?limit=1")
	cur := str(p1.body, "next_cursor")
	if len(items(p1)) != 1 || cur == "" {
		t.Fatalf("trang 1: %v", p1.body)
	}
	p2 := a.reader30001.appGet("/v1/documents?limit=1&cursor=" + cur)
	if len(items(p2)) != 1 || refsOf(p2)[0] == refsOf(p1)[0] {
		t.Fatalf("trang 2: %v", p2.body)
	}
}

// Đồng bộ ngoại tuyến (US-LIB-05, US-NEWS-02): upsert khi xuất bản, tombstone khi gỡ; lọc theo phạm vi.
func TestSyncRetireTombstone(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	src := f.mod.SyncSource()
	if !slices.Equal(src.Entities(), []string{"document", "library_item"}) { // library_item: danh mục cá nhân (US-LIB-05)
		t.Fatalf("entities: %v", src.Entities())
	}
	d := createDoc(t, a.p89Editor, map[string]any{"kind": "guide", "audience": map[string]any{"level": "province", "units": []string{"89"}},
		"title": "Sổ tay phòng trừ đạo ôn", "body": "Phun thuốc khi bệnh chớm xuất hiện."})
	publish(t, a.p89Editor, a.p89Reviewer, d)

	changes := func(sc authz.Scope, after int64) ([]syncport.Change, int64) {
		ctx := tenant.WithID(context.Background(), f.tenant)
		ch, last, err := src.Changes(ctx, sc, "document", after, 100)
		if err != nil {
			t.Fatal(err)
		}
		return ch, last
	}
	ch, last := changes(authz.Scope{Units: []string{"30001"}}, 0) // xã thuộc tỉnh 89
	if len(ch) != 1 || ch[0].Op != syncport.OpUpsert || ch[0].ID.String() != d.ref {
		t.Fatalf("upsert: %+v", ch)
	}
	var payload map[string]any
	_ = json.Unmarshal(ch[0].Data, &payload)
	if payload["title"] != "Sổ tay phòng trừ đạo ôn" || payload["search_text"] == "" || payload["body"] == "" {
		t.Fatalf("payload: %v", payload)
	}
	if other, _ := changes(authz.Scope{Units: []string{"92", "31001"}}, 0); len(other) != 0 {
		t.Fatalf("tỉnh 92 không được nhận tài liệu tỉnh 89: %+v", other)
	}
	if again, l2 := changes(authz.Scope{Units: []string{"30001"}}, last); len(again) != 0 || l2 != last {
		t.Fatalf("không có thay đổi mới: %+v %d", again, l2)
	}

	expect(t, a.p89Editor.adminPost("/v1/documents/"+d.ref+"/retire", map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity, "validation_failed")
	expect(t, a.p89Editor.adminPost("/v1/documents/"+d.ref+"/retire", map[string]any{"reason": "Thay bằng sổ tay 2027"}), http.StatusOK, "")
	ch, _ = changes(authz.Scope{Units: []string{"30001"}}, last)
	if len(ch) != 1 || ch[0].Op != syncport.OpTombstone || ch[0].Data != nil || ch[0].Version <= last {
		t.Fatalf("tombstone: %+v", ch)
	}
	expect(t, a.reader30001.appGet("/v1/documents/"+d.ref), http.StatusNotFound, "content.document_not_found")
	expect(t, a.p89Editor.adminPost("/v1/documents/"+d.ref+"/retire", map[string]any{"reason": "Gỡ lần thứ hai"}), http.StatusConflict, "content.invalid_transition")
	// Gỡ không xóa: bề mặt quản trị vẫn đọc được lịch sử.
	g := a.p89Editor.adminGet("/v1/documents/" + d.ref)
	if str(g.body, "document", "status") != "retired" || len(get(g.body, "versions").([]any)) != 1 {
		t.Fatalf("sau khi gỡ: %v", g.body)
	}
}

// Bộ lọc thân bài: bỏ script và thuộc tính on* trước khi lưu vào sổ.
func TestBodySanitisedOnSave(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	r := a.natEditor.adminPost("/v1/documents", map[string]any{"kind": "news", "audience": map[string]any{"level": "national"}, "category": "technique",
		"title": "Tin kỹ thuật", "body_format": "html",
		"body": `<p onclick="steal()">Gieo sạ <b onmouseover="x()">đúng lịch</b></p><script>fetch('//evil')</script><a href="javascript:alert(1)">bấm</a>`})
	expect(t, r, http.StatusCreated, "")
	body := str(r.body, "version", "body")
	if body != `<p>Gieo sạ <b>đúng lịch</b></p><a rel="noopener noreferrer nofollow">bấm</a>` {
		t.Fatalf("thân sau lọc: %s", body)
	}
	expect(t, a.natEditor.adminPost("/v1/documents", map[string]any{"kind": "news", "audience": map[string]any{"level": "national"},
		"title": "Tin", "body": "x", "tags": []string{"rice_variety:IR50404"}}), http.StatusUnprocessableEntity, "content.tag_invalid")
	// US-CMS-01 AC-3: tin tức thiếu chuyên mục không xuất bản được.
	nd := createDoc(t, a.natEditor, map[string]any{"kind": "news", "audience": map[string]any{"level": "national"}, "title": "Tin chưa có chuyên mục", "body": "x"})
	expect(t, a.natEditor.adminPost("/v1/documents/"+nd.ref+"/submit", map[string]any{"version_no": 1}), http.StatusOK, "")
	expect(t, a.natReviewer.adminPost("/v1/documents/"+nd.ref+"/publish", map[string]any{"version_no": 1}), http.StatusUnprocessableEntity, "validation_failed")
}

func docxBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>Quy trình 1 phải 5 giảm</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Giảm lượng giống gieo sạ.</w:t></w:r></w:p></w:body></w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// US-LIB-03, US-CMS-02: trích Word vào bản nháp; PDF chưa hỗ trợ; tệp khác bị từ chối.
func TestExtract(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	r := a.natEditor.upload("/v1/documents/extract", "quy-trinh.docx", docxBytes(t), "")
	expect(t, r, http.StatusOK, "")
	if r.body["title"] != "Quy trình 1 phải 5 giảm" || r.body["body"] != "# Quy trình 1 phải 5 giảm\n\nGiảm lượng giống gieo sạ." || r.body["status"] != "draft_only" {
		t.Fatalf("trích: %v", r.body)
	}
	// PDF có lớp chữ tiếng Việt (tệp mẫu sinh bằng fpdf, phông Unicode nhúng).
	vi, err := os.ReadFile("../domain/testdata/quy-trinh-vi.pdf")
	if err != nil {
		t.Fatal(err)
	}
	r = a.natEditor.upload("/v1/documents/extract", "quy-trinh.pdf", vi, "")
	expect(t, r, http.StatusOK, "")
	if r.body["title"] != "Quy trình canh tác lúa giảm phát thải" || !strings.Contains(r.body["body"].(string), "Không đốt rơm rạ; thu gom để ủ phân hữu cơ") || r.body["status"] != "draft_only" {
		t.Fatalf("trích PDF: %v", r.body)
	}
	enc, err := pdftest.Generate([]pdftest.Block{{Text: "Tai lieu mat"}}, pdftest.Options{Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	r = a.natEditor.upload("/v1/documents/extract", "mat.pdf", enc, "")
	expect(t, r, http.StatusUnprocessableEntity, "content.extract_unsupported")
	if d, _ := r.body["detail"].(string); !strings.Contains(d, "mã hóa") {
		t.Fatalf("PDF mã hóa phải nêu lý do: %v", r.body)
	}
	scan, err := pdftest.Generate(nil, pdftest.Options{ImageOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, a.natEditor.upload("/v1/documents/extract", "scan.pdf", scan, ""), http.StatusUnprocessableEntity, "content.extract_unsupported")
	expect(t, a.natEditor.upload("/v1/documents/extract", "a.pdf", []byte("%PDF-1.7\n..."), ""), http.StatusUnprocessableEntity, "content.extract_failed")
	expect(t, a.natEditor.upload("/v1/documents/extract", "hong.docx", []byte("PK\x03\x04hong"), ""), http.StatusUnprocessableEntity, "content.extract_failed")
	expect(t, a.natEditor.upload("/v1/documents/extract", "a.txt", []byte("văn bản"), ""), http.StatusUnsupportedMediaType, "content.file_type_not_allowed")
	expect(t, a.reader89.upload("/v1/documents/extract", "quy-trinh.docx", docxBytes(t), ""), http.StatusForbidden, "forbidden")
}

func pngBytes(size int) []byte {
	b := make([]byte, size)
	copy(b, "\x89PNG\r\n\x1a\n")
	for i := 8; i < size; i++ {
		b[i] = byte(i * 7)
	}
	return b
}

// Tệp đính kèm: kiểm SHA-256, giới hạn 15 MB (vượt giới hạn chung 1 MB), URL có hạn khi đọc.
func TestAttachments(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	d := createDoc(t, a.natEditor, procedureBody("Quy trình có ảnh minh họa"))
	img := pngBytes(3 << 20)
	expect(t, a.natEditor.upload("/v1/documents/"+d.ref+"/attachments", "anh.png", img, strings.Repeat("0", 64)), http.StatusUnprocessableEntity, "content.checksum_mismatch")
	up := a.natEditor.upload("/v1/documents/"+d.ref+"/attachments", "ảnh bìa.png", img, objstore.SHA256Hex(img))
	expect(t, up, http.StatusCreated, "")
	if up.body["content_type"] != "image/png" || up.body["size"].(float64) != float64(len(img)) {
		t.Fatalf("tải lên: %v", up.body)
	}
	big := pngBytes(16 << 20)
	expect(t, a.natEditor.upload("/v1/documents/"+d.ref+"/attachments", "lon.png", big, objstore.SHA256Hex(big)), http.StatusRequestEntityTooLarge, "")
	exe := []byte("MZ\x90\x00 chương trình")
	expect(t, a.natEditor.upload("/v1/documents/"+d.ref+"/attachments", "x.exe", exe, objstore.SHA256Hex(exe)), http.StatusUnsupportedMediaType, "content.file_type_not_allowed")

	attID := str(up.body, "id")
	expect(t, a.natEditor.adminPost("/v1/documents/"+d.ref+"/versions", map[string]any{"title": "x", "body": "y",
		"attachments": []map[string]any{{"id": ids.New().String()}}}), http.StatusUnprocessableEntity, "content.attachment_not_found")
	r := a.natEditor.adminPost("/v1/documents/"+d.ref+"/versions", map[string]any{
		"title": "Quy trình có ảnh minh họa", "body": "![bìa](att:" + attID + ")", "tags": []string{"farm_stage:sowing"},
		"attachments": []map[string]any{{"id": attID, "role": "cover"}},
	})
	expect(t, r, http.StatusCreated, "")
	publish(t, a.natEditor, a.natReviewer, doc{ref: d.ref, no: 2})
	g := a.reader30001.appGet("/v1/documents/" + d.ref)
	atts, _ := get(g.body, "version", "attachments").([]any)
	if len(atts) != 1 {
		t.Fatalf("tệp đính kèm: %v", g.body)
	}
	at := atts[0].(map[string]any)
	if at["role"] != "cover" || at["file_name"] != "ảnh bìa.png" || !strings.HasPrefix(at["url"].(string), "http://files.test/_objstore/") || at["url_expires_at"] == nil {
		t.Fatalf("tệp: %v", at)
	}
}

// Tenant: người đọc tenant khác không thấy tài liệu (RLS, R-31/R-32).
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	d := createDoc(t, a.natEditor, procedureBody("Quy trình của tenant A"))
	publish(t, a.natEditor, a.natReviewer, d)
	other := f.asTenant(ids.New(), orgNational)
	expect(t, other.appGet("/v1/documents/"+d.ref), http.StatusNotFound, "content.document_not_found")
	if refs := refsOf(other.appGet("/v1/documents")); len(refs) != 0 {
		t.Fatalf("tenant khác thấy: %v", refs)
	}
	otherEditor := f.asTenant(other.tid, orgNational, port.RoleEditorNational)
	expect(t, otherEditor.adminPost("/v1/documents/"+d.ref+"/versions", map[string]any{"title": "chiếm", "body": "x"}), http.StatusNotFound, "content.document_not_found")
	ch, _, err := f.mod.SyncSource().Changes(tenant.WithID(context.Background(), other.tid), authz.Scope{All: true}, "document", 0, 100)
	if err != nil || len(ch) != 0 {
		t.Fatalf("đồng bộ tenant khác: %v %v", ch, err)
	}
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi lần đầu, không tạo bản ghi thứ hai.
func TestIdempotentPost(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	b, _ := json.Marshal(procedureBody("Quy trình gọi lặp"))
	k := key()
	r1 := a.natEditor.raw(f.admin.URL, http.MethodPost, "/v1/documents", k, "application/json", b)
	r2 := a.natEditor.raw(f.admin.URL, http.MethodPost, "/v1/documents", k, "application/json", b)
	expect(t, r1, http.StatusCreated, "")
	expect(t, r2, http.StatusCreated, "")
	if r2.hdr.Get(idempotency.ReplayedHeader) != "true" || str(r1.body, "document", "ref") != str(r2.body, "document", "ref") {
		t.Fatalf("phát lại: %v / %v", r1.body, r2.body)
	}
	// Lớp thứ hai: sổ chống trùng theo idem_key kể cả khi bảng idempotency bị bỏ qua.
	ref := str(r1.body, "document", "ref")
	sub := []byte(`{"version_no":1}`)
	sk := key()
	expect(t, a.natEditor.raw(f.admin.URL, http.MethodPost, "/v1/documents/"+ref+"/submit", sk, "application/json", sub), http.StatusOK, "")
	expect(t, a.natEditor.raw(f.admin.URL, http.MethodPost, "/v1/documents/"+ref+"/submit", sk, "application/json", sub), http.StatusOK, "")
	var docs, versions, events int
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		ctx := context.Background()
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_content.document WHERE title IS NULL AND created_by = $1", a.natEditor.actor).Scan(&docs); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_content.document_version WHERE document_ref = $1", uuid.MustParse(ref)).Scan(&versions); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM pf_content.document_event WHERE document_ref = $1", uuid.MustParse(ref)).Scan(&events)
	})
	if err != nil {
		t.Fatal(err)
	}
	if docs != 1 || versions != 1 || events != 1 {
		t.Fatalf("trùng bản ghi: docs=%d versions=%d events=%d", docs, versions, events)
	}
}

// HTX US-NEWS-03: xuất bản cảnh báo khẩn phát pf.content.alert.published kèm vùng ảnh hưởng.
func TestAlertPublishedEvent(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	d := createDoc(t, a.p89Editor, map[string]any{"kind": "alert", "audience": map[string]any{"level": "commune", "units": []string{"30001", "30002"}},
		"title": "Cảnh báo rầy nâu bùng phát", "summary": "Mật độ rầy vượt ngưỡng", "body": "Kiểm tra ruộng ngay."})
	publish(t, a.p89Editor, a.p89Reviewer, d)
	if n := f.outboxCount(f.tenant, port.EventAlertPublished, d.ref); n != 1 {
		t.Fatalf("muốn 1 alert.published, có %d", n)
	}
	var units []string
	var province string
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT ARRAY(SELECT jsonb_array_elements_text(payload->'audience_units')), payload->>'province_code'
		    FROM shared.outbox WHERE event_type = $1 AND payload->>'document_ref' = $2`, port.EventAlertPublished, d.ref).Scan(&units, &province)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(units, []string{"30001", "30002"}) || province != "89" {
		t.Fatalf("vùng ảnh hưởng: %v %s", units, province)
	}
	// Nhân bản (US-CMS-02 AC-1): bản sao là bản nháp giữ nguyên nội dung.
	dup := a.p89Editor.adminPost("/v1/documents/"+d.ref+"/duplicate", nil)
	expect(t, dup, http.StatusCreated, "")
	if str(dup.body, "document", "status") != "draft" || str(dup.body, "version", "title") != "Cảnh báo rầy nâu bùng phát" || str(dup.body, "document", "ref") == d.ref {
		t.Fatalf("nhân bản: %v", dup.body)
	}
}

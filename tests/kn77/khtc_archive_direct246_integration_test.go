package kn77_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	pfexport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The saved synthetic report is created by the existing owner/exporter HTTP
// fixture. Reads below cross the actual assembled router, signed fixture OIDC,
// accounts context selection/revalidation and migrated PostgreSQL. No authority
// is copied from the report writer or injected into a request context.
type archive246Fixture struct {
	f                    *reportFx
	s                    *agScenario
	u                    *agUser
	assignment           string
	period, export, word string
	probe                *archive246Exporter
	counter              *archive199Exporter
}

type archive246Exporter struct {
	exportport.Exporter
	admin                     *pgxpool.Pool
	reads, histories, missing atomic.Int64
	tamper, inTransaction     atomic.Bool
	guard                     *archive246DB
}

func (e *archive246Exporter) observe(ctx context.Context, subject, action string, run *uuid.UUID) {
	if e.guard != nil && e.guard.active.Load() != 0 {
		e.inTransaction.Store(true)
	}
	p, ok := authz.FromContext(ctx)
	if !ok {
		e.missing.Add(1)
		return
	}
	var n int
	err := e.admin.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_archive_access WHERE tenant_id=$1 AND actor_ref=$2 AND trace_id=$3 AND (action=$4 OR ($4='word_attempt' AND action='zip_entry_attempt')) AND period_id=$5 AND ($6::uuid IS NULL OR run_id=$6)`,
		p.TenantID, p.ActorRef, telemetry.TraceID(ctx), action, strings.TrimPrefix(subject, "khtc_report:"), run).Scan(&n)
	if err != nil || n != 1 {
		e.missing.Add(1)
	}
}

func (e *archive246Exporter) Run(ctx context.Context, subject string, id uuid.UUID) (exportport.Run, error) {
	e.reads.Add(1)
	e.observe(ctx, subject, "word_attempt", &id)
	return e.Exporter.Run(ctx, subject, id)
}

func (e *archive246Exporter) Runs(ctx context.Context, subject string) ([]exportport.Run, error) {
	e.histories.Add(1)
	e.observe(ctx, subject, "read_history", nil)
	runs, err := e.Exporter.Runs(ctx, subject)
	if err == nil && len(runs) > 0 && e.tamper.Load() {
		runs[0].DocNumber += "-drift"
	}
	return runs, err
}

func newArchive246Fixture(t *testing.T) *archive246Fixture {
	t.Helper()
	f := newReportFx(t)
	counter := installArchive199Exporter(t, f)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	x := f.export(p.ID, d.Version, true)
	w := f.word(p.ID, x.ID, "archive246-bound-fixture")
	st := harness.NewAt(t, f.clk)
	s := &agScenario{t: t, s: st, clk: f.clk, tid: f.tid}
	s.rootA = s.admin(authz.RolePlatformAdmin)
	adm := func(path string, body any, status int) agResp {
		return s.want(s.rootA.adm(http.MethodPost, path, body), status, path)
	}
	adm("/v1/masterdata/seed", map[string]any{}, http.StatusOK)
	s.nat = str(obj(adm("/v1/orgs", map[string]any{"kind": "ttkn_national", "code": "ARCH246-NAT", "name": "TTKN kiểm thử 246"}, http.StatusCreated).body, "org"), "ref")
	s.p91 = str(obj(adm("/v1/orgs", map[string]any{"kind": "ttkn_province", "code": "ARCH246-PROV", "name": "TTKN tỉnh kiểm thử 246", "parent_ref": s.nat, "admin_unit_code": agProv}, http.StatusCreated).body, "org"), "ref")
	departments := adm("/v1/orgs/"+s.p91+"/departments/seed", map[string]any{}, http.StatusOK)
	for _, department := range arr(departments.body, "created") {
		if str(department, "code") == khtcDept {
			adm("/v1/departments/"+str(department, "id")+"/status", map[string]any{"status": "active", "reason": "Mở phòng KHTC cho kiểm thử kho 246"}, http.StatusCreated)
		}
	}
	u, assignments := s.createKN("Cán bộ đọc kho 246", "archive246-reader@example.test", "0912460001", asgKN(authz.KNTinh, s.p91, []string{khtcDept, dTT}, "chuyen_vien"))
	u = u.as(assignments[authz.KNTinh], khtcDept)
	probe := &archive246Exporter{Exporter: counter.Exporter, admin: st.AdminPF()}
	st.Mods.Extension.UseExporter(probe)
	return &archive246Fixture{f: f, s: s, u: u, assignment: assignments[authz.KNTinh], period: p.ID, export: x.ID, word: w.ID, probe: probe, counter: counter}
}

type archive246Response struct {
	status int
	body   []byte
	trace  string
	hash   string
}

func archive246Request(t *testing.T, u *agUser, method, path string, body any) archive246Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.s.s.App.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	trace := telemetry.NewTraceID()
	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set(authz.ContextHeader, u.ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "archive246-"+ids.New().String())
	req.Header.Set("traceparent", fmt.Sprintf("00-%s-0000000000000001-01", trace))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	content, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return archive246Response{status: res.StatusCode, body: content, trace: trace, hash: res.Header.Get("X-Content-SHA256")}
}

func (a *archive246Fixture) audit(t *testing.T, r archive246Response, action string) {
	t.Helper()
	var actor, selected uuid.UUID
	var at time.Time
	var n int
	err := a.probe.admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_extension.khtc_archive_access WHERE tenant_id=$1 AND trace_id=$2 AND action=$3`, a.s.tid, r.trace, action).Scan(&n)
	if err != nil || n != 1 {
		t.Errorf("direct Word request returned without its one committed %s audit: count=%d err=%v", action, n, err)
		return
	}
	err = a.probe.admin.QueryRow(context.Background(), `SELECT actor_ref,context_ref,event_time FROM pf_extension.khtc_archive_access WHERE tenant_id=$1 AND trace_id=$2 AND action=$3`, a.s.tid, r.trace, action).Scan(&actor, &selected, &at)
	if err != nil || actor != a.u.id || selected.String() != a.u.ctx || !at.Equal(a.f.clk.Now()) {
		t.Errorf("access audit lost authenticated actor/current context/clock: actor=%s context=%s at=%s err=%v", actor, selected, at, err)
	}
}

func TestArchive246DirectWordCommittedAudit(t *testing.T) {
	a := newArchive246Fixture(t)
	for _, tc := range []struct{ name, path, action string }{
		{"direct history", reportPath(a.period) + "/words", "read_history"},
		{"direct stored Word GET", reportPath(a.period) + "/words/" + a.word + "/download", "word_attempt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := archive246Request(t, a.u, http.MethodGet, tc.path, nil)
			if r.status != http.StatusOK {
				t.Fatalf("native read: %d %s", r.status, r.body)
			}
			a.audit(t, r, tc.action)
		})
	}
	if a.probe.missing.Load() != 0 {
		t.Errorf("public exporter metadata/byte port ran before required audit COMMIT: calls=%d", a.probe.missing.Load())
	}
	// The snapshot-specific route is an existing #199 control, not the gap under test.
	r := archive246Request(t, a.u, http.MethodGet, archive199Base(a.period, a.export)+"/"+a.word+"/download", nil)
	if r.status != http.StatusOK {
		t.Fatalf("existing snapshot Word control: %d %s", r.status, r.body)
	}
	a.audit(t, r, "word_attempt")
}

func (a *archive246Fixture) endpoints() []archive227Endpoint {
	return []archive227Endpoint{
		{"direct history", http.MethodGet, reportPath(a.period) + "/words", nil},
		{"direct stored Word GET", http.MethodGet, reportPath(a.period) + "/words/" + a.word + "/download", nil},
		{"snapshot stored Word GET", http.MethodGet, archive199Base(a.period, a.export) + "/" + a.word + "/download", nil},
		{"mixed ZIP POST", http.MethodPost, "/v1/extension/khtc/exported-reports/zip", map[string]any{"items": []map[string]any{
			{"period_id": a.period, "export_id": a.export, "format": "excel"},
			{"period_id": a.period, "export_id": a.export, "format": "word", "run_id": a.word},
		}}},
	}
}

func archive246NoData(t *testing.T, r archive246Response, status int) {
	t.Helper()
	if r.status != status || bytes.HasPrefix(r.body, []byte("PK")) || bytes.Contains(r.body, []byte(`"items":`)) || bytes.Contains(r.body, []byte(`"content":`)) {
		t.Fatalf("expected no data/status %d, got %d %s", status, r.status, r.body)
	}
}

func TestArchive246DirectAuditInsertAndDeferredCommitFailure(t *testing.T) {
	a := newArchive246Fixture(t)
	first := archive246Request(t, a.u, http.MethodGet, reportPath(a.period)+"/words/"+a.word+"/download", nil)
	if first.status != http.StatusOK {
		t.Fatalf("fixture direct read: %d %s", first.status, first.body)
	}
	count := func() int {
		var n int
		if err := a.probe.admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_extension.khtc_archive_access WHERE tenant_id=$1`, a.s.tid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var schema, partition string
	if err := a.probe.admin.QueryRow(context.Background(), `SELECT n.nspname,c.relname FROM pf_extension.khtc_archive_access a JOIN pg_class c ON c.oid=a.tableoid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE a.tenant_id=$1 ORDER BY a.seq DESC LIMIT 1`, a.s.tid).Scan(&schema, &partition); err != nil {
		t.Fatal(err)
	}
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprintf("deferred_commit=%t", deferred), func(t *testing.T) {
			name := "test_archive246_" + strings.ReplaceAll(ids.New().String(), "-", "")
			fn, trigger := pgx.Identifier{"pf_extension", name}.Sanitize(), pgx.Identifier{name}.Sanitize()
			table := pgx.Identifier{"pf_extension", "khtc_archive_access"}.Sanitize()
			clause := "CREATE TRIGGER " + trigger + " BEFORE INSERT ON " + table + " FOR EACH ROW "
			if deferred {
				table = pgx.Identifier{schema, partition}.Sanitize()
				clause = "CREATE CONSTRAINT TRIGGER " + trigger + " AFTER INSERT ON " + table + " DEFERRABLE INITIALLY DEFERRED FOR EACH ROW "
			}
			function := `CREATE FUNCTION ` + fn + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='` + a.s.tid.String() + `'::uuid THEN RAISE EXCEPTION 'controlled direct Word audit unavailable'; END IF; RETURN NEW; END $$; `
			if _, err := a.probe.admin.Exec(context.Background(), function+clause+"EXECUTE FUNCTION "+fn+"()"); err != nil {
				t.Fatal(err)
			}
			cleanup := func() {
				if _, err := a.probe.admin.Exec(context.Background(), "DROP TRIGGER IF EXISTS "+trigger+" ON "+table+"; DROP FUNCTION IF EXISTS "+fn+"()"); err != nil {
					t.Error(err)
				}
			}
			t.Cleanup(cleanup)
			before, reads, histories := count(), a.probe.reads.Load(), a.probe.histories.Load()
			for _, ep := range a.endpoints() {
				r := archive246Request(t, a.u, ep.method, ep.path, ep.body)
				archive246NoData(t, r, http.StatusInternalServerError)
				if count() != before || a.probe.reads.Load() != reads || a.probe.histories.Load() != histories {
					t.Fatalf("audit rollback/port boundary violated by %s", ep.name)
				}
			}
			cleanup()
			for _, ep := range a.endpoints() {
				r := archive246Request(t, a.u, ep.method, ep.path, ep.body)
				if r.status != http.StatusOK {
					t.Fatalf("audit recovery %s: %d %s", ep.name, r.status, r.body)
				}
				if strings.HasSuffix(ep.path, "/download") && (!bytes.Equal(first.body, r.body) || r.hash != first.hash) {
					t.Fatal("audit recovery changed stored Word byte/hash")
				}
			}
		})
	}
	if a.probe.missing.Load() != 0 {
		t.Fatal("an exporter port saw no committed access attempt")
	}
}

func TestArchive246CurrentNativeAuthorityBeforeWordAndZIP(t *testing.T) {
	a := newArchive246Fixture(t)
	department := a.u.as(a.assignment, dTT)
	missing := *a.u
	missing.ctx = ""
	unknown := *a.u
	unknown.ctx = ids.New().String()
	other, otherAssignments := a.s.createKN("Cán bộ khác 246", "archive246-other@example.test", "0912460002", asgKN(authz.KNTinh, a.s.p91, []string{khtcDept}, "chuyen_vien"))
	other = other.as(otherAssignments[authz.KNTinh], khtcDept)
	foreign := *a.u
	foreign.ctx = other.ctx
	// Another tenant's signed fixture identity cannot use this selected context.
	crossTenant := *a.u
	crossTenant.token = a.s.s.Issuer.Sign(t, authztest.Token{Subject: a.u.id, TenantID: ids.New(), Roles: a.s.realm(a.u.id), TTL: time.Hour})
	for _, tc := range []struct {
		name   string
		user   *agUser
		status int
	}{{"current selected department", department, 403}, {"missing context", &missing, 428}, {"unknown context", &unknown, 403}, {"foreign actor context", &foreign, 403}, {"cross tenant context", &crossTenant, 403}} {
		t.Run(tc.name, func(t *testing.T) {
			reads, histories := a.probe.reads.Load(), a.probe.histories.Load()
			for _, ep := range a.endpoints() {
				archive246NoData(t, archive246Request(t, tc.user, ep.method, ep.path, ep.body), tc.status)
			}
			if a.probe.reads.Load() != reads || a.probe.histories.Load() != histories {
				t.Fatal("denied authority reached public exporter metadata/bytes")
			}
		})
	}
	readonly, asgs := a.s.createKN("BGĐ chỉ xem 246", "archive246-readonly@example.test", "0912460003", asgKN(authz.KNTinh, a.s.p91, []string{khtcDept}, "pho_giam_doc"))
	a.s.want(a.s.rootA.adm(http.MethodPut, "/v1/accounts/users/"+readonly.id.String()+"/assignments/"+asgs[authz.KNTinh]+"/attributes", map[string]any{"province_group": "ban_giam_doc", "reason": "Kiểm quyền đọc kho 246"}), http.StatusOK, "set native read-only")
	readonly = readonly.as(asgs[authz.KNTinh], khtcDept)
	for _, ep := range a.endpoints() {
		r := archive246Request(t, readonly, ep.method, ep.path, ep.body)
		if ep.method == http.MethodPost {
			// Preserve the current accounts blanket read-only POST policy.
			archive246NoData(t, r, 403)
		} else if r.status != http.StatusOK {
			t.Fatalf("existing KHTC read-only GET: %d %s", r.status, r.body)
		}
	}
	adminID := a.s.s.Directory.Seed(accport.NewUser{Username: "archive246-admin", Email: "archive246-admin@example.test", FirstName: "Admin", LastName: "246", TenantID: a.s.tid}, true, authz.RolePlatformAdmin)
	adminAsg := a.s.want(a.s.rootA.adm(http.MethodPost, "/v1/accounts/users/"+adminID.String()+"/assignments", map[string]any{"role": authz.KNAdmin, "department_codes": []string{}, "commune_codes": []string{}}), http.StatusCreated, "grant native Admin KN")
	admin := &agUser{s: a.s, id: adminID, tid: a.s.tid, token: a.s.s.Issuer.Sign(t, authztest.Token{Subject: adminID, TenantID: a.s.tid, Roles: a.s.realm(adminID), MFA: true, TTL: time.Hour})}
	admin = admin.as(str(obj(adminAsg.body, "assignment"), "assignment_id"), "")
	admin.token = a.s.s.Issuer.Sign(t, authztest.Token{Subject: admin.id, TenantID: a.s.tid, Roles: a.s.realm(admin.id), MFA: false, TTL: time.Hour})
	reads, histories := a.probe.reads.Load(), a.probe.histories.Load()
	for _, ep := range a.endpoints() {
		archive246NoData(t, archive246Request(t, admin, ep.method, ep.path, ep.body), 403)
	}
	a.s.want(a.s.rootA.adm(http.MethodPost, "/v1/accounts/users/"+a.u.id.String()+"/assignments/"+a.assignment+"/revoke", map[string]any{"reason": "Thu hồi vai đọc kho để kiểm thử 246"}), http.StatusOK, "revoke native assignment")
	for _, ep := range a.endpoints() {
		archive246NoData(t, archive246Request(t, a.u, ep.method, ep.path, ep.body), 403)
	}
	if a.probe.reads.Load() != reads || a.probe.histories.Load() != histories {
		t.Fatal("missing admin MFA/revoked current context reached FilePort")
	}
}

func TestArchive246UnlinkedAndChangedWordMetadataUnavailable(t *testing.T) {
	a := newArchive246Fixture(t)
	// The SAME missing-binding negative crosses real OIDC/accounts middleware.
	// No exporter lookup-by-bytes is used to invent an unknown-ID classification.
	readsBeforeUnknown, historiesBeforeUnknown := a.probe.reads.Load(), a.probe.histories.Load()
	unknown := archive246Request(t, a.u, http.MethodGet, reportPath(a.period)+"/words/"+ids.New().String()+"/download", nil)
	archive246NoData(t, unknown, http.StatusServiceUnavailable)
	var missing struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(unknown.body, &missing); err != nil || missing.Code != "extension.dependency_unavailable" || missing.Detail != "Word đã lưu chưa có chứng cứ liên kết với bản xuất; chưa thể xem hoặc tải" || a.probe.reads.Load() != readsBeforeUnknown || a.probe.histories.Load() != historiesBeforeUnknown {
		t.Fatalf("native unknown/unattested ID must be exact 503 with no metadata/byte port: code=%s detail=%s err=%v", missing.Code, missing.Detail, err)
	}
	a.probe.tamper.Store(true)
	r := archive246Request(t, a.u, http.MethodGet, reportPath(a.period)+"/words", nil)
	archive246NoData(t, r, 503)
	a.audit(t, r, "read_history")
	a.probe.tamper.Store(false)
	a.counter.corruptExport = true
	status, body := a.f.do(http.MethodPost, reportPath(a.period)+"/exports/"+a.export+"/word", authz.KNTinh, khtcDept, a.f.khtcUser, "archive246-unlinked-fixture", map[string]any{})
	a.counter.corruptExport = false
	if status != 503 {
		t.Fatalf("fixture must fail attestation after real export commit: %d %s", status, body)
	}
	unlinked := a.counter.last
	reads, exports := a.probe.reads.Load(), a.counter.exports.Load()
	r = archive246Request(t, a.u, http.MethodGet, reportPath(a.period)+"/words/"+unlinked.ID.String()+"/download", nil)
	archive246NoData(t, r, 503)
	if !bytes.Contains(r.body, []byte("chưa có chứng cứ liên kết")) || a.probe.reads.Load() != reads {
		t.Fatal("unlinked Word reached FilePort or lost explicit unavailable reason")
	}
	r = archive246Request(t, a.u, http.MethodGet, reportPath(a.period)+"/words", nil)
	archive246NoData(t, r, 503)
	a.audit(t, r, "read_history")
	var bindings int
	if err := a.probe.admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_extension.khtc_word_binding WHERE tenant_id=$1 AND run_id=$2`, a.s.tid, unlinked.ID).Scan(&bindings); err != nil || bindings != 0 {
		t.Fatalf("read backfilled unlinked Word: bindings=%d err=%v", bindings, err)
	}
	bound := archive246Request(t, a.u, http.MethodGet, reportPath(a.period)+"/words/"+a.word+"/download", nil)
	if bound.status != 200 || bound.hash == "" || a.counter.exports.Load() != exports {
		t.Fatalf("existing bound read regenerated or became unavailable: %d", bound.status)
	}
}

type archive246DB struct {
	tenant.Beginner
	active atomic.Int64
}
type archive246Tx struct {
	pgx.Tx
	owner *archive246DB
	once  sync.Once
}

func (d *archive246DB) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.Beginner.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	d.active.Add(1)
	return &archive246Tx{Tx: tx, owner: d}, nil
}
func (tx *archive246Tx) Commit(ctx context.Context) error {
	err := tx.Tx.Commit(ctx)
	tx.once.Do(func() { tx.owner.active.Add(-1) })
	return err
}
func (tx *archive246Tx) Rollback(ctx context.Context) error {
	err := tx.Tx.Rollback(ctx)
	tx.once.Do(func() { tx.owner.active.Add(-1) })
	return err
}

func TestArchive246DirectReadsPoolOneNoPortInTransaction(t *testing.T) {
	a := newArchive246Fixture(t)
	pool := a.s.s.Env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	guard := &archive246DB{Beginner: pool}
	module := extension.New(guard, a.f.clk, extension.Deps{})
	exporter, err := pfexport.New(pool, a.f.clk, pfexport.Deps{Access: exportport.AccessFunc(func(context.Context) bool { return false }), Templates: []exportport.TemplateRegistration{extension.ExportTemplates()}})
	if err != nil {
		t.Fatal(err)
	}
	a.probe.Exporter, a.probe.guard = exporter.Exporter(), guard
	module.UseExporter(a.probe)
	mods := a.s.s.Mods
	mods.Extension = module
	server := httptest.NewServer(assembly.NewRouter(assembly.Deps{Log: telemetry.NewLogger("archive246-pool1", io.Discard), Verifier: a.s.s.Issuer.Verifier(), Ops: pool, Modules: mods.List()}))
	defer server.Close()
	previous := a.s.s.App
	a.s.s.App = server
	defer func() { a.s.s.App = previous }()
	for _, ep := range a.endpoints() {
		r := archive246Request(t, a.u, ep.method, ep.path, ep.body)
		if r.status != 200 {
			t.Fatalf("pool1 %s: %d %s", ep.name, r.status, r.body)
		}
	}
	if a.probe.inTransaction.Load() || a.probe.missing.Load() != 0 || guard.active.Load() != 0 {
		t.Fatal("public metadata/byte I/O occurred in owner DBTX or before COMMIT")
	}
}

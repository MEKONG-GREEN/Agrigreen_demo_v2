package kn77_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	pfexport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Every runtime operation below crosses real owner/exporter HTTP + PostgreSQL.
// The wrapper counts public port calls; it never implements numbering/rendering.
type archive199Exporter struct {
	corruptExport bool
	last          exportport.Run
	forced        *exportport.Run
	exportport.Exporter
	exports, reads atomic.Int64
	corrupt        atomic.Bool
}

func (e *archive199Exporter) Export(ctx context.Context, req exportport.Request) (exportport.Run, error) {
	e.exports.Add(1)
	if e.forced != nil {
		return *e.forced, nil
	}
	run, err := e.Exporter.Export(ctx, req)
	if err == nil {
		e.last = run
	}
	if err == nil && e.corruptExport {
		run.InputHash = "invalid"
	}
	return run, err
}
func (e *archive199Exporter) Run(ctx context.Context, subject string, id uuid.UUID) (exportport.Run, error) {
	e.reads.Add(1)
	run, err := e.Exporter.Run(ctx, subject, id)
	if err == nil && e.corrupt.Load() {
		run.Body = append([]byte(nil), run.Body...)
		run.Body[0] ^= 1
	}
	return run, err
}

type archive199Words struct {
	ExportVersion int64  `json:"export_version"`
	ExportName    string `json:"export_name"`
	Items         []struct {
		RunID    string `json:"run_id"`
		BodyHash string `json:"body_hash"`
		Actor    string `json:"actor_ref"`
	} `json:"items"`
	HasMore bool `json:"has_more"`
}

func archive199Base(pid, xid string) string { return reportPath(pid) + "/exports/" + xid + "/words" }

func TestArchive199StoredWordBindingAndMixedZIP(t *testing.T) {
	f := newReportFx(t)
	counter := installArchive199Exporter(t, f)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	x1 := f.export(p.ID, d.Version, true)
	excel := f.download(p.ID, x1.ID)
	w1 := f.word(p.ID, x1.ID, "archive199-first")
	c, word := f.call(http.MethodGet, p.ID, "/words/"+w1.ID+"/download", nil)
	f.ok(c, word, nil)
	list := func(xid, query string) archive199Words {
		t.Helper()
		var out archive199Words
		c, b := f.call(http.MethodGet, p.ID, "/exports/"+xid+"/words"+query, nil)
		f.ok(c, b, &out)
		return out
	}
	rows := list(x1.ID, "")
	if len(rows.Items) != 1 || rows.Items[0].RunID != w1.ID || rows.Items[0].BodyHash != w1.BodyHash || rows.Items[0].Actor != f.khtcUser.String() || rows.ExportVersion != x1.Version || rows.ExportName == "" {
		t.Fatalf("binding metadata: %+v", rows)
	}
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	ctx := context.Background()
	var bindings int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_word_binding WHERE tenant_id=$1 AND run_id=$2`, f.tid, w1.ID).Scan(&bindings); err != nil || bindings != 1 {
		t.Fatalf("one binding: %d %v", bindings, err)
	}
	// Same idem retry cannot append a second binding, including after idem TTL.
	pgtest.Get(t).ExpireLedgerIdem(t, migrations.PF, "khtc:word_binding:"+w1.ID)
	if again := f.word(p.ID, x1.ID, "archive199-first"); again.ID != w1.ID {
		t.Fatal("retry changed saved run")
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_word_binding WHERE tenant_id=$1 AND run_id=$2`, f.tid, w1.ID).Scan(&bindings); err != nil || bindings != 1 {
		t.Fatal("retry duplicated binding", bindings, err)
	}
	// A fresh HTTP key replaying the same stored port result rechecks owner binding.
	replay := counter.last
	counter.forced = &replay
	same := f.word(p.ID, x1.ID, "archive199-link-recheck-after-ttl")
	counter.forced = nil
	if same.ID != w1.ID {
		t.Fatal("link replay changed run")
	}
	// The SQL invariant also survives expired ledger idem independently of app checks.
	duplicateSQL := `INSERT INTO pf_extension.khtc_word_binding (period_id,export_id,run_id,export_input_hash,export_body_hash,word_version,template_key,template_version,template_hash,word_input_hash,body_hash,body_size,file_name,content_type,doc_number,doc_number_auto,word_actor_ref,word_at,id,tenant_id,stream_key,event_time,actor_ref,idem_key,rule_version)
 SELECT period_id,export_id,run_id,export_input_hash,export_body_hash,word_version,template_key,template_version,template_hash,word_input_hash,body_hash,body_size,file_name,content_type,doc_number,doc_number_auto,word_actor_ref,word_at,$3,tenant_id,stream_key,event_time,actor_ref,'archive199:duplicate:'||$3::uuid::text,rule_version FROM pf_extension.khtc_word_binding WHERE tenant_id=$1 AND run_id=$2`
	_, duplicateErr := admin.Exec(ctx, duplicateSQL, f.tid, w1.ID, ids.New())
	var duplicatePG *pgconn.PgError
	if !errors.As(duplicateErr, &duplicatePG) || duplicatePG.Code != "23505" || duplicatePG.ConstraintName != "khtc_word_binding_once" {
		t.Fatalf("bind-once SQL bypass after TTL: %v", duplicateErr)
	}
	// Alter the live draft: reads must remain byte-identical, without Export calls.
	c, b := f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "texts": map[string]string{"B": "Nguồn soạn đã đổi sau xuất"}})
	f.ok(c, b, &d)
	x2 := f.export(p.ID, d.Version, true)
	before := counter.exports.Load()
	c, again := f.call(http.MethodGet, p.ID, "/exports/"+x1.ID+"/words/"+w1.ID+"/download", nil)
	f.ok(c, again, nil)
	if !bytes.Equal(again, word) || counter.exports.Load() != before {
		t.Fatal("saved Word was rendered/changed")
	}
	// A valid run from the same period cannot be paired with another snapshot.
	c, b = f.call(http.MethodGet, p.ID, "/exports/"+x2.ID+"/words/"+w1.ID+"/download", nil)
	if c != 404 {
		t.Fatalf("misbound snapshot: %d %s", c, b)
	}
	if got := list(x2.ID, ""); len(got.Items) != 0 {
		t.Fatal("legacy/other snapshot inferred", got)
	}
	// Same exporter idem, changed frozen input: core replay is not authority to bind.
	c, b = f.do(http.MethodPost, reportPath(p.ID)+"/exports/"+x2.ID+"/word", authz.KNTinh, khtcDept, f.khtcUser, "archive199-first", map[string]any{})
	if c != 422 {
		t.Fatalf("wrong-input HTTP idem replay: %d %s", c, b)
	}
	if got := list(x2.ID, ""); len(got.Items) != 0 {
		t.Fatal("wrong cached run appended binding", got)
	}
	// Independently replay a real old run through the port under a fresh HTTP key.
	oldRun := counter.last
	counter.forced = &oldRun
	c, b = f.do(http.MethodPost, reportPath(p.ID)+"/exports/"+x2.ID+"/word", authz.KNTinh, khtcDept, f.khtcUser, "archive199-port-replay", map[string]any{})
	counter.forced = nil
	if c != 503 {
		t.Fatalf("wrong frozen-input port replay: %d %s", c, b)
	}
	if got := list(x2.ID, ""); len(got.Items) != 0 {
		t.Fatal("port replay created wrong binding", got)
	}
	w2 := f.word(p.ID, x1.ID, "")
	if got := list(x1.ID, "?limit=1"); len(got.Items) != 1 || !got.HasMore || got.Items[0].RunID != w2.ID {
		t.Fatal("word pagination", got)
	}
	items := []map[string]any{{"period_id": p.ID, "export_id": x1.ID}, {"period_id": p.ID, "export_id": x1.ID, "format": "word", "run_id": w1.ID}}
	zipRequest := func(items []map[string]any) (int, []byte) {
		return f.do(http.MethodPost, "/v1/extension/khtc/exported-reports/zip", authz.KNTinh, khtcDept, f.khtcUser, "", map[string]any{"items": items})
	}
	c, body := zipRequest(items)
	f.ok(c, body, nil)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(zr.File) != 2 {
		t.Fatal("mixed ZIP", err)
	}
	want := [][]byte{excel, word}
	seen := map[string]bool{}
	for i, file := range zr.File {
		if strings.ContainsAny(file.Name, "/\\") || seen[file.Name] {
			t.Fatal("unsafe/duplicate ZIP name", file.Name)
		}
		seen[file.Name] = true
		r, e := file.Open()
		if e != nil {
			t.Fatal(e)
		}
		content, e := io.ReadAll(r)
		_ = r.Close()
		if e != nil || !bytes.Equal(content, want[i]) {
			t.Fatal("ZIP did not preserve saved bytes", i, e)
		}
		sum := sha256.Sum256(content)
		if i == 1 && hex.EncodeToString(sum[:]) != w1.BodyHash {
			t.Fatal("Word SHA drift")
		}
	}
	if counter.exports.Load() != before+2 {
		t.Fatal("ZIP invoked renderer/numbering", counter.exports.Load(), before)
	} // only explicit w2 Export
	// All batch metadata is checked before any exporter read.
	invalid := append([]map[string]any{}, items...)
	invalid = append(invalid, map[string]any{"period_id": p.ID, "export_id": x2.ID, "format": "word", "run_id": w1.ID})
	reads := counter.reads.Load()
	c, b = zipRequest(invalid)
	if c != 404 || counter.reads.Load() != reads {
		t.Fatalf("invalid batch read bytes: %d %s", c, b)
	}
	counter.corrupt.Store(true)
	c, b = f.call(http.MethodGet, p.ID, "/exports/"+x1.ID+"/words/"+w1.ID+"/download", nil)
	if c != 503 {
		t.Fatalf("corrupt saved Word: %d %s", c, b)
	}
	counter.corrupt.Store(false)
	// An actual pf/export run whose binding failed stays outside the archive.
	counter.corruptExport = true
	c, b = f.do(http.MethodPost, reportPath(p.ID)+"/exports/"+x2.ID+"/word", authz.KNTinh, khtcDept, f.khtcUser, "archive199-unbound", map[string]any{})
	counter.corruptExport = false
	if c != 503 {
		t.Fatalf("unattested run accepted: %d %s", c, b)
	}
	unbound := counter.last
	c, b = f.call(http.MethodGet, p.ID, "/words/"+unbound.ID.String()+"/download", nil)
	f.ok(c, b, nil)
	if got := list(x2.ID, ""); len(got.Items) != 0 {
		t.Fatal("unbound Word guessed by period/version", got)
	}
	c, b = f.call(http.MethodGet, p.ID, "/exports/"+x2.ID+"/words/"+unbound.ID.String()+"/download", nil)
	if c != 404 {
		t.Fatalf("unbound archive download: %d %s", c, b)
	}
	// Audit contains actual selected context/actor and attempts, not a delivery claim.
	var audits, wrong int
	if err = admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE actor_ref<>$2 OR context_ref='00000000-0000-0000-0000-000000000000'::uuid OR event_time<>$3) FROM pf_extension.khtc_archive_access WHERE tenant_id=$1`, f.tid, f.khtcUser, f.clk.Now()).Scan(&audits, &wrong); err != nil || audits < 8 || wrong != 0 {
		t.Fatal("audit facts", audits, wrong, err)
	}
	// Tenant-specific audit failure: no exporter read and no bytes may escape.
	fn := "test_archive199_" + strings.ReplaceAll(ids.New().String(), "-", "")
	sql := `CREATE FUNCTION pf_extension.` + fn + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='` + f.tid.String() + `'::uuid THEN RAISE EXCEPTION 'controlled audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER ` + fn + ` BEFORE INSERT ON pf_extension.khtc_archive_access FOR EACH ROW EXECUTE FUNCTION pf_extension.` + fn + `() `
	if _, err = admin.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		if _, e := admin.Exec(ctx, `DROP TRIGGER IF EXISTS `+fn+` ON pf_extension.khtc_archive_access; DROP FUNCTION IF EXISTS pf_extension.`+fn+`() `); e != nil {
			t.Error(e)
		}
	}
	t.Cleanup(cleanup)
	reads = counter.reads.Load()
	c, b = f.call(http.MethodGet, p.ID, "/exports/"+x1.ID+"/words/"+w1.ID+"/download", nil)
	if c < 500 || bytes.Equal(b, word) || counter.reads.Load() != reads {
		t.Fatalf("audit failure leaked Word: %d", c)
	}
	c, b = zipRequest(items)
	if c < 500 || counter.reads.Load() != reads || bytes.HasPrefix(b, []byte("PK")) {
		t.Fatalf("audit failure leaked ZIP: %d", c)
	}
	cleanup()
	for _, role := range []string{authz.KNXa, authz.KNTinh} {
		c, b = f.do(http.MethodGet, archive199Base(p.ID, x1.ID), role, dTT, f.khtcUser, "", nil)
		if c != 403 {
			t.Fatalf("role/dept expanded: %d %s", c, b)
		}
	}
	old := f.tid
	f.tid = ids.New()
	c, b = f.call(http.MethodGet, p.ID, "/exports/"+x1.ID+"/words/"+w1.ID+"/download", nil)
	f.tid = old
	if c != 404 {
		t.Fatalf("cross tenant: %d %s", c, b)
	}
	// Concurrent reused HTTP key across two frozen snapshots has one winner only.
	var beforeBindings int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_word_binding WHERE tenant_id=$1`, f.tid).Scan(&beforeBindings); err != nil {
		t.Fatal(err)
	}
	results := make(chan int, 2)
	for _, xid := range []string{x1.ID, x2.ID} {
		go func(exportID string) {
			status, _ := f.do(http.MethodPost, reportPath(p.ID)+"/exports/"+exportID+"/word", authz.KNTinh, khtcDept, f.khtcUser, "archive199-concurrent-shared-key", map[string]any{})
			results <- status
		}(xid)
	}
	first, second := <-results, <-results
	if !((first == 201 && second == 422) || (first == 422 && second == 201)) {
		t.Fatalf("concurrent snapshots: %d %d", first, second)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_word_binding WHERE tenant_id=$1`, f.tid).Scan(&bindings); err != nil || bindings != beforeBindings+1 {
		t.Fatal("concurrency appended wrong/duplicate binding", beforeBindings, bindings, err)
	}
	for _, query := range []string{"?limit=1e100000", "?offset=1e-100000", "?limit=" + strings.Repeat("9", 100000), "?limit=0", "?offset=-1"} {
		c, b = f.call(http.MethodGet, p.ID, "/exports/"+x1.ID+"/words"+query, nil)
		if c != 422 {
			t.Fatalf("query accepted: %d %s", c, b)
		}
	}
}

func installArchive199Exporter(t *testing.T, f *reportFx) *archive199Exporter {
	t.Helper()
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	ex, err := pfexport.New(pool, f.clk, pfexport.Deps{Access: exportport.AccessFunc(func(context.Context) bool { return false }), Templates: []exportport.TemplateRegistration{extension.ExportTemplates()}})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &archive199Exporter{Exporter: ex.Exporter()}
	f.ext.UseExporter(wrapped)
	return wrapped
}

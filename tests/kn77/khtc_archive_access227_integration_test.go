package kn77_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The request crosses the actual owner HTTP/middleware/SQL route. A public
// traceparent lets the assertion identify this request, not an earlier read.
func archive227Request(t *testing.T, f *reportFx, method, path, role, dept string, actor uuid.UUID, body any) (int, []byte, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	kn := authz.KNContext{Role: role, Department: dept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}}
	if role == authz.KNAdmin {
		kn = authz.KNContext{Role: role, Scope: authz.Scope{All: true}}
	}
	knJSON, err := json.Marshal(kn)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Test-KN", string(knJSON))
	req.Header.Set("X-Test-Actor", actor.String())
	req.Header.Set("Idempotency-Key", "archive227-"+ids.New().String())
	trace := telemetry.NewTraceID()
	req.Header.Set("traceparent", fmt.Sprintf("00-%s-0000000000000001-01", trace))
	rec := httptest.NewRecorder()
	f.eng.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), trace
}

func TestArchive227ReadAuditIsCommittedBeforeMetadata(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	x := f.export(p.ID, d.Version, true)
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	ctx := context.Background()
	base := "/v1/extension/khtc/exported-reports"
	query := "?year=2026&period_id=" + p.ID
	for _, tc := range []struct{ name, path, action, format string }{
		{"list", base + query, "read_list", "metadata"},
		{"period choices", base + "/periods" + query, "read_periods", "metadata"},
		{"field choices", base + "/fields" + query, "read_fields", "metadata"},
		{"history", reportPath(p.ID) + "/exports", "read_history", "metadata"},
		{"filtered history", reportPath(p.ID) + "/exports?field=khong_co", "read_history", "metadata"},
		{"preview", reportPath(p.ID) + "/exports/" + x.ID + "/preview", "preview_attempt", "preview"},
		{"Excel", reportPath(p.ID) + "/exports/" + x.ID + "/download", "excel_attempt", "excel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after int
			if err := admin.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_archive_access WHERE tenant_id=$1 AND action=$2`, f.tid, tc.action).Scan(&before); err != nil {
				t.Fatal(err)
			}
			status, body, trace := archive227Request(t, f, http.MethodGet, tc.path, authz.KNTinh, khtcDept, f.khtcUser, nil)
			f.ok(status, body, nil)
			if err := admin.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_archive_access WHERE tenant_id=$1 AND action=$2`, f.tid, tc.action).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != before+1 {
				t.Fatalf("successful %s returned data without its committed access audit: before=%d after=%d", tc.name, before, after)
			}
			var actor, selected uuid.UUID
			var at time.Time
			var recordedTrace, format, rule string
			if err := admin.QueryRow(ctx, `SELECT actor_ref,context_ref,event_time,trace_id,format,rule_version FROM pf_extension.khtc_archive_access WHERE tenant_id=$1 AND action=$2 ORDER BY seq DESC LIMIT 1`, f.tid, tc.action).Scan(&actor, &selected, &at, &recordedTrace, &format, &rule); err != nil {
				t.Fatal(err)
			}
			if actor != f.khtcUser || selected == uuid.Nil || !at.Equal(f.clk.Now()) || recordedTrace != trace || format != tc.format || rule != "archive-read-v1" {
				t.Fatalf("audit must use the actual caller/context/clock/request trace and read semantics: actor=%s context=%s at=%s trace=%s format=%s rule=%s", actor, selected, at, recordedTrace, format, rule)
			}
		})
	}
}

type archive227Endpoint struct {
	name, method, path string
	body               any
}

func archive227Endpoints(period, exportID, word string) []archive227Endpoint {
	base := "/v1/extension/khtc/exported-reports"
	query := "?year=2026&period_id=" + period
	file := reportPath(period) + "/exports/" + exportID
	return []archive227Endpoint{
		{"list", http.MethodGet, base + query, nil},
		{"period choices", http.MethodGet, base + "/periods" + query, nil},
		{"field choices", http.MethodGet, base + "/fields" + query, nil},
		{"history", http.MethodGet, reportPath(period) + "/exports", nil},
		{"filtered history", http.MethodGet, reportPath(period) + "/exports?field=khong_co", nil},
		{"preview", http.MethodGet, file + "/preview", nil},
		{"Excel", http.MethodGet, file + "/download", nil},
		{"Word list", http.MethodGet, file + "/words", nil},
		{"stored Word GET", http.MethodGet, file + "/words/" + word + "/download", nil},
		{"mixed ZIP POST", http.MethodPost, base + "/zip", map[string]any{"items": []map[string]any{
			{"period_id": period, "export_id": exportID, "format": "excel"},
			{"period_id": period, "export_id": exportID, "format": "word", "run_id": word},
		}}},
	}
}

func TestArchive227ReadAuditFailureAndCommitRollback(t *testing.T) {
	f := newReportFx(t)
	counter := installArchive199Exporter(t, f)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	x := f.export(p.ID, d.Version, true)
	frozenExcel := f.download(p.ID, x.ID)
	w := f.word(p.ID, x.ID, "archive227-failure-fixture")
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	ctx := context.Background()
	count := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_archive_access WHERE tenant_id=$1`, f.tid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// A deferred constraint trigger on the physical partition fails at COMMIT,
	// after INSERT itself succeeded. This exercises the actual transaction boundary.
	var schema, partition string
	if err := admin.QueryRow(ctx, `SELECT n.nspname,c.relname FROM pf_extension.khtc_archive_access a JOIN pg_class c ON c.oid=a.tableoid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE a.tenant_id=$1 ORDER BY a.seq DESC LIMIT 1`, f.tid).Scan(&schema, &partition); err != nil {
		t.Fatal(err)
	}
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprintf("deferred_commit=%t", deferred), func(t *testing.T) {
			name := "test_archive227_" + strings.ReplaceAll(ids.New().String(), "-", "")
			fn := pgx.Identifier{"pf_extension", name}.Sanitize()
			trigger := pgx.Identifier{name}.Sanitize()
			table := pgx.Identifier{"pf_extension", "khtc_archive_access"}.Sanitize()
			clause := "CREATE TRIGGER " + trigger + " BEFORE INSERT ON " + table + " FOR EACH ROW "
			if deferred {
				table = pgx.Identifier{schema, partition}.Sanitize()
				clause = "CREATE CONSTRAINT TRIGGER " + trigger + " AFTER INSERT ON " + table + " DEFERRABLE INITIALLY DEFERRED FOR EACH ROW "
			}
			function := `CREATE FUNCTION ` + fn + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='` + f.tid.String() + `'::uuid THEN RAISE EXCEPTION 'controlled archive access audit unavailable'; END IF; RETURN NEW; END $$; `
			if _, err := admin.Exec(ctx, function+clause+"EXECUTE FUNCTION "+fn+"()"); err != nil {
				t.Fatal(err)
			}
			cleanup := func() {
				if _, err := admin.Exec(ctx, "DROP TRIGGER IF EXISTS "+trigger+" ON "+table+"; DROP FUNCTION IF EXISTS "+fn+"()"); err != nil {
					t.Error(err)
				}
			}
			t.Cleanup(cleanup)
			before, reads, renders := count(), counter.reads.Load(), counter.exports.Load()
			for _, ep := range archive227Endpoints(p.ID, x.ID, w.ID) {
				status, body, _ := archive227Request(t, f, ep.method, ep.path, authz.KNTinh, khtcDept, f.khtcUser, ep.body)
				if status < 500 || status >= 600 || bytes.HasPrefix(body, []byte("PK")) || bytes.Contains(body, []byte(`"items":`)) || bytes.Contains(body, []byte(`"content":`)) {
					t.Fatalf("audit failure returned %s data: status=%d", ep.name, status)
				}
				if count() != before || counter.reads.Load() != reads || counter.exports.Load() != renders {
					t.Fatalf("audit rollback/port boundary violated for %s", ep.name)
				}
			}
			cleanup()
			for _, ep := range archive227Endpoints(p.ID, x.ID, w.ID) {
				status, body, _ := archive227Request(t, f, ep.method, ep.path, authz.KNTinh, khtcDept, f.khtcUser, ep.body)
				if status != http.StatusOK {
					t.Fatalf("%s did not recover after audit restoration: status=%d", ep.name, status)
				}
				if ep.name == "Excel" && !bytes.Equal(body, frozenExcel) {
					t.Fatal("audit recovery changed stored Excel bytes")
				}
			}
			if counter.exports.Load() != renders {
				t.Fatal("read recovery rendered or allocated another Word")
			}
		})
	}
}

func TestArchive227ExistingAuthorityBeforeAllReadPaths(t *testing.T) {
	f := newReportFx(t)
	counter := installArchive199Exporter(t, f)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	x := f.export(p.ID, d.Version, true)
	excel := f.download(p.ID, x.ID)
	w := f.word(p.ID, x.ID, "archive227-authority-fixture")
	wordPath := reportPath(p.ID) + "/exports/" + x.ID + "/words/" + w.ID + "/download"
	status, frozenWord, _ := archive227Request(t, f, http.MethodGet, wordPath, authz.KNTinh, khtcDept, f.khtcUser, nil)
	f.ok(status, frozenWord, nil)
	reads, renders := counter.reads.Load(), counter.exports.Load()
	for _, who := range []struct{ role, dept string }{{authz.KNTinh, dTT}, {authz.KNXa, khtcDept}, {authz.KNTW, dTT}} {
		for _, ep := range archive227Endpoints(p.ID, x.ID, w.ID) {
			// Each call gets a new operation key, including the real stored Word
			// GET and mixed ZIP POST; a successful cached response cannot mask denial.
			status, body, _ := archive227Request(t, f, ep.method, ep.path, who.role, who.dept, f.khtcUser, ep.body)
			if status != http.StatusForbidden || bytes.HasPrefix(body, []byte("PK")) || bytes.Contains(body, []byte(`"items":`)) || bytes.Contains(body, []byte(`"content":`)) {
				t.Fatalf("role=%s dept=%s %s must deny before data: status=%d", who.role, who.dept, ep.name, status)
			}
		}
	}
	if counter.reads.Load() != reads || counter.exports.Load() != renders {
		t.Fatal("denied current role/department reached stored-file or rendering port")
	}
	// RLS is still decisive for immutable identities under another tenant.
	own := f.tid
	f.tid = ids.New()
	for _, ep := range archive227Endpoints(p.ID, x.ID, w.ID) {
		status, body, _ := archive227Request(t, f, ep.method, ep.path, authz.KNTinh, khtcDept, f.khtcUser, ep.body)
		if status != http.StatusNotFound && (status != http.StatusOK || bytes.Contains(body, []byte(x.ID))) {
			f.tid = own
			t.Fatalf("cross-tenant %s exposed a saved identity: status=%d", ep.name, status)
		}
	}
	f.tid = own
	if counter.reads.Load() != reads || counter.exports.Load() != renders {
		t.Fatal("cross-tenant request reached stored-file or renderer")
	}
	// Live sources may change; access logging never refreshes stored artifacts.
	f.src.set(nil, "archive227-live-source-changed")
	if got := f.download(p.ID, x.ID); !bytes.Equal(got, excel) {
		t.Fatal("read audit changed frozen Excel")
	}
	status, body, _ := archive227Request(t, f, http.MethodGet, wordPath, authz.KNTinh, khtcDept, f.khtcUser, nil)
	f.ok(status, body, nil)
	if !bytes.Equal(body, frozenWord) || counter.exports.Load() != renders {
		t.Fatal("read audit changed or re-rendered frozen Word")
	}
}

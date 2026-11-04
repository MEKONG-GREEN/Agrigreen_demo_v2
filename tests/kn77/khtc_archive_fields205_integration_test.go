package kn77_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
)

func TestArchive205FieldMetadataFromFrozenExport(t *testing.T) {
	f := newReportFx(t)
	counter := installArchive199Exporter(t, f)
	status, body := f.do(http.MethodPost, "/v1/kn/khtc/catalogs/field", authz.KNAdmin, "", uuid.Nil, "", map[string]any{"code": "chan_nuoi", "name": "Chăn nuôi"})
	f.ok(status, body, nil)
	for _, entry := range f.planBody["entries"].([]map[string]any) {
		if entry["kind"] == "program" {
			entry["field_code"] = "trong_trot"
		}
		if entry["ref"] == f.price || entry["ref"] == f.news {
			entry["field_code"] = "chan_nuoi"
		}
	}
	f.planBody["base_version"] = 2
	f.planBody["decision_no"] = "03/QD-KH205"
	status, body = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", f.planBody)
	f.ok(status, body, nil)
	p := f.open(41, dTT, dTTTT)
	draft := f.refresh(p.ID, 0)
	var export reportExport
	status, body = f.call(http.MethodPost, p.ID, "/exports", map[string]any{"base_version": draft.Version, "acknowledge_provisional": true, "filter": map[string]any{"department": dTT}})
	f.ok(status, body, &export)
	frozenExcel := f.download(p.ID, export.ID)
	if len(frozenExcel) == 0 {
		t.Fatal("positive control did not persist a real Excel")
	}
	var archive struct {
		Items []struct {
			ID    string   `json:"id"`
			Codes []string `json:"field_codes"`
			State string   `json:"field_status"`
		} `json:"items"`
	}
	status, body = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026&versions=true&period_id="+p.ID, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(status, body, &archive)
	if len(archive.Items) != 1 || archive.Items[0].ID != export.ID {
		t.Fatal("archive did not return the native export")
	}
	if !slices.Equal(archive.Items[0].Codes, []string{"trong_trot"}) || archive.Items[0].State != "complete" {
		t.Fatalf("frozen selected rows must inherit trong_trot only: codes=%v state=%q", archive.Items[0].Codes, archive.Items[0].State)
	}

	word := f.word(p.ID, export.ID, "archive205-native-binding")
	beforeReads, beforeExports := counter.reads.Load(), counter.exports.Load()
	wordPath := reportPath(p.ID) + "/exports/" + export.ID + "/words/" + word.ID + "/download?field=trong_trot"
	mixed := map[string]any{"field": "trong_trot", "items": []map[string]any{{"period_id": p.ID, "export_id": export.ID, "format": "excel"}, {"period_id": p.ID, "export_id": export.ID, "format": "word", "run_id": word.ID}}}
	for _, role := range []string{authz.KNTinh, authz.KNXa} {
		status, body = f.do(http.MethodGet, wordPath, role, dTT, f.khtcUser, uuid.NewString(), nil)
		if status != 403 || bytes.HasPrefix(body, []byte("PK")) {
			t.Fatalf("forbidden Word produced status%d", status)
		}
		status, body = f.do(http.MethodPost, "/v1/extension/khtc/exported-reports/zip", role, dTT, f.khtcUser, uuid.NewString(), mixed)
		if status != 403 || bytes.HasPrefix(body, []byte("PK")) {
			t.Fatalf("forbidden mixed ZIP produced status%d", status)
		}
	}
	if counter.reads.Load() != beforeReads || counter.exports.Load() != beforeExports {
		t.Fatal("denied Word/ZIP reached stored-file exporter port")
	}
	status, _ = f.call(http.MethodGet, p.ID, "/exports/"+export.ID+"/words/"+word.ID+"/download?field=chan_nuoi", nil)
	if status != 404 || counter.reads.Load() != beforeReads {
		t.Fatal("mismatched field reached Word port")
	}
	status, body = f.call(http.MethodGet, p.ID, "/exports/"+export.ID+"/download?field=trong_trot", nil)
	f.ok(status, body, nil)
	if !bytes.Equal(body, frozenExcel) {
		t.Fatal("field filtering changed saved Excel bytes")
	}
	for _, tail := range []string{"/download", "/preview", "/words"} {
		status, _ = f.call(http.MethodGet, p.ID, "/exports/"+export.ID+tail+"?field=chan_nuoi", nil)
		if status != 404 {
			t.Fatalf("field mismatch %s status%d", tail, status)
		}
	}
	status, _ = f.call(http.MethodGet, p.ID, "/exports/"+export.ID+"/preview?field=trong_trot", nil)
	if status != 200 {
		t.Fatalf("matching preview status%d", status)
	}
	status, _ = f.call(http.MethodGet, p.ID, "/exports/"+export.ID+"/words?field=trong_trot", nil)
	if status != 200 {
		t.Fatalf("matching Word history status%d", status)
	}
	var history struct {
		Items []reportExport `json:"items"`
	}
	status, body = f.call(http.MethodGet, p.ID, "/exports?field=chan_nuoi", nil)
	f.ok(status, body, &history)
	if len(history.Items) != 0 {
		t.Fatal("field mismatch leaked export history")
	}
	status, body = f.call(http.MethodGet, p.ID, "/exports?field=trong_trot", nil)
	f.ok(status, body, &history)
	if len(history.Items) != 1 || history.Items[0].ID != export.ID {
		t.Fatal("matching export history missing")
	}
	mixed["field"] = "chan_nuoi"
	beforeReads = counter.reads.Load()
	status, _ = f.do(http.MethodPost, "/v1/extension/khtc/exported-reports/zip", authz.KNTinh, khtcDept, f.khtcUser, uuid.NewString(), mixed)
	if status != 404 || counter.reads.Load() != beforeReads {
		t.Fatal("mismatched ZIP field reached file port")
	}
	mixed["field"] = "trong_trot"
	status, body = f.do(http.MethodPost, "/v1/extension/khtc/exported-reports/zip", authz.KNTinh, khtcDept, f.khtcUser, uuid.NewString(), mixed)
	f.ok(status, body, nil)
	if !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatal("matching ZIP missing immutable files")
	}

}

func TestArchive205CompleteChoicesPast10001AndLegacyStates(t *testing.T) {
	f := newReportFx(t)
	status, body := f.do(http.MethodPost, "/v1/kn/khtc/catalogs/field", authz.KNAdmin, "", uuid.Nil, "", map[string]any{"code": "chan_nuoi", "name": "Chăn nuôi"})
	f.ok(status, body, nil)
	setFields := func(base int, code string, partial bool) {
		t.Helper()
		for _, e := range f.planBody["entries"].([]map[string]any) {
			e["field_code"] = code
			if partial && e["ref"] != f.item {
				e["field_code"] = ""
			}
		}
		f.planBody["base_version"] = base
		f.planBody["decision_no"] = "205/QD-KH"
		status, body = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", f.planBody)
		f.ok(status, body, nil)
	}
	makeExport := func(no int, document string) (string, string) {
		t.Helper()
		p := f.open(no, dTT, dTTTT)
		d := f.refresh(p.ID, 0)
		status, body = f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "admin": map[string]any{"document_no": document}})
		f.ok(status, body, &d)
		x := f.export(p.ID, d.Version, true)
		return p.ID, x.ID
	}
	setFields(2, "trong_trot", false)
	rarePeriod, rareExport := makeExport(41, "205-RARE")
	setFields(3, "chan_nuoi", false)
	commonPeriod, commonExport := makeExport(42, "205-COMMON")
	idsForClones := make([]uuid.UUID, 10001)
	for i := range idsForClones {
		idsForClones[i] = ids.New()
	}
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	result, err := pool.Exec(context.Background(), `INSERT INTO pf_extension.khtc_report_export
 (id,tenant_id,stream_key,period_id,draft_id,payload,input_hash,export_filter,file_name,body,body_hash,provisional,actor_ref,event_time,idem_key,rule_version)
 SELECT fixture.id,x.tenant_id,x.stream_key,x.period_id,x.draft_id,x.payload,x.input_hash,x.export_filter,x.file_name,x.body,x.body_hash,x.provisional,x.actor_ref,x.event_time+fixture.n*interval '1 second','archive205:clone:'||fixture.id::text,x.rule_version
 FROM pf_extension.khtc_report_export x CROSS JOIN unnest($3::uuid[]) WITH ORDINALITY AS fixture(id,n)
 WHERE x.id=$1 AND x.tenant_id=$2`, commonExport, f.tid, idsForClones)
	if err != nil || result.RowsAffected() != 10001 {
		t.Fatalf("native10001metadata setup count%d error%v", result.RowsAffected(), err)
	}
	type fieldsPage struct {
		Items []struct {
			Code string `json:"code"`
		} `json:"items"`
		HasMore bool   `json:"has_more"`
		After   string `json:"next_cursor"`
	}
	getFields := func(query string) fieldsPage {
		t.Helper()
		var out fieldsPage
		status, body = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports/fields?"+query, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
		f.ok(status, body, &out)
		return out
	}
	first := getFields("year=2026&versions=true&limit=1")
	if len(first.Items) != 1 || first.Items[0].Code != "chan_nuoi" || !first.HasMore || first.After != "chan_nuoi" {
		t.Fatalf("firstcompletefieldpage%+v", first)
	}
	second := getFields("year=2026&versions=true&limit=1&after=" + first.After)
	if len(second.Items) != 1 || second.Items[0].Code != "trong_trot" || second.HasMore {
		t.Fatal("older field starved past10001newer metadata", second)
	}
	for _, query := range []string{"year=2025", "year=2026&kind=year", "year=2026&field_status=unknown", "year=2026&period_id=" + commonPeriod + "&field=trong_trot"} {
		if got := getFields(query); len(got.Items) != 0 {
			t.Fatal("combined field choice filter ignored", query, got)
		}
	}
	got := getFields("year=2026&versions=true&kind=week&q=205-RARE&period_id=" + rarePeriod + "&field=trong_trot")
	if len(got.Items) != 1 || got.Items[0].Code != "trong_trot" {
		t.Fatal("complete intersected choices", got)
	}
	var rows struct {
		Items []struct {
			ID    string `json:"id"`
			State string `json:"field_status"`
		} `json:"items"`
	}
	status, body = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026&versions=true&field=trong_trot", authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(status, body, &rows)
	if len(rows.Items) != 1 || rows.Items[0].ID != rareExport {
		t.Fatal("list filtered after candidate limit")
	}
	var periods struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	status, body = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports/periods?year=2026&versions=true&field=trong_trot", authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(status, body, &periods)
	if len(periods.Items) != 1 || periods.Items[0].ID != rarePeriod {
		t.Fatal("period choices starved or ignored field")
	}
	setFields(4, "", false)
	unknownPeriod, unknownExport := makeExport(43, "205-UNKNOWN")
	setFields(5, "trong_trot", true)
	_, partialExport := makeExport(44, "205-PARTIAL")
	for _, check := range []struct{ state, id string }{{"unknown", unknownExport}, {"partial", partialExport}} {
		status, body = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026&versions=true&field_status="+check.state, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
		f.ok(status, body, &rows)
		if len(rows.Items) != 1 || rows.Items[0].ID != check.id || rows.Items[0].State != check.state {
			t.Fatalf("explicitlegacy%smetadata%+v", check.state, rows)
		}
	}
	// A live master field exists, but missing frozen entry fields stay unknown.
	before := f.download(unknownPeriod, unknownExport)
	if len(before) == 0 {
		t.Fatal("unknown field file lost bytes")
	}
	status, body = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026", authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(status, body, &rows)
	found := false
	for _, row := range rows.Items {
		found = found || row.ID == unknownExport
	}
	if !found {
		t.Fatal("default archive dropped unknown legacy metadata")
	}
	for _, key := range []string{"year", "limit", "offset"} {
		for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "9999999999999999999999999999999999999999", "1.00001"} {
			path := "/v1/extension/khtc/exported-reports/fields?year=2026&" + key + "=" + raw
			if key == "year" {
				path = "/v1/extension/khtc/exported-reports/fields?year=" + raw
			}
			status, _ = f.do(http.MethodGet, path, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
			if status != 422 {
				t.Fatalf("numeric query %s produced%d", key, status)
			}
		}
	}
}

func TestArchive205PeriodNameDocumentCollisionCompatibility(t *testing.T) {
	f := newReportFx(t)
	a := f.open(39, dTT, dTTTT)
	b := f.open(40, dTT, dTTTT)
	for _, item := range []struct {
		p   khtcPeriod
		doc string
	}{{a, "205-own-document"}, {b, a.Name}} {
		d := f.refresh(item.p.ID, 0)
		status, body := f.call(http.MethodPost, item.p.ID, "/draft", map[string]any{"base_version": d.Version, "admin": map[string]any{"document_no": item.doc}})
		f.ok(status, body, &d)
		f.export(item.p.ID, d.Version, true)
	}
	for _, check := range []struct {
		query string
		want  string
	}{
		{"q=" + url.QueryEscape(a.Name), a.ID},
		{"document_q=" + url.QueryEscape(a.Name), b.ID},
		{"q=" + url.QueryEscape(a.Name) + "&document_q=" + url.QueryEscape(a.Name), a.ID},
		{"q=" + url.QueryEscape(b.Name) + "&document_q=205-own-document", ""},
		{"q=" + url.QueryEscape(b.Name) + "&document_q=" + url.QueryEscape(a.Name) + "&field_status=unknown", b.ID},
		{"q=" + url.QueryEscape(b.Name) + "&document_q=" + url.QueryEscape(a.Name) + "&field=trong_trot", ""},
	} {
		var got struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		status, body := f.do(http.MethodGet, "/v1/extension/khtc/exported-reports/periods?year=2026&versions=true&limit=1&"+check.query, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
		f.ok(status, body, &got)
		if check.want == "" {
			if len(got.Items) != 0 {
				t.Fatal("period-name/document intersection leaked", check.query, got)
			}
		} else if len(got.Items) != 1 || got.Items[0].ID != check.want {
			t.Fatal("period-name q compatibility broken", check.query, got)
		}
	}
}

// TestArchive205LegacyMissingRowCountFailsBeforeEgress creates explicitly
// malformed historical shape through append-only fixture SQL; it never rewrites
// a native export or disables its matching-draft/hash-chain guards.
func TestArchive205LegacyMissingRowCountFailsBeforeEgress(t *testing.T) {
	f := newReportFx(t)
	status, body := f.do(http.MethodPost, "/v1/kn/khtc/catalogs/field", authz.KNAdmin, "", uuid.Nil, "", map[string]any{"code": "chan_nuoi", "name": "Chăn nuôi"})
	f.ok(status, body, nil)
	for _, entry := range f.planBody["entries"].([]map[string]any) {
		if entry["kind"] == "program" {
			entry["field_code"] = "trong_trot"
		}
		if entry["ref"] == f.price || entry["ref"] == f.news {
			entry["field_code"] = "chan_nuoi"
		}
	}
	f.planBody["base_version"] = 2
	f.planBody["decision_no"] = "03/QD-KH205-LEGACY"
	status, body = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", f.planBody)
	f.ok(status, body, nil)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	original := f.export(p.ID, d.Version, true)
	var multi struct {
		Items []struct {
			ID    string   `json:"id"`
			Codes []string `json:"field_codes"`
			State string   `json:"field_status"`
		} `json:"items"`
	}
	status, body = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026&versions=true&period_id="+p.ID, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(status, body, &multi)
	if len(multi.Items) != 1 || multi.Items[0].ID != original.ID || multi.Items[0].State != "complete" || !slices.Equal(multi.Items[0].Codes, []string{"chan_nuoi", "trong_trot"}) {
		t.Fatal("native unfiltered multi-field snapshot control failed", multi)
	}
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	var expectedRecords int64
	if err := pool.QueryRow(context.Background(), `SELECT jsonb_array_length(payload#>'{data,rows}') FROM pf_extension.khtc_report_export WHERE tenant_id=$1 AND id=$2`, f.tid, original.ID).Scan(&expectedRecords); err != nil || expectedRecords <= 0 {
		t.Fatalf("valid control row count%d: %v", expectedRecords, err)
	}
	checkpoint := &reportMeter{}
	f.ext.UseEgress(checkpoint, meter.ModeEnforce, nil)
	validBytes := f.download(p.ID, original.ID)
	if len(validBytes) == 0 || len(checkpoint.events) != 1 || checkpoint.events[0].Records != expectedRecords || checkpoint.events[0].Bytes != int64(len(validBytes)) {
		t.Fatalf("valid Excel200 egress control not established: events=%d", len(checkpoint.events))
	}
	for _, nullRows := range []bool{false, true} {
		draftID, exportID := ids.New(), ids.New()
		tx, err := pool.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(context.Background(), `INSERT INTO pf_extension.khtc_report_draft
   (id,tenant_id,stream_key,period_id,action,payload,input_hash,event_time,actor_ref,idem_key,rule_version)
   SELECT $3::uuid,x.tenant_id,'khtc_report_draft:'||x.period_id::text,x.period_id,'save',
   CASE WHEN $4::boolean THEN jsonb_set(x.payload,'{data,rows}','null'::jsonb) ELSE x.payload #- '{data,rows}' END,
   x.input_hash,x.event_time+interval '1 second',x.actor_ref,'archive205:legacy-draft:'||$3::uuid::text,x.rule_version
   FROM pf_extension.khtc_report_export x WHERE x.id=$1 AND x.tenant_id=$2`, original.ID, f.tid, draftID, nullRows)
		if err == nil {
			_, err = tx.Exec(context.Background(), `INSERT INTO pf_extension.khtc_report_export
    (id,tenant_id,stream_key,period_id,draft_id,payload,input_hash,export_filter,file_name,body,body_hash,provisional,event_time,actor_ref,idem_key,rule_version)
    SELECT $4::uuid,x.tenant_id,x.stream_key,x.period_id,d.id,d.payload,d.input_hash,x.export_filter,x.file_name,x.body,x.body_hash,x.provisional,
    x.event_time+interval '2 seconds',x.actor_ref,'archive205:legacy-export:'||$4::uuid::text,x.rule_version
    FROM pf_extension.khtc_report_export x JOIN pf_extension.khtc_report_draft d ON d.tenant_id=x.tenant_id AND d.period_id=x.period_id AND d.id=$3
    WHERE x.id=$1 AND x.tenant_id=$2`, original.ID, f.tid, draftID, exportID)
		}
		if err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("append-only legacy shape fixture null=%t: %v", nullRows, err)
		}
		if err = tx.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{"", "&field_status=unknown"} {
			var list struct {
				Items []struct {
					ID    string   `json:"id"`
					Codes []string `json:"field_codes"`
					State string   `json:"field_status"`
				} `json:"items"`
			}
			status, body = f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026&versions=true&period_id="+p.ID+suffix, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
			f.ok(status, body, &list)
			found := false
			for _, row := range list.Items {
				if row.ID == exportID.String() {
					found = true
					if row.State != "unknown" || len(row.Codes) != 0 {
						t.Fatal("legacy row-count metadata invented", row)
					}
				}
			}
			if !found {
				t.Fatal("default/unknown archive lost legacy file", exportID)
			}
		}
		before := len(checkpoint.events)
		status, body = f.do(http.MethodGet, "/v1/extension/khtc/periods/"+p.ID+"/report/exports/"+exportID.String()+"/download", authz.KNTinh, khtcDept, f.khtcUser, "", nil)
		var fail struct {
			Code string `json:"code"`
		}
		if status != http.StatusServiceUnavailable || json.Unmarshal(body, &fail) != nil || fail.Code != "extension.dependency_unavailable" || bytes.HasPrefix(body, []byte("PK")) {
			t.Fatalf("unknown row count must503 before Excel bytes, null=%t status%d code%q", nullRows, status, fail.Code)
		}
		if len(checkpoint.events) != before {
			t.Fatal("unknown row count emitted a fictitious egress event instead of failing closed")
		}
	}
	if !bytes.Equal(f.download(p.ID, original.ID), validBytes) {
		t.Fatal("legacy fixture changed original stored Excel bytes")
	}
}

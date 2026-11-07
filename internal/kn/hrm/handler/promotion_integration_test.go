package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func promoRules(quotaPct string) map[string]any {
	return map[string]any{"valid_from": "2026-06-01", "note": "W11.25 kiểm thử: chỉ tiêu NLTH",
		"payload": map[string]any{"min_rating": "HTNV", "rating_years": 3, "achievement_years": 5,
			"discipline_extension_months": map[string]any{"khien_trach": 6, "canh_cao": 6, "cach_chuc": 12}, "nlth_quota_pct": quotaPct,
			"nlth_retirement_months": 12, "nlth_retirement_within_months": 12, "tnvk_start_pct": "5", "tnvk_step_pct": "1", "tnvk_cycle_months": 12,
			"tnvk_first_months": 36, "remind_months": []int{1, 3, 6}, "retirement_age_male_months": 738, "retirement_age_female_months": 684}}
}

func items(m map[string]any, key string) []map[string]any {
	var out []map[string]any
	if xs, ok := m[key].([]any); ok {
		for _, x := range xs {
			out = append(out, x.(map[string]any))
		}
	}
	return out
}

func byStaff(xs []map[string]any, staff, kind string) map[string]any {
	for _, x := range xs {
		if x["staff_id"] == staff && (kind == "" || x["kind"] == kind) {
			return x
		}
	}
	return nil
}

func (f *recFixture) multipartFile(as, path, name string, content []byte, want int) map[string]any {
	f.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(content)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Test-As", as)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if rec.Code != want {
		f.t.Fatalf("%s POST %s: %d (muốn %d) %s", as, path, rec.Code, want, rec.Body.String())
	}
	return m
}

// FN-09 v4 on native PostgreSQL: personal months, current profile, unrestricted
// due-date years, exact states, direct export, permissions and immutable legacy reads.
func TestHRMPromotionHTTPV4(t *testing.T) {
	off := newRecFixture(t, false)
	off.call("hcth", http.MethodGet, "/v1/hrm/promotion", nil, http.StatusForbidden)
	f := newRecFixture(t, true)
	withDocs(t, f)
	if err := db.WithModuleTenantTx(t.Context(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error {
		for _, d := range domain.DefaultItems() {
			attrs, _ := json.Marshal(d.Attrs)
			if _, err := store.InsertItem(t.Context(), tx, ids.New(), f.tid, d.Item, attrs, d.Placeholder, f.svc.Clock.Now(), f.who["admin"].ActorRef, "catalog:"+string(d.Kind)+":"+d.Code); err != nil {
				return err
			}
		}
		for _, sc := range domain.DefaultScales() {
			if _, err := store.InsertScale(t.Context(), tx, ids.New(), f.tid, sc, true, f.svc.Clock.Now(), f.who["admin"].ActorRef, "scale:"+sc.GradeCode); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.call("admin", http.MethodPost, "/v1/hrm/docs/folders/defaults", nil, http.StatusCreated)
	mk := func(name, phone, step, since, months string) string {
		payload := map[string]any{"grade": "01.003", "step": step, "step_since": since}
		if months != "" {
			payload["promotion_months"] = months
		}
		return f.call("hcth", http.MethodPost, "/v1/hrm/staff", staffBody("vien_chuc", name, phone, "nam", "1980-03-04", nil,
			ev("unit", "2015-03-01", map[string]any{"unit": "trong_trot"}), ev("position", "2015-03-01", map[string]any{"position": "vien_chuc"}),
			ev("work_start", "2015-03-01", map[string]any{}), ev("grade", "2015-03-01", payload)), http.StatusCreated)["id"].(string)
	}
	a := mk("Trần Văn An", "0913000001", "5", "2023-11-01", "36")
	b := mk("Lê Văn Bình", "0913000002", "4", "2025-03-01", "24")
	last := mk("Phạm Văn Cuối", "0913000003", "9", "2023-06-01", "")
	missing := mk("Ngô Văn Thiếu", "0913000004", "2", "2025-01-01", "")
	e := mk("Võ Văn Em", "0913000005", "2", "2026-01-31", "1")
	uni := mk("Nguyễn Văn Đại Học", "0913000006", "3", "2025-03-01", "36")
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+b+"/ratings", map[string]any{"year": 2025, "rating": "KHTNV"}, http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+b+"/events", ev("discipline", "2025-05-01", map[string]any{"form": "canh_cao"}), http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+uni+"/events", ev("unit", "2028-01-01", map[string]any{"unit": "chan_nuoi"}), http.StatusCreated)
	// Catalogs/scales were inserted without any promotion/conversion rule versions.
	admin := f.env.AdminPool(t, migrations.PF)
	var rules int
	if err := admin.QueryRow(t.Context(), "SELECT count(*) FROM pf_hrm.rule_param WHERE tenant_id=$1", f.tid).Scan(&rules); err != nil || rules != 0 {
		t.Fatalf("no-rule fixture: %d %v", rules, err)
	}
	all := f.call("hcth", http.MethodGet, "/v1/hrm/promotion", nil, http.StatusOK)
	xs := items(all, "items")
	if len(xs) != 4 || byStaff(xs, last, "") != nil || len(items(all, "missing")) != 1 || items(all, "missing")[0]["id"] != missing {
		t.Fatalf("regular/missing/last: %v", all)
	}
	for _, key := range []string{"summary", "rules", "last_scan", "window"} {
		if _, ok := all[key]; ok {
			t.Fatalf("out-of-BRD block %s", key)
		}
	}
	rb := byStaff(xs, b, "nltx")
	if rb["due"] != "2027-03-01" || rb["cond"] != "ok" || rb["promotion_months"] != json.Number("24") {
		t.Fatalf("personal NB07/KHTNV: %v", rb)
	}
	if re := byStaff(xs, e, "nltx"); re["due"] != "2026-02-28" {
		t.Fatalf("month end: %v", re)
	}
	if got := items(f.call("admin", http.MethodGet, "/v1/hrm/promotion?year=2028&month=3&at=2020-01-01", nil, http.StatusOK), "items"); len(got) != 1 || got[0]["staff_id"] != uni || got[0]["unit"] != "trong_trot" {
		t.Fatalf("current profile, any future year: %v", got)
	}
	if got := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion?year=2027&month=3&status=pending&unit=trong_trot&q=Bình", nil, http.StatusOK), "items"); len(got) != 1 || got[0]["staff_id"] != b {
		t.Fatalf("filters: %v", got)
	}
	for _, q := range []string{"year=1e100000", "year=1e-100000", "year=" + strings.Repeat("9", 10000), "year=0", "month=13", "status=listed"} {
		f.call("hcth", http.MethodGet, "/v1/hrm/promotion?"+q, nil, http.StatusUnprocessableEntity)
	}
	for _, as := range []string{"ld_tt", "vc_tt", "xa", "tw", "realmonly"} {
		f.call(as, http.MethodGet, "/v1/hrm/promotion", nil, http.StatusForbidden)
	}
	if got := items(f.call("other", http.MethodGet, "/v1/hrm/promotion", nil, http.StatusOK), "items"); len(got) != 0 {
		t.Fatal("tenant leak")
	}
	for _, route := range []struct{ method, path string }{{"POST", "cases/include"}, {"POST", "cases/exclude"}, {"GET", "nlth-pool"}, {"POST", "proposals"}, {"POST", "proposals/withdraw"}} {
		f.call("admin", route.method, "/v1/hrm/promotion/"+route.path, map[string]any{}, http.StatusNotFound)
	}
	ra := byStaff(xs, a, "nltx")
	key := ra["key"].(string)
	detail := f.call("hcth", http.MethodGet, "/v1/hrm/promotion/cases/"+key, nil, http.StatusOK)
	if len(detail["basis"].([]any)) != 2 || detail["ratings"] != nil || detail["awards"] != nil {
		t.Fatalf("basis: %v", detail)
	}
	exportBody := map[string]any{"keys": []any{key, rb["key"]}, "review_month": "2026-11", "preparer_id": a, "signer_title": "Phó Giám đốc", "notes": map[string]string{key: "Kiểm thử NL02"}}
	f.call("viewonly", http.MethodPost, "/v1/hrm/promotion/exports", exportBody, http.StatusForbidden)
	noPreparer := map[string]any{}
	for k, v := range exportBody {
		if k != "preparer_id" {
			noPreparer[k] = v
		}
	}
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/exports", noPreparer, http.StatusUnprocessableEntity)
	exportBody["reserved"] = map[string]string{key: "0.12"}

	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 10000)} {
		f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+a+"/events", ev("step", "2026-10-01", map[string]any{"step": "5", "step_since": "2023-11-01", "promotion_months": raw}), http.StatusUnprocessableEntity)
		exportBody["months"] = map[string]string{key: raw}
		f.call("hcth", http.MethodPost, "/v1/hrm/promotion/exports", exportBody, http.StatusUnprocessableEntity)
		delete(exportBody, "months")
		exportBody["reserved_after"] = map[string]string{key: raw}
		f.call("hcth", http.MethodPost, "/v1/hrm/promotion/exports", exportBody, http.StatusUnprocessableEntity)
		delete(exportBody, "reserved_after")
	}
	exported := items(f.call("hcth", http.MethodPost, "/v1/hrm/promotion/exports", exportBody, http.StatusCreated), "items")
	if len(exported) != 1 || exported[0]["form"] != "mau1" || exported[0]["count"] != json.Number("2") {
		t.Fatalf("only form1: %v", exported)
	}
	exp := exported[0]
	eid := exp["id"].(string)
	w, data := f.raw("hcth", "/v1/hrm/promotion/exports/"+eid+"/file")
	if w.Code != http.StatusOK || w.Header().Get("X-Export-Drift") != "false" || w.Header().Get("X-Export-Sha256") != exp["sha256"] {
		t.Fatal("form1 hash drift")
	}
	txt := xlsxText(t, data, "mau1")
	for _, want := range []string{"|20\n", "|3,66|", "|3,99|", "|0,33|", "|1||Kiểm thử NL02"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("NL02 missing %q: %s", want, txt)
		}
	}
	if strings.Count(txt, "|0,12|") < 2 {
		t.Fatal("NL02 retained-after must default to retained coefficient entered for this export")
	}
	// A is named, B is absent: only A changes. No Return event is written.
	dec := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", map[string]any{"export_id": eid, "decision_no": "540/QĐ-SNNMT", "decided_on": "2026-10-10", "issuer": "Sở NN&MT", "people": []any{map[string]any{"case_key": key}}}, http.StatusCreated)
	if dec["returned"] != json.Number("0") {
		t.Fatalf("absent person returned: %v", dec)
	}
	after := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion", nil, http.StatusOK), "items")
	if byStaff(after, b, "nltx")["status"] != "exported" {
		t.Fatal("absent person's status changed")
	}
	decidedRows := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion?status=decided", nil, http.StatusOK), "items")
	if len(decidedRows) != 1 || decidedRows[0]["staff_id"] != a {
		t.Fatalf("decided: %v", decidedRows)
	}
	var returned int
	if err := admin.QueryRow(t.Context(), "SELECT count(*) FROM pf_hrm.promotion_event WHERE tenant_id=$1 AND action='return'", f.tid).Scan(&returned); err != nil || returned != 0 {
		t.Fatalf("return events: %d %v", returned, err)
	}
	did := dec["id"].(string)
	f.multipartFile("hcth", "/v1/hrm/promotion/decisions/"+did+"/file", "540.pdf", pdf, http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions/"+did+"/cancel", map[string]any{"reason": "Nhập sai số quyết định"}, http.StatusOK)
	if got := byStaff(items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion", nil, http.StatusOK), "items"), a, "nltx"); got == nil || got["key"] != key || got["status"] != "exported" {
		t.Fatalf("canceled exported decision must replay to waiting: %v", got)
	}
	// Independent regular decision updates status even before export.
	direct := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", map[string]any{"kind": "nltx", "decision_no": "541/QĐ", "decided_on": "2026-02-01", "issuer": "Sở NN&MT", "people": []any{map[string]any{"staff_id": e, "to_step": 3, "effective_on": "2026-02-28"}}}, http.StatusCreated)
	decidedRows = items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion?status=decided", nil, http.StatusOK), "items")
	if len(decidedRows) != 1 || decidedRows[0]["staff_id"] != e {
		t.Fatalf("direct decided: %v", decidedRows)
	}
	directID := direct["id"].(string)
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions/"+directID+"/cancel", map[string]any{"reason": "Kiểm thử hủy quyết định độc lập"}, http.StatusOK)
	// Historical independent decisions may have a keyed Undecide event, without
	// any export. Replay must preserve pending rather than invent export history.
	legacyUndecideID := ids.New()
	eKey := byStaff(xs, e, "nltx")["key"].(string)
	if err := db.WithModuleTenantTx(t.Context(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error {
		did := uuid.MustParse(directID)
		_, err := store.InsertPromoEvent(t.Context(), tx, f.tid, store.PromoEvent{ID: legacyUndecideID, StaffID: uuid.MustParse(e), CaseKey: eKey,
			Kind: "nltx", Action: domain.PromoActUndecide, DecisionID: &did, Reason: "Legacy independent decision canceled", Actor: f.who["hcth"].ActorRef}, f.svc.Clock.Now(), "legacy:undecide:"+eKey)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var hashBefore, hashAfter string
	if err := admin.QueryRow(t.Context(), "SELECT row_hash::text FROM pf_hrm.promotion_event WHERE id=$1", legacyUndecideID).Scan(&hashBefore); err != nil {
		t.Fatal(err)
	}
	if got := byStaff(items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion", nil, http.StatusOK), "items"), e, "nltx"); got == nil || got["key"] != eKey || got["status"] != "pending" {
		t.Fatalf("canceled independent decision without export must replay to pending: %v", got)
	}
	if got := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion?status=decided", nil, http.StatusOK), "items"); len(got) != 0 {
		t.Fatalf("canceled decisions still override active status: %v", got)
	}
	if err := admin.QueryRow(t.Context(), "SELECT row_hash::text FROM pf_hrm.promotion_event WHERE id=$1", legacyUndecideID).Scan(&hashAfter); err != nil || hashAfter != hashBefore {
		t.Fatalf("projection mutated legacy Undecide ledger: %s -> %s (%v)", hashBefore, hashAfter, err)
	}
	f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", map[string]any{"kind": "nltx", "decision_no": "QĐ-future", "decided_on": "2026-10-01", "issuer": "Sở NN&MT", "people": []any{map[string]any{"staff_id": a, "to_step": 6, "effective_on": "2026-11-01"}}}, http.StatusCreated)
	futureRows := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion?year=2026&month=11", nil, http.StatusOK), "items")
	if len(futureRows) != 1 || futureRows[0]["staff_id"] != a || futureRows[0]["status"] != "decided" {
		t.Fatalf("future independent decision duplicated a pending row: %v", futureRows)
	}
	for _, kind := range []string{"nlth", "tnvk"} {
		body := map[string]any{"kind": kind, "decision_no": "QĐ-" + kind, "decided_on": "2026-01-01", "issuer": "Sở NN&MT", "people": []any{map[string]any{"staff_id": last, "to_step": 9, "pct_to": "5", "effective_on": "2026-06-01"}}}
		f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", body, http.StatusCreated)
		if got := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion/decisions?kind="+kind, nil, http.StatusOK), "items"); len(got) != 1 {
			t.Fatal("legacy decision kind lost")
		}
	}
	if got := items(f.call("hcth", http.MethodGet, "/v1/hrm/promotion", nil, http.StatusOK), "items"); byStaff(got, last, "") != nil {
		t.Fatal("FN10 legacy kind leaked into FN09")
	}
	// Add-only fixture of pre-v4 Mẫu2/4 exports, including hashes and immutable snapshots.
	for _, form := range []string{"mau2", "mau4"} {
		row := store.PromoExport{ID: ids.New(), Action: "export", Form: form, ReviewMonth: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), AsOf: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			KnownAt: dayPtrForTest(time.Date(2026, 7, 1, 1, 0, 0, 0, time.UTC)), Params: map[string]string{"signer_title": "Giám đốc"}, StaffIDs: []uuid.UUID{uuid.MustParse(last)},
			FileName: domain.FormFileName(form, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)), SHA256: strings.Repeat("0", 64), ByteSize: 4096, Actor: f.who["hcth"].ActorRef,
			Items: []domain.ExportItem{{StaffID: uuid.MustParse(last), CaseKey: last + ":tnvk:v-20260601", Kind: domain.PromoTNVK, Grade: "01.003", FromStep: 9, ToStep: 9, FromCoeff: "4.98", ToCoeff: "4.98", PctTo: "5", Due: "2026-06-01", NextDue: "2027-06-01", MonthsEnjoyed: "1"}}}
		insert := func() {
			t.Helper()
			if err := db.WithModuleTenantTx(t.Context(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error {
				_, err := store.InsertPromoExport(t.Context(), tx, f.tid, row, *row.KnownAt, "legacy:"+row.ID.String())
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		insert()
		_, original := f.raw("hcth", "/v1/hrm/promotion/exports/"+row.ID.String()+"/file")
		hash := sha256.Sum256(original)
		row.ID = ids.New()
		row.SHA256 = hex.EncodeToString(hash[:])
		insert()
		var before, afterHash string
		if err := admin.QueryRow(t.Context(), "SELECT row_hash::text FROM pf_hrm.promotion_export WHERE id=$1", row.ID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		rw, reloaded := f.raw("hcth", "/v1/hrm/promotion/exports/"+row.ID.String()+"/file")
		if rw.Code != http.StatusOK || rw.Header().Get("X-Export-Drift") != "false" || !bytes.Equal(original, reloaded) {
			t.Fatalf("%s legacy bytes/hash changed", form)
		}
		if err := admin.QueryRow(t.Context(), "SELECT row_hash::text FROM pf_hrm.promotion_export WHERE id=$1", row.ID).Scan(&afterHash); err != nil || before != afterHash {
			t.Fatal("legacy ledger changed")
		}
	}
	// No PII in append-only promotion ledgers; RLS and mutation guards stay enforced.
	var dump string
	if err := admin.QueryRow(context.Background(), `SELECT concat_ws(' ',(SELECT string_agg(to_jsonb(x)::text,' ') FROM pf_hrm.promotion_export x WHERE tenant_id=$1),(SELECT string_agg(to_jsonb(x)::text,' ') FROM pf_hrm.promotion_decision x WHERE tenant_id=$1))`, f.tid).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dump, "Trần Văn An") || strings.Contains(dump, "0913000001") {
		t.Fatal("PII in promotion ledger")
	}
	for _, q := range []string{"UPDATE pf_hrm.promotion_decision SET issuer='x'", "DELETE FROM pf_hrm.promotion_event", "UPDATE pf_hrm.promotion_export SET file_name='x.xlsx'"} {
		if err := db.WithModuleTenantTx(t.Context(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), q); return err }); err == nil {
			t.Fatal("ledger mutation allowed")
		}
	}
}

func dayPtrForTest(t time.Time) *time.Time { return &t }

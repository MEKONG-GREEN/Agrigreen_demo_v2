package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestHRMAppraisalLegacyReadAndMigration281(t *testing.T) {
	a := newAppraisalFixture(t)
	f := a.f
	seedLegacyAnnualRating281(t, f, a.staff["xa"], 2024, "HTNV")
	ctx := context.Background()
	pool := f.env.AdminPool(t, migrations.PF)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Chỉ fixture owner của cụm tạm: dựng lại trạng thái ngay trước migration HC-2.
	if _, err = tx.Exec(ctx, `DROP TRIGGER appraisal_proposal_v4_write ON pf_hrm.appraisal_event`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, f.tid.String()); err != nil {
		t.Fatal(err)
	}
	period, staff := ids.New(), uuid.MustParse(a.staff["xa"])
	form := domain.AppraisalForm{Code: "legacy_281", Name: "Phiếu lịch sử", Group: domain.AppraisalCommune, Criteria: []domain.AppraisalCriterion{{Code: "tc1", Title: "Tiêu chí lịch sử", Maximum: "90"}}, Bands: []domain.AppraisalBand{{Minimum: "81", Rating: "HTXSNV"}, {Minimum: "63", Rating: "HTTNV"}, {Minimum: "45", Rating: "HTNV"}, {Minimum: "0", Rating: "KHTNV"}}}
	appendEvent := func(kind, key, action string, value any, periodRef, staffRef *uuid.UUID) store.AppraisalEvent {
		t.Helper()
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		event, e := store.InsertAppraisal(ctx, tx, f.tid, store.AppraisalEvent{ID: ids.New(), Kind: kind, Key: key, PeriodRef: periodRef, StaffRef: staffRef, Action: action, Payload: raw, RequestHash: strings.Repeat("a", 64), IdemKey: "legacy281:" + ids.New().String(), Actor: f.who["hcth"].ActorRef}, f.svc.Clock.Now())
		if e != nil {
			t.Fatal(e)
		}
		persisted, ok, e := store.AppraisalVersion(ctx, tx, event.ID)
		if e != nil || !ok {
			t.Fatalf("đọc fixture: %v", e)
		}
		return persisted
	}
	formRow := appendEvent("form", form.Code, "publish", form, nil, nil)
	summary := domain.DefaultAppraisalSummary("MS05")
	frozen := app.AppraisalPeriod{ID: period, Year: 2025, Quarter: 3, NodeRef: f.who["hcth"].KN.UnitRef, Label: "Quý III/2025", CutoffOn: "2025-09-30", Forms: map[string]app.AppraisalTemplate{domain.AppraisalCommune: {ID: formRow.ID, Kind: "form", Code: form.Code, Form: &form, Version: 1}}, Summaries: map[string]app.AppraisalTemplate{"MS05": {Summary: &summary}}, SignedDocuments: map[string]uuid.UUID{}, Warnings: []string{}, Counts: map[string]int{}}
	periodRow := appendEvent("period", "2025-q3", "open", frozen, &period, nil)
	var subject uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT subject_ref FROM pf_hrm.staff WHERE tenant_id=$1 AND staff_id=$2 ORDER BY seq DESC LIMIT 1`, f.tid, staff).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	entry := domain.AppraisalEntry{Member: domain.AppraisalMember{StaffID: staff, SubjectRef: subject, Code: "XA-legacy281", Type: domain.StaffCanBoXa, Group: domain.AppraisalCommune, ManagementUnit: "trong_trot", SummaryKind: "MS05"}, Status: domain.AppraisalSelfSubmitted, Self: domain.AppraisalScoring{Answers: []domain.AppraisalAnswer{{Code: "tc1", Points: "82", Note: "Nội dung lịch sử"}}, Total: "82", Suggested: "HTXSNV", Complete: true}, SelfRating: "HTXSNV", Reason: "Căn cứ phiếu lịch sử"}
	entryRow := appendEvent("entry", period.String()+":"+staff.String(), "proxy_self", entry, &period, &staff)
	layout := app.AppraisalScoreLayout{Version: "hrm.appraisal-scores/v1", Title: "TỔNG HỢP ĐIỂM LỊCH SỬ {QUY}/{NAM}", Columns: []domain.AppraisalColumn{{Key: "staff", Title: "Họ và tên"}, {Key: "self_points", Title: "Tổng điểm tự đánh giá"}}, Ratings: summary.Ratings}
	exportID := ids.New()
	exported := app.AppraisalExport{ID: exportID, PeriodID: period, PeriodVersion: periodRow.ID, Kind: app.AppraisalScores, Format: "xlsx", EntryVersions: []uuid.UUID{entryRow.ID}, ScoreLayout: &layout, FileName: "legacy281.xlsx", SHA256: strings.Repeat("a", 64), Count: 1, Status: "ready", Actor: f.who["hcth"].ActorRef, At: f.svc.Clock.Now()}
	appendEvent("export", exportID.String(), "export", exported, &period, nil)
	fingerprint := func() (int, string) {
		t.Helper()
		var n int
		var hash string
		e := tx.QueryRow(ctx, `SELECT count(*),encode(sha256(convert_to(string_agg(to_jsonb(e)::text,E'\n' ORDER BY id,recorded_at),'UTF8')),'hex') FROM pf_hrm.appraisal_event e WHERE tenant_id=$1 AND entity_kind<>'access'`, f.tid).Scan(&n, &hash)
		if e != nil {
			t.Fatal(e)
		}
		return n, hash
	}
	ratingFingerprint := func() (int, string) {
		t.Helper()
		var n int
		var hash string
		e := tx.QueryRow(ctx, `SELECT count(*),encode(sha256(convert_to(string_agg(to_jsonb(r)::text,E'\n' ORDER BY id),'UTF8')),'hex') FROM pf_hrm.annual_rating r WHERE tenant_id=$1`, f.tid).Scan(&n, &hash)
		if e != nil {
			t.Fatal(e)
		}
		return n, hash
	}
	beforeCount, beforeHash := fingerprint()
	var legacyIDs []uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT array_agg(id ORDER BY id) FROM pf_hrm.appraisal_event WHERE tenant_id=$1 AND entity_kind<>'access'`, f.tid).Scan(&legacyIDs); err != nil {
		t.Fatal(err)
	}
	ratingCount, ratingHash := ratingFingerprint()
	if _, err = tx.Exec(ctx, `GRANT INSERT ON pf_hrm.annual_rating TO r_hrm`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SAVEPOINT migration281"); err != nil {
		t.Fatal(err)
	}
	sql, e := migrations.FS.ReadFile("pf/20261107203926_pf_hrm_appraisal_proposal_v4.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	upgradeSQL := strings.Replace(string(sql), "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
	if _, err = tx.Exec(ctx, upgradeSQL); err != nil {
		t.Fatal(err)
	}
	afterCount, afterHash := fingerprint()
	if afterCount != beforeCount || afterHash != beforeHash {
		t.Fatal("migration đổi số dòng/nội dung/chuỗi băm lịch sử")
	}
	t.Logf("legacy count before=%d after=%d sha256 before=%s after=%s", beforeCount, afterCount, beforeHash, afterHash)
	if n, h := ratingFingerprint(); n != ratingCount || h != ratingHash {
		t.Fatal("migration đổi annual_rating lịch sử")
	}
	t.Logf("legacy annual_rating count before=%d after=%d sha256 before=%s after=%s", ratingCount, ratingCount, ratingHash, ratingHash)
	for _, old := range []store.AppraisalEvent{formRow, entryRow} {
		persisted, ok, e := store.AppraisalVersion(ctx, tx, old.ID)
		if e != nil || !ok || !bytes.Equal(persisted.Payload, old.Payload) {
			t.Fatalf("lịch sử không nguyên vẹn: %v", e)
		}
	}
	for _, tc := range []struct {
		kind, action string
		payload      []byte
	}{{"form", "publish", formRow.Payload}, {"entry", "proxy_self", entryRow.Payload}, {"entry", "manager_submit", entryRow.Payload}, {"export", "export", mustAppraisalJSON281(t, exported)}, {"period", "open", periodRow.Payload}} {
		if _, err = tx.Exec(ctx, "SAVEPOINT guard281"); err != nil {
			t.Fatal(err)
		}
		test := store.AppraisalEvent{ID: ids.New(), Kind: tc.kind, Key: ids.New().String(), Action: tc.action, Payload: tc.payload, RequestHash: strings.Repeat("b", 64), IdemKey: ids.New().String(), Actor: f.who["hcth"].ActorRef}
		if tc.kind == "entry" || tc.kind == "period" || tc.kind == "export" {
			test.PeriodRef = &period
		}
		if tc.kind == "entry" {
			test.StaffRef = &staff
		}
		_, err = store.InsertAppraisal(ctx, tx, f.tid, test, f.svc.Clock.Now())
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("writer %s/%s chưa bị thu quyền: %v", tc.kind, tc.action, err)
		}
		if _, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT guard281"); err != nil {
			t.Fatal(err)
		}
	}
	var canWrite bool
	if err = tx.QueryRow(ctx, `SELECT has_table_privilege('r_hrm','pf_hrm.annual_rating','INSERT')`).Scan(&canWrite); err != nil || canWrite {
		t.Fatalf("annual writer còn quyền: %v %v", canWrite, err)
	}
	// Diễn tập rollback migration bằng savepoint; không xóa/cập nhật dữ liệu.
	if _, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT migration281"); err != nil {
		t.Fatal(err)
	}
	if n, h := fingerprint(); n != beforeCount || h != beforeHash {
		t.Fatal("rollback đổi lịch sử đánh giá")
	}
	if n, h := ratingFingerprint(); n != ratingCount || h != ratingHash {
		t.Fatal("rollback đổi lịch sử annual_rating")
	}
	var triggerCount int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='pf_hrm.appraisal_event'::regclass AND tgname='appraisal_proposal_v4_write'`).Scan(&triggerCount); err != nil || triggerCount != 0 {
		t.Fatalf("rollback trigger: %d %v", triggerCount, err)
	}
	if err = tx.QueryRow(ctx, `SELECT has_table_privilege('r_hrm','pf_hrm.annual_rating','INSERT')`).Scan(&canWrite); err != nil || !canWrite {
		t.Fatalf("rollback quyền INSERT: %v %v", canWrite, err)
	}
	if _, err = tx.Exec(ctx, upgradeSQL); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT has_table_privilege('r_hrm','pf_hrm.annual_rating','INSERT')`).Scan(&canWrite); err != nil || canWrite {
		t.Fatal("reapply chưa thu quyền annual writer")
	}
	if n, h := fingerprint(); n != beforeCount || h != beforeHash {
		t.Fatal("reapply đổi lịch sử đánh giá")
	}
	if n, h := ratingFingerprint(); n != ratingCount || h != ratingHash {
		t.Fatal("reapply đổi lịch sử annual_rating")
	}
	t.Logf("migration281 rollback/reapply count=%d sha256=%s annual_count=%d annual_sha256=%s", beforeCount, beforeHash, ratingCount, ratingHash)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	base := "/v1/hrm/appraisals/periods/" + period.String()
	detail := appraisalCall(t, f, "ld_tt", http.MethodGet, base+"/entries/"+staff.String(), "", nil, http.StatusOK)
	assertReadOnly := func(value any) {
		t.Helper()
		v := value.(map[string]any)
		if v["legacy_read_only"] != true || v["can_manage"] != false || v["can_center"] != false {
			t.Fatal("phiên bản lịch sử vẫn có writer cap", v)
		}
	}
	item := detail["item"].(map[string]any)
	assertReadOnly(item)
	for _, history := range detail["history"].([]any) {
		assertReadOnly(history)
	}
	history := appraisalCall(t, f, "hcth", http.MethodGet, base+"/entries/"+staff.String()+"/history", "", nil, http.StatusOK)
	for _, v := range history["items"].([]any) {
		assertReadOnly(v)
	}
	if item["legacy_read_only"] != true || item["can_manage"] != false || item["self"].(map[string]any)["total"] != "82" {
		t.Fatal("reader mất điểm lịch sử hoặc còn ghi")
	}
	appraisalCall(t, f, "ld_tt", http.MethodPost, base+"/entries/"+staff.String(), "legacy281-deny-edit", domain.AppraisalEntryChange{Action: "manager_submit", Version: 1, SelfRating: "HTTNV", CollectiveRating: "HTTNV"}, http.StatusUnprocessableEntity)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "legacy281-deny-period", app.AppraisalPeriodChange{Action: "close", Version: 1, AcknowledgeUnsigned: true}, http.StatusUnprocessableEntity)
	appraisalCall(t, f, "hcth", http.MethodGet, "/v1/hrm/appraisals/templates/form/legacy_281/history", "", nil, http.StatusOK)
	assertReadOnly(appraisalCall(t, f, "hcth", http.MethodGet, base+"/entries/"+staff.String()+"/versions/"+entryRow.ID.String(), "", nil, http.StatusOK))
	path := base + "/exports/" + exportID.String() + "/file"
	one, two := appraisalRequest(f, "hcth", http.MethodGet, path, "", nil), appraisalRequest(f, "hcth", http.MethodGet, path, "", nil)
	if one.Code != http.StatusOK || two.Code != http.StatusOK || !bytes.Equal(one.Body.Bytes(), two.Body.Bytes()) {
		t.Fatalf("không tải lại bảng điểm cũ nguyên vẹn: %d/%d", one.Code, two.Code)
	}
	sum := sha256.Sum256(one.Body.Bytes())
	t.Logf("legacy score export sha256=%s bytes=%d", hex.EncodeToString(sum[:]), one.Body.Len())
	var finalCount int
	var finalHash string
	err = pool.QueryRow(ctx, `SELECT count(*),encode(sha256(convert_to(string_agg(to_jsonb(e)::text,E'\n' ORDER BY id,recorded_at),'UTF8')),'hex') FROM pf_hrm.appraisal_event e WHERE tenant_id=$1 AND entity_kind<>'access'`, f.tid).Scan(&finalCount, &finalHash)
	if err != nil || finalCount != beforeCount || finalHash != beforeHash {
		t.Fatal("đọc/ghi bị chặn đã thay dữ liệu lịch sử")
	}
	// Legacy chỉ đọc không còn là một workflow đang mở để chặn chuyển đổi v4.
	a.input.Year, a.input.CutoffOn, a.input.ManagerDueAt = 2025, "2025-09-30", "2025-10-20T16:59:59Z"
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, key := range []string{"281-cutover-concurrent-a", "281-cutover-concurrent-b"} {
		go func(key string) {
			responses <- appraisalRequest(f, "hcth", http.MethodPost, "/v1/hrm/appraisals/periods", key, a.input)
		}(key)
	}
	created, conflicts := 0, 0
	current := ""
	for range 2 {
		w := <-responses
		switch w.Code {
		case http.StatusCreated:
			created++
			var saved struct {
				ID string `json:"id"`
			}
			if e := json.Unmarshal(w.Body.Bytes(), &saved); e != nil {
				t.Fatal(e)
			}
			current = saved.ID
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("cutover concurrent status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if created != 1 || conflicts != 1 || current == "" {
		t.Fatalf("cutover concurrent created=%d conflict=%d", created, conflicts)
	}
	t.Logf("same-period concurrent cutover created=%d conflict=%d", created, conflicts)
	if current == period.String() {
		t.Fatal("v4 ghi đè đợt cũ")
	}
	err = pool.QueryRow(ctx, `SELECT count(*),encode(sha256(convert_to(string_agg(to_jsonb(e)::text,E'\n' ORDER BY id,recorded_at),'UTF8')),'hex') FROM pf_hrm.appraisal_event e WHERE tenant_id=$1 AND id=ANY($2::uuid[])`, f.tid, legacyIDs).Scan(&finalCount, &finalHash)
	if err != nil || finalCount != beforeCount || finalHash != beforeHash {
		t.Fatal("cutover v4 đổi dòng lịch sử")
	}
	t.Logf("legacy same-period cutover preserved count=%d sha256=%s", finalCount, finalHash)
	appraisalCall(t, f, "hcth", http.MethodPost, "/v1/hrm/appraisals/periods", "281-cutover-v4-duplicate", a.input, http.StatusConflict)
}
func mustAppraisalJSON281(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

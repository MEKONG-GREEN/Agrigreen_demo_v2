package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func appraisalEntryFingerprint(t *testing.T, a appraisalFixture) (int, string) {
	t.Helper()
	var count int
	var hashes string
	err := db.WithModuleTenantTx(context.Background(), a.f.svc.DB, app.DBModule, a.f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*),coalesce(string_agg(encode(row_hash,'hex'),',' ORDER BY id,recorded_at),'') FROM pf_hrm.appraisal_event WHERE entity_kind='entry'`).Scan(&count, &hashes)
	})
	if err != nil {
		t.Fatal(err)
	}
	return count, hashes
}

func TestHRMRemovedSelfHTTPHC0(t *testing.T) {
	a := newAppraisalFixture(t)
	period := a.open(t, "hc0-open-period")
	base := "/v1/hrm/appraisals/periods/" + period
	item := a.change(t, period, "xa", "ld_tt", "hc0-proxy-kept", a.proxyInput(t, "xa", a.answers), http.StatusCreated)
	beforeCount, beforeHash := appraisalEntryFingerprint(t, a)
	for _, who := range []string{"xa", "vc_tt"} {
		// Existing FN-20 overrides must not resurrect a removed actor capability.
		a.f.call("admin", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "user", "grantee": a.f.who[who].ActorRef.String(), "perms": map[string]any{"assessment": "all"}}, http.StatusCreated)
		for _, path := range []string{"/v1/hrm/appraisals/periods", base + "/entries", base + "/entries/" + a.staff[who], base + "/entries/" + a.staff[who] + "/history", base + "/entries/" + a.staff[who] + "/versions/" + item["id"].(string)} {
			appraisalCall(t, a.f, who, http.MethodGet, path, "", nil, http.StatusForbidden)
		}
		for _, action := range []string{"self_draft", "self_submit"} {
			a.change(t, period, who, who, "hc0-deny-"+who+action, domain.AppraisalEntryChange{Action: action, Version: 1, Answers: a.answers}, http.StatusForbidden)
		}
	}
	for _, action := range []string{"self_draft", "self_submit"} {
		a.change(t, period, "vc_tt", "hcth", "hc0-center-deny-"+action, domain.AppraisalEntryChange{Action: action, Version: 1, Answers: a.answers}, http.StatusUnprocessableEntity)
	}
	for _, who := range []string{"admin", "hcth", "xa"} {
		r := appraisalRequest(a.f, who, http.MethodPost, base+"/entries/"+a.staff["xa"]+"/guest-link", "hc0-removed-guest", nil)
		if r.Code != http.StatusNotFound {
			t.Fatalf("%s guest endpoint: %d want404", who, r.Code)
		}
	}
	// r_hrm itself cannot append the retired actions, even with a valid payload.
	for _, action := range []string{"self_draft", "self_submit"} {
		err := db.WithModuleTenantTx(context.Background(), a.f.svc.DB, app.DBModule, a.f.tid, func(tx pgx.Tx) error {
			_, err := store.InsertAppraisal(context.Background(), tx, a.f.tid, store.AppraisalEvent{ID: ids.New(), Kind: "entry", Key: period + ":" + a.staff["xa"], PeriodRef: uuidPointer(uuid.MustParse(period)), StaffRef: uuidPointer(uuid.MustParse(a.staff["xa"])), Action: action, Payload: []byte(`{}`), RequestHash: strings.Repeat("a", 64), IdemKey: "hc0-direct-" + action, Actor: a.f.who["xa"].ActorRef}, a.f.svc.Clock.Now())
			return err
		})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("DB còn nhận %s: %v", action, err)
		}
	}
	count, hashes := appraisalEntryFingerprint(t, a)
	if count != beforeCount || hashes != beforeHash {
		t.Fatal("lệnh bị chặn đã thêm/sửa/mất lịch sử")
	}
	history := appraisalCall(t, a.f, "hcth", http.MethodGet, base+"/entries/"+a.staff["xa"]+"/history", "", nil, http.StatusOK)
	if len(history["items"].([]any)) != 2 {
		t.Fatal("HCTH mất lịch sử")
	}
	for _, field := range []string{"self", "self_rating", "proxy_document"} {
		if a.detail(t, period, "xa")[field] == nil {
			t.Fatalf("mất trường lịch sử %s", field)
		}
	}
	if _, present := a.detail(t, period, "xa")["can_self"]; present {
		t.Fatal("API còn quảng bá capability tự đánh giá")
	}
	a.change(t, period, "xa", "ld_tt", "hc0-manager-kept", domain.AppraisalEntryChange{Action: "manager_submit", Version: 2, Answers: a.answers, CollectiveRating: "HTTNV", Reason: "Biên bản họp tập thể đánh giá"}, http.StatusCreated)
}

func uuidPointer(id uuid.UUID) *uuid.UUID { return &id }

func TestHRMRemovedSelfMigrationPreservesHistoryHC0(t *testing.T) {
	a := newAppraisalFixture(t)
	ctx := context.Background()
	tx, err := a.f.env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Only in this rollback-only, private pgtest database: reconstruct the old
	// insertion rule, put an authentic ledger event in it, then apply the new rule.
	if _, err = tx.Exec(ctx, `DROP TRIGGER appraisal_no_self_write ON pf_hrm.appraisal_event`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, a.f.tid.String()); err != nil {
		t.Fatal(err)
	}
	period, staff := ids.New(), ids.New()
	row := store.AppraisalEvent{ID: ids.New(), Kind: "entry", Key: period.String() + ":" + staff.String(), PeriodRef: &period, StaffRef: &staff, Action: "self_submit", Payload: []byte(`{"self_rating":"HTTNV","self":{"total":"70","complete":true}}`), RequestHash: strings.Repeat("a", 64), IdemKey: "hc0-legacy-event", Actor: a.f.who["xa"].ActorRef}
	if row, err = store.InsertAppraisal(ctx, tx, a.f.tid, row, a.f.svc.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	fingerprint := func() (int, string) {
		t.Helper()
		var n int
		var hash string
		if err := tx.QueryRow(ctx, `SELECT count(*),string_agg(encode(row_hash,'hex'),',' ORDER BY seq) FROM pf_hrm.appraisal_event WHERE tenant_id=$1 AND entity_key=$2`, a.f.tid, row.Key).Scan(&n, &hash); err != nil {
			t.Fatal(err)
		}
		return n, hash
	}
	beforeCount, beforeHash := fingerprint()
	sql, err := migrations.FS.ReadFile("pf/20261106155212_pf_hrm_remove_self_appraisal.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	trigger := string(sql)[strings.Index(string(sql), "CREATE TRIGGER appraisal_no_self_write"):]
	if _, err = tx.Exec(ctx, trigger); err != nil {
		t.Fatal(err)
	}
	afterCount, afterHash := fingerprint()
	if beforeCount != afterCount || beforeHash != afterHash || afterCount != 1 {
		t.Fatal("migration làm đổi bản ghi/chuỗi băm cũ")
	}
	old, ok, err := store.AppraisalLatest(ctx, tx, "entry", row.Key)
	var persisted domain.AppraisalEntry
	decodeErr := json.Unmarshal(old.Payload, &persisted)
	if err != nil || !ok || decodeErr != nil || persisted.Self.Total != "70" || persisted.SelfRating != "HTTNV" {
		t.Fatalf("không đọc lại nguyên dữ liệu cũ: %s %v", old.Payload, err)
	}
	for _, action := range []string{"self_draft", "self_submit"} {
		if _, err = tx.Exec(ctx, "SAVEPOINT hc0_write"); err != nil {
			t.Fatal(err)
		}
		next := row
		next.ID = ids.New()
		next.Action = action
		next.IdemKey = "hc0-rejected-" + action
		_, err = store.InsertAppraisal(ctx, tx, a.f.tid, next, a.f.svc.Clock.Now())
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("migration chưa chặn %s: %v", action, err)
		}
		if _, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT hc0_write"); err != nil {
			t.Fatal(err)
		}
	}
	n, hash := fingerprint()
	if n != afterCount || hash != afterHash {
		t.Fatal("ghi bị chặn đã đổi lịch sử")
	}
}

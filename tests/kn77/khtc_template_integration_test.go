package kn77_test

// W11.65 native authoring + sealed department forms on private PostgreSQL.
// The existing report fixture has a documented source port golden; no result from
// that port is claimed as a native module publication. Form submit/seal/status,
// template ledgers, tenant RLS, role guards and idempotency here are real.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	templateapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/app"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	templatestore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const templatePath = "/v1/extension/khtc/templates/"

func templateCtx(t *testing.T, tid, actor uuid.UUID, role string, viewOnly bool) context.Context {
	t.Helper()
	k := authz.KNContext{Role: role, Department: khtcDept, OrgRef: ids.New(), Scope: authz.Scope{All: true}, ViewOnly: viewOnly}
	p := bindKNTestSession(t, authz.Principal{ActorRef: actor, TenantID: tid, KN: &k})
	return tenant.WithID(authz.WithPrincipal(context.Background(), p), tid)
}
func templateSave(t *testing.T, f *reportFx, key string, base int64, d templatedomain.Definition) templatestore.Version {
	t.Helper()
	var v templatestore.Version
	c, b := f.do(http.MethodPost, templatePath+key+"/versions", authz.KNAdmin, "", f.khtcUser, "", templateapp.SaveInput{BaseVersion: base, Definition: d})
	f.ok(c, b, &v)
	return v
}
func templatePublish(t *testing.T, f *reportFx, key string, v templatestore.Version, from string) templatestore.Publication {
	t.Helper()
	var p templatestore.Publication
	c, b := f.do(http.MethodPost, templatePath+key+"/publish", authz.KNAdmin, "", f.khtcUser, "", templateapp.PublishInput{BaseVersion: v.Version, VersionID: v.ID, AppliesFrom: from})
	f.ok(c, b, &p)
	return p
}

func TestW1165_PublishPeriodStartHashAndRevert(t *testing.T) {
	f := newReportFx(t)
	ctx := templateCtx(t, f.tid, f.khtcUser, authz.KNTinh, false)
	provider := f.ext.KHTCFixedTemplates()
	if _, err := provider.Resolve(ctx, "mau_1", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)); !errors.Is(err, templateapp.ErrUnpublished) {
		t.Fatalf("unpublished: %v", err)
	}
	seed := templatedomain.Seed("mau_1")
	v1 := templateSave(t, f, "mau_1", 0, seed)
	templatePublish(t, f, "mau_1", v1, "2026-10-07")
	if _, err := provider.Resolve(ctx, "mau_1", time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)); !errors.Is(err, templateapp.ErrUnpublished) {
		t.Fatalf("selected by cutoff rather than start: %v", err)
	}
	first, err := provider.Resolve(ctx, "mau_1", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != v1.ID || first.Hash != v1.Hash || first.Version != 1 {
		t.Fatalf("wrong pin: %+v", first)
	}
	changed := templatedomain.Canonical(seed)
	changed.Nodes = append(changed.Nodes, templatedomain.Node{ID: "new_child", Parent: "B", Order: 10, Title: "Đánh giá bổ sung", Repeat: "none", Body: "NỘI DUNG MỚI"})
	v2 := templateSave(t, f, "mau_1", 1, changed)
	templatePublish(t, f, "mau_1", v2, "2026-10-14")
	old, err := provider.Resolve(ctx, "mau_1", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil || old.ID != v1.ID {
		t.Fatalf("future publication displaced old period: %v %+v", err, old)
	}
	next, err := provider.Resolve(ctx, "mau_1", time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	if err != nil || next.ID != v2.ID {
		t.Fatalf("new period: %v %+v", err, next)
	}
	var restored templatestore.Version
	c, b := f.do(http.MethodPost, templatePath+"mau_1/restore", authz.KNAdmin, "", f.khtcUser, "", templateapp.RestoreInput{BaseVersion: 2, VersionID: v1.ID})
	f.ok(c, b, &restored)
	if restored.Version != 3 || restored.ID == v1.ID || restored.Hash != v1.Hash || restored.Action != "restore" {
		t.Fatalf("restore overwrote old definition: %+v", restored)
	}
	templatePublish(t, f, "mau_1", restored, "2026-10-21")
	last, err := provider.Resolve(ctx, "mau_1", time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
	if err != nil || last.ID != restored.ID {
		t.Fatalf("revert: %v %+v", err, last)
	}
	kept, err := provider.ByID(ctx, v1.ID)
	if err != nil || string(kept.Definition) != string(first.Definition) {
		t.Fatalf("historical definition changed: %v", err)
	}
	foreign := templateCtx(t, ids.New(), ids.New(), authz.KNAdmin, false)
	if _, err = provider.ByID(foreign, v1.ID); !errors.Is(err, templateapp.ErrNotFound) {
		t.Fatalf("foreign tenant read: %v", err)
	}
	for _, role := range []string{authz.KNTinh, authz.KNXa, authz.KNTW} {
		c, b = f.do(http.MethodGet, templatePath+"mau_1", role, khtcDept, f.khtcUser, "", nil)
		if c != http.StatusForbidden {
			t.Fatalf("non-admin %s: %d %s", role, c, b)
		}
	}
}

func templatePreview(t *testing.T, f *reportFx, pid string) templateapp.Preview {
	t.Helper()
	var p templateapp.Preview
	definition := templatedomain.Seed("mau_1")
	c, b := f.do(http.MethodPost, templatePath+"mau_1/preview", authz.KNAdmin, "", f.khtcUser, "", templateapp.PreviewInput{PeriodID: uuid.MustParse(pid), Definition: &definition, Scenario: "actual"})
	f.ok(c, b, &p)
	return p
}
func previewRow(t *testing.T, p templateapp.Preview, ref string) templatedomain.RenderedRow {
	t.Helper()
	for _, n := range p.Document.Nodes {
		if n.ID == "A.II.2" {
			for _, r := range n.Rows {
				if r.Ref == ref {
					return r
				}
			}
		}
	}
	t.Fatal("report item missing from preview", ref)
	return templatedomain.RenderedRow{}
}
func TestW1165_PreviewSealedFormStatusAndValues(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	before := templatePreview(t, f, p.ID)
	row := previewRow(t, before, f.item)
	if row.Status != "no_send" || !strings.Contains(row.Tokens[0].Text, "chưa gửi số liệu") {
		t.Fatalf("missing form treated as zero: %+v", row)
	}
	c, b := f.send(p.ID, dTT, "draft", 0, ttSheet(f.item, "999"))
	f.ok(c, b, nil)
	unsent := templatePreview(t, f, p.ID)
	if got := previewRow(t, unsent, f.item); got.Status != "no_send" {
		t.Fatalf("draft leaked into preview: %+v", got)
	}
	c, b = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(c, b, nil)
	after := templatePreview(t, f, p.ID)
	actual := previewRow(t, after, f.item)
	found := false
	for _, token := range actual.Tokens {
		if token.Variable == "so_trong_ky" {
			found = true
			if token.Text != "10" || !token.Locked || token.Source != "form" {
				t.Fatalf("source token: %+v", token)
			}
		}
	}
	if !found || after.Context.InputHash == before.Context.InputHash || !strings.Contains(actual.Preamble, "CT KN trồng trọt") {
		t.Fatalf("preview did not use sealed form/plan: %+v", actual)
	}
	p2 := f.open(42, dTT, dTTTT)
	c, b = f.send(p2.ID, dTT, "submit", 0, ttSheet(f.item, "0"))
	f.ok(c, b, nil)
	zero := previewRow(t, templatePreview(t, f, p2.ID), f.item)
	if zero.Status != "no_activity" {
		t.Fatalf("submitted zero classification: %+v", zero)
	}
}

func TestW1165_ConcurrencyIdempotencyReadOnlyAndNativeSession(t *testing.T) {
	f := newReportFx(t)
	env := pgtest.Get(t)
	config := env.Pool(t, migrations.PF, dbadmin.AppPlatform).Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s := &templateapp.Service{DB: db.ForModule(pool, extension.DBModule), Clock: f.clk}
	ctx := templateCtx(t, f.tid, f.khtcUser, authz.KNAdmin, false)
	in := templateapp.SaveInput{Definition: templatedomain.Seed("mau_1")}
	key := "save-" + ids.New().String()
	first, err := s.Save(ctx, "mau_1", in, key)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.Save(ctx, "mau_1", in, key)
	if err != nil || replay.ID != first.ID {
		t.Fatalf("lost response retry created another version: %v", err)
	}
	changed := in
	changed.Definition = templatedomain.Canonical(in.Definition)
	changed.Definition.Nodes[0].Body = "Đổi nội dung"
	if _, err = s.Save(ctx, "mau_1", changed, key); !errors.Is(err, problem.IdempotencyConflict) {
		t.Fatalf("same key changed payload: %v", err)
	}
	results := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Save(ctx, "mau_1", templateapp.SaveInput{BaseVersion: 1, Definition: templatedomain.Seed("mau_1")}, ids.New().String())
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	passed, conflict := 0, 0
	for e := range results {
		if e == nil {
			passed++
		} else if errors.Is(e, templateapp.ErrVersion) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if passed != 1 || conflict != 3 {
		t.Fatalf("concurrent writes: pass=%d conflict=%d", passed, conflict)
	}
	readonly := templateCtx(t, f.tid, f.khtcUser, authz.KNAdmin, true)
	var denied *problem.Problem
	if _, err = s.Save(readonly, "mau_1", in, ids.New().String()); !errors.As(err, &denied) || denied.Status != http.StatusForbidden || denied.Code != "forbidden" {
		t.Fatalf("read-only write: %v", err)
	}
	native := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: f.tid, ActorRef: f.khtcUser, Roles: []string{authz.RolePlatformAdmin}, KN: &authz.KNContext{Role: authz.KNAdmin}})
	if _, err = s.Save(native, "mau_1", in, ids.New().String()); err == nil {
		t.Fatal("native/legacy context bypassed the KN owner projection")
	}
}

func TestW1165_NarrativeWarningAndHostileHTTP(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	c, b := f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "texts": map[string]string{"B": "Lời văn lịch sử cần giữ"}})
	f.ok(c, b, nil)
	definition := templatedomain.Seed("mau_1")
	templateSave(t, f, "mau_1", 0, definition)
	for i := range definition.Nodes {
		if definition.Nodes[i].ID == "B" {
			definition.Nodes[i].TextSlot = false
			definition.Nodes[i].Body = "Nội dung mới"
			definition.Nodes[i].Bindings = nil
		}
	}
	in := templateapp.SaveInput{BaseVersion: 1, Definition: definition}
	c, b = f.do(http.MethodPost, templatePath+"mau_1/versions", authz.KNAdmin, "", f.khtcUser, "", in)
	if c != http.StatusConflict || !strings.Contains(string(b), "khtc_template_text_in_use") {
		t.Fatalf("lost narrative warning: %d %s", c, b)
	}
	in.AcknowledgeTextLoss = true
	c, b = f.do(http.MethodPost, templatePath+"mau_1/versions", authz.KNAdmin, "", f.khtcUser, "", in)
	f.ok(c, b, nil)
	if f.read(p.ID).Content.Texts["B"] != "Lời văn lịch sử cần giữ" {
		t.Fatal("authoring changed a historical draft")
	}
	for _, number := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
		raw := json.RawMessage(`{"base_version":` + number + `,"definition":{"key":"mau_1","nodes":[]}}`)
		c, b = f.do(http.MethodPost, templatePath+"mau_1/versions", authz.KNAdmin, "", f.khtcUser, "", raw)
		if c != http.StatusBadRequest {
			t.Fatalf("hostile integer: %d %s", c, b)
		}
	}
}

func TestW1165_RolloutIsPerKeyAndPeriodStart(t *testing.T) {
	f := newReportFx(t)
	provider := f.ext.KHTCFixedTemplates()
	ctx := templateCtx(t, f.tid, f.khtcUser, authz.KNTinh, false)
	before := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	effective := before.AddDate(0, 0, 7)
	legacy, err := provider.Select(ctx, "mau_1", before)
	if err != nil || legacy.Mode != "legacy" || legacy.WordVersion != 1 {
		t.Fatalf("explicit legacy source: %v %+v", err, legacy)
	}
	if err = templatedomain.ValidateLegacy(legacy); err != nil {
		t.Fatal(err)
	}
	original := string(legacy.Definition)
	legacy.Definition[0] = '!'
	again, err := provider.Select(ctx, "mau_1", before)
	if err != nil || string(again.Definition) != original {
		t.Fatal("returned legacy definition mutated the registry", err)
	}
	v := templateSave(t, f, "mau_1", 0, templatedomain.Seed("mau_1"))
	templatePublish(t, f, "mau_1", v, effective.Format(time.DateOnly))
	old, err := provider.Select(ctx, "mau_1", before)
	if err != nil || old.Mode != "legacy" || old.LegacyUntil == nil || !old.LegacyUntil.Equal(effective) || string(old.Definition) != original {
		t.Fatalf("future publication displaced a legacy period: %v %+v", err, old)
	}
	for _, key := range []string{"mau_2", "mau_3", "mau_4", "mau_5"} {
		pinned, e := provider.Select(ctx, key, effective)
		if e != nil || pinned.Mode != "legacy" || pinned.LegacyUntil != nil {
			t.Fatalf("M1 activated %s: %v %+v", key, e, pinned)
		}
	}
	active, err := provider.Select(ctx, "mau_1", effective)
	if err != nil || active.Mode != "published" || active.ID != v.ID || active.Hash != v.Hash || active.Rule != v.Rule {
		t.Fatalf("effective snapshot: %v %+v", err, active)
	}
}

func TestW1165_DatabaseRejectsMismatchedPublishedVersion(t *testing.T) {
	f := newReportFx(t)
	v := templateSave(t, f, "mau_1", 0, templatedomain.Seed("mau_1"))
	ctx := templateCtx(t, f.tid, f.khtcUser, authz.KNAdmin, false)
	p := templatestore.Publication{ID: ids.New(), Key: "mau_1", VersionID: v.ID, Version: v.Version + 1, Hash: v.Hash, RequestHash: strings.Repeat("0", 64), AppliesFrom: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Actor: f.khtcUser, At: f.clk.Now()}
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	err := tenant.WithTenantTx(ctx, db.ForModule(pool, extension.DBModule), f.tid, func(tx pgx.Tx) error {
		return templatestore.Publish(ctx, tx, f.tid, &p, ids.New().String(), templatestore.Support{})
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.ConstraintName != "khtc_template_publication_version" {
		t.Fatalf("publication version mismatch accepted: %v", err)
	}
}

func TestW1165_PreviewBypassesPublishedSelectionWithoutPersisting(t *testing.T) {
	f := newReportFx(t)
	period := f.open(41, dTT)
	context := templateCtx(t, f.tid, f.khtcUser, authz.KNAdmin, false)
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	countDrafts := func() int {
		var n int
		err := tenant.WithTenantTx(context, db.ForModule(pool, extension.DBModule), f.tid, func(tx pgx.Tx) error {
			return tx.QueryRow(context, `SELECT count(*) FROM pf_extension.khtc_report_draft WHERE payload->'data'->'period'->>'id'=$1`, period.ID).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	// A structurally valid definition can refer to an unavailable source. The
	// editor must still preview a repaired explicit definition using native inputs.
	broken := templatedomain.Seed("mau_1")
	broken.Nodes = append(broken.Nodes, templatedomain.Node{ID: "unavailable", Parent: "B", Order: 100, Title: "Nguồn chưa có", Repeat: "none", Body: "{value}", Bindings: []templatedomain.Binding{{Variable: "value", Source: "form", Field: "value", Ref: "missing-source"}}})
	version := templateSave(t, f, "mau_1", 0, broken)
	templatePublish(t, f, "mau_1", version, "2026-10-07")
	preview := templatePreview(t, f, period.ID)
	if preview.Context.PeriodID.String() != period.ID || preview.Context.InputHash == "" {
		t.Fatal("native capture missing")
	}
	if got := countDrafts(); got != 0 {
		t.Fatalf("preview persisted %d drafts", got)
	}
}

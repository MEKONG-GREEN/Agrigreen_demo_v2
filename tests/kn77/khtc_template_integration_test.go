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
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
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
)

const templatePath = "/v1/extension/khtc/templates/"

// DTOs describe the HTTP contract only. Definitions are read from the real
// Admin endpoint; assembly tests do not import the implementation packages.
type templateBindingDTO struct {
	Variable string `json:"variable"`
	Source   string `json:"source"`
	Field    string `json:"field"`
	Ref      string `json:"ref,omitempty"`
}
type templateNodeDTO struct {
	ID          string               `json:"id"`
	Parent      string               `json:"parent"`
	Order       int                  `json:"order"`
	Title       string               `json:"title"`
	Repeat      string               `json:"repeat"`
	Tab         string               `json:"tab,omitempty"`
	Department  string               `json:"department,omitempty"`
	Preamble    string               `json:"preamble,omitempty"`
	Body        string               `json:"body"`
	Bindings    []templateBindingDTO `json:"bindings"`
	TextSlot    bool                 `json:"text_slot"`
	LockedAdmin []string             `json:"locked_admin"`
}
type templateDefinitionDTO struct {
	Key   string            `json:"key"`
	Nodes []templateNodeDTO `json:"nodes"`
}
type templateVersionDTO struct {
	ID      uuid.UUID `json:"id"`
	Version int64     `json:"version"`
	Hash    string    `json:"hash"`
	Rule    string    `json:"rule"`
	Action  string    `json:"action"`
}
type templatePublicationDTO struct {
	ID uuid.UUID `json:"id"`
}
type templateSaveDTO struct {
	BaseVersion         int64                 `json:"base_version"`
	Definition          templateDefinitionDTO `json:"definition"`
	AcknowledgeTextLoss bool                  `json:"acknowledge_text_loss"`
}
type templateTokenDTO struct {
	Text     string `json:"text"`
	Variable string `json:"variable"`
	Source   string `json:"source"`
	Locked   bool   `json:"locked"`
}
type templateRenderedRowDTO struct {
	Ref      string             `json:"ref"`
	Preamble string             `json:"preamble"`
	Status   string             `json:"status"`
	Tokens   []templateTokenDTO `json:"tokens"`
}
type templatePreviewDTO struct {
	Context  port.KHTCTemplateContext `json:"context"`
	Document struct {
		Nodes []struct {
			ID   string                   `json:"id"`
			Rows []templateRenderedRowDTO `json:"rows"`
		} `json:"nodes"`
	} `json:"document"`
}

func templateDefinition(t *testing.T, f *reportFx, key string) templateDefinitionDTO {
	t.Helper()
	var detail struct {
		Definition templateDefinitionDTO `json:"definition"`
	}
	code, body := f.do(http.MethodGet, templatePath+key, authz.KNAdmin, "", f.khtcUser, "", nil)
	f.ok(code, body, &detail)
	if detail.Definition.Key != key || len(detail.Definition.Nodes) == 0 {
		t.Fatalf("missing HTTP definition: %s", body)
	}
	return detail.Definition
}
func cloneTemplateDefinition(t *testing.T, d templateDefinitionDTO) templateDefinitionDTO {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var out templateDefinitionDTO
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func templateProblemCode(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

func templateCtx(t *testing.T, tid, actor uuid.UUID, role string, viewOnly bool) context.Context {
	t.Helper()
	k := authz.KNContext{Role: role, Department: khtcDept, OrgRef: ids.New(), Scope: authz.Scope{All: true}, ViewOnly: viewOnly}
	p := bindKNTestSession(t, authz.Principal{ActorRef: actor, TenantID: tid, KN: &k})
	return tenant.WithID(authz.WithPrincipal(context.Background(), p), tid)
}
func templateSave(t *testing.T, f *reportFx, key string, base int64, d templateDefinitionDTO) templateVersionDTO {
	t.Helper()
	var v templateVersionDTO
	c, b := f.do(http.MethodPost, templatePath+key+"/versions", authz.KNAdmin, "", f.khtcUser, "", templateSaveDTO{BaseVersion: base, Definition: d})
	f.ok(c, b, &v)
	return v
}
func templatePublish(t *testing.T, f *reportFx, key string, v templateVersionDTO, from string) templatePublicationDTO {
	t.Helper()
	var p templatePublicationDTO
	c, b := f.do(http.MethodPost, templatePath+key+"/publish", authz.KNAdmin, "", f.khtcUser, "", map[string]any{"base_version": v.Version, "version_id": v.ID, "applies_from": from})
	f.ok(c, b, &p)
	return p
}

func TestW1165_PublishPeriodStartHashAndRevert(t *testing.T) {
	f := newReportFx(t)
	ctx := templateCtx(t, f.tid, f.khtcUser, authz.KNTinh, false)
	provider := f.ext.KHTCFixedTemplates()
	if _, err := provider.Resolve(ctx, "mau_1", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)); templateProblemCode(err) != "extension.khtc_template_unpublished" {
		t.Fatalf("unpublished: %v", err)
	}
	seed := templateDefinition(t, f, "mau_1")
	v1 := templateSave(t, f, "mau_1", 0, seed)
	templatePublish(t, f, "mau_1", v1, "2026-10-07")
	if _, err := provider.Resolve(ctx, "mau_1", time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)); templateProblemCode(err) != "extension.khtc_template_unpublished" {
		t.Fatalf("selected by cutoff rather than start: %v", err)
	}
	first, err := provider.Resolve(ctx, "mau_1", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != v1.ID || first.Hash != v1.Hash || first.Version != 1 {
		t.Fatalf("wrong pin: %+v", first)
	}
	changed := cloneTemplateDefinition(t, seed)
	changed.Nodes = append(changed.Nodes, templateNodeDTO{ID: "new_child", Parent: "B", Order: 10, Title: "Đánh giá bổ sung", Repeat: "none", Body: "NỘI DUNG MỚI"})
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
	var restored templateVersionDTO
	c, b := f.do(http.MethodPost, templatePath+"mau_1/restore", authz.KNAdmin, "", f.khtcUser, "", map[string]any{"base_version": 2, "version_id": v1.ID})
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
	if _, err = provider.ByID(foreign, v1.ID); templateProblemCode(err) != "extension.khtc_template_not_found" {
		t.Fatalf("foreign tenant read: %v", err)
	}
	for _, role := range []string{authz.KNTinh, authz.KNXa, authz.KNTW} {
		c, b = f.do(http.MethodGet, templatePath+"mau_1", role, khtcDept, f.khtcUser, "", nil)
		if c != http.StatusForbidden {
			t.Fatalf("non-admin %s: %d %s", role, c, b)
		}
	}
}

func templatePreview(t *testing.T, f *reportFx, pid string, explicit ...templateDefinitionDTO) templatePreviewDTO {
	t.Helper()
	var p templatePreviewDTO
	definition := templateDefinition(t, f, "mau_1")
	if len(explicit) > 0 {
		definition = explicit[0]
	}
	c, b := f.do(http.MethodPost, templatePath+"mau_1/preview", authz.KNAdmin, "", f.khtcUser, "", map[string]any{"period_id": uuid.MustParse(pid), "definition": definition, "scenario": "actual"})
	f.ok(c, b, &p)
	return p
}
func previewRow(t *testing.T, p templatePreviewDTO, ref string) templateRenderedRowDTO {
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
	return templateRenderedRowDTO{}
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

func TestW1165_NarrativeWarningAndHostileHTTP(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	d := f.refresh(p.ID, 0)
	c, b := f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "texts": map[string]string{"B": "Lời văn lịch sử cần giữ"}})
	f.ok(c, b, nil)
	definition := templateDefinition(t, f, "mau_1")
	templateSave(t, f, "mau_1", 0, definition)
	for i := range definition.Nodes {
		if definition.Nodes[i].ID == "B" {
			definition.Nodes[i].TextSlot = false
			definition.Nodes[i].Body = "Nội dung mới"
			definition.Nodes[i].Bindings = nil
		}
	}
	in := templateSaveDTO{BaseVersion: 1, Definition: definition}
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
	if !json.Valid(legacy.Definition) || len(legacy.Hash) != 64 || legacy.WordKey == "" || len(legacy.WordHash) != 64 || legacy.Rule == "" {
		t.Fatalf("legacy snapshot lacks immutable descriptor/fingerprints: %+v", legacy)
	}
	original := string(legacy.Definition)
	legacy.Definition[0] = '!'
	again, err := provider.Select(ctx, "mau_1", before)
	if err != nil || string(again.Definition) != original {
		t.Fatal("returned legacy definition mutated the registry", err)
	}
	v := templateSave(t, f, "mau_1", 0, templateDefinition(t, f, "mau_1"))
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
	repaired := templateDefinition(t, f, "mau_1")
	broken := cloneTemplateDefinition(t, repaired)
	broken.Nodes = append(broken.Nodes, templateNodeDTO{ID: "unavailable", Parent: "B", Order: 100, Title: "Nguồn chưa có", Repeat: "none", Body: "{value}", Bindings: []templateBindingDTO{{Variable: "value", Source: "form", Field: "value", Ref: "missing-source"}}})
	version := templateSave(t, f, "mau_1", 0, broken)
	templatePublish(t, f, "mau_1", version, "2026-10-07")
	preview := templatePreview(t, f, period.ID, repaired)
	if preview.Context.PeriodID.String() != period.ID || preview.Context.InputHash == "" {
		t.Fatal("native capture missing")
	}
	if got := countDrafts(); got != 0 {
		t.Fatalf("preview persisted %d drafts", got)
	}
}

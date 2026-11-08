package kn77_test

// #275 native boundary evidence. The fixture binds the real KN owner registry;
// it replaces account middleware and does not prove a deployed OIDC session.
import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
)

func cn275Call(f *aggFx, path, key string, actor uuid.UUID, k authz.KNContext, body any) (int, []byte) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(idempotency.Header, key)
	req.Header.Set("X-Test-Actor", actor.String())
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func cn275Form(t *testing.T, f *aggFx) designEditView {
	t.Helper()
	c, b := f.call(http.MethodGet, "/v1/extension/cnts/form-designs", authz.KNTinh, nil)
	var list struct {
		Items []designEditView `json:"items"`
	}
	f.ok(c, b, &list)
	for _, v := range list.Items {
		if v.Code == "CNTS-BM17" {
			return v
		}
	}
	t.Fatal("missing native template")
	return designEditView{}
}

func TestCNTS275SelectedVersionRuntime(t *testing.T) {
	config := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform).Config().Copy()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := newAggFxPool(t, pool, facilityDir{})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	original := cn275Form(t, f)
	fields := append(append([]designEditField{}, original.Fields...), designEditField{Key: "cua", Label: "Cua", Type: "decimal", Required: true, NonNegative: true})
	c, b = designAs(f, http.MethodPost, "/v1/extension/cnts/form-designs/CNTS-BM17/versions", authz.KNTinh, "tong_hop", map[string]any{"base_version": 1, "title": "BM17 v2", "fields": fields})
	var saved designSaved
	f.ok(c, b, &saved)
	in := map[string]any{"code": "CNTS-BM17", "template_id": *original.TemplateID, "template_version": int64(1), "from_date": "2026-09-29", "to_date": "2026-10-05", "units": []string{"X1"}}
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/period-series", authz.KNTinh, in)
	var published series
	f.ok(c, b, &published)
	c, b = f.call(http.MethodGet, "/v1/forms/periods/"+published.Periods[0].ID+"?profile=kn_cnts", authz.KNTinh, nil)
	var view formsPeriodView
	f.ok(c, b, &view)
	if view.Period.TemplateID != *original.TemplateID || view.Period.TemplateVersion != 1 {
		t.Fatalf("explicit selection ignored: want immutable v1=%s, got %s", *original.TemplateID, b)
	}
	f.submit(published.Periods[0].ID, "X1", catchVals("4", "0", "1", "0"))
	f.closePeriod(published.Periods[0].ID)
	frozen := f.admin.query("SELECT encode(row_hash,'hex') FROM pf_forms.close_snapshot WHERE tenant_id=$1 AND period_ref=$2::uuid", f.tid, published.Periods[0].ID)
	if len(frozen) != 1 {
		t.Fatal("old native snapshot missing")
	}

	in["template_id"], in["template_version"] = saved.Version.TemplateID, int64(2)
	in["from_date"], in["to_date"] = "2026-10-06", "2026-10-12"
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/period-series", authz.KNTinh, in)
	var next series
	f.ok(c, b, &next)
	c, b = f.call(http.MethodPost, "/v1/forms/periods/"+next.Periods[0].ID+"/submissions?profile=kn_cnts", authz.KNXa, map[string]any{"unit": "X1", "values": catchVals("4", "0", "1", "0")})
	if c < 400 {
		t.Fatalf("selected v2 required field must be enforced by native runtime: %d %s", c, b)
	}
	values := catchVals("4", "0", "1", "0")
	values["cua"] = "2.5"
	for _, bad := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
		hostile := map[string]string{}
		for k, v := range values {
			hostile[k] = v
		}
		hostile["cua"] = bad
		c, b = f.call(http.MethodPost, "/v1/forms/periods/"+next.Periods[0].ID+"/submissions?profile=kn_cnts", authz.KNXa, map[string]any{"unit": "X1", "values": hostile, "explanation": "Giải trình gửi bổ sung"})
		if c < 400 {
			t.Fatalf("hostile decimal accepted: %d", c)
		}
	}
	f.submit(next.Periods[0].ID, "X1", values)
	c, b = f.call(http.MethodGet, "/v1/forms/periods/"+published.Periods[0].ID+"?profile=kn_cnts", authz.KNTinh, nil)
	f.ok(c, b, &view)
	if view.Period.TemplateVersion != 1 || len(view.Units) != 1 || view.Units[0].Submission == nil || view.Units[0].Submission.Values["ca"] != "4" {
		t.Fatalf("old submission changed: %s", b)
	}
	afterFrozen := f.admin.query("SELECT encode(row_hash,'hex') FROM pf_forms.close_snapshot WHERE tenant_id=$1 AND period_ref=$2::uuid", f.tid, published.Periods[0].ID)
	if len(afterFrozen) != 1 || afterFrozen[0][0] != frozen[0][0] {
		t.Fatal("frozen old schema/value hash changed")
	}
	if _, ok := view.Units[0].Submission.Values["cua"]; ok {
		t.Fatalf("old schema upgraded: %s", b)
	}
}

func TestCNTS275DefinitionDurableReplay(t *testing.T) {
	f := newAggFx(t, facilityDir{})
	actor := ids.New()
	k := authz.KNContext{ID: ids.New(), Role: authz.KNTinh, Department: aggDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}, ProvinceGroup: "tong_hop"}
	body := map[string]any{"code_suffix": "REPLAY275", "title": "Biểu điều tra", "fields": []designEditField{{Key: "so_con", Label: "Số con", Type: "integer", Required: true}}}
	const path = "/v1/extension/cnts/form-designs"
	c, b := cn275Call(f, path, "create-275-original", actor, k, body)
	var created designSaved
	f.ok(c, b, &created)
	// Evict only this test's HTTP cache to exercise the durable public owner path.
	if _, err := f.adminExec("DELETE FROM shared.idempotency_key WHERE tenant_id='" + f.tid.String() + "'"); err != nil {
		t.Fatal(err)
	}
	fields := append(append([]designEditField{}, created.Version.Fields...), designEditField{Key: "ghi_chu", Label: "Ghi chú", Type: "text"})
	c, b = cn275Call(f, path+"/"+created.Code+"/versions", "save-275-second", actor, k, map[string]any{"base_version": 1, "title": "Biểu v2", "fields": fields})
	f.ok(c, b, nil)
	c, b = cn275Call(f, path, "create-275-original", actor, k, body)
	var replay designSaved
	f.ok(c, b, &replay)
	if replay.Version.TemplateID != created.Version.TemplateID || replay.Version.Version != 1 {
		t.Fatalf("retry must return original definition receipt: %s", b)
	}
}

func cn275Context(f *aggFx) authz.KNContext {
	return authz.KNContext{ID: ids.New(), Role: authz.KNTinh, Department: aggDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}, ProvinceGroup: "tong_hop"}
}

func TestCNTS275SelectedTupleGuard(t *testing.T) {
	f := newAggFx(t, facilityDir{})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	selected := cn275Form(t, f)
	actor, k := ids.New(), cn275Context(f)
	for name, tuple := range map[string]map[string]any{
		"missing": {}, "missing version": {"template_id": *selected.TemplateID},
		"identity mismatch": {"template_id": ids.New().String(), "template_version": 1},
		"future version":    {"template_id": *selected.TemplateID, "template_version": 99},
	} {
		t.Run(name, func(t *testing.T) {
			in := map[string]any{"code": "CNTS-BM17", "from_date": "2026-10-06", "to_date": "2026-10-12", "units": []string{"X1"}}
			for key, val := range tuple {
				in[key] = val
			}
			c, b := cn275Call(f, "/v1/extension/cnts/period-series", "guard275-"+ids.New().String(), actor, k, in)
			if c < 400 {
				t.Fatalf("selection must be rejected before any intent/native write: %d %s", c, b)
			}
		})
	}
	for _, table := range []string{"pf_extension.cnts_publish_intent", "pf_extension.cnts_publication", "pf_forms.period"} {
		if n := len(f.admin.query("SELECT id FROM "+table+" WHERE tenant_id=$1", f.tid)); n != 0 {
			t.Fatalf("rejected selection wrote %s: %d", table, n)
		}
	}
}

type cn275LostPublisher struct {
	formsport.Publisher
	catalog formsport.TemplateCatalog
	durable formsport.DurablePublisher
	fail    bool
	calls   int
}

func (p *cn275LostPublisher) ListTemplates(ctx context.Context, profile string) ([]formsport.TemplateVersion, error) {
	return p.catalog.ListTemplates(ctx, profile)
}
func (p *cn275LostPublisher) TemplateHistory(ctx context.Context, profile, code string, before int64, limit int) ([]formsport.TemplateVersion, error) {
	return p.catalog.TemplateHistory(ctx, profile, code, before, limit)
}
func (p *cn275LostPublisher) TemplateVersion(ctx context.Context, profile, code string, version int64) (formsport.TemplateVersion, error) {
	return p.catalog.TemplateVersion(ctx, profile, code, version)
}
func (p *cn275LostPublisher) PublishBatchOnce(ctx context.Context, in formsport.BatchInput, key string) ([]formsport.PublishedPeriod, error) {
	p.calls++
	out, err := p.durable.PublishBatchOnce(ctx, in, key)
	if err == nil && p.fail {
		p.fail = false
		return nil, problem.Internal.WithDetail("test-only lost ACK after native commit")
	}
	return out, err
}

func TestCNTS275PublicationLostACKCurrentAuthorityAndExactReceipt(t *testing.T) {
	var lost *cn275LostPublisher
	f := newAggFx(t, facilityDir{}, func(fm *forms.Module, kn *extension.Module) {
		pub := fm.Publisher()
		lost = &cn275LostPublisher{Publisher: pub, catalog: fm.TemplateCatalog(), durable: pub.(formsport.DurablePublisher), fail: true}
		kn.UseFormsPublisher(lost)
	})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	selected := cn275Form(t, f)
	actor, k := ids.New(), cn275Context(f)
	in := map[string]any{"code": "CNTS-BM17", "template_id": *selected.TemplateID, "template_version": 1, "from_date": "2026-09-29", "to_date": "2026-10-05", "units": []string{"X1"}}
	const path = "/v1/extension/cnts/period-series"
	const key = "lost-ack275-publish"
	c, b = cn275Call(f, path, key, actor, k, in)
	if c != 500 {
		t.Fatalf("lost ACK injection: %d %s", c, b)
	}
	if lost.calls != 1 || len(f.admin.query("SELECT id FROM pf_forms.period WHERE tenant_id=$1", f.tid)) != 1 {
		t.Fatal("native commit did not happen exactly once")
	}
	rows := f.admin.query("SELECT id::text FROM pf_forms.period WHERE tenant_id=$1", f.tid)
	f.closePeriod(rows[0][0].(string))
	// A later definition must not change the original dispatch or its receipt.
	fs := append(append([]designEditField{}, selected.Fields...), designEditField{Key: "moi", Label: "Mới", Type: "integer"})
	c, b = cn275Call(f, "/v1/extension/cnts/form-designs/CNTS-BM17/versions", "advance275-head", actor, k, map[string]any{"base_version": 1, "title": "BM17 v2", "fields": fs})
	f.ok(c, b, nil)
	c, b = cn275Call(f, path, key, actor, k, in)
	f.ok(c, b, nil)
	original := append([]byte(nil), b...)
	if lost.calls != 2 {
		t.Fatal("must recover from native durable receipt")
	}
	if _, err := f.adminExec("DELETE FROM shared.idempotency_key WHERE tenant_id='" + f.tid.String() + "'"); err != nil {
		t.Fatal(err)
	}
	c, b = cn275Call(f, path, key, actor, k, in)
	f.ok(c, b, nil)
	if !bytes.Equal(original, b) || lost.calls != 2 {
		t.Fatalf("durable KN receipt changed or redispatched: %s", b)
	}
	for name, change := range map[string]func(*authz.KNContext){
		"read only":        func(k *authz.KNContext) { k.ViewOnly = true },
		"wrong department": func(k *authz.KNContext) { k.Department = "trong_trot" },
		"leader":           func(k *authz.KNContext) { k.ProvinceGroup = "lanh_dao_phong" },
		"missing group":    func(k *authz.KNContext) { k.ProvinceGroup = "" },
		"revoked scope":    func(k *authz.KNContext) { k.Scope = authz.Scope{Units: []string{"X2"}} },
		"expired":          func(k *authz.KNContext) { k.ExpiresAt = f.clk.Now().Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			narrow := k
			change(&narrow)
			c, b := cn275Call(f, path, key, actor, narrow, in)
			if c < 400 {
				t.Fatalf("current authority bypassed by cached success: %d %s", c, b)
			}
		})
	}
	c, b = cn275Call(f, path, key, ids.New(), k, in)
	if c < 400 {
		t.Fatal("different actor replayed")
	}
	changed := map[string]any{}
	for x, v := range in {
		changed[x] = v
	}
	changed["units"] = []string{"X2"}
	c, b = cn275Call(f, path, key, actor, k, changed)
	if c < 400 {
		t.Fatal("different target replayed")
	}
	changed["units"] = in["units"]
	changed["template_version"] = 2
	c, b = cn275Call(f, path, key, actor, k, changed)
	if c < 400 {
		t.Fatal("different version replayed")
	}
	for table, want := range map[string]int{"pf_forms.period": 1, "pf_forms.period_event": 2, "pf_extension.cnts_publish_intent": 1, "pf_extension.cnts_publish_receipt": 1, "pf_extension.cnts_publication": 1} {
		if got := len(f.admin.query("SELECT id FROM "+table+" WHERE tenant_id=$1", f.tid)); got != want {
			t.Fatalf("%s duplicated: %d want%d", table, got, want)
		}
	}
}

func TestCNTS275DefinitionCASAndPolicyValidation(t *testing.T) {
	var pub formsport.Publisher
	f := newAggFx(t, facilityDir{}, func(fm *forms.Module, _ *extension.Module) { pub = fm.Publisher() })
	actor, k := ids.New(), authz.KNContext{ID: ids.New(), Role: authz.KNTinh, Department: aggDept, ProvinceGroup: "tong_hop", Scope: authz.Scope{Units: []string{"X1"}}}
	fields := []designEditField{{Key: "so_con", Label: "Số con", Type: "integer", Required: true}}
	c, b := cn275Call(f, "/v1/extension/cnts/form-designs", "cas275-create", actor, k, map[string]any{"code_suffix": "CAS275", "title": "CAS", "fields": fields})
	f.ok(c, b, nil)
	principal := bindKNTestSession(t, authz.Principal{ActorRef: actor, TenantID: f.tid, KN: &k})
	ctx := authz.WithPrincipal(context.Background(), principal)
	base := int64(1)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, suffix := range []string{"A", "B"} {
		wg.Add(1)
		go func(suffix string) {
			defer wg.Done()
			raw, _ := json.Marshal(fields)
			_, err := pub.DefineTemplate(ctx, formsport.TemplateDef{Profile: "kn_cnts", Code: "CNTS-DT-CAS275", Title: suffix, Fields: raw, BaseVersion: &base}, "cas275-"+suffix)
			results <- err
		}(suffix)
	}
	wg.Wait()
	close(results)
	wins, losses := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else {
			losses++
		}
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("CAS: winners=%d losers=%d", wins, losses)
	}
	// Owner validation is the same for direct Publisher, never just the KN UI.
	two := int64(2)
	for _, bad := range []designEditField{{Key: "so_con", Label: "Số con", Type: "integer", Unit: strings.Repeat("x", 81)}, {Key: "so_con", Label: "Số con", Type: "choice", Options: []string{"A", "A"}}} {
		raw, _ := json.Marshal([]designEditField{bad})
		if _, err := pub.DefineTemplate(ctx, formsport.TemplateDef{Profile: "kn_cnts", Code: "CNTS-DT-CAS275", Title: "Invalid", Fields: raw, BaseVersion: &two}, "invalid275-"+ids.New().String()); err == nil {
			t.Fatal("invalid direct native schema persisted")
		}
	}
}

func TestCNTS275FacilityPreflightNoRejectedIntent(t *testing.T) {
	inside, outside := ids.New(), ids.New()
	f := newAggFx(t, facilityDir{inside: "X1", outside: "Z9"})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	c, b = f.call(http.MethodGet, "/v1/extension/cnts/form-designs", authz.KNTinh, nil)
	var designs struct {
		Items []designEditView `json:"items"`
	}
	f.ok(c, b, &designs)
	var template designEditView
	for _, v := range designs.Items {
		if v.Code == "CNTS-BM01" {
			template = v
		}
	}
	actor, k := ids.New(), cn275Context(f)
	for _, unit := range []string{ids.New().String(), outside.String(), "bad-uuid"} {
		in := map[string]any{"code": "CNTS-BM01", "template_id": *template.TemplateID, "template_version": 1, "from_date": "2026-10-01", "to_date": "2026-10-01", "units": []string{unit}}
		c, b := cn275Call(f, "/v1/extension/cnts/period-series", "facility275-"+ids.New().String(), actor, k, in)
		if c < 400 {
			t.Fatalf("invalid/out-of-scope facility accepted: %d %s", c, b)
		}
	}
	for _, table := range []string{"pf_forms.period", "pf_extension.cnts_publish_intent", "pf_extension.cnts_publication"} {
		if len(f.admin.query("SELECT id FROM "+table+" WHERE tenant_id=$1", f.tid)) != 0 {
			t.Fatalf("rejected facility appended %s", table)
		}
	}
}

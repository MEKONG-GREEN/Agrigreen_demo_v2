package kn77_test

// W11.63 rÃºt gá»n â€” phiáº¿u "BÃ¡o cÃ¡o cho KHTC" trÃªn module THáº¬T qua HTTP: pf/extension (Káº¿ hoáº¡ch nÄƒm W11.61, Ká»³ KHTC W11.62,
// phiáº¿u /v1/extension/khtc/periods/{id}/forms/{dept}/*, sá»• pf 20261013165229) + pf/forms (ká»³ native kn_khtc, cá»•ng Entry,
// Validator kn_khtc) trÃªn PostgreSQL. TÃªn kiá»ƒm thá»­ theo dÃ²ng KH- cá»§a docs/uat/khtc-v2.2-acceptance.md.

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

const (
	dTT   = partyport.DeptTrongTrot
	dCNTS = partyport.DeptChanNuoiThuySan
	dTTTT = partyport.DeptThongTinTruyenThong
	dHCTH = partyport.DeptHanhChinhTongHop
)

// kfFx lÃ  fixture phiáº¿u KHTC: Ká»³ KHTC + phiáº¿u + Káº¿ hoáº¡ch nÄƒm 2026 cÃ³ háº¡ng má»¥c cá»§a TT (nháº­p + nguá»“n module), TTTT (sá»‘ phiáº¿u giÃ¡
// nguá»“n module + tin bÃ i), CNTS.
type kfFx struct {
	*khtcFx
	khtcUser uuid.UUID
	item     string // TT "MÃ´ hÃ¬nh lÃºa cháº¥t lÆ°á»£ng cao" â€” 100 ha
	itemMod  string // TT "Diá»‡n tÃ­ch gieo sáº¡" â€” nguá»“n module
	price    string // TTTT "Sá»‘ phiáº¿u giÃ¡ cáº£ Ä‘Ã£ thu tháº­p" â€” nguá»“n module
	news     string // TTTT "Tin bÃ i"
	planBody map[string]any
}

func newKHTCFormFx(t *testing.T) *kfFx {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)) // 08:00 Thá»© TÆ° 14/10 giá» VN â€” trÆ°á»›c háº¡n tuáº§n 41
	kn := extension.New(pool, clk, extension.Deps{})
	fm, err := forms.New(pool, clk, forms.Deps{Profiles: []formsport.Profile{platformasm.KNKHTCProfile},
		Validators: map[string]formsport.Validator{platformasm.KNKHTCProfile.Name: kn.KHTCFormValidator()}})
	if err != nil {
		t.Fatal(err)
	}
	cw := &crosswalk{closed: true}
	kn.UseKHTCPeriods(extension.KHTCDeps{Forms: fm.Publisher(), Control: fm.Control(), Entry: fm.Entry(), Crosswalk: cw,
		MaxSubmissions: platformasm.KNKHTCProfile.MaxSubmissions})
	base := &khtcFx{t: t, clk: clk, cw: cw, tid: ids.New(), node: ids.New()}
	base.eng = httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 8<<20)
	g := base.eng.Group("/v1", func(c *gin.Context) {
		var k authz.KNContext
		_ = json.Unmarshal([]byte(c.GetHeader("X-Test-KN")), &k)
		actor, _ := uuid.Parse(c.GetHeader("X-Test-Actor"))
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.Principal{ActorRef: actor, TenantID: base.tid, KN: &k}))
	}, idempotency.Middleware(ops))
	fm.RegisterApp(g)
	kn.RegisterApp(g)
	f := &kfFx{khtcFx: base, khtcUser: ids.New()}
	f.seedPlan()
	c, b := f.do(http.MethodPost, "/v1/extension/khtc/form-template/install", authz.KNAdmin, "", uuid.Nil, "", nil)
	f.ok(c, b, nil)
	return f
}

func (f *kfFx) seedPlan() {
	for _, c := range []struct{ kind, code, name string }{{"unit", "ha", "HÃ©c ta"}, {"unit", "tin_bai", "Tin bÃ i"},
		{"unit", "phieu", "Phiáº¿u"}, {"field", "trong_trot", "Trá»“ng trá»t"}} {
		code, b := f.do(http.MethodPost, "/v1/kn/khtc/catalogs/"+c.kind, authz.KNAdmin, "", uuid.Nil, "", map[string]any{"code": c.code, "name": c.name})
		f.ok(code, b, nil)
	}
	prog := func(code, name string) string {
		var r struct{ Ref string }
		c, b := f.do(http.MethodPost, "/v1/kn/khtc/programs", authz.KNTinh, khtcDept, f.khtcUser, "",
			map[string]any{"code": code, "name": name, "kind": "program", "field_code": "trong_trot"})
		f.ok(c, b, &r)
		return r.Ref
	}
	p3, p1 := prog("CT-III", "CT KN trá»“ng trá»t"), prog("CT-I", "CT thÃ´ng tin tuyÃªn truyá»n")
	e3, g3, e1 := ids.New().String(), ids.New().String(), ids.New().String()
	f.item, f.itemMod, f.price, f.news = ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String()
	f.planBody = map[string]any{"base_version": 0, "entries": []map[string]any{
		{"ref": e1, "tab": "programs", "kind": "program", "program_ref": p1, "sort_order": 1},
		{"ref": f.price, "tab": "programs", "kind": "item", "parent_ref": e1, "name": "Sá»‘ phiáº¿u giÃ¡ cáº£ Ä‘Ã£ thu tháº­p", "unit_code": "phieu",
			"planned_qty": "500", "department_code": dTTTT, "data_source": "module", "source_key": "price.tickets"},
		{"ref": f.news, "tab": "programs", "kind": "item", "parent_ref": e1, "name": "Tin bÃ i", "unit_code": "tin_bai", "planned_qty": "50",
			"department_code": dTTTT},
		{"ref": e3, "tab": "programs", "kind": "program", "program_ref": p3, "sort_order": 3},
		{"ref": g3, "tab": "programs", "kind": "group", "parent_ref": e3, "name": "XÃ¢y dá»±ng mÃ´ hÃ¬nh"},
		{"ref": f.item, "tab": "programs", "kind": "item", "parent_ref": g3, "name": "MÃ´ hÃ¬nh lÃºa cháº¥t lÆ°á»£ng cao", "unit_code": "ha",
			"planned_qty": "100", "department_code": dTT},
		{"ref": f.itemMod, "tab": "programs", "kind": "item", "parent_ref": g3, "name": "Diá»‡n tÃ­ch gieo sáº¡", "unit_code": "ha",
			"planned_qty": "5000", "department_code": dTT, "data_source": "module", "source_key": "trong_trot.consolidation"},
	}}
	c, b := f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", f.planBody)
	f.ok(c, b, nil)
}

type kfRow struct {
	EntryRef      string            `json:"entry_ref"`
	Name          string            `json:"name"`
	ModuleSourced bool              `json:"module_sourced"`
	Cells         map[string]string `json:"cells"`
	OverPlan      bool              `json:"over_plan"`
}

type kfIssue struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	EntryRef string `json:"entry_ref"`
	Letter   string `json:"letter"`
	Section  string `json:"section"`
}

type kfForm struct {
	State                string `json:"state"`
	Attempt              int64  `json:"attempt"`
	LimitReached         bool   `json:"limit_reached"`
	Editable             bool   `json:"editable"`
	CanRequestSupplement bool   `json:"can_request_supplement"`
	CanConfirm           bool   `json:"can_confirm"`
	RequestNote          string `json:"request_note"`
	Grid                 bool   `json:"grid"`
	ContentSource        string `json:"content_source"`
	Valid                bool   `json:"valid"`
	Template             struct {
		Code    string `json:"code"`
		Columns []struct {
			Letter, Kind string
		} `json:"columns"`
	} `json:"template"`
	Tabs []struct {
		Label string  `json:"label"`
		Rows  []kfRow `json:"rows"`
	} `json:"tabs"`
	Issues     []kfIssue `json:"issues"`
	AdjPeriods []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"adj_periods"`
	Submissions []struct {
		Attempt  int64  `json:"attempt"`
		Decision string `json:"decision"`
	} `json:"submissions"`
}

func (k kfForm) row(t *testing.T, ref string) kfRow {
	t.Helper()
	for _, tab := range k.Tabs {
		for _, r := range tab.Rows {
			if r.EntryRef == ref {
				return r
			}
		}
	}
	t.Fatalf("khÃ´ng cÃ³ dÃ²ng %s", ref)
	return kfRow{}
}

func (k kfForm) has(rule, ref, letter string) bool {
	return slices.ContainsFunc(k.Issues, func(i kfIssue) bool {
		return i.Rule == rule && (ref == "" || i.EntryRef == ref) && (letter == "" || i.Letter == letter)
	})
}

func (f *kfFx) open(no int, depts ...string) khtcPeriod {
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "week", "year": 2026, "no": no, "departments": depts})
	f.ok(c, b, &p)
	return p
}

func formPath(pid, dept string) string { return "/v1/extension/khtc/periods/" + pid + "/forms/" + dept }

func (f *kfFx) form(pid, dept, role, as string) kfForm {
	f.t.Helper()
	var out kfForm
	c, b := f.do(http.MethodGet, formPath(pid, dept), role, as, uuid.Nil, "", nil)
	f.ok(c, b, &out)
	return out
}

func (f *kfFx) deptForm(pid, dept string) kfForm { return f.form(pid, dept, authz.KNTinh, dept) }

// send gá»i draft/submit cá»§a phÃ²ng; tráº£ mÃ£ vÃ  thÃ¢n.
func (f *kfFx) send(pid, dept, op string, base int64, sheet map[string]any) (int, []byte) {
	return f.do(http.MethodPost, formPath(pid, dept)+"/"+op, authz.KNTinh, dept, uuid.Nil, "", map[string]any{"base_attempt": base, "sheet": sheet})
}

func (f *kfFx) reviewForm(pid, dept, decision, note string) (int, []byte) {
	return f.do(http.MethodPost, formPath(pid, dept)+"/review", authz.KNTinh, khtcDept, f.khtcUser, "",
		map[string]any{"decision": decision, "note": note})
}

func ttSheet(item, done string) map[string]any {
	return map[string]any{"cells": map[string]any{item: map[string]string{"done": done, "people": "30"}},
		"texts": map[string]string{"cong_tac_khac": "Táº­p huáº¥n 2 lá»›p IPM"}}
}

// KH-FN-PN-03.01: phiáº¿u má»›i hiá»‡n ngay sau khi KHTC má»Ÿ ká»³, á»Ÿ cáº£ 4 phÃ²ng TT, CNTS, TTTT, HCTH; má»—i phÃ²ng chá»‰ tháº¥y phiáº¿u cá»§a
// mÃ¬nh; tráº¡ng thÃ¡i Ä‘á»•i ngay sau khi gá»­i; ká»³ há»§y â†’ phiáº¿u gá»¡ khá»i module cÃ¡c phÃ²ng.
func TestKH_FN_PN_03_01_TaskVisibleInEachDepartment(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT, dCNTS, dTTTT, dHCTH)
	tasks := func(dept string) []map[string]any {
		var out struct{ Items []map[string]any }
		c, b := f.do(http.MethodGet, "/v1/extension/khtc/my-forms?department="+dept, authz.KNTinh, dept, uuid.Nil, "", nil)
		f.ok(c, b, &out)
		return out.Items
	}
	for _, d := range []string{dTT, dCNTS, dTTTT, dHCTH} {
		it := tasks(d)
		if len(it) != 1 || it[0]["period_id"] != p.ID || it[0]["state"] != "not_started" || it[0]["actionable"] != true ||
			it[0]["max_submissions"].(float64) != 3 {
			t.Fatalf("%s: %+v", d, it)
		}
	}
	c, b := f.do(http.MethodGet, "/v1/extension/khtc/my-forms?department="+dCNTS, authz.KNTinh, dTT, uuid.Nil, "", nil)
	f.fail(c, b, "forbidden")
	c, b = f.do(http.MethodGet, formPath(p.ID, dCNTS), authz.KNTinh, dTT, uuid.Nil, "", nil)
	f.fail(c, b, "forbidden")
	// HCTH: chá»‰ má»¥c lá»i vÄƒn (khÃ´ng tab chÆ°Æ¡ng trÃ¬nh).
	h := f.deptForm(p.ID, dHCTH)
	if h.Template.Code != "KH-HCTH" || len(h.Tabs) != 0 {
		t.Fatalf("HCTH: %+v", h)
	}
	c, b = f.send(p.ID, dHCTH, "submit", 0, map[string]any{"texts": map[string]string{"cchc": "RÃ  soÃ¡t 12 thá»§ tá»¥c hÃ nh chÃ­nh"}})
	f.ok(c, b, nil)
	if it := tasks(dHCTH); it[0]["state"] != "submitted" || it[0]["actionable"] != false || it[0]["attempt"].(float64) != 1 {
		t.Fatalf("sau gá»­i: %+v", it)
	}
	// KHTC xem Ä‘Æ°á»£c phiáº¿u cá»§a má»i phÃ²ng.
	var all struct{ Items []map[string]any }
	c, b = f.khtc(http.MethodGet, "/my-forms?department="+dHCTH, f.khtcUser, "", nil)
	f.ok(c, b, &all)
	if len(all.Items) != 1 {
		t.Fatalf("KHTC xem: %s", b)
	}
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Má»Ÿ nháº§m tuáº§n bÃ¡o cÃ¡o"})
	f.ok(c, b, nil)
	if it := tasks(dTT); len(it) != 0 {
		t.Fatalf("ká»³ há»§y pháº£i gá»¡ phiáº¿u: %+v", it)
	}
}

// KH-FN-PN-03.02 / KH-UR-PN-02: háº¡ng má»¥c cÃ³ nguá»“n module (BR-DL-10) khÃ´ng cÃ³ Ã´ nháº­p â€” sá»‘ phiáº¿u giÃ¡ cá»§a TTTT, diá»‡n tÃ­ch gieo
// sáº¡ cá»§a TT; cá»™t C, D "Tá»« Káº¿ hoáº¡ch nÄƒm" Ä‘iá»n tá»« phiÃªn báº£n káº¿ hoáº¡ch; bÃ i ná»™p tháº³ng /v1/forms khÃ´ng cÃ³ báº£ng Ä‘Ã£ kiá»ƒm bá»‹ cháº·n.
func TestKH_FN_PN_03_02_ModuleSourcedCellsReadOnly(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT, dTTTT)
	tt := f.deptForm(p.ID, dTT)
	if tt.Template.Code != "KH-TT" || !tt.Grid || len(tt.Tabs) != 1 || tt.Tabs[0].Label != "CT II: CT KN trá»“ng trá»t" || !tt.Editable {
		t.Fatalf("phiáº¿u TT: %+v", tt)
	}
	kinds := ""
	for _, c := range tt.Template.Columns {
		kinds += c.Letter + c.Kind[:2] + " "
	}
	if kinds != "Afi Bfi Cfr Dfr Epr Fin Gfo Hfo Ifo Jin Kin Lin Min " {
		t.Fatalf("cá»™t: %s", kinds)
	}
	if r := tt.row(t, f.item); r.ModuleSourced || r.Cells["C"] != "ha" || r.Cells["D"] != "100" || r.Cells["E"] != "0" {
		t.Fatalf("dÃ²ng nháº­p: %+v", r)
	}
	if r := tt.row(t, f.itemMod); !r.ModuleSourced || r.Cells["H"] != "" {
		t.Fatalf("dÃ²ng nguá»“n module: %+v", r)
	}
	ttt := f.deptForm(p.ID, dTTTT)
	if r := ttt.row(t, f.price); !r.ModuleSourced || ttt.Tabs[0].Label != "CT I: CT thÃ´ng tin tuyÃªn truyá»n" {
		t.Fatalf("sá»‘ phiáº¿u giÃ¡: %+v", ttt.Tabs)
	}
	// NhÃ¡p Ä‘Æ°á»£c lÆ°u cáº£ khi cÃ³ lá»—i (bÃ¡o táº¡i Ã´); gá»­i bá»‹ cháº·n.
	sheet := map[string]any{"cells": map[string]any{f.price: map[string]string{"done": "14"}, f.news: map[string]string{"done": "3"}},
		"texts": map[string]string{"cong_tac_khac": "ÄÄƒng 3 tin"}}
	c, b := f.send(p.ID, dTTTT, "draft", 0, sheet)
	var d kfForm
	f.ok(c, b, &d)
	if d.State != "draft" || d.ContentSource != "draft" || !d.has("template", f.price, "F") || d.Valid {
		t.Fatalf("nhÃ¡p: %s", b)
	}
	c, b = f.send(p.ID, dTTTT, "submit", 0, sheet)
	f.fail(c, b, "extension.khtc_form_invalid")
	delete(sheet["cells"].(map[string]any), f.price)
	c, b = f.send(p.ID, dTTTT, "submit", 0, sheet)
	f.ok(c, b, &d)
	if d.State != "submitted" || d.Attempt != 1 || d.ContentSource != "submitted" {
		t.Fatalf("gá»­i: %s", b)
	}
	// BÃ i ná»™p tháº³ng cap/forms (khÃ´ng qua kiá»ƒm tra cá»§a máº«u) bá»‹ Validator kn_khtc cháº·n.
	c, b = f.do(http.MethodPost, "/v1/forms/periods/"+p.FormsPeriodID+"/submissions?profile=kn_khtc", authz.KNTinh, dTT, uuid.Nil, "",
		map[string]any{"unit": dTT, "values": map[string]string{"ket_qua": "bá» qua kiá»ƒm tra"}})
	f.fail(c, b, "forms.values_invalid")
	c, b = f.do(http.MethodPost, "/v1/forms/periods/"+p.FormsPeriodID+"/submissions?profile=kn_khtc", authz.KNTinh, dTT, uuid.Nil, "",
		map[string]any{"unit": dTT, "values": map[string]string{"bang_so_lieu": ids.New().String()}})
	f.fail(c, b, "forms.values_invalid")
}

// KH-FN-PN-03.02 / KH-UR-PN-03: lÆ°u nhÃ¡p khÃ´ng giá»›i háº¡n; gá»­i thÃ¬ khÃ³a; chá»‰ gá»­i láº¡i khi KHTC yÃªu cáº§u bá»• sung; tá»‘i Ä‘a 3 láº§n
// gá»­i, láº§n 3 tá»± khÃ³a (Q-081); ká»³ Ä‘Ã³ng khÃ´ng sá»­a.
func TestKH_FN_PN_03_02_ResubmitOnlyOnRequest(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT)
	for i := 0; i < 3; i++ {
		c, b := f.send(p.ID, dTT, "draft", 0, ttSheet(f.item, "10"))
		f.ok(c, b, nil)
	}
	if st, _, _, _, _ := f.get(p.ID).form(t, dTT); st != "draft" {
		t.Fatalf("tiáº¿n Ä‘á»™ ká»³ pháº£i hiá»‡n LÆ°u nhÃ¡p: %s", st)
	}
	c, b := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(c, b, nil)
	c, b = f.send(p.ID, dTT, "draft", 1, ttSheet(f.item, "11"))
	f.fail(c, b, "extension.khtc_form_locked")
	c, b = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "11"))
	f.fail(c, b, "extension.khtc_form_locked")
	c, b = f.reviewForm(p.ID, dTT, "returned", "Bá»• sung sá»‘ ngÆ°á»i tham gia lá»›p 2")
	f.ok(c, b, nil)
	k := f.deptForm(p.ID, dTT)
	if k.State != "supplement" || !k.Editable || k.RequestNote != "Bá»• sung sá»‘ ngÆ°á»i tham gia lá»›p 2" || k.ContentSource != "submitted" {
		t.Fatalf("yÃªu cáº§u bá»• sung: %+v", k)
	}
	c, b = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "12"))
	f.fail(c, b, "forms.submission_conflict")
	c, b = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "12"))
	f.ok(c, b, nil)
	c, b = f.reviewForm(p.ID, dTT, "returned", "Kiá»ƒm tra láº¡i sá»‘ liá»‡u")
	f.ok(c, b, nil)
	c, b = f.send(p.ID, dTT, "submit", 2, ttSheet(f.item, "13"))
	f.ok(c, b, nil)
	r := f.form(p.ID, dTT, authz.KNTinh, khtcDept)
	if r.Attempt != 3 || !r.LimitReached || r.CanRequestSupplement || !r.CanConfirm || len(r.Submissions) != 3 {
		t.Fatalf("láº§n 3: %+v", r)
	}
	if v := r.row(t, f.item).Cells["H"]; v != "13" {
		t.Fatalf("rÃ  soÃ¡t dÃ¹ng láº§n gá»­i cuá»‘i: H=%s", v)
	}
	c, b = f.reviewForm(p.ID, dTT, "returned", "ThÃªm ná»¯a")
	f.fail(c, b, "extension.khtc_form_not_reviewable")
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", f.khtcUser, "", map[string]any{})
	f.ok(c, b, nil)
	k = f.deptForm(p.ID, dTT)
	if k.State != "locked" || k.Editable {
		t.Fatalf("ká»³ Ä‘Ã³ng: %+v", k)
	}
	c, b = f.send(p.ID, dTT, "draft", 3, ttSheet(f.item, "14"))
	f.fail(c, b, "extension.khtc_form_locked")
}

// KH-FN-TI-02.04 / KH-UR-TI-05: KHTC xem káº¿t quáº£ 3 quy táº¯c (yÃªu cáº§u cá»§a máº«u, sá»‘ Ã¢m pháº£i cÃ³ lÃ½ do, lÅ©y káº¿ > 100% KH), yÃªu
// cáº§u bá»• sung kÃ¨m ná»™i dung (phÃ²ng tháº¥y ngay), xÃ¡c nháº­n; phÃ²ng chuyÃªn mÃ´n khÃ´ng rÃ  soÃ¡t Ä‘Æ°á»£c.
func TestKH_FN_TI_02_04_ReviewRulesAndRequest(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT)
	// (1) Má»¥c lá»i vÄƒn báº¯t buá»™c cá»§a máº«u thiáº¿u â†’ cháº·n gá»­i.
	c, b := f.send(p.ID, dTT, "submit", 0, map[string]any{"cells": map[string]any{f.item: map[string]string{"done": "120"}}})
	f.fail(c, b, "extension.khtc_form_invalid")
	var pr struct {
		Issues []kfIssue `json:"issues"`
	}
	_ = json.Unmarshal(b, &pr)
	if !slices.ContainsFunc(pr.Issues, func(i kfIssue) bool { return i.Rule == "template" && i.Section == "cong_tac_khac" }) {
		t.Fatalf("thiáº¿u káº¿t quáº£ kiá»ƒm tra trong lá»—i: %s", b)
	}
	// (1) Sá»‘ Ã¢m á»Ÿ Thá»±c hiá»‡n trong ká»³ â†’ cháº·n.
	c, b = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "-1"))
	f.fail(c, b, "extension.khtc_form_invalid")
	// (3) LÅ©y káº¿ 120/100 â†’ cáº£nh bÃ¡o, váº«n gá»­i.
	c, b = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "120"))
	f.ok(c, b, nil)
	r := f.form(p.ID, dTT, authz.KNTinh, khtcDept)
	row := r.row(t, f.item)
	if !r.Valid || !r.has("over_plan", f.item, "H") || !row.OverPlan || row.Cells["H"] != "120" || row.Cells["I"] != "120" ||
		!r.CanRequestSupplement || !r.CanConfirm {
		t.Fatalf("rÃ  soÃ¡t: %+v", r)
	}
	c, b = f.do(http.MethodPost, formPath(p.ID, dTT)+"/review", authz.KNTinh, dTT, uuid.Nil, "", map[string]any{"decision": "accepted"})
	f.fail(c, b, "forbidden")
	c, b = f.reviewForm(p.ID, dTT, "returned", "")
	f.fail(c, b, "validation_failed")
	c, b = f.reviewForm(p.ID, dTT, "returned", "LÅ©y káº¿ vÆ°á»£t káº¿ hoáº¡ch â€” kiá»ƒm tra diá»‡n tÃ­ch mÃ´ hÃ¬nh")
	f.ok(c, b, nil)
	if k := f.deptForm(p.ID, dTT); k.RequestNote != "LÅ©y káº¿ vÆ°á»£t káº¿ hoáº¡ch â€” kiá»ƒm tra diá»‡n tÃ­ch mÃ´ hÃ¬nh" || k.State != "supplement" {
		t.Fatalf("phÃ²ng nháº­n yÃªu cáº§u: %+v", k)
	}
	c, b = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "80"))
	f.ok(c, b, nil)
	c, b = f.reviewForm(p.ID, dTT, "accepted", "")
	f.ok(c, b, nil)
	if k := f.deptForm(p.ID, dTT); k.State != "confirmed" || k.Editable {
		t.Fatalf("xÃ¡c nháº­n: %+v", k)
	}
	c, b = f.reviewForm(p.ID, dTT, "returned", "Sau xÃ¡c nháº­n")
	f.fail(c, b, "extension.khtc_form_not_reviewable")
}

// KH-FN-PN-03.02 (BR-DL-05, BR-DL-12): "LÅ©y káº¿ ká»³ trÆ°á»›c" láº¥y tá»« ká»³ KHTC Ä‘Ã£ Ä‘Ã³ng; "Äiá»u chá»‰nh ká»³ trÆ°á»›c" báº¯t buá»™c chá»n ká»³ Ä‘Ã£
// khÃ³a vÃ  lÃ½ do (sá»‘ Ã¢m khÃ´ng lÃ½ do â†’ quy táº¯c 2), chá»‰ cá»™ng vÃ o lÅ©y káº¿; háº¡ng má»¥c Ä‘Ã£ cÃ³ sá»‘ liá»‡u chá»‰ ngá»«ng, khÃ´ng bá» khá»i Káº¿
// hoáº¡ch nÄƒm (W11.61 EntryUsage).
func TestKH_FN_PN_03_02_PriorPeriodAdjustment(t *testing.T) {
	f := newKHTCFormFx(t)
	w41 := f.open(41, dTT)
	c, b := f.send(w41.ID, dTT, "submit", 0, ttSheet(f.item, "30"))
	f.ok(c, b, nil)
	c, b = f.reviewForm(w41.ID, dTT, "accepted", "")
	f.ok(c, b, nil)
	c, b = f.khtc(http.MethodPost, "/periods/"+w41.ID+"/close", f.khtcUser, "", map[string]any{})
	f.ok(c, b, nil)
	f.clk.Advance(7 * 24 * time.Hour) // 21/10: tuáº§n 42 (14/10â€“20/10)
	w42 := f.open(42, dTT)
	k := f.deptForm(w42.ID, dTT)
	if v := k.row(t, f.item).Cells["E"]; v != "30" {
		t.Fatalf("LÅ©y káº¿ ká»³ trÆ°á»›c: %s", v)
	}
	if len(k.AdjPeriods) != 1 || k.AdjPeriods[0].ID != w41.ID {
		t.Fatalf("ká»³ Ä‘Æ°á»£c Ä‘iá»u chá»‰nh: %+v", k.AdjPeriods)
	}
	sheet := ttSheet(f.item, "10")
	sheet["adjustments"] = []map[string]any{{"entry_ref": f.item, "amount": "-5", "reason": "Ghi thá»«a tuáº§n 41"}}
	c, b = f.send(w42.ID, dTT, "submit", 0, sheet)
	f.fail(c, b, "extension.khtc_form_invalid") // chÆ°a chá»n ká»³ Ä‘Æ°á»£c Ä‘iá»u chá»‰nh
	sheet["adjustments"] = []map[string]any{{"entry_ref": f.item, "period_id": w41.ID, "amount": "-5"}}
	c, b = f.send(w42.ID, dTT, "submit", 0, sheet)
	f.fail(c, b, "extension.khtc_form_invalid")
	var pr struct{ Issues []kfIssue }
	_ = json.Unmarshal(b, &pr)
	if !slices.ContainsFunc(pr.Issues, func(i kfIssue) bool { return i.Rule == "negative_reason" }) {
		t.Fatalf("sá»‘ Ã¢m khÃ´ng lÃ½ do: %s", b)
	}
	sheet["adjustments"] = []map[string]any{{"entry_ref": f.item, "period_id": w41.ID, "amount": "-5", "reason": "Ghi thá»«a 5 ha á»Ÿ tuáº§n 41"}}
	c, b = f.send(w42.ID, dTT, "submit", 0, sheet)
	f.ok(c, b, &k)
	if r := k.row(t, f.item); r.Cells["F"] != "" && r.Cells["G"] != "-5" || r.Cells["H"] != "35" || r.Cells["I"] != "35" {
		t.Fatalf("lÅ©y káº¿ sau Ä‘iá»u chá»‰nh: %+v", r.Cells)
	}
	// W11.61 EntryUsage: bá» háº¡ng má»¥c Ä‘Ã£ cÃ³ sá»‘ liá»‡u khá»i káº¿ hoáº¡ch â†’ cháº·n (chá»‰ ngá»«ng theo dÃµi).
	body := map[string]any{"base_version": 1, "decision_no": "88/QÄ-SNN", "decision_date": "2026-10-20", "effective_from": "2026-10-21"}
	var keep []map[string]any
	for _, e := range f.planBody["entries"].([]map[string]any) {
		if e["ref"] != f.item {
			keep = append(keep, e)
		}
	}
	body["entries"] = keep
	c, b = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", body)
	f.fail(c, b, "extension.khtc_entry_in_use")
}

package kn77_test

// W11.63 rút gọn — phiếu "Báo cáo cho KHTC" trên module THẬT qua HTTP: pf/extension (Kế hoạch năm W11.61, Kỳ KHTC W11.62,
// phiếu /v1/extension/khtc/periods/{id}/forms/{dept}/*, sổ pf 20261015070628) + pf/forms (kỳ native kn_khtc, cổng Entry,
// Validator kn_khtc) trên PostgreSQL. Tên kiểm thử theo dòng KH- của docs/uat/khtc-v2.2-acceptance.md.

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

const (
	dTT   = partyport.DeptTrongTrot
	dCNTS = partyport.DeptChanNuoiThuySan
	dTTTT = partyport.DeptThongTinTruyenThong
	dHCTH = partyport.DeptHanhChinhTongHop
)

// kfFx là fixture phiếu KHTC: Kỳ KHTC + phiếu + Kế hoạch năm 2026 có hạng mục của TT (nhập + nguồn module), TTTT (số phiếu giá
// nguồn module + tin bài), CNTS.
type kfFx struct {
	ext *extension.Module
	*khtcFx
	khtcUser uuid.UUID
	item     string // TT "Mô hình lúa chất lượng cao" — 100 ha
	itemMod  string // TT "Diện tích gieo sạ" — nguồn module
	price    string // TTTT "Số phiếu giá cả đã thu thập" — nguồn module
	news     string // TTTT "Tin bài"
	planBody map[string]any
}

func newKHTCFormFx(t *testing.T) *kfFx {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)) // 08:00 Thứ Tư 14/10 giờ VN — trước hạn tuần 41
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
	base.eng = httpx.NewEngine(slog.New(slog.NewTextHandler(io.MultiWriter(io.Discard, tlog{t}), nil)), 8<<20)
	base.eng.Use(func(c *gin.Context) {
		c.Next()
		for _, issue := range c.Errors {
			for err := issue.Err; err != nil; err = errors.Unwrap(err) {
				t.Logf("HTTP internal cause: %v", err)
				var pg *pgconn.PgError
				if errors.As(err, &pg) {
					t.Logf("PG detail=%s where=%s", pg.Detail, pg.Where)
				}
			}
		}
	})
	g := base.eng.Group("/v1", func(c *gin.Context) {
		var k authz.KNContext
		_ = json.Unmarshal([]byte(c.GetHeader("X-Test-KN")), &k)
		actor, _ := uuid.Parse(c.GetHeader("X-Test-Actor"))
		// Như httpx.Authenticate: người gọi + tenant ở ctx (cổng Kế hoạch năm đọc tenant từ ctx).
		ctx := tenant.WithID(c.Request.Context(), base.tid)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, authz.Principal{ActorRef: actor, TenantID: base.tid, KN: &k}))
		scope, _ := json.Marshal(k)
		c.Request = c.Request.WithContext(idempotency.WithAuthorizationScope(c.Request.Context(), scope))
	}, idempotency.Middleware(ops))
	fm.RegisterApp(g)
	kn.RegisterApp(g)
	f := &kfFx{khtcFx: base, khtcUser: ids.New(), ext: kn}
	f.seedPlan()
	c, b := f.do(http.MethodPost, "/v1/extension/khtc/form-template/install", authz.KNAdmin, "", uuid.Nil, "", nil)
	f.ok(c, b, nil)
	return f
}

func (f *kfFx) seedPlan() {
	for _, c := range []struct{ kind, code, name string }{{"unit", "ha", "Héc ta"}, {"unit", "tin_bai", "Tin bài"},
		{"unit", "phieu", "Phiếu"}, {"field", "trong_trot", "Trồng trọt"}} {
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
	p3, p1 := prog("CT-III", "CT KN trồng trọt"), prog("CT-I", "CT thông tin tuyên truyền")
	e3, g3, e1 := ids.New().String(), ids.New().String(), ids.New().String()
	f.item, f.itemMod, f.price, f.news = ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String()
	f.planBody = map[string]any{"base_version": 0, "entries": []map[string]any{
		{"ref": e1, "tab": "programs", "kind": "program", "program_ref": p1, "sort_order": 1},
		{"ref": f.price, "tab": "programs", "kind": "item", "parent_ref": e1, "name": "Số phiếu giá cả đã thu thập", "unit_code": "phieu",
			"planned_qty": "500", "department_code": dTTTT, "data_source": "module", "source_key": "price.tickets"},
		{"ref": f.news, "tab": "programs", "kind": "item", "parent_ref": e1, "name": "Tin bài", "unit_code": "tin_bai", "planned_qty": "50",
			"department_code": dTTTT},
		{"ref": e3, "tab": "programs", "kind": "program", "program_ref": p3, "sort_order": 3},
		{"ref": g3, "tab": "programs", "kind": "group", "parent_ref": e3, "name": "Xây dựng mô hình"},
		{"ref": f.item, "tab": "programs", "kind": "item", "parent_ref": g3, "name": "Mô hình lúa chất lượng cao", "unit_code": "ha",
			"planned_qty": "100", "department_code": dTT},
		{"ref": f.itemMod, "tab": "programs", "kind": "item", "parent_ref": g3, "name": "Diện tích gieo sạ", "unit_code": "ha",
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
	t.Fatalf("không có dòng %s", ref)
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

// send gọi draft/submit của phòng; trả mã và thân.
func (f *kfFx) send(pid, dept, op string, base int64, sheet map[string]any) (int, []byte) {
	return f.do(http.MethodPost, formPath(pid, dept)+"/"+op, authz.KNTinh, dept, uuid.Nil, "", map[string]any{"base_attempt": base, "sheet": sheet})
}

func (f *kfFx) reviewForm(pid, dept, decision, note string) (int, []byte) {
	return f.do(http.MethodPost, formPath(pid, dept)+"/review", authz.KNTinh, khtcDept, f.khtcUser, "",
		map[string]any{"decision": decision, "note": note})
}

func ttSheet(item, done string) map[string]any {
	return map[string]any{"cells": map[string]any{item: map[string]string{"done": done, "people": "30"}},
		"texts": map[string]string{"cong_tac_khac": "Tập huấn 2 lớp IPM"}}
}

// KH-FN-PN-03.01: phiếu mới hiện ngay sau khi KHTC mở kỳ, ở cả 4 phòng TT, CNTS, TTTT, HCTH; mỗi phòng chỉ thấy phiếu của
// mình; trạng thái đổi ngay sau khi gửi; kỳ hủy → phiếu gỡ khỏi module các phòng.
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
	// HCTH: chỉ mục lời văn (không tab chương trình).
	h := f.deptForm(p.ID, dHCTH)
	if h.Template.Code != "KH-HCTH" || len(h.Tabs) != 0 {
		t.Fatalf("HCTH: %+v", h)
	}
	c, b = f.send(p.ID, dHCTH, "submit", 0, map[string]any{"texts": map[string]string{"cchc": "Rà soát 12 thủ tục hành chính"}})
	f.ok(c, b, nil)
	if it := tasks(dHCTH); it[0]["state"] != "submitted" || it[0]["actionable"] != false || it[0]["attempt"].(float64) != 1 {
		t.Fatalf("sau gửi: %+v", it)
	}
	// KHTC xem được phiếu của mọi phòng.
	var all struct{ Items []map[string]any }
	c, b = f.khtc(http.MethodGet, "/my-forms?department="+dHCTH, f.khtcUser, "", nil)
	f.ok(c, b, &all)
	if len(all.Items) != 1 {
		t.Fatalf("KHTC xem: %s", b)
	}
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Mở nhầm tuần báo cáo"})
	f.ok(c, b, nil)
	if it := tasks(dTT); len(it) != 0 {
		t.Fatalf("kỳ hủy phải gỡ phiếu: %+v", it)
	}
}

// KH-FN-PN-03.02 / KH-UR-PN-02: hạng mục có nguồn module (BR-DL-10) không có ô nhập — số phiếu giá của TTTT, diện tích gieo
// sạ của TT; cột C, D "Từ Kế hoạch năm" điền từ phiên bản kế hoạch; bài nộp thẳng /v1/forms không có bảng đã kiểm bị chặn.
func TestKH_FN_PN_03_02_ModuleSourcedCellsReadOnly(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT, dTTTT)
	tt := f.deptForm(p.ID, dTT)
	if tt.Template.Code != "KH-TT" || !tt.Grid || len(tt.Tabs) != 1 || tt.Tabs[0].Label != "CT II: CT KN trồng trọt" || !tt.Editable {
		t.Fatalf("phiếu TT: %+v", tt)
	}
	kinds := ""
	for _, c := range tt.Template.Columns {
		kinds += c.Letter + c.Kind[:2] + " "
	}
	if kinds != "Afi Bfi Cfr Dfr Epr Fin Gfo Hfo Ifo Jin Kin Lin Min " {
		t.Fatalf("cột: %s", kinds)
	}
	if r := tt.row(t, f.item); r.ModuleSourced || r.Cells["C"] != "ha" || r.Cells["D"] != "100" || r.Cells["E"] != "0" {
		t.Fatalf("dòng nhập: %+v", r)
	}
	if r := tt.row(t, f.itemMod); !r.ModuleSourced || r.Cells["H"] != "" {
		t.Fatalf("dòng nguồn module: %+v", r)
	}
	ttt := f.deptForm(p.ID, dTTTT)
	if r := ttt.row(t, f.price); !r.ModuleSourced || ttt.Tabs[0].Label != "CT I: CT thông tin tuyên truyền" {
		t.Fatalf("số phiếu giá: %+v", ttt.Tabs)
	}
	// Nháp được lưu cả khi có lỗi (báo tại ô); gửi bị chặn.
	sheet := map[string]any{"cells": map[string]any{f.price: map[string]string{"done": "14"}, f.news: map[string]string{"done": "3"}},
		"texts": map[string]string{"cong_tac_khac": "Đăng 3 tin"}}
	c, b := f.send(p.ID, dTTTT, "draft", 0, sheet)
	var d kfForm
	f.ok(c, b, &d)
	if d.State != "draft" || d.ContentSource != "draft" || !d.has("template", f.price, "F") || d.Valid {
		t.Fatalf("nháp: %s", b)
	}
	c, b = f.send(p.ID, dTTTT, "submit", 0, sheet)
	f.fail(c, b, "extension.khtc_form_invalid")
	delete(sheet["cells"].(map[string]any), f.price)
	c, b = f.send(p.ID, dTTTT, "submit", 0, sheet)
	f.ok(c, b, &d)
	if d.State != "submitted" || d.Attempt != 1 || d.ContentSource != "submitted" {
		t.Fatalf("gửi: %s", b)
	}
	// Bài nộp thẳng cap/forms (không qua kiểm tra của mẫu) bị Validator kn_khtc chặn.
	c, b = f.do(http.MethodPost, "/v1/forms/periods/"+p.FormsPeriodID+"/submissions?profile=kn_khtc", authz.KNTinh, dTT, uuid.Nil, "",
		map[string]any{"unit": dTT, "values": map[string]string{"ket_qua": "bỏ qua kiểm tra"}})
	f.fail(c, b, "forms.values_invalid")
	c, b = f.do(http.MethodPost, "/v1/forms/periods/"+p.FormsPeriodID+"/submissions?profile=kn_khtc", authz.KNTinh, dTT, uuid.Nil, "",
		map[string]any{"unit": dTT, "values": map[string]string{"bang_so_lieu": ids.New().String()}})
	f.fail(c, b, "forms.values_invalid")
}

// KH-FN-PN-03.02 / KH-UR-PN-03: lưu nháp không giới hạn; gửi thì khóa; chỉ gửi lại khi KHTC yêu cầu bổ sung; tối đa 3 lần
// gửi, lần 3 tự khóa (Q-081); kỳ đóng không sửa.
func TestKH_FN_PN_03_02_ResubmitOnlyOnRequest(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT)
	for i := 0; i < 3; i++ {
		c, b := f.send(p.ID, dTT, "draft", 0, ttSheet(f.item, "10"))
		f.ok(c, b, nil)
	}
	if st, _, _, _, _ := f.get(p.ID).form(t, dTT); st != "draft" {
		t.Fatalf("tiến độ kỳ phải hiện Lưu nháp: %s", st)
	}
	c, b := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(c, b, nil)
	c, b = f.send(p.ID, dTT, "draft", 1, ttSheet(f.item, "11"))
	f.fail(c, b, "extension.khtc_form_locked")
	c, b = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "11"))
	f.fail(c, b, "extension.khtc_form_locked")
	c, b = f.reviewForm(p.ID, dTT, "returned", "Bổ sung số người tham gia lớp 2")
	f.ok(c, b, nil)
	k := f.deptForm(p.ID, dTT)
	if k.State != "supplement" || !k.Editable || k.RequestNote != "Bổ sung số người tham gia lớp 2" || k.ContentSource != "submitted" {
		t.Fatalf("yêu cầu bổ sung: %+v", k)
	}
	c, b = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "12"))
	f.fail(c, b, "forms.submission_conflict")
	c, b = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "12"))
	f.ok(c, b, nil)
	c, b = f.reviewForm(p.ID, dTT, "returned", "Kiểm tra lại số liệu")
	f.ok(c, b, nil)
	c, b = f.send(p.ID, dTT, "submit", 2, ttSheet(f.item, "13"))
	f.ok(c, b, nil)
	r := f.form(p.ID, dTT, authz.KNTinh, khtcDept)
	if r.Attempt != 3 || !r.LimitReached || r.CanRequestSupplement || !r.CanConfirm || len(r.Submissions) != 3 {
		t.Fatalf("lần 3: %+v", r)
	}
	if v := r.row(t, f.item).Cells["H"]; v != "13" {
		t.Fatalf("rà soát dùng lần gửi cuối: H=%s", v)
	}
	c, b = f.reviewForm(p.ID, dTT, "returned", "Thêm nữa")
	f.fail(c, b, "extension.khtc_form_not_reviewable")
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", f.khtcUser, "", map[string]any{})
	f.ok(c, b, nil)
	k = f.deptForm(p.ID, dTT)
	if k.State != "locked" || k.Editable {
		t.Fatalf("kỳ đóng: %+v", k)
	}
	c, b = f.send(p.ID, dTT, "draft", 3, ttSheet(f.item, "14"))
	f.fail(c, b, "extension.khtc_form_locked")
}

// KH-FN-TI-02.04 / KH-UR-TI-05: KHTC xem kết quả 3 quy tắc (yêu cầu của mẫu, số âm phải có lý do, lũy kế > 100% KH), yêu
// cầu bổ sung kèm nội dung (phòng thấy ngay), xác nhận; phòng chuyên môn không rà soát được.
func TestKH_FN_TI_02_04_ReviewRulesAndRequest(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT)
	// (1) Mục lời văn bắt buộc của mẫu thiếu → chặn gửi.
	c, b := f.send(p.ID, dTT, "submit", 0, map[string]any{"cells": map[string]any{f.item: map[string]string{"done": "120"}}})
	f.fail(c, b, "extension.khtc_form_invalid")
	var pr struct {
		Issues []kfIssue `json:"issues"`
	}
	_ = json.Unmarshal(b, &pr)
	if !slices.ContainsFunc(pr.Issues, func(i kfIssue) bool { return i.Rule == "template" && i.Section == "cong_tac_khac" }) {
		t.Fatalf("thiếu kết quả kiểm tra trong lỗi: %s", b)
	}
	// (1) Số âm ở Thực hiện trong kỳ → chặn.
	c, b = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "-1"))
	f.fail(c, b, "extension.khtc_form_invalid")
	// (3) Lũy kế 120/100 → cảnh báo, vẫn gửi.
	c, b = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "120"))
	f.ok(c, b, nil)
	r := f.form(p.ID, dTT, authz.KNTinh, khtcDept)
	row := r.row(t, f.item)
	if !r.Valid || !r.has("over_plan", f.item, "H") || !row.OverPlan || row.Cells["H"] != "120" || row.Cells["I"] != "120" ||
		!r.CanRequestSupplement || !r.CanConfirm {
		t.Fatalf("rà soát: %+v", r)
	}
	c, b = f.do(http.MethodPost, formPath(p.ID, dTT)+"/review", authz.KNTinh, dTT, uuid.Nil, "", map[string]any{"decision": "accepted"})
	f.fail(c, b, "forbidden")
	c, b = f.reviewForm(p.ID, dTT, "returned", "")
	f.fail(c, b, "validation_failed")
	c, b = f.reviewForm(p.ID, dTT, "returned", "Lũy kế vượt kế hoạch — kiểm tra diện tích mô hình")
	f.ok(c, b, nil)
	if k := f.deptForm(p.ID, dTT); k.RequestNote != "Lũy kế vượt kế hoạch — kiểm tra diện tích mô hình" || k.State != "supplement" {
		t.Fatalf("phòng nhận yêu cầu: %+v", k)
	}
	c, b = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "80"))
	f.ok(c, b, nil)
	c, b = f.reviewForm(p.ID, dTT, "accepted", "")
	f.ok(c, b, nil)
	if k := f.deptForm(p.ID, dTT); k.State != "confirmed" || k.Editable {
		t.Fatalf("xác nhận: %+v", k)
	}
	c, b = f.reviewForm(p.ID, dTT, "returned", "Sau xác nhận")
	f.fail(c, b, "extension.khtc_form_not_reviewable")
}

// KH-FN-PN-03.02 (BR-DL-05, BR-DL-12): "Lũy kế kỳ trước" lấy từ kỳ KHTC đã đóng; "Điều chỉnh kỳ trước" bắt buộc chọn kỳ đã
// khóa và lý do (số âm không lý do → quy tắc 2), chỉ cộng vào lũy kế; hạng mục đã có số liệu chỉ ngừng, không bỏ khỏi Kế
// hoạch năm (W11.61 EntryUsage).
func TestKH_FN_PN_03_02_PriorPeriodAdjustment(t *testing.T) {
	f := newKHTCFormFx(t)
	w41 := f.open(41, dTT)
	c, b := f.send(w41.ID, dTT, "submit", 0, ttSheet(f.item, "30"))
	f.ok(c, b, nil)
	c, b = f.reviewForm(w41.ID, dTT, "accepted", "")
	f.ok(c, b, nil)
	c, b = f.khtc(http.MethodPost, "/periods/"+w41.ID+"/close", f.khtcUser, "", map[string]any{})
	f.ok(c, b, nil)
	f.clk.Advance(7 * 24 * time.Hour) // 21/10: tuần 42 (14/10–20/10)
	w42 := f.open(42, dTT)
	k := f.deptForm(w42.ID, dTT)
	if v := k.row(t, f.item).Cells["E"]; v != "30" {
		t.Fatalf("Lũy kế kỳ trước: %s", v)
	}
	if len(k.AdjPeriods) != 1 || k.AdjPeriods[0].ID != w41.ID {
		t.Fatalf("kỳ được điều chỉnh: %+v", k.AdjPeriods)
	}
	sheet := ttSheet(f.item, "10")
	sheet["adjustments"] = []map[string]any{{"entry_ref": f.item, "amount": "-5", "reason": "Ghi thừa tuần 41"}}
	c, b = f.send(w42.ID, dTT, "submit", 0, sheet)
	f.fail(c, b, "extension.khtc_form_invalid") // chưa chọn kỳ được điều chỉnh
	sheet["adjustments"] = []map[string]any{{"entry_ref": f.item, "period_id": w41.ID, "amount": "-5"}}
	c, b = f.send(w42.ID, dTT, "submit", 0, sheet)
	f.fail(c, b, "extension.khtc_form_invalid")
	var pr struct{ Issues []kfIssue }
	_ = json.Unmarshal(b, &pr)
	if !slices.ContainsFunc(pr.Issues, func(i kfIssue) bool { return i.Rule == "negative_reason" }) {
		t.Fatalf("số âm không lý do: %s", b)
	}
	sheet["adjustments"] = []map[string]any{{"entry_ref": f.item, "period_id": w41.ID, "amount": "-5", "reason": "Ghi thừa 5 ha ở tuần 41"}}
	c, b = f.send(w42.ID, dTT, "submit", 0, sheet)
	f.ok(c, b, &k)
	if r := k.row(t, f.item); r.Cells["F"] != "" && r.Cells["G"] != "-5" || r.Cells["H"] != "35" || r.Cells["I"] != "35" {
		t.Fatalf("lũy kế sau điều chỉnh: %+v", r.Cells)
	}
	// W11.61 EntryUsage: bỏ hạng mục đã có số liệu khỏi kế hoạch → chặn (chỉ ngừng theo dõi).
	body := map[string]any{"base_version": 1, "decision_no": "88/QĐ-SNN", "decision_date": "2026-10-20", "effective_from": "2026-10-21"}
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

// KH-FN-PN-03.02 (W11.63, mở #4): "Gửi KHTC" niêm phong bảng rồi nộp cap/forms; nộp lỗi giữa hai giao dịch để lại bảng mồ
// côi — bảng đó không làm hạng mục thành "đã có số liệu" (W11.61 EntryUsage) và không chặn gửi lại.
func TestKH_FN_PN_03_02_OrphanSealedSheetIgnored(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT)
	// Chốt kỳ native thẳng ở cap/forms (sổ KHTC vẫn mở) → bảng niêm phong được, bài nộp bị từ chối.
	c, b := f.do(http.MethodPost, "/v1/forms/periods/"+p.FormsPeriodID+"/close?profile=kn_khtc", authz.KNTinh, khtcDept, f.khtcUser, "", map[string]any{})
	f.ok(c, b, nil)
	c, b = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "30"))
	f.fail(c, b, "forms.period_closed")
	if k := f.deptForm(p.ID, dTT); k.Attempt != 0 || len(k.Submissions) != 0 {
		t.Fatalf("bảng mồ côi không phải lần gửi: %+v", k)
	}
	// Bảng mồ côi không tính "đã có số liệu": bỏ hạng mục khỏi kế hoạch (phiên bản 2) được.
	body := map[string]any{"base_version": 1, "decision_no": "90/QĐ-SNN", "decision_date": "2026-10-13", "effective_from": "2026-12-01"}
	var keep []map[string]any
	for _, e := range f.planBody["entries"].([]map[string]any) {
		if e["ref"] != f.item {
			keep = append(keep, e)
		}
	}
	body["entries"] = keep
	c, b = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", body)
	f.ok(c, b, nil)
	// Không chặn gửi lại: mở lại kỳ native, gửi lại → lần gửi 1.
	c, b = f.do(http.MethodPost, "/v1/forms/periods/"+p.FormsPeriodID+"/reopen?profile=kn_khtc", authz.KNTinh, khtcDept, f.khtcUser, "",
		map[string]any{"reason": "Chốt nhầm kỳ native khi kiểm thử"})
	f.ok(c, b, nil)
	var k kfForm
	c, b = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "30"))
	f.ok(c, b, &k)
	if k.State != "submitted" || k.Attempt != 1 {
		t.Fatalf("gửi lại: %+v", k)
	}
}

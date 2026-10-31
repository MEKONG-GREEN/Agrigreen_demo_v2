// Test-only native fixture; production interfaces remain unchanged.
package adhocintegration_test

import (
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
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
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/knadhoc"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
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
	nativeForms   formsport.Publisher
	nativeControl formsport.PeriodControl
	nativeEntry   formsport.Entry
	ext           *extension.Module
	*khtcFx
	khtcUser uuid.UUID
	item     string // TT "Mô hình lúa chất lượng cao" — 100 ha
	itemMod  string // TT "Diện tích gieo sạ" — nguồn module
	price    string // TTTT "Số phiếu giá cả đã thu thập" — nguồn module
	news     string // TTTT "Tin bài"
	planBody map[string]any
}

// newKHTCFormFxWith: wrapControl khác nil bọc cổng điều khiển kỳ native cap/forms (P0-POOL-SHARED-2: chặn/làm chậm CancelPeriod).
func newKHTCFormFxWith(t *testing.T, wrapControl func(formsport.PeriodControl) formsport.PeriodControl) *kfFx {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	kn := extension.New(pool, clk, extension.Deps{})
	fm, err := forms.New(pool, clk, forms.Deps{Profiles: []formsport.Profile{fixtureKHTCProfile},
		Validators: map[string]formsport.Validator{fixtureKHTCProfile.Name: kn.KHTCFormValidator()}})
	if err != nil {
		t.Fatal(err)
	}
	cw := &crosswalk{closed: true}
	control := fm.Control()
	if wrapControl != nil {
		control = wrapControl(control)
	}
	kn.UseKHTCPeriods(extension.KHTCDeps{Forms: fm.Publisher(), Control: control, Entry: fm.Entry(), Crosswalk: cw,
		MaxSubmissions: fixtureKHTCProfile.MaxSubmissions})
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
		if k.ID == uuid.Nil {
			k.ID = uuid.NewSHA1(base.tid, []byte(actor.String()+":"+k.Role+":"+k.Department))
		}

		ctx := tenant.WithID(c.Request.Context(), base.tid)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, bindKNTestSession(t, authz.Principal{ActorRef: actor, TenantID: base.tid, KN: &k, MFA: k.Role == authz.KNAdmin && c.GetHeader("X-Test-No-MFA") != "true"})))
		scope, _ := json.Marshal(k)
		c.Request = c.Request.WithContext(idempotency.WithAuthorizationScope(c.Request.Context(), scope))
	}, idempotency.Middleware(ops))
	fm.RegisterApp(g)
	kn.RegisterApp(g)
	f := &kfFx{khtcFx: base, khtcUser: ids.New(), ext: kn, nativeForms: fm.Publisher(), nativeControl: control, nativeEntry: fm.Entry()}
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

func (f *kfFx) open(no int, depts ...string) khtcPeriod {
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "week", "year": 2026, "no": no, "departments": depts})
	f.ok(c, b, &p)
	return p
}

func formPath(pid, dept string) string { return "/v1/extension/khtc/periods/" + pid + "/forms/" + dept }

// send gọi draft/submit của phòng; trả mã và thân.
func (f *kfFx) send(pid, dept, op string, base int64, sheet map[string]any) (int, []byte) {
	return f.do(http.MethodPost, formPath(pid, dept)+"/"+op, authz.KNTinh, dept, uuid.Nil, "", map[string]any{"base_attempt": base, "sheet": sheet})
}

func ttSheet(item, done string) map[string]any {
	return map[string]any{"cells": map[string]any{item: map[string]string{"done": done, "people": "30"}},
		"texts": map[string]string{"cong_tac_khac": "Tập huấn 2 lớp IPM"}}
}

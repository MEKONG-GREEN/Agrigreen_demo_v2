package kn77_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// cntsFx: kỳ native cap/forms của Phòng CNTS (profile kn_cnts) trên cùng tenant với vòng đời báo cáo Trồng trọt.
type cntsFx struct {
	srv         *httptest.Server
	pub         formsport.Publisher
	tinh, xa    caller
	tinhCtx     context.Context
	periodA     string // BM-T1, tuần 2026-W37, xã c1 + c2
	periodB     string // BM-T2, tuần 2026-W37, xã c1
	xaActorText string
}

const cntsDept = partyport.DeptChanNuoiThuySan

// cntsRuleTag là thẻ lịch CNTS của D8 (reportcycle/domain.CNTSRule.Tag(): Thứ Ba – Thứ Hai, chan_nuoi_thuy_san:v3) —
// gốc lắp ráp/kiểm thử chỉ import port/ (R-02) nên ghi chữ.
const cntsRuleTag = "kn.period_rule/v2;week_start=tue;week=year;month=week_end;cal=chan_nuoi_thuy_san:v3"

func setupCNTS(t *testing.T, f *fixture) *cntsFx {
	t.Helper()
	ctx := context.Background()
	// k_ref.org: Nút TTKN tỉnh 89 (bản chiếu của pf/party — view lấy tỉnh của kỳ native từ Nút phát hành).
	if err := tenant.WithTenantTx(ctx, f.env.AdminPool(t, migrations.PF), f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO k_ref.org (tenant_id, ref, kind, code, admin_unit_code, active, source_seq)
		    VALUES ($1, $2, 'ttkn_province', 'TTKN89', '89', true, 1)`, f.tenant, f.p89)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fm, err := forms.New(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), f.clk, forms.Deps{Profiles: platformasm.KNProfiles})
	if err != nil {
		t.Fatal(err)
	}
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 8<<20)
	eng.Use(logErrors(t))
	fm.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(),
		idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api"))))
	x := &cntsFx{srv: httptest.NewServer(eng), pub: fm.Publisher()}
	t.Cleanup(x.srv.Close)
	c1, c2 := f.communes89[0], f.communes89[1]
	x.tinh = f.kn(f.tenant, authz.KNTinh, cntsDept, f.p89, nil)
	xaActor := ids.New()
	x.xaActorText = xaActor.String()
	x.xa = f.knFor(f.tenant, xaActor, authz.KNXa, cntsDept, f.p89, []string{c1, c2}, nil)
	f.mu.Lock()
	tinhKC := *f.ctxs[x.tinh.ctxID]
	f.mu.Unlock()
	x.tinhCtx = authz.WithPrincipal(ctx, authz.Principal{ActorRef: x.tinh.actor, TenantID: f.tenant, KN: &tinhKC})
	adminCtx := authz.WithPrincipal(ctx, authz.Principal{ActorRef: ids.New(), TenantID: f.tenant,
		KN: &authz.KNContext{ID: ids.New(), Role: authz.KNAdmin, Scope: authz.Scope{All: true}}})
	fields := json.RawMessage(`[{"key":"heo","label":"Heo","type":"integer","required":true},
	  {"key":"heo_luy_ke","label":"Heo lũy kế","type":"integer"},{"key":"ty_le","label":"Tỷ lệ","type":"decimal"},
	  {"key":"ghi_chu","label":"Ghi chú","type":"text"}]`)
	week := []formsport.PeriodDef{{Label: "Tuần 37/2026", DueAt: f.clk.Now().Add(30 * 24 * time.Hour), Kind: "week", Key: "2026-W37",
		From: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), Rule: cntsRuleTag}}
	for _, p := range []struct {
		code  string
		units []string
		dst   *string
	}{{"BM-T1", []string{c1, c2}, &x.periodA}, {"BM-T2", []string{c1}, &x.periodB}} {
		ref, err := x.pub.DefineTemplate(adminCtx, formsport.TemplateDef{Profile: platformasm.KNCNTSProfile.Name, Code: p.code,
			Title: "Biểu kiểm thử " + p.code, Fields: fields}, "")
		if err != nil {
			t.Fatalf("cài biểu %s: %v", p.code, err)
		}
		out, err := x.pub.PublishBatch(x.tinhCtx, formsport.BatchInput{Profile: platformasm.KNCNTSProfile.Name, TemplateID: ref.ID,
			UnitKind: formsport.UnitCommune, Units: p.units, Periods: week}, "")
		if err != nil || len(out) != 1 {
			t.Fatalf("phát hành %s: %+v %v", p.code, out, err)
		}
		*p.dst = out[0].ID.String()
	}
	return x
}

func (x *cntsFx) post(who caller, p string, body any) resp {
	return who.do(x.srv, http.MethodPost, p, key(), body)
}

// TestPFPUB_ClosedFigures (ADR-0053): KHTC đọc số liệu đã chốt của HAI nguồn qua MỘT bộ đọc (extension/port.ClosedFigures)
// trên họ hợp đồng closed_figures/v1 — CNTS kỳ native cap/forms (pf_forms_pub, pf 20261013065757) và Trồng trọt vòng đời báo
// cáo cũ (pf_extension_pub, pf 20261013065758): chốt → thấy; mở lại → cờ về false, số cũ giữ; chốt lại → số mới; key_closed
// (Q-048) theo mọi biểu của khóa kỳ; cách ly tenant (RLS qua view); không dữ liệu cá nhân; vai r_extension đọc view
// _pub mà không đọc được bảng nội bộ của pf/forms.
func TestPFPUB_ClosedFigures(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	c.issue(t, "T01", "T05")
	x := setupCNTS(t, f)
	c1 := f.communes89[0]
	ctx := context.Background()
	reader := f.ext.ClosedFigures()
	read := func(q extport.ClosedFiguresQuery) []extport.ClosedFigure {
		t.Helper()
		out, err := reader.ClosedFigures(ctx, f.tenant, q)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	cnts := func() map[string]extport.ClosedFigure {
		t.Helper()
		m := map[string]extport.ClosedFigure{}
		for _, r := range read(extport.ClosedFiguresQuery{Department: cntsDept, Province: "89", PeriodType: "week", PeriodKeys: []string{"2026-W37"}}) {
			if r.Source != extport.ClosedSourceForms || r.ProvinceCode != "89" || r.WeekYear == nil || *r.WeekYear != 2026 {
				t.Fatalf("dòng CNTS: %+v", r)
			}
			m[r.FormCode] = r
		}
		if len(m) != 2 {
			t.Fatalf("CNTS W37: muốn 2 biểu, có %v", m)
		}
		return m
	}
	type unitRow struct {
		Key, Unit, UnitKind string
		Parent, Region      *string
		NilReport           bool `json:"nil_report"`
		Cells               map[string]string
	}
	unitCells := func(r extport.ClosedFigure, unit string) map[string]string {
		t.Helper()
		var rows []struct {
			Unit     string            `json:"unit"`
			UnitKind string            `json:"unit_kind"`
			Cells    map[string]string `json:"cells"`
		}
		if err := json.Unmarshal(r.Rows, &rows); err != nil {
			t.Fatalf("rows %s: %v", r.Rows, err)
		}
		for _, u := range rows {
			if u.Unit == unit {
				if u.UnitKind != "commune" {
					t.Fatalf("unit_kind: %+v", u)
				}
				return u.Cells
			}
		}
		return nil
	}

	// Chưa chốt: dòng có, cờ false, chưa có số đã chốt.
	m := cnts()
	if a := m["BM-T1"]; a.FormClosed || a.KeyClosed || a.KeyClosedAll || a.SnapshotID != nil || a.Rows != nil || a.ClosedAt != nil {
		t.Fatalf("trước chốt: %+v", a)
	}

	// Xã nộp (kèm trường văn bản có tên người — không được công bố), Tỉnh chốt BM-T1.
	zero := int64(0)
	x.xa.must(x.post(x.xa, "/v1/forms/periods/"+x.periodA+"/submissions?profile=kn_cnts", map[string]any{"unit": c1,
		"values": map[string]string{"heo": "10", "heo_luy_ke": "120", "ty_le": "1.50", "ghi_chu": "Ông Nguyễn Văn A báo"}, "base_version": zero}),
		http.StatusCreated, "xã nộp BM-T1")
	closePath := func(p string) string { return "/v1/forms/periods/" + p + "/close?profile=kn_cnts" }
	reopenPath := func(p string) string { return "/v1/forms/periods/" + p + "/reopen?profile=kn_cnts" }
	x.tinh.must(x.post(x.tinh, closePath(x.periodA), map[string]any{}), http.StatusOK, "chốt BM-T1")
	m = cnts()
	a := m["BM-T1"]
	if !a.FormClosed || !a.PeriodClosed || a.KeyClosed || a.KeyClosedAll || a.SnapshotID == nil || a.SnapshotNo == nil || *a.SnapshotNo != 1 ||
		a.Provisional == nil || !*a.Provisional || *a.UnitsTotal != 2 || *a.UnitsWithData != 1 || len(a.MissingUnits) != 1 || a.ClosedAt == nil {
		t.Fatalf("chốt BM-T1 (BM-T2 còn mở → key_closed false): %+v", a)
	}
	if got := unitCells(a, c1); len(got) != 3 || got["heo"] != "10" || got["heo_luy_ke"] != "120" || got["ty_le"] != "1.50" {
		t.Fatalf("ô số đã chốt (chỉ trường số, gồm lũy kế của nguồn): %v", got)
	}
	if string(a.Totals) != "[]" {
		t.Fatalf("kỳ native không tổng hợp: %s", a.Totals)
	}
	// Không dữ liệu cá nhân: không văn bản tự do, không mã người nộp/rà soát.
	for _, bad := range []string{"Nguyễn", "ghi_chu", x.xaActorText, "submitted_by", "reviewed_by", "submission_id"} {
		if strings.Contains(string(a.Rows), bad) {
			t.Fatalf("rows lộ %q: %s", bad, a.Rows)
		}
	}
	var parsed []unitRow
	if err := json.Unmarshal(a.Rows, &parsed); err != nil || len(parsed) != 1 || parsed[0].Key != c1 || parsed[0].Parent != nil || parsed[0].NilReport {
		t.Fatalf("dạng dòng hợp đồng: %s %v", a.Rows, err)
	}

	// Chốt BM-T2 (không bài) → mọi biểu của khóa W37 đã chốt.
	x.tinh.must(x.post(x.tinh, closePath(x.periodB), map[string]any{}), http.StatusOK, "chốt BM-T2")
	m = cnts()
	for code, r := range m {
		if !r.FormClosed || !r.KeyClosed || !r.KeyClosedAll {
			t.Fatalf("%s sau chốt mọi biểu: %+v", code, r)
		}
	}
	closed := m["BM-T1"]

	// Mở lại BM-T1 cho xã c1 → cờ về false (cả key_closed của BM-T2); số đã chốt giữ nguyên dù xã nộp lại.
	x.tinh.must(x.post(x.tinh, reopenPath(x.periodA), map[string]any{"units": []string{c1}, "reason": "Xã nhập nhầm số heo tuần 37"}),
		http.StatusOK, "mở lại BM-T1")
	one := int64(1)
	x.xa.must(x.post(x.xa, "/v1/forms/periods/"+x.periodA+"/submissions?profile=kn_cnts", map[string]any{"unit": c1,
		"values": map[string]string{"heo": "15", "heo_luy_ke": "125"}, "base_version": one}), http.StatusCreated, "xã nộp lại khi mở")
	m = cnts()
	if r := m["BM-T1"]; r.FormClosed || r.PeriodClosed || r.KeyClosed || *r.SnapshotID != *closed.SnapshotID || unitCells(r, c1)["heo"] != "10" ||
		!r.ClosedAt.Equal(*closed.ClosedAt) {
		t.Fatalf("đang mở lại: %+v", r)
	}
	if r := m["BM-T2"]; !r.FormClosed || r.KeyClosed || r.KeyClosedAll {
		t.Fatalf("BM-T2 khi BM-T1 mở lại: %+v", r)
	}
	// Nguồn gốc (W11.64): băm số chỉ đổi khi số đã chốt đổi — đang mở lại vẫn là băm lúc chốt.
	if r := m["BM-T1"]; closed.FiguresHash == nil || r.FiguresHash == nil || *r.FiguresHash != *closed.FiguresHash ||
		r.ConfirmationStatus != extport.ConfirmationConfirmed || r.SourceModule != "pf/forms" || closed.StatusHash == nil {
		t.Fatalf("băm khi mở lại: %+v", r)
	}

	// Chốt lại → ảnh chụp mới, số mới, cờ trở lại.
	f.clk.Advance(time.Hour)
	x.tinh.must(x.post(x.tinh, closePath(x.periodA), map[string]any{}), http.StatusOK, "chốt lại BM-T1")
	m = cnts()
	if r := m["BM-T1"]; !r.FormClosed || !r.KeyClosed || !r.KeyClosedAll || *r.SnapshotID == *closed.SnapshotID || *r.SnapshotNo != 2 ||
		unitCells(r, c1)["heo"] != "15" || !r.ClosedAt.After(*closed.ClosedAt) || *r.FiguresHash == *closed.FiguresHash ||
		!strings.Contains(string(r.Rows), `"confirmation": "confirmed"`) {
		t.Fatalf("sau chốt lại: %+v", r)
	}

	// Danh tính lịch nguồn (D8) và quy đổi sang tuần KHTC (crosswalk v1, ADR-0053 §6): kỳ CNTS 08/09–14/09 quy vào tuần KHTC
	// 2026-W37 (09/09 – 15/09, chứa ngày cuối 14/09) → Converted; đã chốt; Sum = số đã chốt.
	// Kỳ CNTS theo lịch D8 Thứ Ba – Thứ Hai (08/09 – 14/09, thẻ mang cal=chan_nuoi_thuy_san:v3) → phiên bản 3.
	if r := m["BM-T1"]; r.CalendarID != cntsDept || r.CalendarVersion == nil || *r.CalendarVersion != 3 || r.PeriodKey != "2026-W37" {
		t.Fatalf("danh tính lịch nguồn: %+v", r)
	}
	conv, err := reader.Converted(ctx, f.tenant, extport.ClosedFiguresQuery{Department: cntsDept}, extport.CrosswalkTarget{})
	if err != nil || len(conv) != 2 {
		t.Fatalf("quy đổi: %+v %v", conv, err)
	}
	for _, o := range conv {
		if o.TargetKey != "2026-W37" || !o.Converted || !o.Closed || !o.KeyClosed || o.RuleVersion != extport.CrosswalkV1 || len(o.Sources) != 1 {
			t.Fatalf("kỳ KHTC: %+v", o)
		}
		if o.FormCode == "BM-T1" && (o.Sum["heo"] != "15" || o.Last["heo_luy_ke"] != "125") {
			t.Fatalf("số quy đổi: %+v", o)
		}
	}

	// Trồng trọt (vòng đời báo cáo cũ) qua CÙNG bộ đọc và cùng cột: xã gửi, Tỉnh chốt kỳ 1 (tuần W37).
	tid := str(c.task(t, c.xa1, "T01", c1, c.p1), "id")
	c.xa1.must(submit(c.xa1, tid, "submitted", map[string]string{"F": "20"}), http.StatusCreated, "gửi T01")
	tt := func() map[string]extport.ClosedFigure {
		t.Helper()
		out := map[string]extport.ClosedFigure{}
		for _, r := range read(extport.ClosedFiguresQuery{Department: dept, PeriodKeys: []string{"2026-W37"}}) {
			if r.Source != extport.ClosedSourceReportCycle || r.SourcePeriodID.String() != c.p1 {
				t.Fatalf("dòng Trồng trọt: %+v", r)
			}
			out[r.FormCode] = r
		}
		return out
	}
	if r := tt()["T01"]; r.FormClosed || r.SnapshotID != nil || r.Rows != nil {
		t.Fatalf("Trồng trọt trước chốt: %+v", r)
	}
	c.tinh.must(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/close", map[string]any{}), http.StatusOK, "chốt kỳ Trồng trọt")
	ttm := tt()
	r01 := ttm["T01"]
	if len(ttm) != 2 || !r01.FormClosed || !r01.PeriodClosed || !r01.KeyClosed || !r01.KeyClosedAll || r01.SnapshotID == nil || r01.SnapshotNo == nil ||
		r01.ProvinceCode != "89" || r01.PeriodType != "week" || r01.CalendarID != dept || r01.CalendarVersion == nil || *r01.CalendarVersion != 1 || r01.WeekYear == nil || *r01.WeekYear != 2026 {
		t.Fatalf("Trồng trọt sau chốt: %+v", ttm)
	}
	if got := unitCells(r01, c1); got["F"] != "20" {
		t.Fatalf("ô Trồng trọt qua cột chung: %v (%s)", got, r01.Rows)
	}
	var totals []map[string]any
	if err := json.Unmarshal(r01.Totals, &totals); err != nil || totalCell(totals, "province", "", "M") != "20" {
		t.Fatalf("dòng Toàn tỉnh của nguồn: %s %v", r01.Totals, err)
	}

	// Port KHTC (ADR-0053 §7): một kỳ KHTC 2026-W37 (Thứ Tư 09/09 – Thứ Ba 15/09) gồm Trồng trọt (vòng đời báo cáo,
	// tuần nguồn 07/09 – 13/09), CNTS (kỳ native), Báo giá (pf_price_pub — chưa có bảng giá duyệt → Missing).
	ps, err := f.ext.KHTCSources().Periods(ctx, f.tenant, extport.KHTCQuery{Province: "89", From: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
		To: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)})
	if err != nil || len(ps) != 1 || ps[0].TargetKey != "2026-W37" || !ps[0].Closed || len(ps[0].Departments) != 3 {
		t.Fatalf("kỳ KHTC: %+v %v", ps, err)
	}
	for _, df := range ps[0].Departments {
		switch df.DepartmentCode {
		case cntsDept:
			if !df.Closed || df.SourceModule != "pf/forms" || len(df.Forms) != 2 {
				t.Fatalf("CNTS: %+v", df)
			}
		case dept:
			tf := df.Forms[0]
			if !df.Closed || df.SourceModule != "pf/extension" || len(df.Forms) != 2 || !tf.Converted || tf.FiguresHash == "" ||
				tf.Sources[0].ClosedAt == nil || tf.Sources[0].FiguresHash == nil || tf.Sources[0].CalendarID != dept {
				t.Fatalf("Trồng trọt: %+v", df)
			}
		case "thong_tin_truyen_thong":
			// View giá có (W11.76, đọc thật qua r_price_pub) nhưng tenant chưa có bảng giá duyệt → Missing; không kỳ nguồn
			// nào quy vào → coi là đã chốt (KHTCQA, D17) nên kỳ KHTC chốt khi Trồng trọt và CNTS đã chốt.
			if df.Unavailable || !df.Missing || !df.Closed || df.SourceModule != "pf/price" {
				t.Fatalf("Báo giá: %+v", df)
			}
		default:
			t.Fatalf("phòng lạ: %+v", df)
		}
	}

	// Không lọc phòng: cả hai nguồn trong một lần đọc; lọc biểu chỉ lọc dòng trả về.
	all := read(extport.ClosedFiguresQuery{PeriodKeys: []string{"2026-W37"}})
	srcs := map[string]int{}
	for _, r := range all {
		srcs[r.Source]++
	}
	if srcs[extport.ClosedSourceForms] != 2 || srcs[extport.ClosedSourceReportCycle] != 2 {
		t.Fatalf("hai nguồn: %v", srcs)
	}
	if only := read(extport.ClosedFiguresQuery{Department: cntsDept, FormCodes: []string{"BM-T2"}}); len(only) != 1 || !only[0].KeyClosedAll {
		t.Fatalf("lọc biểu: %+v", only)
	}

	// Cách ly tenant: tenant khác → 0 dòng; đọc view không đặt tenant (RLS bảng gốc qua view) → 0 dòng.
	if other := read0(t, reader, ids.New()); len(other) != 0 {
		t.Fatalf("tenant khác thấy %d dòng", len(other))
	}
	for _, v := range []string{"pf_forms_pub.v1_closed_figures", "pf_extension_pub.v1_closed_figures"} {
		if n := f.count(`SELECT count(*) FROM ` + v); n != 0 {
			t.Fatalf("%s không tenant: %d dòng", v, n)
		}
	}

	// Vai r_extension (giao dịch của module): đọc view _pub của pf/forms qua thành viên r_forms_pub, KHÔNG đọc được bảng
	// nội bộ pf_forms (42501).
	extDB := shareddb.ForModule(f.extPool, extension.DBModule)
	err = tenant.WithTenantTx(ctx, extDB, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "SELECT 1 FROM pf_forms.period LIMIT 1")
		return e
	})
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "42501" {
		t.Fatalf("r_extension phải bị từ chối đọc pf_forms.period: %v", err)
	}
}

func read0(t *testing.T, r extport.ClosedFigures, tid uuid.UUID) []extport.ClosedFigure {
	t.Helper()
	out, err := r.ClosedFigures(context.Background(), tid, extport.ClosedFiguresQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

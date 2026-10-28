package kn77_test

// W11.44 — Tổng hợp CNTS Cấp Tỉnh trên module THẬT qua HTTP: pf/forms (kỳ native kn_cnts — BM-01, BM-17 nộp/chốt) +
// pf/extension (/v1/extension/cnts/bm02|bm18|aggregates|shares, sổ pf 20261013230931) + view công bố
// pf_extension_pub.v1_closed_figures nhánh cnts_aggregate (pf 20261013230936, họ closed_figures/v1 — ADR-0053) và bộ đọc KHTC trên PostgreSQL.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
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

type provinceOrgs struct{}

func (provinceOrgs) Get(_ context.Context, ref uuid.UUID) (partyport.Org, error) {
	return partyport.Org{Ref: ref, Kind: partyport.OrgTTKNProvince, AdminUnitCode: "89", Active: true}, nil
}

type facilityDir map[uuid.UUID]string

func (f facilityDir) FacilityCommunes(_ context.Context, refs []uuid.UUID, _ time.Time) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	for _, r := range refs {
		if c, ok := f[r]; ok {
			out[r] = c
		}
	}
	return out, nil
}

type aggFx struct {
	*khtcFx
	admin *pgtestAdmin
	kn    *extension.Module
}

type pgtestAdmin struct {
	query func(sql string, args ...any) [][]any
	exec  func(sql string) error
}

func (f *aggFx) adminExec(sql string) (struct{}, error) { return struct{}{}, f.admin.exec(sql) }

const aggDept = partyport.DeptChanNuoiThuySan

// newAggFx dựng pf/forms + pf/extension thật; wire (tùy chọn) nối thêm cổng cho gói khác (W11.50 chiến dịch điều tra).
func newAggFx(t *testing.T, facs facilityDir, wire ...func(*forms.Module, *extension.Module)) *aggFx {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	adm := env.AdminPool(t, migrations.PF)
	// 10:00 Thứ Tư 07/10/2026 giờ VN: tuần CNTS 40 (29/09–05/10) đã kết thúc, hạn 12:00 06/10 đã qua (gửi trễ có giải trình).
	clk := clock.NewFixed(time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC))
	fm, err := forms.New(pool, clk, forms.Deps{Profiles: []formsport.Profile{platformasm.KNCNTSProfile}, Facilities: facs})
	if err != nil {
		t.Fatal(err)
	}
	kn := extension.New(pool, clk, extension.Deps{Orgs: provinceOrgs{}})
	kn.UseFormsPublisher(fm.Publisher())
	kn.UseFormsFigures(fm.Figures())
	for _, w := range wire {
		w(fm, kn)
	}
	f := &khtcFx{t: t, clk: clk, tid: ids.New(), node: ids.New()}
	f.eng = httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 8<<20)
	g := f.eng.Group("/v1", func(c *gin.Context) {
		var k authz.KNContext
		_ = json.Unmarshal([]byte(c.GetHeader("X-Test-KN")), &k)
		actor, _ := uuid.Parse(c.GetHeader("X-Test-Actor"))
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindKNTestSession(t, authz.Principal{ActorRef: actor, TenantID: f.tid, KN: &k})))
	}, idempotency.Middleware(ops))
	fm.RegisterApp(g)
	kn.RegisterApp(g)
	// View công bố có chủ NOBYPASSRLS → đọc trong giao dịch có app.tenant_id của tenant kiểm thử.
	q := func(sql string, args ...any) [][]any {
		ctx := context.Background()
		tx, err := adm.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.tid.String()); err != nil {
			t.Fatal(err)
		}
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out [][]any
		for rows.Next() {
			v, err := rows.Values()
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, v)
		}
		return out
	}
	ex := func(sql string) error {
		_, err := adm.Exec(context.Background(), sql)
		return err
	}
	return &aggFx{khtcFx: f, admin: &pgtestAdmin{query: q, exec: ex}, kn: kn}
}

// call gọi API với phiên Cấp Tỉnh/Xã CNTS (phạm vi X1, X2) hoặc Admin.
func (f *aggFx) call(method, path, role string, body any) (int, []byte) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	k := authz.KNContext{Role: role, Department: aggDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}}
	if role == authz.KNAdmin {
		k = authz.KNContext{Role: role, Scope: authz.Scope{All: true}}
	}
	if role == authz.KNXa {
		k.OrgRef = ids.New()
	}
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	req.Header.Set("X-Test-Actor", ids.New().String())
	if method == http.MethodPost {
		req.Header.Set(idempotency.Header, "t-"+ids.New().String())
	}
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

type series struct {
	Periods []struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	} `json:"periods"`
}

func (f *aggFx) publish(code, from, to string, units ...string) series {
	var s series
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-series", authz.KNTinh, map[string]any{"code": code, "from_date": from,
		"to_date": to, "units": units})
	f.ok(c, b, &s)
	return s
}

func (f *aggFx) submit(pid, unit string, values map[string]string) {
	c, b := f.call(http.MethodPost, "/v1/forms/periods/"+pid+"/submissions?profile=kn_cnts", authz.KNXa, map[string]any{"unit": unit,
		"values": values, "explanation": "Gửi bổ sung sau hạn do mất mạng"})
	f.ok(c, b, nil)
}

func (f *aggFx) closePeriod(pid string) {
	c, b := f.call(http.MethodPost, "/v1/forms/periods/"+pid+"/close?profile=kn_cnts", authz.KNTinh, nil)
	f.ok(c, b, nil)
}

type cols struct{ Prev, Cur, QTD, YTD string }

type bm18 struct {
	Week     struct{ Key string } `json:"week"`
	Prev     *struct{ Key string }
	Communes []struct {
		CommuneCode  string          `json:"commune_code"`
		Cells        map[string]cols `json:"cells"`
		Reported     bool            `json:"reported"`
		MissingWeeks []string        `json:"missing_weeks"`
	} `json:"communes"`
	Catch      map[string]cols `json:"catch"`
	AquaOutput cols            `json:"aqua_output"`
	Total      cols            `json:"total"`
	Sources    struct {
		Count     int  `json:"count"`
		Closed    int  `json:"closed"`
		AllClosed bool `json:"all_closed"`
	} `json:"sources"`
	Aggregate struct {
		State      string `json:"state"`
		CloseCount int    `json:"close_count"`
		Changed    bool   `json:"changed"`
	} `json:"aggregate"`
}

func catchVals(ca, tom, muc, khac string) map[string]string {
	return map[string]string{"ca": ca, "tom": tom, "muc": muc, "khac": khac}
}

func TestCNTSAggregate_BM18_PL2_CloseShare_W1144(t *testing.T) {
	f := newAggFx(t, facilityDir{})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	// BM-17 tuần 39 (22/09–28/09, quý III) và 40 (29/09–05/10, ngày cuối thuộc quý IV) cho X1, X2.
	s := f.publish("CNTS-BM17", "2026-09-22", "2026-10-05", "X1", "X2")
	if len(s.Periods) != 2 || s.Periods[0].Key != "2026-W39" || s.Periods[1].Key != "2026-W40" {
		t.Fatalf("kỳ BM-17: %+v", s.Periods)
	}
	w39, w40 := s.Periods[0].ID, s.Periods[1].ID
	f.submit(w39, "X1", catchVals("2", "1", "0", "0"))
	f.submit(w39, "X2", catchVals("3", "0", "0", "0.5"))
	f.submit(w40, "X1", catchVals("4", "0", "1", "0"))

	t.Run("bảng Phụ lục 2: tuần trước, trong kỳ, lũy kế quý/năm theo ngày cuối tuần, xã thiếu", func(t *testing.T) {
		var v bm18
		c, b := f.call(http.MethodGet, "/v1/extension/cnts/bm18?week=2026-W40", authz.KNTinh, nil)
		f.ok(c, b, &v)
		if v.Prev == nil || v.Prev.Key != "2026-W39" || v.Sources.Count != 1 || v.Sources.AllClosed {
			t.Fatalf("nguồn: %s", b)
		}
		tot := v.Catch["tong"]
		if tot.Cur != "5" || tot.Prev != "6.5" || tot.QTD != "5" || tot.YTD != "11.5" {
			t.Fatalf("khai thác: %+v", tot)
		}
		for _, cm := range v.Communes {
			if cm.CommuneCode == "X2" && (cm.Reported || len(cm.MissingWeeks) != 1) {
				t.Fatalf("X2 thiếu tuần 40: %+v", cm)
			}
		}
	})

	t.Run("nhập nuôi trồng, tổng sản lượng = khai thác + nuôi trồng (ngọc trai không cộng tấn)", func(t *testing.T) {
		var v bm18
		c, b := f.call(http.MethodPost, "/v1/extension/cnts/bm18/aquaculture", authz.KNTinh, map[string]any{"week": "2026-W40",
			"entries": []map[string]any{{"object": "ca_tra", "area": "10", "output": "7.25"}, {"object": "ngoc_trai", "output": "3"}}})
		f.ok(c, b, &v)
		if v.AquaOutput.Cur != "7.25" || v.Total.Cur != "12.25" {
			t.Fatalf("tổng: %+v %+v", v.AquaOutput, v.Total)
		}
		c, b = f.call(http.MethodPost, "/v1/extension/cnts/bm18/aquaculture", authz.KNTinh, map[string]any{"week": "2026-W40",
			"entries": []map[string]any{{"object": "khong_co", "output": "1"}}})
		f.fail(c, b, "validation_failed")
		c, b = f.call(http.MethodPost, "/v1/extension/cnts/bm18/aquaculture", authz.KNXa, map[string]any{"week": "2026-W40",
			"entries": []map[string]any{{"object": "ca_tra", "output": "1"}}})
		if c != http.StatusForbidden {
			t.Fatalf("Cấp Xã không nhập nuôi trồng: %d %s", c, b)
		}
	})

	t.Run("chốt bảng: chặn khi nguồn chưa chốt (D17); chốt → view công bố có lũy kế của nguồn (D18)", func(t *testing.T) {
		c, b := f.call(http.MethodPost, "/v1/extension/cnts/aggregates/close", authz.KNTinh, map[string]any{"form": "CNTS-BM18", "period_key": "2026-W40"})
		f.fail(c, b, "extension.cnts_aggregate_sources_open")
		f.closePeriod(w39)
		f.closePeriod(w40)
		c, b = f.call(http.MethodPost, "/v1/extension/cnts/aggregates/close", authz.KNTinh, map[string]any{"form": "CNTS-BM18", "period_key": "2026-W40"})
		f.ok(c, b, nil)
		rows := f.admin.query(`SELECT calendar_id, calendar_version, period_type, form_closed, key_closed, snapshot_no, provisional,
		    (SELECT sum((r->'cells'->>'tong_san_luong')::numeric) FROM jsonb_array_elements(rows) r)::text,
		    (SELECT sum((r->'cells'->>'tong_san_luong_luy_ke_nam')::numeric) FROM jsonb_array_elements(rows) r)::text,
		    (SELECT count(*) FROM jsonb_array_elements(rows) r WHERE r->>'confirmation' = 'confirmed'), source_module, figures_hash
		  FROM pf_extension_pub.v1_closed_figures WHERE tenant_id = $1 AND source = 'cnts_aggregate' AND form_code = 'CNTS-BM18' AND period_key = '2026-W40'`, f.tid)
		if len(rows) != 1 {
			t.Fatalf("view: %v", rows)
		}
		r := rows[0]
		if r[0] != "chan_nuoi_thuy_san" || r[1] != int32(3) || r[2] != "week" || r[3] != true || r[4] != true || r[5] != int64(1) ||
			r[6] != true || r[10] != "pf/extension" {
			t.Fatalf("cột view: %v", r)
		}
		if r[7] != "12.25" || r[8] != "18.75" {
			t.Fatalf("Σ tổng sản lượng (kỳ, lũy kế năm): %v %v", r[7], r[8])
		}
		// Bộ đọc KHTC (PFPUB — ClosedFigures/KHTCSources) thấy bảng tổng hợp qua cùng họ closed_figures/v1, không đổi mã.
		figs, err := f.kn.ClosedFigures().ClosedFigures(context.Background(), f.tid, extport.ClosedFiguresQuery{
			Department: aggDept, PeriodType: "week", PeriodKeys: []string{"2026-W40"}, FormCodes: []string{"CNTS-BM18"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(figs) != 1 || figs[0].Source != "cnts_aggregate" || !figs[0].FormClosed || !bytes.Contains(figs[0].Rows, []byte(`"tong_san_luong_luy_ke_nam"`)) {
			t.Fatalf("bộ đọc KHTC: %+v", figs)
		}
		// Bảng đã chốt: nhập nuôi trồng bị chặn.
		c, b = f.call(http.MethodPost, "/v1/extension/cnts/bm18/aquaculture", authz.KNTinh, map[string]any{"week": "2026-W40",
			"entries": []map[string]any{{"object": "ca_tra", "output": "1"}}})
		f.fail(c, b, "extension.cnts_aggregate_closed")
		c, b = f.call(http.MethodPost, "/v1/extension/cnts/aggregates/close", authz.KNTinh, map[string]any{"form": "CNTS-BM18", "period_key": "2026-W40"})
		f.fail(c, b, "extension.cnts_aggregate_state")
	})

	t.Run("mở lại: cờ về false, số cũ vẫn công bố; chốt lại → snapshot_no 2", func(t *testing.T) {
		c, b := f.call(http.MethodPost, "/v1/extension/cnts/aggregates/reopen", authz.KNTinh, map[string]any{"form": "CNTS-BM18",
			"period_key": "2026-W40", "reason": "Bổ sung số cá tra tuần 40"})
		f.ok(c, b, nil)
		rows := f.admin.query(`SELECT form_closed, snapshot_no FROM pf_extension_pub.v1_closed_figures WHERE tenant_id = $1 AND source = 'cnts_aggregate'`, f.tid)
		if len(rows) != 1 || rows[0][0] != false || rows[0][1] != int64(1) {
			t.Fatalf("mở lại: %v", rows)
		}
		c, b = f.call(http.MethodPost, "/v1/extension/cnts/bm18/aquaculture", authz.KNTinh, map[string]any{"week": "2026-W40",
			"entries": []map[string]any{{"object": "ca_tra", "area": "10", "output": "8"}}})
		f.ok(c, b, nil)
		c, b = f.call(http.MethodPost, "/v1/extension/cnts/aggregates/close", authz.KNTinh, map[string]any{"form": "CNTS-BM18", "period_key": "2026-W40"})
		f.ok(c, b, nil)
		rows = f.admin.query(`SELECT form_closed, snapshot_no FROM pf_extension_pub.v1_closed_figures WHERE tenant_id = $1 AND source = 'cnts_aggregate'`, f.tid)
		if rows[0][0] != true || rows[0][1] != int64(2) {
			t.Fatalf("chốt lại: %v", rows)
		}
		// Sổ chỉ ghi thêm.
		if _, err := f.adminExec(`UPDATE pf_extension.cnts_aqua_entry SET output = 0`); err == nil {
			t.Fatal("sổ nuôi trồng phải chặn UPDATE")
		}
	})

	t.Run("phiên bản tệp Chi cục: số không đổi → giữ phiên bản; Excel có TỔNG + 4 sheet khai thác", func(t *testing.T) {
		var r struct {
			Share struct {
				ID      string `json:"id"`
				Version int64  `json:"version"`
				Basis   string `json:"basis"`
			} `json:"share"`
			Replayed bool `json:"replayed"`
		}
		c, b := f.call(http.MethodPost, "/v1/extension/cnts/shares", authz.KNTinh, map[string]any{"form": "CNTS-BM18", "period_type": "week", "period_key": "2026-W40"})
		f.ok(c, b, &r)
		if c != http.StatusCreated || r.Share.Version != 1 || r.Share.Basis != "closed" {
			t.Fatalf("phiên bản 1: %d %s", c, b)
		}
		c, b = f.call(http.MethodPost, "/v1/extension/cnts/shares", authz.KNTinh, map[string]any{"form": "CNTS-BM18", "period_type": "week", "period_key": "2026-W40"})
		f.ok(c, b, &r)
		if !r.Replayed || r.Share.Version != 1 {
			t.Fatalf("số không đổi phải giữ phiên bản: %s", b)
		}
		c, b = f.call(http.MethodGet, "/v1/extension/cnts/shares/"+r.Share.ID+"/xlsx", authz.KNTinh, nil)
		if c != http.StatusOK {
			t.Fatalf("Excel: %d %s", c, b)
		}
		x, err := excelize.OpenReader(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = x.Close() }()
		if sh := x.GetSheetList(); len(sh) != 5 || sh[0] != "TỔNG" {
			t.Fatalf("sheet: %v", sh)
		}
	})
}

func TestCNTSAggregate_BM02_Adjustment_W1144(t *testing.T) {
	facA, facB := ids.New(), ids.New()
	f := newAggFx(t, facilityDir{facA: "X1", facB: "X2"})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	// BM-01 ngày 01–02/10/2026 (tuần KSGM 1 tháng 10: 01–07).
	s := f.publish("CNTS-BM01", "2026-10-01", "2026-10-02", facA.String(), facB.String())
	if len(s.Periods) != 2 {
		t.Fatalf("kỳ BM-01: %+v", s.Periods)
	}
	sp := func(heo string) map[string]string {
		return map[string]string{"trau_bo": "1", "heo": heo, "gia_cam": "10", "de": "0"}
	}
	f.submit(s.Periods[0].ID, facA.String(), sp("20"))
	f.submit(s.Periods[1].ID, facA.String(), sp("15"))
	f.submit(s.Periods[0].ID, facB.String(), sp("5"))

	type view struct {
		Total map[string]string `json:"total"`
		Rows  []struct {
			FacilityRef string            `json:"facility_ref"`
			Total       map[string]string `json:"total"`
			MissingDays int               `json:"missing_days"`
			Stale       bool              `json:"stale"`
		} `json:"rows"`
		Adjustments []struct {
			Stale bool `json:"stale"`
		} `json:"adjustments"`
	}
	var v view
	c, b = f.call(http.MethodGet, "/v1/extension/cnts/bm02?key=2026-10-K1", authz.KNTinh, nil)
	f.ok(c, b, &v)
	if v.Total["heo"] != "40" || len(v.Rows) != 2 {
		t.Fatalf("BM-02: %s", b)
	}
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/bm02/adjustments", authz.KNTinh, map[string]any{"week_key": "2026-10-K1",
		"facility_ref": facA.String(), "deltas": map[string]string{"heo": "-3"}, "reason": "Trùng số ca đêm 01/10"})
	f.ok(c, b, nil)
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/bm02/adjustments", authz.KNTinh, map[string]any{"week_key": "2026-10-K1",
		"facility_ref": facA.String(), "deltas": map[string]string{"heo": "1"}, "reason": "ngắn"})
	f.fail(c, b, "validation_failed")
	c, b = f.call(http.MethodGet, "/v1/extension/cnts/bm02?key=2026-10", authz.KNTinh, nil)
	f.ok(c, b, &v)
	if v.Total["heo"] != "37" {
		t.Fatalf("tháng sau điều chỉnh: %s", b)
	}
	for _, r := range v.Rows {
		if r.FacilityRef == facB.String() && r.MissingDays != 1 {
			t.Fatalf("cơ sở B thiếu 1 ngày: %+v", r)
		}
	}
	// BM-01 đổi sau điều chỉnh → "cần xem lại".
	f.submit(s.Periods[1].ID, facA.String(), sp("16"))
	c, b = f.call(http.MethodGet, "/v1/extension/cnts/bm02?key=2026-10-K1", authz.KNTinh, nil)
	f.ok(c, b, &v)
	if len(v.Adjustments) != 1 || !v.Adjustments[0].Stale {
		t.Fatalf("cần xem lại: %s", b)
	}
	// Chốt BM-02 tuần: nguồn chưa chốt → chặn; chốt nguồn → chốt được; điều chỉnh sau chốt → chặn.
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/aggregates/close", authz.KNTinh, map[string]any{"form": "CNTS-BM02", "period_key": "2026-10-K1"})
	f.fail(c, b, "extension.cnts_aggregate_sources_open")
	f.closePeriod(s.Periods[0].ID)
	f.closePeriod(s.Periods[1].ID)
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/aggregates/close", authz.KNTinh, map[string]any{"form": "CNTS-BM02", "period_key": "2026-10-K1"})
	f.ok(c, b, nil)
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/bm02/adjustments", authz.KNTinh, map[string]any{"week_key": "2026-10-K1",
		"facility_ref": facA.String(), "deltas": map[string]string{"heo": "1"}, "reason": "Điều chỉnh sau khi chốt"})
	f.fail(c, b, "extension.cnts_aggregate_closed")
	rows := f.admin.query(`SELECT period_type, (SELECT sum((r->'cells'->>'heo')::numeric) FROM jsonb_array_elements(rows) r)::text
	  FROM pf_extension_pub.v1_closed_figures WHERE tenant_id = $1 AND source = 'cnts_aggregate' AND form_code = 'CNTS-BM02'`, f.tid)
	if len(rows) != 1 || rows[0][0] != "ksgm_week" || rows[0][1] != "38" {
		t.Fatalf("BM-02 công bố: %v", rows)
	}
	// Tệp Chi cục tháng (chưa chốt cả 4 tuần → số đã gửi).
	var r struct {
		Share struct {
			ID    string `json:"id"`
			Basis string `json:"basis"`
		} `json:"share"`
	}
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/shares", authz.KNTinh, map[string]any{"form": "CNTS-BM02", "period_type": "month", "period_key": "2026-10"})
	f.ok(c, b, &r)
	if r.Share.Basis != "submitted" {
		t.Fatalf("cơ sở số liệu tháng: %s", b)
	}
	c, _ = f.call(http.MethodGet, "/v1/extension/cnts/shares/"+r.Share.ID+"/xlsx", authz.KNTinh, nil)
	if c != http.StatusOK {
		t.Fatalf("Excel BM-02: %d", c)
	}
}

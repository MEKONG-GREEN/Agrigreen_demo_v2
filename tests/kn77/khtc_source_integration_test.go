package kn77_test

// W11.64 — "Cập nhật số liệu" trên module THẬT qua HTTP: pf/extension (/v1/extension/khtc/periods/{id}/sources*, sổ
// pf 20261013175705) + Kỳ KHTC (W11.62) + Kế hoạch năm (W11.61) + pf/forms trên PostgreSQL. Port KHTCSources nạp bộ dữ
// liệu đối chứng KHTCQA (tests/golden/khtc, ca G03 — sửa lùi lũy kế) qua đúng bảng chuyển đổi PFPUB (closedfig.Convert,
// Assemble). Kiểm: chỉ cán bộ phụ trách bấm (⚑3), ảnh chụp bất biến + lịch sử cũ → mới khớp golden G03 s3, "Nguồn đã
// thay đổi" theo figures_hash (không theo status_hash), tuần đã đóng giữ số và làm gốc (frozen), kỳ đã đóng không cập nhật.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/closedfig"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
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
	khtcgolden "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/golden/khtc"
)

// goldenReader là port KHTCSources giả: kỳ nguồn Trồng trọt (PL01, ô lua_td/luy_ke) theo một bước của golden.
type goldenReader struct {
	mu      sync.Mutex
	periods []khtcgolden.SourcePeriod
	status  string // đổi → chỉ status_hash đổi (nhận HTX — D2)
}

func (g *goldenReader) set(ps []khtcgolden.SourcePeriod, status string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.periods, g.status = ps, status
}

func hexSHA(s string) *string {
	h := sha256.Sum256([]byte(s))
	x := hex.EncodeToString(h[:])
	return &x
}

func (g *goldenReader) Periods(_ context.Context, _ uuid.UUID, q extport.KHTCQuery) ([]extport.KHTCPeriod, error) {
	g.mu.Lock()
	ps, status := g.periods, g.status
	g.mu.Unlock()
	cal, err := rc.CalendarOf(rc.DeptKeHoachTaiChinh, q.Target.RuleTag)
	if err != nil {
		return nil, err
	}
	t := extport.CrosswalkTarget{Version: extport.CrosswalkV1, CalendarID: cal.ID, RuleTag: cal.Rule.Tag(), PeriodType: rc.PeriodWeek}
	var figs []extport.ClosedFigure
	for _, p := range ps {
		span, err := rc.SpanOfKey(rc.DefaultRule, rc.PeriodWeek, p.Key, nil)
		if err != nil {
			return nil, err
		}
		from, to := span.From, span.To
		ver, sn := rc.CalendarVersionDefault, int64(1)
		rows := `[{"key":"30001","unit":"30001","indicator":"LUA_TD","cells":{"luy_ke":"` + p.Cumulative.String() + `"},"confirmation":"confirmed"}]`
		figs = append(figs, extport.ClosedFigure{DepartmentCode: partyport.DeptTrongTrot, CalendarID: partyport.DeptTrongTrot, CalendarVersion: &ver,
			PeriodType: rc.PeriodWeek, PeriodKey: p.Key, PeriodFrom: &from, PeriodTo: &to, SourceModule: "pf/extension",
			SourcePeriodID: uuid.NewSHA1(uuid.NameSpaceURL, []byte(p.Key)), FormCode: "PL01", FormClosed: p.Closed, PeriodClosed: p.Closed,
			KeyClosed: p.Closed, KeyClosedAll: p.Closed, ClosedAt: &to, SnapshotNo: &sn, Rows: json.RawMessage(rows), Totals: json.RawMessage("[]"),
			ConfirmationStatus: extport.ConfirmationConfirmed, FiguresHash: hexSHA(p.Key + "=" + p.Cumulative.String()),
			StatusHash: hexSHA(p.Key + "=" + status)})
	}
	conv, err := closedfig.Convert(figs, t)
	if err != nil {
		return nil, err
	}
	first, _ := cal.Rule.PeriodOf(rc.PeriodWeek, q.From, nil)
	last, _ := cal.Rule.PeriodOf(rc.PeriodWeek, q.To, nil)
	return closedfig.Assemble(conv, t, cal, first.From, last.To, q.Departments, false, false), nil
}

type srcStatus struct {
	OwnerRef      string `json:"owner_ref"`
	CanRefresh    bool   `json:"can_refresh"`
	SourceChanged bool   `json:"source_changed"`
	Live          bool   `json:"live"`
	Snapshot      *struct {
		No       int64  `json:"no"`
		ActorRef string `json:"actor_ref"`
		Closed   bool   `json:"closed"`
		Target   string `json:"target_calendar"`
		Rows     int    `json:"row_count"`
	} `json:"snapshot"`
	AsOf []struct {
		Department string   `json:"department"`
		FormCode   *string  `json:"form_code"`
		NativeKeys []string `json:"native_keys"`
		ClosedAt   *string  `json:"closed_at"`
		Closed     bool     `json:"closed"`
	} `json:"as_of"`
	Values []struct {
		SourceKey   string  `json:"source_key"`
		Invalid     bool    `json:"invalid"`
		Cumulative  *string `json:"cumulative"`
		Base        *string `json:"base"`
		BaseRef     string  `json:"base_ref"`
		Increment   *string `json:"increment"`
		Closed      bool    `json:"closed"`
		Provisional bool    `json:"provisional"`
		Entries     []struct {
			Name string `json:"name"`
		} `json:"entries"`
	} `json:"values"`
	Changes []struct {
		Kind      string `json:"kind"`
		NativeKey string `json:"native_key"`
	} `json:"changes"`
}

type srcHistory struct {
	Items []struct {
		Snapshot struct {
			No       int64  `json:"no"`
			ActorRef string `json:"actor_ref"`
		} `json:"snapshot"`
		Changes []struct {
			SourceKey string `json:"source_key"`
			Field     string `json:"field"`
			Old       string `json:"old"`
			New       string `json:"new"`
		} `json:"changes"`
	} `json:"items"`
}

// tlog chuyển log lỗi của engine vào log kiểm thử (chẩn đoán HTTP 500).
type tlog struct{ t *testing.T }

func (w tlog) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }

const keyLuaTD = "season:trong_trot:pl01:lua_td:luy_ke"

func (s srcStatus) value(t *testing.T, key string) (cum, base, inc decimal.Decimal, ref string, closed bool) {
	t.Helper()
	for _, v := range s.Values {
		if v.SourceKey == key && v.Cumulative != nil {
			return decimal.RequireFromString(*v.Cumulative), decimal.RequireFromString(*v.Base), decimal.RequireFromString(*v.Increment), v.BaseRef, v.Closed
		}
	}
	t.Fatalf("không có số của %s: %+v", key, s.Values)
	return
}

func TestKH_UR_TI_13_RefreshOwnerOnlyDiffHistory(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	// 15:00 Thứ Tư 16/12/2026 giờ VN — golden G03 s1.
	clk := clock.NewFixed(time.Date(2026, 12, 16, 8, 0, 0, 0, time.UTC))
	fm, err := forms.New(pool, clk, forms.Deps{Profiles: []formsport.Profile{platformasm.KNKHTCProfile}})
	if err != nil {
		t.Fatal(err)
	}
	kn := extension.New(pool, clk, extension.Deps{})
	kn.UseKHTCPeriods(extension.KHTCDeps{Forms: fm.Publisher(), Control: fm.Control(), MaxSubmissions: platformasm.KNKHTCProfile.MaxSubmissions})
	src := &goldenReader{}
	kn.UseKHTCSourceReader(src)
	f := &khtcFx{t: t, clk: clk, tid: ids.New(), node: ids.New()}
	f.eng = httpx.NewEngine(slog.New(slog.NewTextHandler(tlog{t}, nil)), 8<<20)
	g := f.eng.Group("/v1", func(c *gin.Context) {
		var k authz.KNContext
		_ = json.Unmarshal([]byte(c.GetHeader("X-Test-KN")), &k)
		actor, _ := uuid.Parse(c.GetHeader("X-Test-Actor"))
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.Principal{ActorRef: actor, TenantID: f.tid, KN: &k}))
	}, idempotency.Middleware(ops))
	fm.RegisterApp(g)
	kn.RegisterApp(g)

	ds, err := khtcgolden.Load()
	if err != nil {
		t.Fatal(err)
	}
	g03, _ := ds.Case("G03")
	w48 := khtcgolden.SourcePeriod{Key: "2026-W48", To: "2026-12-01", Cumulative: decimal.RequireFromString("3050"), Closed: true}
	step := func(i int) []khtcgolden.SourcePeriod {
		return append([]khtcgolden.SourcePeriod{w48}, g03.Steps[i].SourcePeriods...)
	}

	me, other := ids.New(), ids.New()
	tt := partyport.DeptTrongTrot
	c, b := f.do(http.MethodPost, "/v1/extension/khtc/form-template/install", authz.KNAdmin, "", uuid.Nil, "", nil)
	f.ok(c, b, nil)
	c, b = f.do(http.MethodPost, "/v1/kn/khtc/catalogs/field", authz.KNAdmin, "", uuid.Nil, "", map[string]any{"code": "trong_trot", "name": "Trồng trọt"})
	f.ok(c, b, nil)
	var prog struct{ Ref string }
	c, b = f.do(http.MethodPost, "/v1/kn/khtc/programs", authz.KNTinh, khtcDept, me, "",
		map[string]any{"code": "CT-I", "name": "Chương trình KN trồng trọt", "kind": "program", "field_code": "trong_trot"})
	f.ok(c, b, &prog)
	p1, i1, i2 := ids.New().String(), ids.New().String(), ids.New().String()
	c, b = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, me, "", map[string]any{"base_version": 0,
		"entries": []map[string]any{
			{"ref": p1, "tab": "programs", "kind": "program", "program_ref": prog.Ref, "sort_order": 1},
			{"ref": i1, "tab": "programs", "kind": "item", "parent_ref": p1, "name": "Diện tích xuống giống lúa Thu Đông", "department_code": tt,
				"data_source": "module", "source_key": keyLuaTD},
			{"ref": i2, "tab": "programs", "kind": "item", "parent_ref": p1, "name": "Khóa nguồn cũ", "department_code": tt,
				"data_source": "module", "source_key": "cnts.demo_points"},
		}})
	f.ok(c, b, nil)

	open := func(no int) string {
		var p khtcPeriod
		c, b := f.khtc(http.MethodPost, "/periods", me, "", map[string]any{"report_type": "week", "year": 2026, "no": no, "departments": []string{tt}})
		f.ok(c, b, &p)
		return p.ID
	}
	pw48, pw49, pw50 := open(48), open(49), open(50)
	status := func(id string, role, dept string, actor uuid.UUID) srcStatus {
		t.Helper()
		var s srcStatus
		c, b := f.do(http.MethodGet, "/v1/extension/khtc/periods/"+id+"/sources", role, dept, actor, "", nil)
		f.ok(c, b, &s)
		return s
	}
	refresh := func(id string, actor uuid.UUID, idem string) srcStatus {
		t.Helper()
		var s srcStatus
		c, b := f.khtc(http.MethodPost, "/periods/"+id+"/sources/refresh", actor, idem, nil)
		f.ok(c, b, &s)
		return s
	}

	t.Run("KH-QD-03 chưa chụp: mốc dữ liệu từ port, chưa có số", func(t *testing.T) {
		src.set(step(0), "a")
		s := status(pw49, authz.KNTinh, khtcDept, me)
		if s.Snapshot != nil || !s.Live || !s.CanRefresh || s.OwnerRef != me.String() || len(s.AsOf) != 1 || s.AsOf[0].NativeKeys[0] != "2026-W49" {
			t.Fatalf("trước khi chụp: %+v", s)
		}
	})

	t.Run("⚑3 chỉ cán bộ phụ trách kỳ bấm", func(t *testing.T) {
		c, b := f.khtc(http.MethodPost, "/periods/"+pw49+"/sources/refresh", other, "", nil)
		f.fail(c, b, "extension.khtc_refresh_not_owner")
		c, b = f.do(http.MethodPost, "/v1/extension/khtc/periods/"+pw49+"/sources/refresh", authz.KNTinh, tt, me, "", nil)
		f.fail(c, b, "forbidden")
		if s := status(pw49, authz.KNAdmin, "", uuid.Nil); s.CanRefresh {
			t.Fatal("Admin xem, không bấm")
		}
	})

	t.Run("KH-QD-04 tuần 48 đã đóng giữ số 3.050", func(t *testing.T) {
		src.set(step(0), "a")
		s := refresh(pw48, me, "")
		if cum, _, _, _, _ := s.value(t, keyLuaTD); !cum.Equal(decimal.NewFromInt(3050)) {
			t.Fatalf("W48: %+v", s.Values)
		}
		c, b := f.khtc(http.MethodPost, "/periods/"+pw48+"/close", me, "", map[string]any{"reason": "Phòng chưa gửi phiếu, đóng ngoại lệ tuần 48"})
		f.ok(c, b, nil)
		c, b = f.khtc(http.MethodPost, "/periods/"+pw48+"/sources/refresh", me, "", nil)
		f.fail(c, b, "extension.khtc_period_terminal")
	})

	t.Run("KH-QD-02 G03 s1: gốc frozen/current, trong kỳ 180 và 170", func(t *testing.T) {
		s := refresh(pw49, me, "refresh-w49-s1")
		cum, base, inc, ref, closed := s.value(t, keyLuaTD)
		if !cum.Equal(decimal.NewFromInt(3230)) || !base.Equal(decimal.NewFromInt(3050)) || !inc.Equal(decimal.NewFromInt(180)) ||
			ref != "frozen:2026-W48" || !closed || s.Snapshot == nil || s.Snapshot.No != 1 || s.Snapshot.ActorRef != me.String() ||
			s.Snapshot.Target != "ke_hoach_tai_chinh:v2" {
			t.Fatalf("W49 s1: %+v", s)
		}
		for _, v := range s.Values {
			if v.SourceKey == "cnts.demo_points" && (!v.Invalid || !v.Provisional || v.Cumulative != nil || len(v.Entries) != 1) {
				t.Fatalf("khóa nguồn sai dạng: %+v", v)
			}
		}
		if again := refresh(pw49, me, "refresh-w49-s1"); again.Snapshot.No != 1 {
			t.Fatalf("gửi lặp cùng Idempotency-Key: %+v", again.Snapshot)
		}
		s = refresh(pw50, me, "")
		if _, base, inc, ref, _ := s.value(t, keyLuaTD); !base.Equal(decimal.NewFromInt(3230)) || !inc.Equal(decimal.NewFromInt(170)) || ref != "current:2026-W49" {
			t.Fatalf("W50 s1: %+v", s.Values)
		}
	})

	t.Run("BR-DL-04 nhận HTX (chỉ status_hash) và mở lại không phải nguồn đổi", func(t *testing.T) {
		src.set(step(0), "b")
		if s := status(pw49, authz.KNTinh, khtcDept, me); s.SourceChanged || len(s.Changes) != 0 {
			t.Fatalf("status_hash đổi không cảnh báo: %+v", s.Changes)
		}
		src.set(step(1), "a") // s2: W49 mở lại, KHTC vẫn thấy ảnh chụp 3.230
		if s := status(pw49, authz.KNTinh, khtcDept, me); s.SourceChanged {
			t.Fatalf("mở lại chưa chốt lại: %+v", s.Changes)
		}
	})

	t.Run("BR-DL-04 G03 s3: nguồn chốt lại 3.190 → cảnh báo, không ghi đè; cập nhật → lịch sử khớp golden", func(t *testing.T) {
		src.set(step(2), "a")
		s := status(pw49, authz.KNTinh, khtcDept, me)
		if !s.SourceChanged || len(s.Changes) != 1 || s.Changes[0].Kind != "figures" || s.Changes[0].NativeKey != "2026-W49" {
			t.Fatalf("nguồn đã thay đổi: %+v", s.Changes)
		}
		if cum, _, _, _, _ := s.value(t, keyLuaTD); !cum.Equal(decimal.NewFromInt(3230)) {
			t.Fatal("cảnh báo không ghi đè ảnh chụp")
		}
		f.clk.Advance(24 * time.Hour) // 17/12 15:00 — G03 s3
		if s = refresh(pw49, me, ""); s.SourceChanged || s.Snapshot.No != 2 {
			t.Fatalf("sau cập nhật: %+v", s)
		}
		refresh(pw50, me, "")
		type line struct{ week, field, old, new string }
		var got []line
		for _, pw := range []struct{ id, week string }{{pw49, "2026-W49"}, {pw50, "2026-W50"}} {
			var h srcHistory
			c, b := f.khtc(http.MethodGet, "/periods/"+pw.id+"/sources/history", uuid.Nil, "", nil)
			f.ok(c, b, &h)
			if len(h.Items) != 2 || h.Items[0].Snapshot.No != 2 || h.Items[0].Snapshot.ActorRef != me.String() || len(h.Items[1].Changes) != 0 {
				t.Fatalf("lịch sử %s: %s", pw.week, b)
			}
			for _, ch := range h.Items[0].Changes {
				if ch.SourceKey == keyLuaTD {
					got = append(got, line{pw.week, ch.Field, ch.Old, ch.New})
				}
			}
		}
		want := g03.Steps[2].Diff
		if len(got) != len(want) {
			t.Fatalf("lịch sử %+v, golden %+v", got, want)
		}
		for i, d := range want {
			if got[i].week != d.Week || got[i].field != d.Field || !decimal.RequireFromString(got[i].old).Equal(d.Old) ||
				!decimal.RequireFromString(got[i].new).Equal(d.New) {
				t.Errorf("dòng %d: %+v, golden %+v", i, got[i], d)
			}
		}
	})
}

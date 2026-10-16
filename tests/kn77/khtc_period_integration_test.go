package kn77_test

// W11.62 — Kỳ KHTC trên module THẬT qua HTTP: pf/extension (/v1/extension/khtc/*, sổ pf 20261013120542) + pf/forms
// (kỳ native kn_khtc, /v1/forms/* — phòng nộp, KHTC rà soát) trên PostgreSQL, middleware Idempotency-Key thật.
// Tên kiểm thử con theo nhóm KH2 của BRD KHTC v2.2 (Mục II.4, II.5, FN-TI-02.03).

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

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
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

// crosswalk giả của gói PFPUB: một kỳ nguồn CNTS cho mỗi tuần KHTC, trạng thái chốt điều khiển được.
type crosswalk struct{ closed bool }

func (c *crosswalk) Sources(_ context.Context, kind, key string, _, _ time.Time, depts []string) ([]extport.KHTCSourceRef, error) {
	for _, d := range depts {
		if d == partyport.DeptChanNuoiThuySan && kind == "week" {
			return []extport.KHTCSourceRef{{Department: d, Source: "cnts.bm04", CalendarID: "cnts.week", CalendarVersion: "v1",
				NativeKey: key, Label: "BM-04 " + key}}, nil
		}
	}
	return nil, nil
}

func (c *crosswalk) States(_ context.Context, refs []extport.KHTCSourceRef) ([]extport.KHTCSourceState, error) {
	out := make([]extport.KHTCSourceState, len(refs))
	for i := range refs {
		out[i].Closed = c.closed
	}
	return out, nil
}

type khtcPeriod struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	State          string    `json:"state"`
	FormsPeriodID  string    `json:"forms_period_id"`
	FormsStatus    string    `json:"forms_status"`
	DueAt          time.Time `json:"due_at"`
	MaxSubmissions int       `json:"max_submissions"`
	CanClose       bool      `json:"can_close"`
	CloseForced    bool      `json:"close_forced"`
	Counts         struct {
		Sources int `json:"sources"`
	} `json:"counts"`
	Forms []struct {
		Department           string `json:"department"`
		State                string `json:"state"`
		Attempt              int64  `json:"attempt"`
		LimitReached         bool   `json:"limit_reached"`
		CanRequestSupplement bool   `json:"can_request_supplement"`
		CanConfirm           bool   `json:"can_confirm"`
		Late                 bool   `json:"late"`
	} `json:"forms"`
	Events []struct {
		Action string `json:"action"`
		Forced bool   `json:"forced"`
	} `json:"events"`
	// nháp
	Duplicate       *struct{ ID string } `json:"duplicate"`
	Sources         []json.RawMessage    `json:"sources"`
	TemplateVersion int64                `json:"template_version"`
}

func (p khtcPeriod) form(t *testing.T, dept string) (string, int64, bool, bool, bool) {
	t.Helper()
	for _, f := range p.Forms {
		if f.Department == dept {
			return f.State, f.Attempt, f.LimitReached, f.CanRequestSupplement, f.CanConfirm
		}
	}
	t.Fatalf("không có phiếu %s: %+v", dept, p.Forms)
	return "", 0, false, false, false
}

type khtcFx struct {
	t    *testing.T
	clk  *clock.Fixed
	cw   *crosswalk
	eng  *gin.Engine
	tid  uuid.UUID
	node uuid.UUID
}

func newKHTCFx(t *testing.T) *khtcFx {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	// 08:00 Thứ Tư 14/10/2026 giờ VN — tuần 41 (07/10–13/10) vừa kết thúc, hạn nộp 10:00 hôm nay.
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	fm, err := forms.New(pool, clk, forms.Deps{Profiles: []formsport.Profile{platformasm.KNKHTCProfile}})
	if err != nil {
		t.Fatal(err)
	}
	kn := extension.New(pool, clk, extension.Deps{})
	cw := &crosswalk{}
	kn.UseKHTCPeriods(extension.KHTCDeps{Forms: fm.Publisher(), Control: fm.Control(), Crosswalk: cw,
		MaxSubmissions: platformasm.KNKHTCProfile.MaxSubmissions})
	f := &khtcFx{t: t, clk: clk, cw: cw, tid: ids.New(), node: ids.New()}
	f.eng = httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 8<<20)
	g := f.eng.Group("/v1", func(c *gin.Context) {
		var k authz.KNContext
		_ = json.Unmarshal([]byte(c.GetHeader("X-Test-KN")), &k)
		actor, _ := uuid.Parse(c.GetHeader("X-Test-Actor"))
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.Principal{ActorRef: actor, TenantID: f.tid, KN: &k}))
	}, idempotency.Middleware(ops))
	fm.RegisterApp(g)
	kn.RegisterApp(g)
	return f
}

// do gọi API như phiên (vai, phòng ban, người); idem rỗng → khóa mới. Trả mã HTTP và thân.
func (f *khtcFx) do(method, path, role, dept string, actor uuid.UUID, idem string, body any) (int, []byte) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	requestCtx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()
	req = req.WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	k := authz.KNContext{Role: role, Department: dept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}}
	if role == authz.KNAdmin {
		k = authz.KNContext{Role: role, Scope: authz.Scope{All: true}}
	}
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	if actor == uuid.Nil {
		actor = ids.New()
	}
	req.Header.Set("X-Test-Actor", actor.String())
	if method == http.MethodPost {
		if idem == "" {
			idem = "t-" + ids.New().String()
		}
		req.Header.Set(idempotency.Header, idem)
	}
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func (f *khtcFx) ok(code int, b []byte, out any) {
	f.t.Helper()
	if code >= 300 {
		f.t.Fatalf("HTTP %d: %s", code, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *khtcFx) fail(code int, b []byte, want string) {
	f.t.Helper()
	var p struct{ Code string }
	_ = json.Unmarshal(b, &p)
	if p.Code != want {
		f.t.Fatalf("muốn %s, có HTTP %d %s", want, code, b)
	}
}

const khtcDept = partyport.DeptKeHoachTaiChinh

func (f *khtcFx) khtc(method, path string, actor uuid.UUID, idem string, body any) (int, []byte) {
	return f.do(method, "/v1/extension/khtc"+path, authz.KNTinh, khtcDept, actor, idem, body)
}

func (f *khtcFx) get(id string) khtcPeriod {
	var p khtcPeriod
	c, b := f.khtc(http.MethodGet, "/periods/"+id, uuid.Nil, "", nil)
	f.ok(c, b, &p)
	return p
}

func (f *khtcFx) submit(fid, dept string) string {
	var s struct{ ID string }
	c, b := f.do(http.MethodPost, "/v1/forms/periods/"+fid+"/submissions?profile=kn_khtc", authz.KNTinh, dept, uuid.Nil, "",
		map[string]any{"unit": dept, "values": map[string]string{"ket_qua": "Tập huấn 3 lớp"}})
	f.ok(c, b, &s)
	return s.ID
}

func (f *khtcFx) review(subID, decision string, khtc uuid.UUID) {
	c, b := f.do(http.MethodPost, "/v1/forms/submissions/"+subID+"/review?profile=kn_khtc", authz.KNTinh, khtcDept, khtc, "",
		map[string]any{"decision": decision, "note": "Bổ sung số liệu lớp tập huấn"})
	f.ok(c, b, nil)
}

func TestKHTCPeriod_W1162(t *testing.T) {
	f := newKHTCFx(t)
	me := ids.New()
	tt, cn := partyport.DeptTrongTrot, partyport.DeptChanNuoiThuySan
	week41 := map[string]any{"report_type": "week", "year": 2026, "no": 41, "departments": []string{tt, cn}}
	var p khtcPeriod

	t.Run("KH2 quyền và phiếu cơ sở", func(t *testing.T) {
		c, b := f.do(http.MethodPost, "/v1/extension/khtc/periods", authz.KNTinh, tt, uuid.Nil, "", week41)
		f.fail(c, b, "forbidden")
		c, b = f.khtc(http.MethodPost, "/periods", me, "", week41)
		f.fail(c, b, "forms.template_not_found")
		c, b = f.do(http.MethodPost, "/v1/extension/khtc/form-template/install", authz.KNAdmin, "", uuid.Nil, "", nil)
		f.ok(c, b, nil)
	})

	t.Run("KH2 mở kỳ: tên kỳ, hạn 10:00 Thứ Tư, một phiếu mỗi phòng, nguồn, không trùng", func(t *testing.T) {
		var dr khtcPeriod
		c, b := f.khtc(http.MethodPost, "/periods/preview", me, "", week41)
		f.ok(c, b, &dr)
		if dr.Name != "Tuần 41 năm 2026 (từ ngày 07/10/2026 đến ngày 13/10/2026)" || dr.MaxSubmissions != 3 || len(dr.Sources) != 1 ||
			dr.TemplateVersion == 0 || dr.Duplicate != nil {
			t.Fatalf("nháp: %s", b)
		}
		if got := dr.DueAt.In(clock.Zone()).Format("2006-01-02 15:04"); got != "2026-10-14 10:00" {
			t.Fatalf("hạn nộp KHTC: %s", got)
		}
		c, b = f.khtc(http.MethodPost, "/periods", me, "open-w41-key", week41)
		f.ok(c, b, &p)
		st, _, _, _, _ := p.form(t, tt)
		if p.State != "opened" || len(p.Forms) != 2 || st != "not_started" || p.Counts.Sources != 1 || p.MaxSubmissions != 3 {
			t.Fatalf("mở kỳ: %s", b)
		}
		var again khtcPeriod
		c, b = f.khtc(http.MethodPost, "/periods", me, "open-w41-key", week41)
		f.ok(c, b, &again)
		if again.ID != p.ID {
			t.Fatalf("gọi lặp: %s ≠ %s", again.ID, p.ID)
		}
		c, b = f.khtc(http.MethodPost, "/periods", me, "", week41)
		f.fail(c, b, "extension.khtc_period_duplicate")
	})

	t.Run("KH2 phiếu: yêu cầu bổ sung, lần gửi 3 tự khóa", func(t *testing.T) {
		s1 := f.submit(p.FormsPeriodID, tt)
		p = f.get(p.ID)
		if _, _, _, sup, _ := p.form(t, tt); p.State != "collecting" || !sup {
			t.Fatalf("sau lần 1: %s %+v", p.State, p.Forms)
		}
		f.review(s1, "returned", me)
		if st, _, _, _, _ := f.get(p.ID).form(t, tt); st != "supplement" {
			t.Fatalf("yêu cầu bổ sung: %s", st)
		}
		s2 := f.submit(p.FormsPeriodID, tt)
		f.review(s2, "returned", me)
		f.submit(p.FormsPeriodID, tt)
		p = f.get(p.ID)
		if _, n, lim, sup, conf := p.form(t, tt); n != 3 || !lim || sup || !conf {
			t.Fatalf("lần 3: n=%d lim=%v sup=%v conf=%v", n, lim, sup, conf)
		}
		c, b := f.do(http.MethodPost, "/v1/forms/periods/"+p.FormsPeriodID+"/submissions?profile=kn_khtc", authz.KNTinh, tt, uuid.Nil, "",
			map[string]any{"unit": tt, "values": map[string]string{"ket_qua": "lần 4"}})
		f.fail(c, b, "forms.submission_limit")
	})

	t.Run("KH2 đủ dữ liệu khi mọi phiếu gửi và nguồn chốt, đóng kỳ", func(t *testing.T) {
		f.submit(p.FormsPeriodID, cn)
		if p = f.get(p.ID); p.State != "collecting" {
			t.Fatalf("nguồn chưa chốt: %s", p.State)
		}
		f.cw.closed = true
		if p = f.get(p.ID); p.State != "complete" || !p.CanClose || p.CloseForced {
			t.Fatalf("đủ dữ liệu: %s", p.State)
		}
		c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", me, "", map[string]any{})
		f.ok(c, b, &p)
		if st, _, _, _, _ := p.form(t, tt); p.State != "closed" || p.FormsStatus != "closed" || st != "locked" {
			t.Fatalf("đóng kỳ: %s", b)
		}
		c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", me, "", map[string]any{"reason": "Mở nhầm loại báo cáo"})
		f.fail(c, b, "extension.khtc_period_terminal")
		// BR-DL-13 (W11.61 móc ClosedCutoff): Kế hoạch năm không được có ngày hiệu lực không sau ngày chốt 13/10 của kỳ đã đóng.
		c, b = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, me, "",
			map[string]any{"base_version": 0, "effective_from": "2026-10-01", "entries": []any{}})
		f.fail(c, b, "extension.khtc_effective_date_invalid")
	})

	t.Run("KH2 đóng ngoại lệ khi Trễ hạn ≥ 3 ngày, lý do bắt buộc", func(t *testing.T) {
		var m khtcPeriod
		c, b := f.khtc(http.MethodPost, "/periods", me, "", map[string]any{"report_type": "month", "year": 2026, "no": 10, "departments": []string{tt, cn}})
		f.ok(c, b, &m)
		f.submit(m.FormsPeriodID, tt)
		c, b = f.khtc(http.MethodPost, "/periods/"+m.ID+"/close", me, "", map[string]any{})
		f.fail(c, b, "extension.khtc_period_not_closable")
		f.clk.Advance(18 * 24 * time.Hour) // 01/11: quá hạn 28/10 10:00 hơn 3 ngày
		if m = f.get(m.ID); m.State != "late" || !m.CloseForced {
			t.Fatalf("trễ hạn: %s %v", m.State, m.CloseForced)
		}
		c, b = f.khtc(http.MethodPost, "/periods/"+m.ID+"/close", me, "", map[string]any{})
		f.fail(c, b, "validation_failed")
		c, b = f.khtc(http.MethodPost, "/periods/"+m.ID+"/close", me, "", map[string]any{"reason": "Phòng CNTS chưa gửi phiếu sau 4 ngày quá hạn"})
		f.ok(c, b, &m)
		if st, _, _, _, _ := m.form(t, cn); st != "not_sent" || !m.Events[len(m.Events)-1].Forced {
			t.Fatalf("đóng ngoại lệ: %s", b)
		}
	})

	t.Run("KH2 sửa hạn nộp + D8: bản offline nhận sau hạn cũ, trước hạn mới không trễ; hủy kỳ, mở lại đúng kỳ đã hủy", func(t *testing.T) {
		// Đồng hồ đang ở 01/11 08:00 (giờ VN). Tuần 45: 04/11–10/11, hạn gốc 11/11 10:00.
		w45 := map[string]any{"report_type": "week", "year": 2026, "no": 45, "departments": []string{tt, cn}}
		var k khtcPeriod
		c, b := f.khtc(http.MethodPost, "/periods", me, "", w45)
		f.ok(c, b, &k)
		oldDue := k.DueAt
		newDue := oldDue.Add(48 * time.Hour) // 13/11 10:00
		c, b = f.khtc(http.MethodPost, "/periods/"+k.ID+"/deadline", me, "", map[string]any{"due_at": newDue, "reason": "Sở lùi hạn báo cáo"})
		f.ok(c, b, &k)
		if !k.DueAt.Equal(newDue) {
			t.Fatalf("sửa hạn: %v", k.DueAt)
		}
		f.clk.Advance(11 * 24 * time.Hour) // 12/11 08:00: sau hạn gốc, trước hạn mới
		// Bản ghi trên thiết bị lúc 10/11 (trước hạn gốc), đồng bộ lên máy chủ 12/11 (sau hạn gốc, trước hạn mới): D8 tính trễ
		// theo giờ máy chủ nhận bài, W11.62 so với hạn hiệu lực → không trễ.
		c, b = f.do(http.MethodPost, "/v1/forms/periods/"+k.FormsPeriodID+"/submissions?profile=kn_khtc", authz.KNTinh, tt, uuid.Nil, "",
			map[string]any{"unit": tt, "values": map[string]string{"ket_qua": "Bản offline"},
				"captured_at": oldDue.Add(-24 * time.Hour).Format(time.RFC3339)})
		f.ok(c, b, nil)
		k = f.get(k.ID)
		for _, fr := range k.Forms {
			if fr.Department == tt && fr.Late {
				t.Fatalf("gửi trước hạn mới bị tính trễ: %+v", fr)
			}
			if fr.Department == cn && fr.State != "not_started" {
				t.Fatalf("phòng chưa gửi trước hạn mới phải Chưa nhập: %+v", fr)
			}
		}
		if k.State != "collecting" {
			t.Fatalf("trước hạn mới: %s", k.State)
		}
		f.clk.Advance(2 * 24 * time.Hour) // 14/11 08:00: sau hạn mới → trễ, kể cả bản ghi trên thiết bị trước hạn mới (D8)
		c, b = f.do(http.MethodPost, "/v1/forms/periods/"+k.FormsPeriodID+"/submissions?profile=kn_khtc", authz.KNTinh, cn, uuid.Nil, "",
			map[string]any{"unit": cn, "values": map[string]string{"ket_qua": "Bản offline muộn"},
				"captured_at": newDue.Add(-24 * time.Hour).Format(time.RFC3339)})
		f.ok(c, b, nil)
		for _, fr := range f.get(k.ID).Forms {
			if fr.Department == cn && !fr.Late {
				t.Fatalf("gửi sau hạn mới phải trễ: %+v", fr)
			}
		}
		c, b = f.khtc(http.MethodPost, "/periods/"+k.ID+"/cancel", me, "", map[string]any{"reason": "nhầm"})
		f.fail(c, b, "validation_failed")
		c, b = f.khtc(http.MethodPost, "/periods/"+k.ID+"/cancel", me, "", map[string]any{"reason": "Mở nhầm tuần báo cáo"})
		f.ok(c, b, &k)
		if st, _, _, _, _ := k.form(t, tt); k.State != "cancelled" || k.FormsStatus != "cancelled" || st != "withdrawn" {
			t.Fatalf("hủy kỳ: %s", b)
		}
		c, b = f.khtc(http.MethodPost, "/periods", me, "", w45)
		f.ok(c, b, nil)
		var list struct{ Items []khtcPeriod }
		c, b = f.khtc(http.MethodGet, "/periods?year=2026", me, "", nil)
		f.ok(c, b, &list)
		if len(list.Items) != 4 {
			t.Fatalf("danh sách: %d", len(list.Items))
		}
	})
}

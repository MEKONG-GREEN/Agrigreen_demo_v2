package kn77_test

// W11.62 — Kỳ KHTC trên module THẬT: pf/extension (extension/khtcperiod, sổ pf 20261013070316) + pf/forms (kỳ native kn_khtc)
// trên PostgreSQL. Phòng nộp phiếu, KHTC rà soát qua /v1/forms (route thật); mở/đóng/hủy kỳ qua use case Kỳ KHTC.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	khtcapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/app"
	khtcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/domain"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// crosswalk giả của gói PFPUB: một kỳ nguồn CNTS cho mỗi kỳ KHTC, trạng thái chốt điều khiển được.
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

type khtcFx struct {
	t    *testing.T
	svc  *khtcapp.Service
	clk  *clock.Fixed
	cw   *crosswalk
	r    *gin.Engine
	tid  uuid.UUID
	node uuid.UUID
}

func newKHTCFx(t *testing.T) *khtcFx {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	// 08:00 Thứ Tư 14/10/2026 giờ VN — tuần 41 (07/10–13/10) vừa kết thúc, hạn nộp 10:00 hôm nay.
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	fm, err := forms.New(pool, clk, forms.Deps{Profiles: []formsport.Profile{platformasm.KNKHTCProfile}})
	if err != nil {
		t.Fatal(err)
	}
	cw := &crosswalk{}
	f := &khtcFx{t: t, clk: clk, cw: cw, tid: ids.New(), node: ids.New(),
		svc: &khtcapp.Service{DB: db.ForModule(pool, extension.DBModule), Clock: clk, Forms: fm.Publisher(), Control: fm.Control(),
			Crosswalk: cw, MaxSubmissions: platformasm.KNKHTCProfile.MaxSubmissions}}
	gin.SetMode(gin.TestMode)
	f.r = gin.New()
	f.r.Use(func(c *gin.Context) {
		var k authz.KNContext
		_ = json.Unmarshal([]byte(c.GetHeader("X-Test-KN")), &k)
		actor, _ := uuid.Parse(c.GetHeader("X-Test-Actor"))
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.Principal{ActorRef: actor, TenantID: f.tid, KN: &k}))
	})
	fm.RegisterApp(f.r.Group("/v1"))
	return f
}

func (f *khtcFx) as(role, dept string, actor uuid.UUID) context.Context {
	if actor == uuid.Nil {
		actor = ids.New()
	}
	k := authz.KNContext{Role: role, Department: dept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}}
	if role == authz.KNAdmin {
		k = authz.KNContext{Role: role, Scope: authz.Scope{All: true}}
	}
	return authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: actor, TenantID: f.tid, KN: &k})
}

// call gọi route /v1/forms như phiên (vai, phòng ban, người) — trả JSON.
func (f *khtcFx) call(method, path, role, dept string, actor uuid.UUID, body any) map[string]any {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	k, _ := json.Marshal(authz.KNContext{Role: role, Department: dept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}})
	req.Header.Set("X-Test-KN", string(k))
	req.Header.Set("X-Test-Actor", actor.String())
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	if w.Code >= 300 {
		f.t.Fatalf("%s %s → %d %s", method, path, w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func (f *khtcFx) submit(fid uuid.UUID, dept string) string {
	out := f.call(http.MethodPost, "/v1/forms/periods/"+fid.String()+"/submissions?profile=kn_khtc", authz.KNTinh, dept, ids.New(),
		map[string]any{"unit": dept, "values": map[string]string{"ket_qua": "Tập huấn 3 lớp"}})
	return out["id"].(string)
}

func (f *khtcFx) review(subID, decision string, khtc uuid.UUID) {
	f.call(http.MethodPost, "/v1/forms/submissions/"+subID+"/review?profile=kn_khtc", authz.KNTinh, partyport.DeptKeHoachTaiChinh, khtc,
		map[string]any{"decision": decision, "note": "Bổ sung số liệu lớp tập huấn"})
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var p *problem.Problem
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("muốn %s, có %v", code, err)
	}
}

func formOf(t *testing.T, d khtcapp.Detail, dept string) khtcapp.FormRow {
	t.Helper()
	for _, r := range d.Forms {
		if r.Department == dept {
			return r
		}
	}
	t.Fatalf("không có phiếu %s", dept)
	return khtcapp.FormRow{}
}

func TestKHTCPeriod_W1162(t *testing.T) {
	f := newKHTCFx(t)
	khtcActor := ids.New()
	khtc := f.as(authz.KNTinh, partyport.DeptKeHoachTaiChinh, khtcActor)
	tt, cn := partyport.DeptTrongTrot, partyport.DeptChanNuoiThuySan
	week41 := khtcapp.OpenInput{Request: khtcdomain.Request{Type: khtcdomain.TypeWeek, Year: 2026, No: 41}, Departments: []string{tt, cn}}

	// Phòng khác không mở kỳ; chưa cài phiếu cơ sở → 404.
	_, err := f.svc.Open(f.as(authz.KNTinh, tt, uuid.Nil), week41, "k-tt")
	wantCode(t, err, "forbidden")
	_, err = f.svc.Open(khtc, week41, "k0")
	wantCode(t, err, "forms.template_not_found")
	if _, err := f.svc.InstallTemplate(f.as(authz.KNAdmin, "", uuid.Nil), "tpl"); err != nil {
		t.Fatal(err)
	}

	// Nháp: tên kỳ, khoảng thời gian, hạn tự điền; nguồn CNTS gắn theo bộ quy đổi.
	dr, err := f.svc.Preview(khtc, week41)
	if err != nil {
		t.Fatal(err)
	}
	if dr.Plan.Name != "Tuần 41 năm 2026 (từ ngày 07/10/2026 đến ngày 13/10/2026)" || dr.MaxSubmissions != 3 || len(dr.Sources) != 1 ||
		dr.TemplateVersion == 0 || dr.Duplicate != nil {
		t.Fatalf("nháp: %+v", dr)
	}

	// Mở kỳ: một phiếu mỗi phòng (kỳ native kn_khtc), Đã mở. Gọi lặp cùng khóa → cùng kỳ; khóa khác → trùng.
	d, err := f.svc.Open(khtc, week41, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if d.State != khtcdomain.PeriodOpened || len(d.Forms) != 2 || formOf(t, d, tt).View.State != khtcdomain.FormNotStarted ||
		d.Counts.Sources != 1 || d.Period.MaxSubmissions != 3 {
		t.Fatalf("mở kỳ: %+v", d)
	}
	again, err := f.svc.Open(khtc, week41, "k1")
	if err != nil || again.Period.ID != d.Period.ID {
		t.Fatalf("gọi lặp: %v %v", again.Period.ID, err)
	}
	_, err = f.svc.Open(khtc, week41, "k2")
	wantCode(t, err, "extension.khtc_period_duplicate")
	fid := d.Period.FormsPeriodRef

	// Trồng trọt gửi → Đang thu thập. KHTC yêu cầu bổ sung, phòng gửi lại tới lần 3 → tự khóa (chỉ xác nhận).
	s1 := f.submit(fid, tt)
	if d, _ = f.svc.Get(khtc, d.Period.ID); d.State != khtcdomain.PeriodCollecting || !formOf(t, d, tt).View.CanRequestSupplement {
		t.Fatalf("sau lần gửi 1: %s %+v", d.State, formOf(t, d, tt))
	}
	f.review(s1, "returned", khtcActor)
	if d, _ = f.svc.Get(khtc, d.Period.ID); formOf(t, d, tt).View.State != khtcdomain.FormSupplement {
		t.Fatalf("yêu cầu bổ sung: %+v", formOf(t, d, tt))
	}
	s2 := f.submit(fid, tt)
	f.review(s2, "returned", khtcActor)
	f.submit(fid, tt)
	d, _ = f.svc.Get(khtc, d.Period.ID)
	if v := formOf(t, d, tt).View; v.Attempt != 3 || !v.LimitReached || v.CanRequestSupplement || !v.CanConfirm {
		t.Fatalf("lần 3: %+v", v)
	}

	// CNTS gửi, nguồn chưa chốt → chưa đủ; nguồn chốt → Đủ dữ liệu → đóng kỳ thường.
	f.submit(fid, cn)
	if d, _ = f.svc.Get(khtc, d.Period.ID); d.State != khtcdomain.PeriodCollecting {
		t.Fatalf("nguồn chưa chốt: %s", d.State)
	}
	f.cw.closed = true
	if d, _ = f.svc.Get(khtc, d.Period.ID); d.State != khtcdomain.PeriodComplete || !d.CanClose || d.CloseForced {
		t.Fatalf("đủ dữ liệu: %s %+v", d.State, d.Counts)
	}
	if d, err = f.svc.Close(khtc, d.Period.ID, "", "c1"); err != nil || d.State != khtcdomain.PeriodClosed || d.FormsStatus != "closed" ||
		formOf(t, d, tt).View.State != khtcdomain.FormLocked {
		t.Fatalf("đóng kỳ: %v %s %s", err, d.State, d.FormsStatus)
	}
	_, err = f.svc.Cancel(khtc, d.Period.ID, "Mở nhầm loại báo cáo", "x1")
	wantCode(t, err, "extension.khtc_period_terminal")

	// Tháng 10 (đến 27/10): quá hạn ≥ 3 ngày, còn thiếu → đóng ngoại lệ, lý do bắt buộc; phiếu chưa gửi → Không gửi.
	m10, err := f.svc.Open(khtc, khtcapp.OpenInput{Request: khtcdomain.Request{Type: khtcdomain.TypeMonth, Year: 2026, No: 10},
		Departments: []string{tt, cn}}, "m10")
	if err != nil {
		t.Fatal(err)
	}
	f.submit(m10.Period.FormsPeriodRef, tt)
	_, err = f.svc.Close(khtc, m10.Period.ID, "", "c2")
	wantCode(t, err, "extension.khtc_period_not_closable")
	f.clk.Advance(18 * 24 * time.Hour) // 01/11: quá hạn 28/10 10:00 hơn 3 ngày
	if d, _ = f.svc.Get(khtc, m10.Period.ID); d.State != khtcdomain.PeriodLate || !d.CloseForced {
		t.Fatalf("trễ hạn: %s %v", d.State, d.CloseForced)
	}
	_, err = f.svc.Close(khtc, m10.Period.ID, "", "c3")
	wantCode(t, err, "validation_failed")
	if d, err = f.svc.Close(khtc, m10.Period.ID, "Phòng CNTS chưa gửi phiếu sau 4 ngày quá hạn", "c4"); err != nil ||
		formOf(t, d, cn).View.State != khtcdomain.FormNotSent || !d.Events[len(d.Events)-1].Forced {
		t.Fatalf("đóng ngoại lệ: %v %+v", err, d.Forms)
	}

	// Sửa hạn nộp, hủy kỳ (lý do bắt buộc), mở lại đúng kỳ đó sau khi hủy.
	w43 := khtcapp.OpenInput{Request: khtcdomain.Request{Type: khtcdomain.TypeWeek, Year: 2026, No: 43}, Departments: []string{tt}}
	k43, err := f.svc.Open(khtc, w43, "w43")
	if err != nil {
		t.Fatal(err)
	}
	newDue := k43.DueAt.Add(48 * time.Hour)
	if d, err = f.svc.ChangeDeadline(khtc, k43.Period.ID, khtcapp.DeadlineInput{DueAt: newDue}, "dl"); err != nil || !d.DueAt.Equal(newDue) {
		t.Fatalf("sửa hạn: %v %v", err, d.DueAt)
	}
	_, err = f.svc.Cancel(khtc, k43.Period.ID, "nhầm", "x2")
	wantCode(t, err, "validation_failed")
	if d, err = f.svc.Cancel(khtc, k43.Period.ID, "Mở nhầm tuần báo cáo", "x3"); err != nil || d.State != khtcdomain.PeriodCancelled ||
		d.FormsStatus != "cancelled" || formOf(t, d, tt).View.State != khtcdomain.FormWithdrawn {
		t.Fatalf("hủy kỳ: %v %s %s", err, d.State, d.FormsStatus)
	}
	if _, err := f.svc.Open(khtc, w43, "w43b"); err != nil {
		t.Fatalf("mở lại kỳ đã hủy: %v", err)
	}
	list, err := f.svc.List(khtc, 2026, 10)
	if err != nil || len(list) != 4 {
		t.Fatalf("danh sách: %d %v", len(list), err)
	}
}

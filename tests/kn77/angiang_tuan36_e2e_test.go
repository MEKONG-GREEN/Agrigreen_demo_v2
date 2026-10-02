package kn77_test

// KN v7.7 gói P8 — kịch bản đầu–cuối "An Giang, Tuần 36" (docs/uat/kn-v7.7-acceptance.md §2) trên MODULE THẬT:
// cùng gốc lắp ráp cmd/api-platform/assembly (tests/e2e/harness.NewAt, đồng hồ giả), PostgreSQL thật, vai DB riêng mỗi
// module (R-05), kho định danh thật, ngữ cảnh phiên X-AG-Context THẬT của pf/accounts (P1), cây tổ chức/phòng ban/khu vực
// THẬT của pf/party (P2). Chỉ hai chỗ thay worker: bơm outbox → consumer pf/notify + consumer gửi Chi cục (bộ gửi thư giả),
// và job vòng đời báo cáo gọi trực tiếp với đồng hồ giả.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Địa bàn: An Giang (91) sau sắp xếp 2025; ba xã ở hai Khu vực tạm (Q-04) của seeddata/regions_an_giang.json.
const (
	agProv  = "91"
	agC1    = "30292" // KV883 Thành phố Long Xuyên
	agC2    = "30301" // KV883
	agC3    = "30316" // KV884 Thành phố Châu Đốc
	agKV1   = "KV883"
	agKV2   = "KV884"
	agDept  = "trong_trot"
	agPhone = "0912036036"
)

// t36: 08:00 Thứ Hai 31/08/2026 giờ Việt Nam — đầu tuần ISO 36.
var t36 = time.Date(2026, 8, 31, 1, 0, 0, 0, time.UTC)

type agMailer struct{ sent []repport.Message }

func (m *agMailer) Send(_ context.Context, msg repport.Message) error {
	m.sent = append(m.sent, msg)
	return nil
}

// agUser là một người dùng thật (tài khoản pf/accounts) với một ngữ cảnh phiên đang chọn.
type agUser struct {
	s      *agScenario
	id     uuid.UUID
	tid    uuid.UUID
	token  string
	ctx    string // X-AG-Context
	reason string // X-AG-Reason (chế độ hỗ trợ)
	device string
	// asg/dept để chọn lại ngữ cảnh sau khi đồng hồ giả vượt ContextTTL.
	asg, dept string
}

type agResp struct {
	status int
	raw    []byte
	body   map[string]any
}

func (r agResp) code() string { return str(r.body, "code") }

type agScenario struct {
	t      *testing.T
	s      *harness.Stack
	clk    *clock.Fixed
	tid    uuid.UUID
	mail   *agMailer
	seen   map[uuid.UUID]bool
	steps  []string
	pii    []string // liên hệ (email, SĐT) + họ tên người dân: không được ở API/tệp/log
	staff  []string // họ tên cán bộ: chỉ được hiện ở khung Log (actor_display tra lúc đọc), không ở log máy chủ/tệp gửi
	rootA  *agUser  // Admin nền tảng (MFA) — cũng là người đề nghị mẫu
	rootB  *agUser  // Admin thứ hai — người phát hành mẫu (R-56)
	nat    string
	p91    string
	forms  map[string]string
	batch  string
	period string
}

func (s *agScenario) step(name string) {
	s.t.Helper()
	s.steps = append(s.steps, name)
	s.t.Logf("BƯỚC %d — %s", len(s.steps), name)
}

func (u *agUser) do(base, method, path string, body any) agResp {
	u.s.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost || method == http.MethodPut {
		req.Header.Set("Idempotency-Key", key())
	}
	if u.ctx != "" {
		req.Header.Set(authz.ContextHeader, u.ctx)
	}
	if u.reason != "" {
		req.Header.Set(authz.ReasonHeader, url.PathEscape(u.reason))
	}
	if u.device != "" {
		req.Header.Set(syncport.HeaderDeviceID, u.device)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		u.s.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return agResp{status: res.StatusCode, raw: raw, body: m}
}

func (u *agUser) app(method, path string, body any) agResp {
	return u.do(u.s.s.App.URL, method, path, body)
}
func (u *agUser) adm(method, path string, body any) agResp {
	return u.do(u.s.s.Admin.URL, method, path, body)
}
func (u *agUser) get(path string) agResp            { return u.app(http.MethodGet, path, nil) }
func (u *agUser) post(path string, body any) agResp { return u.app(http.MethodPost, path, body) }

func (s *agScenario) want(r agResp, status int, what string) agResp {
	s.t.Helper()
	if r.status != status {
		s.t.Fatalf("%s: muốn %d, nhận %d %s", what, status, r.status, string(r.raw))
	}
	return r
}

func (s *agScenario) wantCode(r agResp, status int, code, what string) {
	s.t.Helper()
	if r.status != status || (code != "" && r.code() != code) {
		s.t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, string(r.raw))
	}
}

func (s *agScenario) admin(roles ...string) *agUser {
	id := ids.New()
	tok := s.s.Issuer.Sign(s.t, authztest.Token{Subject: id, TenantID: s.tid, Roles: roles, MFA: true, TTL: 3 * time.Hour})
	return &agUser{s: s, id: id, tid: s.tid, token: tok}
}

// createKN: Admin tạo tài khoản KN nhiều vai trò (POST /v1/accounts/kn-users); trả người dùng + assignment_id theo vai.
func (s *agScenario) createKN(name, email, phone string, asgs ...map[string]any) (*agUser, map[string]string) {
	s.t.Helper()
	r := s.want(s.rootA.adm(http.MethodPost, "/v1/accounts/kn-users", map[string]any{"full_name": name, "email": email, "phone": phone,
		"assignments": asgs}), http.StatusCreated, "tạo tài khoản "+name)
	id := uuid.MustParse(str(obj(r.body, "user"), "id"))
	out := map[string]string{}
	for _, a := range arr(r.body, "assignments") {
		out[str(a, "role")] = str(a, "assignment_id")
	}
	tok := s.s.Issuer.Sign(s.t, authztest.Token{Subject: id, TenantID: s.tid, Roles: []string{accport.RoleKNUser}, TTL: 3 * time.Hour})
	s.pii = append(s.pii, email, phone)
	s.staff = append(s.staff, name)
	return &agUser{s: s, id: id, tid: s.tid, token: tok}, out
}

// as chọn ngữ cảnh (POST /v1/me/context) — trả bản sao người dùng mang X-AG-Context.
func (u *agUser) as(assignment, dept string) *agUser {
	u.s.t.Helper()
	c := *u
	c.ctx = ""
	r := u.s.want(c.post("/v1/me/context", map[string]any{"assignment_id": assignment, "department_code": dept}), http.StatusCreated, "chọn ngữ cảnh")
	c.ctx = str(r.body, "context_id")
	c.asg, c.dept = assignment, dept
	return &c
}

// refresh chọn lại ngữ cảnh sau khi đồng hồ giả vượt ContextTTL (24 giờ).
func (u *agUser) refresh() {
	n := u.as(u.asg, u.dept)
	u.ctx = n.ctx
}

func asgKN(role, org string, depts []string, mgmt string, communes ...string) map[string]any {
	m := map[string]any{"role": role, "org_ref": org, "department_codes": depts, "commune_codes": communes, "mgmt_level": mgmt}
	if communes == nil {
		m["commune_codes"] = []string{}
	}
	return m
}

// pump giao mọi sự kiện mới của tenant tới consumer pf/notify và consumer gửi Chi cục (thay outbox-publisher + RabbitMQ).
func (s *agScenario) pump() map[string]int {
	s.t.Helper()
	ctx := context.Background()
	rows, err := s.s.AdminPF().Query(ctx, `SELECT id, tenant_id, event_type, schema_version, occurred_at, recorded_at, coalesce(trace_id, ''),
	    producer, payload FROM shared.outbox WHERE tenant_id = $1 ORDER BY id`, s.tid)
	if err != nil {
		s.t.Fatal(err)
	}
	var envs []outbox.Envelope
	for rows.Next() {
		var e outbox.Envelope
		if err := rows.Scan(&e.EventID, &e.TenantID, &e.EventType, &e.SchemaVersion, &e.OccurredAt, &e.RecordedAt, &e.TraceID, &e.Producer, &e.Payload); err != nil {
			s.t.Fatal(err)
		}
		envs = append(envs, e)
	}
	rows.Close()
	out := map[string]int{}
	for _, e := range envs {
		if s.seen[e.EventID] {
			continue
		}
		s.seen[e.EventID] = true
		for _, c := range s.s.Mods.Notify.Consumers() {
			if slices.Contains(c.Events, e.EventType) {
				if ok, err := inbox.Process(ctx, s.s.PF(notify.DBRole), c.Name, e, c.Handler); err != nil {
					s.t.Fatalf("consumer %s / %s: %v", c.Name, e.EventType, err)
				} else if ok {
					out[c.Name]++
				}
			}
		}
		if e.EventType == repport.EventDispatchRequested {
			if ok, err := inbox.Process(ctx, s.s.PF(reports.DBRole), repport.DispatchConsumer, e, s.s.Mods.Reports.DispatchHandler()); err != nil {
				s.t.Fatalf("consumer gửi Chi cục: %v", err)
			} else if ok {
				out[repport.DispatchConsumer]++
			}
		}
	}
	return out
}

func (s *agScenario) task(u *agUser, form, commune string) map[string]any {
	s.t.Helper()
	l := s.want(u.get("/v1/extension/report-tasks?period_id="+s.period+"&form_code="+form+"&commune="+commune), http.StatusOK, "công việc")
	items := arr(l.body, "items")
	if len(items) != 1 {
		s.t.Fatalf("muốn 1 công việc %s/%s, có %d %s", form, commune, len(items), string(l.raw))
	}
	return items[0]
}

func agSubmit(u *agUser, taskID, kind string, input map[string]string) agResp {
	return u.post("/v1/extension/report-tasks/"+taskID+"/submissions", map[string]any{"kind": kind, "rows": []any{map[string]any{"input": input}}})
}

func (s *agScenario) noPII(what string, raw []byte, extra ...string) {
	s.t.Helper()
	for _, n := range append(slices.Clone(s.pii), extra...) {
		if n != "" && bytes.Contains(raw, []byte(n)) {
			i := bytes.Index(raw, []byte(n))
			s.t.Fatalf("%s lộ dữ liệu cá nhân %q: …%s…", what, n, raw[max(0, i-200):min(len(raw), i+60)])
		}
	}
}

// TestKN77AnGiangTuan36 — kịch bản đầu–cuối KN v7.7 (P1–P7 trên module thật). Các bước và dòng UAT: §2 của
// docs/uat/kn-v7.7-acceptance.md.
func TestKN77AnGiangTuan36(t *testing.T) {
	clk := clock.NewFixed(t36)
	st := harness.NewAt(t, clk)
	s := &agScenario{t: t, s: st, clk: clk, tid: harness.NewTenant(), mail: &agMailer{}, seen: map[uuid.UUID]bool{}, forms: map[string]string{}}
	st.Mods.Reports.UseMailer(s.mail)
	s.rootA = s.admin(authz.RolePlatformAdmin, repport.RoleReportsAdmin)
	s.rootB = s.admin(authz.RolePlatformAdmin, repport.RoleReportsAdmin)
	ctx := context.Background()

	// ---- 1. Admin: danh mục ĐVHC, cây tổ chức 3 cấp, phòng ban, Khu vực (P2) ----
	s.step("Admin khởi tạo ĐVHC, cây tổ chức, phòng ban, Khu vực An Giang")
	s.want(s.rootA.adm(http.MethodPost, "/v1/masterdata/seed", map[string]any{}), http.StatusOK, "seed masterdata")
	s.nat = str(obj(s.want(s.rootA.adm(http.MethodPost, "/v1/orgs", map[string]any{"kind": "ttkn_national", "code": "TTKNQG", "name": "Trung tâm Khuyến nông Quốc gia"}),
		http.StatusCreated, "TTKNQG").body, "org"), "ref")
	s.p91 = str(obj(s.want(s.rootA.adm(http.MethodPost, "/v1/orgs", map[string]any{"kind": "ttkn_province", "code": "TTKN-AG", "name": "TTKN An Giang",
		"admin_unit_code": agProv, "parent_ref": s.nat}), http.StatusCreated, "TTKN An Giang").body, "org"), "ref")
	en := s.want(s.rootA.adm(http.MethodPost, "/v1/orgs/"+s.p91+"/communes/ensure", map[string]any{"code_prefix": "AG"}), http.StatusOK, "Nút xã")
	if num(en.body, "communes") < 100 {
		t.Fatalf("An Giang ≥ 100 xã/phường: %v", en.body["communes"])
	}
	nodes := map[string]string{}
	for _, o := range arr(en.body, "created") {
		nodes[str(o, "admin_unit_code")] = str(o, "ref")
	}
	if nodes[agC3] == "" {
		t.Fatalf("Nút xã %s: %v", agC3, en.body["created"])
	}
	rs := s.want(s.rootA.adm(http.MethodPost, "/v1/regions/seed", map[string]any{"province_code": agProv}), http.StatusOK, "Khu vực tạm Q-04")
	if num(rs.body, "assigned") != num(en.body, "communes") {
		t.Fatalf("mọi xã có Khu vực: %v", rs.body)
	}
	for _, org := range []string{s.p91, s.nat} {
		s.want(s.rootA.adm(http.MethodPost, "/v1/departments", map[string]any{"org_ref": org, "code": agDept, "name": "Phòng Trồng trọt", "sort_order": 1,
			"status": "active", "menu_profile": "trong_trot"}), http.StatusCreated, "phòng ban Trồng trọt")
	}
	if tr := s.want(s.rootA.adm(http.MethodGet, "/v1/orgs/tree", nil), http.StatusOK, "cây tổ chức"); !strings.Contains(string(tr.raw), "Thành phố Châu Đốc") {
		t.Fatalf("cây TTKNQG › TTKN tỉnh › Khu vực › Xã: %.400s", tr.raw)
	}

	// ---- 2. Tài khoản nhiều vai trò (P1) ----
	s.step("Admin tạo tài khoản nhiều vai trò; mỗi người chọn ngữ cảnh vai trò + phòng ban")
	binh, bA := s.createKN("Trần Thị Bình", "binh.tt@ttkn-ag.vi-du.vn", "0912036001", asgKN(authz.KNTinh, s.p91, []string{agDept}, "truong_phong"))
	an, aA := s.createKN("Nguyễn Văn An", "an.nv@ttkn-ag.vi-du.vn", "0912036002",
		asgKN(authz.KNXa, s.p91, []string{agDept}, "chuyen_vien", agC1, agC2), asgKN(authz.KNTW, s.nat, []string{agDept}, "chuyen_vien"))
	cuong, cA := s.createKN("Lê Văn Cường", "cuong.lv@ttkn-ag.vi-du.vn", "0912036003", asgKN(authz.KNXa, s.p91, []string{agDept}, "chuyen_vien", agC3))
	if l := s.want(an.get("/v1/me/contexts"), http.StatusOK, "ngữ cảnh của An"); len(arr(l.body, "assignments")) != 2 {
		t.Fatalf("An có 2 vai trò: %s", l.raw)
	}
	tinh := binh.as(bA[authz.KNTinh], agDept)
	xa1 := an.as(aA[authz.KNXa], agDept)
	tw := an.as(aA[authz.KNTW], agDept) // cùng token, đổi vai trò không đăng nhập lại
	xa2 := cuong.as(cA[authz.KNXa], agDept)
	if m := s.want(xa1.get("/v1/menu"), http.StatusOK, "menu Cấp Xã"); !strings.Contains(string(m.raw), "/kn/") {
		t.Fatalf("menu theo ngữ cảnh: %.300s", m.raw)
	} else {
		s.noPII("menu", m.raw)
	}
	// Header của người khác bị từ chối (vai, phạm vi không lấy từ client).
	spoof := *xa2
	spoof.ctx = xa1.ctx
	s.wantCode(spoof.get("/v1/extension/report-tasks"), http.StatusForbidden, "accounts.context_invalid", "dùng ngữ cảnh của người khác")

	// ---- 3. Mẫu T01/T04 (P3, SoD R-56) ----
	s.step("Admin khởi tạo mẫu, đề nghị T01/T04; người khác phát hành (tự phát hành bị chặn)")
	s.want(s.rootA.adm(http.MethodPost, "/v1/reports/forms/seed", map[string]any{"department_code": agDept}), http.StatusOK, "seed mẫu")
	for _, it := range arr(s.want(s.rootA.adm(http.MethodGet, "/v1/reports/forms?department="+agDept, nil), http.StatusOK, "mẫu").body, "items") {
		tpl := obj(it, "template")
		s.forms[str(tpl, "code")] = str(tpl, "id")
	}
	if len(s.forms) != 14 {
		t.Fatalf("14 mẫu Trồng trọt: %d", len(s.forms))
	}
	for _, c := range []string{"T01", "T04"} {
		base := "/v1/reports/forms/" + s.forms[c] + "/versions/1"
		s.want(s.rootA.adm(http.MethodPost, base+"/propose", map[string]any{"effective_from": "2026-01-05", "effective_label": "Tuần 2/2026"}), http.StatusOK, "đề nghị "+c)
		s.wantCode(s.rootA.adm(http.MethodPost, base+"/publish", map[string]any{}), http.StatusForbidden, "self_approval_forbidden", "tự phát hành "+c)
		s.want(s.rootB.adm(http.MethodPost, base+"/publish", map[string]any{}), http.StatusOK, "phát hành "+c)
	}

	// ---- 4. Cấp Tỉnh phát hành đợt tuần 36 (P4) ----
	s.step("Cấp Tỉnh phát hành Báo cáo tuần 36 (T01, T04) cho 3 xã, Tỉnh giao cột D")
	if l := s.want(tinh.get("/v1/extension/report-forms?report_type=weekly&from_date=2026-08-31"), http.StatusOK, "mẫu tuần"); len(arr(l.body, "items")) != 2 {
		t.Fatalf("2 mẫu tuần đã phát hành: %s", l.raw)
	}
	batch := map[string]any{"report_type": "weekly", "template_ids": []string{s.forms["T01"], s.forms["T04"]},
		"scope": map[string]any{"mode": "communes", "communes": []string{agC1, agC2, agC3}}, "from_date": "2026-08-31", "to_date": "2026-09-06",
		"season_label": "Thu Đông 2026", "note": "Tuần 36",
		"provincial_values": map[string]any{s.forms["T01"]: map[string]any{agC1: map[string]string{"D": "100"}, agC2: map[string]string{"D": "50"}, agC3: map[string]string{"D": "80"}}}}
	s.wantCode(tw.post("/v1/extension/report-batches", batch), http.StatusForbidden, "extension.read_only", "TW phát hành")
	s.wantCode(xa1.post("/v1/extension/report-batches", batch), http.StatusForbidden, "", "Cấp Xã phát hành")
	ib := s.want(tinh.post("/v1/extension/report-batches", batch), http.StatusCreated, "phát hành")
	s.batch = str(obj(ib.body, "batch"), "id")
	if ps := arr(ib.body, "periods"); len(ps) != 1 || num(obj(ib.body, "batch"), "task_count") != 6 {
		t.Fatalf("1 kỳ, 2 biểu × 3 xã = 6 công việc: %s", ib.raw)
	} else {
		s.period = str(ps[0], "id")
		if str(ps[0], "due_at") != "2026-09-08T10:00:00Z" {
			t.Fatalf("hạn Thứ Ba tuần sau 17:00 (Q-17 tạm): %v", ps[0])
		}
	}
	if n := s.pump()["pf.notify.report_cycle"]; n == 0 {
		t.Fatal("thông báo phát hành tới cán bộ xã")
	}
	if l := s.want(xa1.get("/v1/notifications"), http.StatusOK, "hộp thư An"); len(arr(l.body, "items")) == 0 {
		t.Fatalf("Cán bộ xã nhận thông báo công việc mới: %s", l.raw)
	}

	// ---- 5. Cán bộ xã nộp: nháp, gửi, ngoại tuyến, nhập thay ----
	s.step("Hai Cán bộ xã nhập: nháp → gửi (web), một bản qua đồng bộ ngoại tuyến, Admin nhập thay xã 3")
	l1 := s.want(xa1.get("/v1/extension/report-tasks"), http.StatusOK, "công việc An")
	if len(arr(l1.body, "items")) != 4 {
		t.Fatalf("An: 2 xã × 2 biểu = 4: %s", l1.raw)
	}
	if l := s.want(xa2.get("/v1/extension/report-tasks"), http.StatusOK, "công việc Cường"); len(arr(l.body, "items")) != 2 {
		t.Fatalf("Cường: 1 xã × 2 biểu: %s", l.raw)
	}
	s.wantCode(xa1.get("/v1/extension/report-tasks?commune="+agC3), http.StatusForbidden, "extension.out_of_scope", "xã ngoài phạm vi")
	t1c1 := str(s.task(xa1, "T01", agC1), "id")
	s.want(agSubmit(xa1, t1c1, "draft", map[string]string{"F": "10"}), http.StatusCreated, "lưu nháp")
	if r := s.want(agSubmit(xa1, t1c1, "submitted", map[string]string{"E": "90", "F": "20"}), http.StatusCreated, "gửi T01 xã 1"); str(obj(r.body, "task"), "status") != "submitted" {
		t.Fatalf("Đã nộp: %s", r.raw)
	}
	t4c1 := str(s.task(xa1, "T04", agC1), "id")
	if r := s.want(agSubmit(xa1, t4c1, "submitted", map[string]string{"K": "5", "L": "2", "N": "10"}), http.StatusCreated, "gửi T04"); str(obj(arr(obj(r.body, "submission"), "rows")[0], "cells"), "E") != "5" {
		t.Fatalf("T04: E = Σ DT xuống giống các cây: %s", r.raw)
	}
	// Xã 2 qua máy ngoại tuyến: đăng ký thiết bị, pull công việc, push report_submission.
	dev := *xa1
	dev.device = ids.New().String()
	s.want(dev.post("/v1/devices", map[string]any{"device_id": dev.device, "platform": "android", "app_version": "0.7.0"}), http.StatusCreated, "đăng ký thiết bị")
	pl := s.want(dev.get("/v1/sync/pull?limit=500"), http.StatusOK, "pull")
	var t1c2 string
	for _, ch := range arr(pl.body, "changes") {
		if d := obj(ch, "data"); str(ch, "entity") == "report_task" && str(d, "form_code") == "T01" && str(d, "commune_code") == agC2 {
			t1c2 = str(d, "id")
		}
		if d := obj(ch, "data"); str(ch, "entity") == "report_task" && str(d, "commune_code") == agC3 {
			t.Fatalf("pull không có xã ngoài phạm vi: %v", d)
		}
	}
	if t1c2 == "" {
		t.Fatalf("pull có công việc T01 xã 2: %.500s", pl.raw)
	}
	subID := ids.New().String()
	rec := map[string]any{"id": subID, "idem_key": "dev:" + dev.device + ":1", "entity": "report_submission", "op": "create",
		"event_time": "2026-08-31T07:30:00+07:00", "payload": map[string]any{"task_id": t1c2, "kind": "submitted", "base_version": 0,
			"rows": []any{map[string]any{"input": map[string]string{"F": "15"}}}}}
	for i, want := range []string{"accepted", "duplicate"} {
		pr := s.want(dev.post("/v1/sync/push", map[string]any{"device_id": dev.device, "records": []any{rec}}), http.StatusOK, "push")
		if got := str(arr(pr.body, "results")[0], "status"); got != want {
			t.Fatalf("push lần %d: muốn %s, nhận %s", i+1, want, pr.raw)
		}
	}
	if n := s.count(`SELECT count(*) FROM pf_extension.submission_version WHERE tenant_id = $1 AND task_id = $2`, t1c2); n != 1 {
		t.Fatalf("N-07 ngoại tuyến không trùng: %d phiên bản", n)
	}
	// Admin nhập thay xã 3 (chế độ hỗ trợ, gắn lần gán của Cường): lý do bắt buộc; bản nộp mang on_behalf_of.
	sp := s.want(s.rootA.adm(http.MethodPost, "/v1/accounts/support-sessions", map[string]any{"target_assignment_id": cA[authz.KNXa], "department_code": agDept,
		"reason": "Hỗ trợ xã nhập biểu T01 tuần 36 do mất mạng"}), http.StatusCreated, "mở hỗ trợ")
	sup := *s.rootA
	sup.ctx = str(sp.body, "context_id")
	t1c3 := str(s.task(&sup, "T01", agC3), "id")
	s.wantCode(agSubmit(&sup, t1c3, "submitted", map[string]string{"F": "30"}), http.StatusUnprocessableEntity, "", "nhập thay thiếu lý do")
	sup.reason = "Xã báo mất mạng, nhập thay theo điện thoại"
	sa := s.want(agSubmit(&sup, t1c3, "submitted", map[string]string{"F": "30"}), http.StatusCreated, "nhập thay")
	if str(obj(sa.body, "submission"), "on_behalf_of") != cuong.id.String() || str(obj(sa.body, "submission"), "actor_ref") != s.rootA.id.String() {
		t.Fatalf("nhập thay mang on_behalf_of + Admin thật: %v", obj(sa.body, "submission"))
	}
	if lg := s.want(s.rootA.adm(http.MethodGet, "/v1/accounts/support-log?support_session_id="+str(sp.body, "support_session_id"), nil), http.StatusOK, "nhật ký hỗ trợ"); len(arr(lg.body, "items")) < 2 {
		t.Fatalf("nhật ký hỗ trợ attempted + completed: %s", lg.raw)
	}

	// ---- 6. Trạng thái nộp + nhắc ----
	s.step("Cấp Tỉnh xem trạng thái nộp, nhắc các xã chưa nộp T04")
	stt := s.want(tinh.get("/v1/extension/report-batches/"+s.batch+"/status?period_id="+s.period+"&template_id="+s.forms["T01"]), http.StatusOK, "trạng thái T01")
	if num(obj(stt.body, "counts"), "submitted") != 3 {
		t.Fatalf("3 xã đã nộp T01: %s", stt.raw)
	}
	rm := s.want(tinh.post("/v1/extension/report-batches/"+s.batch+"/reminders", map[string]any{"period_id": s.period, "template_id": s.forms["T04"], "all_pending": true,
		"message": "Đề nghị nộp PL 03 trước 17:00 Thứ Ba"}), http.StatusCreated, "nhắc")
	if num(obj(rm.body, "reminder"), "recipient_count") != 2 {
		t.Fatalf("nhắc T04 tới An (xã 2) và Cường (xã 3): %v", obj(rm.body, "reminder"))
	}
	if s.pump()["pf.notify.report_cycle"] == 0 {
		t.Fatal("thông báo nhắc")
	}

	// ---- 7. Rà soát: Cập nhật lần 1 → yêu cầu điều chỉnh → gửi lại → Cập nhật lần 2 (nhật ký ô, tổng Khu vực) ----
	s.step("Cập nhật lần 1; Admin không tự xác nhận bản nhập thay; yêu cầu điều chỉnh → gửi lại → Cập nhật lần 2")
	s.want(tinh.post("/v1/extension/consolidations", map[string]any{"period_id": s.period}), http.StatusCreated, "cập nhật 1")
	v1 := s.want(tinh.get("/v1/extension/consolidations?period_id="+s.period+"&form_code=T01"), http.StatusOK, "bảng 1")
	if got := totalCell(arr(v1.body, "totals"), "province", "", "M"); got != "65" {
		t.Fatalf("Toàn tỉnh M = 20 + 15 + 30: %s", got)
	}
	tsp := s.want(s.rootA.adm(http.MethodPost, "/v1/accounts/support-sessions", map[string]any{"role": authz.KNTinh, "org_ref": s.p91, "department_code": agDept,
		"reason": "Hỗ trợ Cấp Tỉnh rà soát báo cáo tuần 36"}), http.StatusCreated, "hỗ trợ Cấp Tỉnh")
	asTinh := *s.rootA
	asTinh.ctx, asTinh.reason = str(tsp.body, "context_id"), "Hỗ trợ rà soát tuần 36"
	s.wantCode(asTinh.post("/v1/extension/report-tasks/"+t1c3+"/confirm", map[string]any{}), http.StatusForbidden, "self_approval_forbidden", "tự xác nhận bản nhập thay")
	s.want(tinh.post("/v1/extension/report-tasks/"+t1c3+"/confirm", map[string]any{}), http.StatusOK, "Cấp Tỉnh xác nhận")
	s.wantCode(tinh.post("/v1/extension/report-tasks/"+t1c1+"/adjustment-request", map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity, "validation_failed", "lý do ngắn")
	s.want(tinh.post("/v1/extension/report-tasks/"+t1c1+"/adjustment-request", map[string]any{"reason": "Số tuần 36 lệch báo cáo nhanh của xã"}), http.StatusOK, "yêu cầu điều chỉnh")
	s.want(agSubmit(xa1, t1c1, "submitted", map[string]string{"E": "90", "F": "25"}), http.StatusCreated, "gửi lại")
	if pc := s.want(tinh.get("/v1/extension/consolidations/pending-changes?period_id="+s.period), http.StatusOK, "chờ cập nhật"); num(pc.body, "changes") != 1 {
		t.Fatalf("1 thay đổi chờ cập nhật: %s", pc.raw)
	}
	c2 := s.want(tinh.post("/v1/extension/consolidations", map[string]any{"period_id": s.period, "template_ids": []string{s.forms["T01"]}}), http.StatusCreated, "cập nhật 2")
	run2 := arr(c2.body, "runs")[0]
	ch := s.want(tinh.get("/v1/extension/consolidations/"+str(run2, "id")+"/changes"), http.StatusOK, "nhật ký ô")
	kinds := map[string]string{}
	for _, it := range arr(ch.body, "items") {
		kinds[str(it, "letter")] = str(it, "kind") + ":" + str(it, "old") + "→" + str(it, "new")
		if str(it, "changed_by") != an.id.String() {
			t.Fatalf("người sửa ô: %v", it)
		}
	}
	if kinds["F"] != "adjusted:20→25" || kinds["M"] != "computed:20→25" {
		t.Fatalf("nhật ký ô cũ → mới: %v", kinds)
	}
	v2 := s.want(tinh.get("/v1/extension/consolidations?period_id="+s.period+"&form_code=T01"), http.StatusOK, "bảng 2")
	tot := arr(v2.body, "totals")
	if totalCell(tot, "province", "", "M") != "70" || totalCell(tot, "region", agKV1, "M") != "40" || totalCell(tot, "region", agKV2, "M") != "30" {
		t.Fatalf("Toàn tỉnh 70 = KV883 40 + KV884 30: %v", tot)
	}
	s.noPII("bảng tổng hợp", v2.raw)
	s.noPII("nhật ký ô", ch.raw)

	// ---- 8. Dashboard = tổng hợp; Cấp Quốc gia chỉ xem ----
	s.step("Dashboard tỉnh = bảng tổng hợp; Cấp Quốc gia cùng số, mọi thao tác ghi bị chặn")
	d := s.want(tinh.get("/v1/extension/kn-dashboard?period_id="+s.period), http.StatusOK, "dashboard")
	if v := obj(obj(d.body, "cards"), "sown_area")["value"]; v != "70" {
		t.Fatalf("dashboard DT xuống giống = tổng hợp 70: %v", v)
	}
	dtw := s.want(tw.get("/v1/extension/kn-dashboard?province="+agProv+"&period_id="+s.period), http.StatusOK, "dashboard TW")
	if obj(obj(dtw.body, "cards"), "sown_area")["value"] != "70" || dtw.body["read_only"] != true {
		t.Fatalf("TW cùng số, chỉ đọc: %s", dtw.raw)
	}
	s.want(tw.get("/v1/extension/consolidations?period_id="+s.period+"&form_code=T01"), http.StatusOK, "TW xem tổng hợp")
	s.wantCode(tw.post("/v1/extension/consolidations", map[string]any{"period_id": s.period}), http.StatusForbidden, "extension.read_only", "TW cập nhật")
	s.wantCode(agSubmit(tw, t1c1, "submitted", map[string]string{"F": "1"}), http.StatusForbidden, "extension.read_only", "TW gửi")
	s.wantCode(tw.post("/v1/extension/report-periods/"+s.period+"/close", map[string]any{}), http.StatusForbidden, "extension.read_only", "TW chốt")
	s.wantCode(tw.post("/v1/kn/cooperatives", map[string]any{"name": "HTX TW", "commune_code": agC1}), http.StatusForbidden, "party.read_only", "TW thêm HTX")
	s.wantCode(tw.post("/v1/kn/materials", map[string]any{"commune_code": agC1, "recorded_on": "2026-08-31", "lines": []any{map[string]any{"material_code": "PHAN_URE", "quantity": "1"}}}),
		http.StatusForbidden, "", "TW ghi vật tư")

	// ---- 9. Chốt kỳ → mở lại có lý do → hết hạn mở, job chốt lại ----
	s.step("Chốt kỳ (Hệ thống tổng hợp), gửi sau chốt bị từ chối; mở lại xã 1 có lý do; hết hạn mở → job chốt lại, tổng hợp lại")
	cl := s.want(tinh.post("/v1/extension/report-periods/"+s.period+"/close", map[string]any{}), http.StatusOK, "chốt kỳ")
	if str(arr(cl.body, "runs")[0], "trigger") != "system" {
		t.Fatalf("lần tổng hợp Hệ thống lúc chốt: %s", cl.raw)
	}
	s.wantCode(agSubmit(xa1, t1c1, "submitted", map[string]string{"F": "26"}), http.StatusConflict, "extension.period_closed", "gửi sau chốt")
	until := clk.Now().Add(48 * time.Hour).Format(time.RFC3339)
	s.wantCode(tinh.post("/v1/extension/report-periods/"+s.period+"/reopen", map[string]any{"communes": []string{agC1}, "reason": "x", "until": until}),
		http.StatusUnprocessableEntity, "validation_failed", "mở lại thiếu lý do")
	s.want(tinh.post("/v1/extension/report-periods/"+s.period+"/reopen", map[string]any{"communes": []string{agC1}, "template_ids": []string{s.forms["T01"]},
		"reason": "Xã phát hiện nhập nhầm số tuần 36", "until": until}), http.StatusOK, "mở lại")
	s.want(agSubmit(xa1, t1c1, "submitted", map[string]string{"E": "90", "F": "26"}), http.StatusCreated, "gửi trong hạn mở")
	s.wantCode(agSubmit(xa1, t1c2, "submitted", map[string]string{"F": "16"}), http.StatusConflict, "extension.period_closed", "xã 2 vẫn chốt")
	clk.Advance(49 * time.Hour)
	jr, err := st.Mods.Extension.ReportCycleJobRun(ctx, s.tid, clk.Now())
	if err != nil || jr.Reclosed != 1 {
		t.Fatalf("job chốt lại: %+v %v", jr, err)
	}
	for _, u := range []*agUser{tinh, xa1, xa2, tw} {
		u.refresh() // ngữ cảnh phiên hết hạn sau 24 giờ (ContextTTL) — chọn lại, không đăng nhập lại
	}
	v3 := s.want(tinh.get("/v1/extension/consolidations?period_id="+s.period+"&form_code=T01"), http.StatusOK, "bảng sau chốt lại")
	if totalCell(arr(v3.body, "totals"), "province", "", "M") != "71" || str(v3.body, "status_label") != "Đã chốt" {
		t.Fatalf("tổng hợp lại sau chốt lại = 26 + 15 + 30: %s", v3.raw)
	}
	// X-13: đổi Khu vực của Nút xã 3 sau chốt — số đã chốt giữ cấu trúc lúc tổng hợp (region_snapshot), không tính lại.
	s.want(s.rootA.adm(http.MethodPost, "/v1/orgs/"+nodes[agC3]+"/region", map[string]any{"region_code": agKV1, "reason": "Điều chỉnh địa bàn theo huyện cũ"}),
		http.StatusCreated, "đổi Khu vực xã 3 sau chốt")
	v4 := s.want(tinh.get("/v1/extension/consolidations?period_id="+s.period+"&form_code=T01"), http.StatusOK, "bảng sau đổi Khu vực")
	if totalCell(arr(v4.body, "totals"), "region", agKV2, "M") != "30" || totalCell(arr(v4.body, "totals"), "region", agKV1, "M") != "41" {
		t.Fatalf("X-13: bảng đã chốt giữ Khu vực lúc tổng hợp: %v", arr(v4.body, "totals"))
	}
	if dd := s.want(tinh.get("/v1/extension/kn-dashboard?period_id="+s.period), http.StatusOK, "dashboard sau chốt lại"); obj(obj(dd.body, "cards"), "sown_area")["value"] != "71" {
		t.Fatalf("dashboard = tổng hợp sau chốt lại: %s", dd.raw)
	}

	// ---- 10. Chi cục: xuất + email kèm tệp ----
	s.step("Cấp Tỉnh xuất Excel/PDF từ lần tổng hợp gần nhất, gửi email Chi cục kèm tệp + CC (bộ gửi giả)")
	s.want(tinh.post("/v1/reports/dispatch-recipients", map[string]any{"label": "Chi cục Trồng trọt và BVTV An Giang", "address": "trongtrot.chicuc@angiang.vi-du.vn", "kind": "to"}),
		http.StatusCreated, "người nhận")
	s.want(tinh.post("/v1/reports/dispatch-recipients", map[string]any{"label": "Văn thư TTKN", "address": "vanthu@ttkn-ag.vi-du.vn", "kind": "cc"}), http.StatusCreated, "CC")
	s.wantCode(tinh.post("/v1/reports/dispatches", map[string]any{"period_id": s.period, "form_codes": []string{"T01"}, "formats": []string{"gsheet"}}),
		http.StatusConflict, "reports.gsheet_disabled", "Google Sheet tắt mặc định")
	ex := s.want(tinh.post("/v1/reports/dispatches", map[string]any{"period_id": s.period, "form_codes": []string{"T01"}, "formats": []string{"xlsx", "pdf"}}),
		http.StatusCreated, "xuất")
	did := str(obj(ex.body, "dispatch"), "id")
	s.want(tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{}), http.StatusAccepted, "gửi email")
	if s.pump()[repport.DispatchConsumer] != 1 {
		t.Fatal("consumer gửi Chi cục chạy một lần")
	}
	if len(s.mail.sent) != 1 || s.mail.sent[0].To != "trongtrot.chicuc@angiang.vi-du.vn" || len(s.mail.sent[0].CC) != 1 || len(s.mail.sent[0].Attachments) != 2 {
		t.Fatalf("email kèm 2 tệp + CC: %+v", s.mail.sent)
	}
	s.noPII("email Chi cục", []byte(s.mail.sent[0].Subject+s.mail.sent[0].Text), s.staff...)
	found := false
	for _, a := range s.mail.sent[0].Attachments {
		if !strings.HasSuffix(a.Name, ".xlsx") {
			continue
		}
		x, err := excelize.OpenReader(bytes.NewReader(a.Data))
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := x.GetRows(x.GetSheetName(0))
		for _, r := range rows {
			if len(r) > 12 && r[5] == "26" && r[12] == "26" {
				found = true
			}
		}
		s.noPII("tệp Excel Chi cục", []byte(strings.Join(func() []string {
			var out []string
			for _, r := range rows {
				out = append(out, strings.Join(r, "|"))
			}
			return out
		}(), "\n")), s.staff...)
	}
	if !found {
		t.Fatal("số trong tệp Excel = số tổng hợp sau chốt lại (xã 1: F = M = 26)")
	}
	if g := s.want(tinh.get("/v1/reports/dispatches/"+did), http.StatusOK, "log gửi"); str(obj(g.body, "dispatch"), "status") != "sent" {
		t.Fatalf("Đã gửi: %s", g.raw)
	}

	// ---- 11. Lịch công tác tuần (P5) + thông báo ----
	s.step("Cán bộ xã đăng ký lịch tuần sau, gửi duyệt; Trưởng phòng Cấp Tỉnh duyệt; thông báo hai chiều")
	mine := s.want(xa1.get("/v1/extension/work-schedules/mine"), http.StatusOK, "lịch của tôi")
	week := str(mine.body, "week_start")
	if week != "2026-09-07" {
		t.Fatalf("mặc định tuần kế tiếp: %s", mine.raw)
	}
	ws := s.want(xa1.post("/v1/extension/work-schedules", map[string]any{"week_start": week, "action": "submit", "items": []any{
		map[string]any{"day": "2026-09-07", "session": "morning", "start_time": "08:00", "end_time": "10:00", "content": "Kiểm tra đồng ruộng xã " + agC1},
		map[string]any{"day": "2026-09-09", "session": "afternoon", "content": "Tập huấn IPM tại HTX"}}}), http.StatusCreated, "gửi duyệt lịch")
	wid := str(obj(ws.body, "schedule"), "id")
	if l := s.want(tinh.get("/v1/extension/work-schedules?week_start="+week+"&status=pending"), http.StatusOK, "chờ duyệt"); len(arr(l.body, "items")) != 1 {
		t.Fatalf("lịch hiện ngay ở Chờ duyệt: %s", l.raw)
	}
	s.wantCode(xa2.post("/v1/extension/work-schedules/"+wid+"/approve", map[string]any{"version": num(obj(ws.body, "schedule"), "version")}), http.StatusForbidden, "",
		"Cán bộ xã khác duyệt")
	s.want(tinh.post("/v1/extension/work-schedules/"+wid+"/approve", map[string]any{"version": num(obj(ws.body, "schedule"), "version")}), http.StatusOK, "duyệt lịch")
	if n := s.pump()["pf.notify.work_schedule"]; n < 2 {
		t.Fatalf("thông báo gửi duyệt + kết quả duyệt qua pf.notify.work_schedule: %d", n)
	}
	kinds2 := ""
	for _, it := range arr(s.want(xa1.get("/v1/notifications"), http.StatusOK, "hộp thư An").body, "items") {
		kinds2 += str(it, "kind") + ","
	}
	if !strings.Contains(kinds2, "work_schedule_reviewed") {
		t.Fatalf("FN-XA-03.08: Cán bộ xã nhận kết quả duyệt: %s", kinds2)
	}

	// ---- 12. Dữ liệu Cấp Xã (P7): HTX, máy móc, thửa, vật tư — phạm vi ----
	s.step("Cán bộ xã khai HTX, máy móc, thửa, vật tư trong xã phụ trách; ngoài phạm vi bị chặn; vô hiệu hóa HTX ≥ 20 ký tự")
	// Ghi người đại diện/nông dân vào kho định danh cần vai realm vault_registrar: vai kn_user KHÔNG kèm vai kho định danh
	// (quyết định chờ: cấp mặc định cho Cán bộ xã khi tạo tài khoản KN). Ở đây Admin cấp thêm qua API vai realm hiện có và
	// người dùng nhận token mới (Keycloak ghi vai vào token lúc đăng nhập/làm mới).
	s.want(s.rootA.adm(http.MethodPost, "/v1/accounts/users/"+an.id.String()+"/assignment", map[string]any{"org_ref": s.p91, "roles": []string{accport.RoleKNUser, "vault_registrar",
		"vault_display"}}), http.StatusOK, "cấp vai kho định danh cho Cán bộ xã")
	xa1.token = s.s.Issuer.Sign(t, authztest.Token{Subject: an.id, TenantID: s.tid, Roles: []string{accport.RoleKNUser, "vault_registrar", "vault_display"}, TTL: 3 * time.Hour})
	repName, repPhone := "Phạm Thị Duyên", agPhone
	s.pii = append(s.pii, repName, repPhone)
	coop := s.want(xa1.post("/v1/kn/cooperatives", map[string]any{"name": "HTX Nông nghiệp Mỹ Hòa Hưng", "commune_code": agC1, "area_ha": "85.5",
		"representative": map[string]any{"full_name": repName, "phone": repPhone}}), http.StatusCreated, "thêm HTX")
	s.noPII("HTX", coop.raw)
	cref := str(coop.body, "ref")
	s.want(xa1.app(http.MethodPut, "/v1/kn/cooperatives/"+cref, map[string]any{"area_ha": "90", "reason": "Cập nhật theo biên bản kiểm tra"}), http.StatusOK, "sửa HTX")
	if lg := s.want(xa1.get("/v1/kn/cooperatives/"+cref+"/log"), http.StatusOK, "Log HTX"); !strings.Contains(string(lg.raw), `"85.5"`) || !strings.Contains(string(lg.raw), `"90"`) {
		t.Fatalf("Log HTX cũ → mới: %s", lg.raw)
	} else {
		s.noPII("Log HTX", lg.raw)
	}
	s.wantCode(xa1.post("/v1/kn/cooperatives", map[string]any{"name": "HTX ngoài xã", "commune_code": agC3}), http.StatusForbidden, "party.out_of_scope", "HTX xã ngoài phạm vi")
	s.wantCode(xa2.get("/v1/kn/cooperatives/"+cref), http.StatusNotFound, "party.org_not_found", "Cán bộ xã khác đọc HTX")
	mc := s.want(xa1.app(http.MethodPut, "/v1/kn/machine-counts/"+cref, map[string]any{"lines": []any{map[string]any{"machine_type": "MLD", "quantity": 2},
		map[string]any{"machine_type": "MGD", "quantity": 1}}}), http.StatusOK, "khai báo máy")
	if num(mc.body, "total") != 3 {
		t.Fatalf("tổng máy HTX: %s", mc.raw)
	}
	s.wantCode(xa1.app(http.MethodPut, "/v1/kn/machine-counts/"+cref, map[string]any{"lines": []any{map[string]any{"machine_type": "MLD", "quantity": -1}}}),
		http.StatusUnprocessableEntity, "", "số máy âm")
	s.wantCode(tinh.app(http.MethodPut, "/v1/kn/machine-counts/"+cref, map[string]any{"lines": []any{}}), http.StatusForbidden, "mechanization.read_only", "Cấp Tỉnh khai máy")
	if ov := s.want(xa1.get("/v1/kn/machine-counts?commune="+agC1), http.StatusOK, "tổng xã"); !strings.Contains(string(ov.raw), "MLD") {
		t.Fatalf("tổng xã theo loại máy: %s", ov.raw)
	}
	lon, lat := 105.4352, 10.3861
	sq := func(dx float64) map[string]any {
		x0, y0, w := lon+dx, lat, 0.0009
		return map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{x0, y0}, {x0 + w, y0}, {x0 + w, y0 + w}, {x0, y0 + w}, {x0, y0}}}}
	}
	s.want(xa1.post("/v1/kn/plots", map[string]any{"commune_code": agC1, "lot_name": "Lô bờ kênh", "farmer_name": "Võ Văn Em", "note": "Lúa OM18",
		"declared_area_ha": "1.0", "shape": "square", "geometry": sq(0)}), http.StatusCreated, "vẽ thửa")
	s.pii = append(s.pii, "Võ Văn Em")
	s.wantCode(xa1.post("/v1/kn/plots", map[string]any{"commune_code": agC1, "lot_name": "Lô chồng", "declared_area_ha": "1.0", "shape": "square", "geometry": sq(0.0004)}),
		http.StatusConflict, "geo.plot_overlap", "thửa chồng lấn")
	s.wantCode(xa1.post("/v1/kn/plots", map[string]any{"commune_code": agC3, "lot_name": "Lô xã khác", "declared_area_ha": "1.0", "shape": "square", "geometry": sq(0.01)}),
		http.StatusForbidden, "geo.out_of_scope", "thửa xã ngoài phạm vi")
	s.want(xa1.post("/v1/kn/materials", map[string]any{"commune_code": agC1, "recorded_on": "2026-09-02", "lines": []any{map[string]any{"material_code": "PHAN_URE", "quantity": "120.5"}}}),
		http.StatusCreated, "vật tư")
	if sm := s.want(tinh.get("/v1/kn/materials/summary"), http.StatusOK, "tổng hợp vật tư"); !strings.Contains(string(sm.raw), agC1) {
		t.Fatalf("Cấp Tỉnh xem tổng hợp vật tư theo xã: %s", sm.raw)
	}
	// Quyết định P8 (a): lý do vô hiệu hóa ≥ 20 ký tự ở mọi tầng (= CHECK sổ org_event) → 422, không 500.
	s.wantCode(xa1.post("/v1/kn/cooperatives/"+cref+"/deactivate", map[string]any{"reason": "HTX giải thể theo Q"}), http.StatusUnprocessableEntity, "validation_failed",
		"lý do 19 ký tự")
	dv := s.want(xa1.post("/v1/kn/cooperatives/"+cref+"/deactivate", map[string]any{"reason": "HTX giải thể theo QĐ"}), http.StatusOK, "vô hiệu hóa 20 ký tự")
	if w := obj(dv.body, "warnings"); num(w, "machines") != 3 || num(w, "plots") != 0 {
		t.Fatalf("cảnh báo dữ liệu liên kết (3 máy): %s", dv.raw)
	}

	// ---- 13. Sổ chỉ ghi thêm, chuỗi băm, cách ly tenant, không PII ----
	s.step("verify_chain mọi sổ KN, thử UPDATE/DELETE sổ, cách ly tenant, không PII trong log")
	s.verifyLedgers()
	other := s.s.Issuer.Sign(t, authztest.Token{Subject: an.id, TenantID: ids.New(), Roles: []string{accport.RoleKNUser}, TTL: time.Hour})
	ot := *xa1
	ot.token = other
	s.wantCode(ot.get("/v1/extension/report-tasks"), http.StatusForbidden, "accounts.context_invalid", "ngữ cảnh ở tenant khác")
	oAdm := &agUser{s: s, id: ids.New(), token: s.s.Issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: true, TTL: time.Hour})}
	if l := s.want(oAdm.adm(http.MethodGet, "/v1/departments?org_ref="+s.p91, nil), http.StatusOK, "tenant khác: phòng ban"); len(arr(l.body, "items")) != 0 {
		t.Fatalf("tenant khác thấy phòng ban: %s", l.raw)
	}
	if err := tenant.WithTenantTx(ctx, s.s.PF(extension.DBRole), ids.New(), func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.report_task`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("RLS: tenant khác đếm công việc = %d %v", n, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.noPII("log máy chủ", []byte(s.s.Logs.String()), s.staff...)

	t.Logf("TestKN77AnGiangTuan36: %d bước đạt", len(s.steps))
	if os.Getenv("AG_KN77_STEPS") != "" {
		for i, n := range s.steps {
			t.Logf("%d. %s", i+1, n)
		}
	}
}

func (s *agScenario) count(q string, args ...any) int {
	s.t.Helper()
	var n int
	if err := s.s.AdminPF().QueryRow(context.Background(), q, append([]any{s.tid}, args...)...).Scan(&n); err != nil {
		s.t.Fatal(err)
	}
	return n
}

// knLedgers: mọi sổ (apply_ledger) của KN v7.7 — migration pf 000060–000079.
var knLedgers = []string{
	"pf_accounts.role_assignment_event", "pf_accounts.support_session_event", "pf_accounts.support_action_log", "pf_accounts.account_profile_change",
	"pf_party.department_event", "pf_party.region_event", "pf_party.org_region_event", "pf_party.kn_profile_event",
	"pf_reports.form_assignment_event", "pf_reports.form_template_event", "pf_reports.dispatch_recipient_event", "pf_reports.dispatch_event",
	"pf_extension.report_batch_event", "pf_extension.report_task_event", "pf_extension.provincial_value", "pf_extension.submission_version",
	"pf_extension.review_event", "pf_extension.reminder_event", "pf_extension.period_event", "pf_extension.consolidation_run",
	"pf_extension.work_schedule_event", "pf_extension.work_schedule_item", "pf_extension.material_record", "pf_mechanization.coop_machine_count_event",
}

// verifyLedgers chạy shared.verify_chain cho mọi luồng của mọi sổ KN của tenant (sổ phải có dữ liệu, trừ hồ sơ tài khoản)
// và thử UPDATE/DELETE trên từng sổ có dữ liệu — trigger chỉ ghi thêm phải chặn (RB-1).
func (s *agScenario) verifyLedgers() {
	s.t.Helper()
	ctx := context.Background()
	pool := s.s.AdminPF()
	for _, tbl := range knLedgers {
		rows, err := pool.Query(ctx, `SELECT DISTINCT stream_key FROM `+tbl+` WHERE tenant_id = $1`, s.tid)
		if err != nil {
			s.t.Fatalf("%s: %v", tbl, err)
		}
		var streams []string
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				s.t.Fatal(err)
			}
			streams = append(streams, k)
		}
		rows.Close()
		if len(streams) == 0 {
			if tbl != "pf_accounts.account_profile_change" {
				s.t.Fatalf("sổ %s không có dữ liệu sau kịch bản", tbl)
			}
			continue
		}
		var checked int64
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			for _, k := range streams {
				v, err := ledger.Verify(ctx, tx, tbl, s.tid, k)
				if err != nil {
					return err
				}
				if !v.OK {
					s.t.Fatalf("chuỗi băm %s/%s đứt: %+v", tbl, k, v)
				}
				checked += v.RowsChecked
			}
			return nil
		})
		if err != nil || checked == 0 {
			s.t.Fatalf("verify_chain %s: %d dòng, %v", tbl, checked, err)
		}
		for _, q := range []string{`UPDATE ` + tbl + ` SET seq = seq WHERE tenant_id = $1`, `DELETE FROM ` + tbl + ` WHERE tenant_id = $1`} {
			if _, err := pool.Exec(ctx, q, s.tid); err == nil {
				s.t.Fatalf("RB-1: %s phải bị chặn", q)
			}
		}
	}
}

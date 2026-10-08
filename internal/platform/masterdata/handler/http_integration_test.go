// Kiểm thử tích hợp của pf/masterdata qua HTTP (Application API + Admin API) và qua port,
// trên PostgreSQL thật (pgtest) với token giả (authztest).
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	t     *testing.T
	env   *pgtest.Env
	mod   *masterdata.Module
	iss   *authztest.FakeIssuer
	app   *httptest.Server
	admin *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := pgtest.Get(t)
	iss := authztest.New(t)
	mod := masterdata.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{})
	ops := env.Pool(t, migrations.PF, "platform_api")
	log := telemetry.NewLogger("test", io.Discard)

	appR := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(appR.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	admR := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(admR.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))

	f := &fixture{t: t, env: env, mod: mod, iss: iss, app: httptest.NewServer(appR), admin: httptest.NewServer(admR)}
	t.Cleanup(f.app.Close)
	t.Cleanup(f.admin.Close)
	return f
}

type caller struct {
	f     *fixture
	actor uuid.UUID
	tid   uuid.UUID
	token string
}

func (f *fixture) caller(tid uuid.UUID, roles ...string) caller {
	actor := ids.New()
	return caller{f: f, actor: actor, tid: tid, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, MFA: true})}
}

// ctx là context như sau middleware xác thực — để gọi port trực tiếp như module khác.
func (c caller) ctx() context.Context {
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: c.actor, TenantID: c.tid})
	return tenant.WithID(ctx, c.tid)
}

func (c caller) do(srv *httptest.Server, method, path, idem string, body any) (int, map[string]any) {
	c.f.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m
}

func (c caller) adm(method, path string, body any) (int, map[string]any) {
	idem := ""
	if method == http.MethodPost {
		idem = "k-" + ids.New().String()
	}
	return c.do(c.f.admin, method, path, idem, body)
}

func (c caller) get(path string) (int, map[string]any) {
	return c.do(c.f.app, http.MethodGet, path, "", nil)
}

// seeded tạo một tenant mới đã nạp dữ liệu nền.
func (f *fixture) seeded() uuid.UUID {
	f.t.Helper()
	tid := ids.New()
	res, err := f.mod.Seed(context.Background(), tid)
	if err != nil {
		f.t.Fatalf("seed: %v", err)
	}
	if res.Units.Created < 200 || res.Items < 20 || res.Params < 9 {
		f.t.Fatalf("seed thiếu dữ liệu: %+v", res)
	}
	return tid
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func day(s string) time.Time {
	d, _ := time.ParseInLocation(time.DateOnly, s, clock.Zone())
	return d
}

func TestSeedIsIdempotent(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	res, err := f.mod.Seed(context.Background(), tid)
	if err != nil {
		t.Fatal(err)
	}
	if res.Units.Created != 0 || res.Units.Closed != 0 || res.Items != 0 || res.Params != 0 {
		t.Fatalf("nạp lặp không được thêm gì: %+v", res)
	}
}

// Successor sau sáp nhập 2025: mã cũ tra ra mã hiện hành; mã 91 dùng lại cho đơn vị mới.
func TestSuccessorResolutionAfterMerger(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	u := f.caller(tid)
	units := f.mod.AdminUnits()

	for _, c := range []struct {
		code, at string
		want     []string
	}{
		{"89", "2026-03-01", []string{"91"}}, // An Giang cũ → An Giang mới
		{"95", "2026-03-01", []string{"96"}}, // Bạc Liêu → Cà Mau
		// TP Long Xuyên (huyện cũ 883) → 4 đơn vị cấp xã mới.
		{"883", "2026-03-01", []string{"30292", "30301", "30307", "30313"}},
		{"30280", "2026-03-01", []string{"30307"}},          // P. Mỹ Bình → P. Long Xuyên
		{"31178", "2026-03-01", []string{"31120", "31168"}}, // P. Bùi Hữu Nghĩa nhập một phần vào hai phường
		{"91", "2024-05-01", []string{"91"}},                // Kiên Giang lúc còn hiệu lực
		{"30742", "2026-03-01", []string{"30742"}},          // P. Rạch Giá (mã dùng lại)
		{"31135", "2024-05-01", []string{"31135"}},          // P. Tân An cũ lúc còn hiệu lực
	} {
		got, err := units.Current(u.ctx(), c.code, day(c.at))
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("Current(%s, %s) = %v, %v; muốn %v", c.code, c.at, got, err, c.want)
		}
	}
	// Cùng mã 91, hai thời điểm → hai đơn vị khác nhau.
	old, err := units.Get(u.ctx(), "91", day("2024-05-01"))
	if err != nil || old.Name != "Kiên Giang" || old.ValidTo == nil || !slices.Equal(old.SuccessorCodes, []string{"91"}) {
		t.Fatalf("91 năm 2024: %+v %v", old, err)
	}
	cur, err := units.Get(u.ctx(), "91", day("2026-01-01"))
	if err != nil || cur.Name != "An Giang" || cur.ValidTo != nil {
		t.Fatalf("91 năm 2026: %+v %v", cur, err)
	}
	if _, err := units.Get(u.ctx(), "89", day("2026-01-01")); !errors.Is(err, partyport.ErrUnitNotFound) {
		t.Fatalf("89 đã đóng hiệu lực: %v", err)
	}
	// Qua HTTP.
	code, body := u.get("/v1/masterdata/admin-units/94/current?at=2026-02-01")
	if code != 200 || !slices.Equal(strs(body["current_codes"]), []string{"92"}) {
		t.Fatalf("HTTP current: %d %v", code, body)
	}

	// Chia tách giả định qua Admin API: P. Cái Răng (31186) → hai đơn vị từ 2027.
	// Mã KT-* là mã kiểm thử, không phải mã TCTK.
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	split := map[string]any{
		"format": "agrigreen.admin_units/v1", "source": "Kiểm thử chia tách",
		"units": []map[string]any{
			{"code": "31186", "name": "Phường Cái Răng", "level": "commune", "parent_code": "92", "valid_from": "2025-07-01",
				"valid_to": "2027-01-01", "successors": []string{"KT-31186-A", "KT-31186-B"}},
			{"code": "KT-31186-A", "name": "Cái Răng A", "level": "commune", "parent_code": "92", "valid_from": "2027-01-01"},
			{"code": "KT-31186-B", "name": "Cái Răng B", "level": "commune", "parent_code": "92", "valid_from": "2027-01-01"},
		},
	}
	if code, body := admin.adm("POST", "/v1/masterdata/admin-units/import", split); code != 200 || body["created"] != 2.0 || body["closed"] != 1.0 {
		t.Fatalf("nhập chia tách: %d %v", code, body)
	}
	got, err := units.Current(u.ctx(), "31186", day("2027-06-01"))
	if err != nil || !slices.Equal(got, []string{"KT-31186-A", "KT-31186-B"}) {
		t.Fatalf("sau chia tách: %v %v", got, err)
	}
	// Nhập lặp cùng tệp: không đổi gì (idempotent theo nội dung).
	if code, body := admin.adm("POST", "/v1/masterdata/admin-units/import", split); code != 200 || body["unchanged"] != 3.0 {
		t.Fatalf("nhập lặp: %d %v", code, body)
	}
	// Sửa lịch sử bị chặn: đổi tên phiên bản đã có → xung đột.
	split["units"].([]map[string]any)[1]["name"] = "Tên khác"
	if code, body := admin.adm("POST", "/v1/masterdata/admin-units/import", split); code != 409 || body["code"] != "masterdata.admin_unit_conflict" {
		t.Fatalf("sửa nội dung phiên bản cũ: %d %v", code, body)
	}
	// Kế thừa trỏ tới mã không tồn tại → 422.
	badRef := map[string]any{"format": "agrigreen.admin_units/v1", "source": "x", "units": []map[string]any{
		{"code": "31153", "name": "Phường Ô Môn", "level": "commune", "parent_code": "92", "valid_from": "2025-07-01",
			"valid_to": "2027-01-01", "successors": []string{"KT-KHONG-CO"}},
	}}
	if code, body := admin.adm("POST", "/v1/masterdata/admin-units/import", badRef); code != 422 || body["code"] != "masterdata.admin_unit_invalid_reference" {
		t.Fatalf("kế thừa không tồn tại: %d %v", code, body)
	}
	// Chồng lấp hiệu lực cùng mã → 409.
	overlap := map[string]any{"format": "agrigreen.admin_units/v1", "source": "x", "units": []map[string]any{
		{"code": "31153", "name": "Phường Ô Môn 2", "level": "commune", "parent_code": "92", "valid_from": "2026-01-01"},
	}}
	if code, body := admin.adm("POST", "/v1/masterdata/admin-units/import", overlap); code != 409 || body["code"] != "masterdata.admin_unit_overlap" {
		t.Fatalf("chồng lấp: %d %v", code, body)
	}
	// Không có vai masterdata_admin → 403.
	if code, _ := u.adm("POST", "/v1/masterdata/admin-units/import", split); code != 403 {
		t.Fatalf("thiếu vai: %d", code)
	}
}

// Descendants theo thời điểm: cây sau sắp xếp chỉ có tỉnh → xã; cây cũ có tỉnh → huyện.
// Đây là nền của phạm vi theo cây TW → Tỉnh → Xã (party.ScopeResolver, ADR-0009).
func TestDescendantsAtTime(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	u := f.caller(tid)
	units := f.mod.AdminUnits()

	now, err := units.Descendants(u.ctx(), "91", day("2026-01-01"))
	if err != nil || now[0] != "91" || !slices.Contains(now, "30742") || !slices.Contains(now, "30307") {
		t.Fatalf("An Giang mới: %v %v", now, err)
	}
	if slices.Contains(now, "883") || slices.Contains(now, "31135") {
		t.Fatalf("lẫn đơn vị ngoài tỉnh/ngoài thời điểm: %v", now)
	}
	// 30742 năm 2024 là phường của Kiên Giang (91 cũ), không thuộc An Giang cũ (89).
	old, err := units.Descendants(u.ctx(), "89", day("2024-01-01"))
	if err != nil || !slices.Contains(old, "883") || !slices.Contains(old, "30280") || slices.Contains(old, "30742") {
		t.Fatalf("An Giang cũ 2024: %v %v", old, err)
	}
	if _, err := units.Descendants(u.ctx(), "89", day("2026-01-01")); !errors.Is(err, partyport.ErrUnitNotFound) {
		t.Fatalf("89 không còn hiệu lực năm 2026: %v", err)
	}

	// HTTP: cây và danh sách con có cursor.
	code, tree := u.get("/v1/masterdata/admin-units/96/tree?at=2026-01-01")
	if code != 200 || tree["code"] != "96" || len(tree["children"].([]any)) < 10 {
		t.Fatalf("cây Cà Mau: %d %v", code, tree)
	}
	code, p1 := u.get("/v1/masterdata/admin-units?at=2026-01-01&limit=4")
	cur, _ := p1["next_cursor"].(string)
	if code != 200 || len(p1["items"].([]any)) != 4 || cur == "" {
		t.Fatalf("trang 1 tỉnh: %d %v", code, p1)
	}
	_, p2 := u.get("/v1/masterdata/admin-units?at=2026-01-01&limit=4&cursor=" + cur)
	if len(p2["items"].([]any)) != 2 || p2["next_cursor"] != nil {
		t.Fatalf("trang 2 tỉnh (6 tỉnh sau sắp xếp): %v", p2)
	}
	_, old14 := u.get("/v1/masterdata/admin-units?at=2024-01-01&limit=50")
	if len(old14["items"].([]any)) != 14 {
		t.Fatalf("trước sắp xếp phải có 14 tỉnh: %v", old14)
	}
}

// Descendants(tỉnh) trả đủ mọi đơn vị con của tỉnh theo danh mục chính thức (số liệu
// khớp docs/requirements/danh-muc-hanh-chinh.md): sau 01/07/2025 là tỉnh + các xã;
// ngày 30/06/2025 là tỉnh cũ + huyện + xã/phường/thị trấn.
func TestDescendantsCoverWholeProvince(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	u := f.caller(tid)
	units := f.mod.AdminUnits()

	check := func(code, at string, want int) {
		t.Helper()
		got, err := units.Descendants(u.ctx(), code, day(at))
		if err != nil || len(got) != want || got[0] != code {
			t.Errorf("Descendants(%s, %s): %d đơn vị, %v; muốn %d", code, at, len(got), err, want)
			return
		}
		seen := map[string]bool{}
		for _, c := range got {
			if seen[c] {
				t.Errorf("Descendants(%s, %s): trùng %s", code, at, c)
			}
			seen[c] = true
		}
	}
	// Tỉnh mới: 1 + số xã/phường/đặc khu.
	for code, n := range map[string]int{"91": 102, "92": 103, "82": 102, "86": 124, "96": 64, "80": 96} {
		check(code, "2026-01-01", 1+n)
	}
	// Tỉnh cũ ngày 30/06/2025: 1 + số huyện + số xã.
	for code, n := range map[string][2]int{
		"89": {11, 155}, "91": {15, 143}, "87": {12, 141}, "82": {11, 164}, "86": {8, 102}, "83": {9, 148}, "84": {9, 104},
		"92": {9, 80}, "94": {11, 108}, "93": {8, 75}, "96": {9, 100}, "95": {7, 64}, "72": {9, 94}, "80": {15, 186},
	} {
		check(code, "2025-06-30", 1+n[0]+n[1])
	}
}

func propose(t *testing.T, c caller, key, value, eff string, correction *string) string {
	t.Helper()
	body := map[string]any{"key": key, "value": value, "unit": "ha/máy/vụ", "effective_from": eff, "source_doc": "QĐ số 1/QĐ-KTHT"}
	if correction != nil {
		body["correction_reason"] = *correction
	}
	code, b := c.adm("POST", "/v1/masterdata/params/versions", body)
	if code != 201 {
		t.Fatalf("đề xuất %s=%s: %d %v", key, value, code, b)
	}
	id := b["id"].(string)
	if code, b := c.adm("POST", "/v1/masterdata/params/versions/"+id+"/submit", map[string]any{}); code != 200 || b["status"] != "submitted" {
		t.Fatalf("gửi duyệt: %d %v", code, b)
	}
	return id
}

func approve(t *testing.T, c caller, id string) map[string]any {
	t.Helper()
	code, b := c.adm("POST", "/v1/masterdata/params/versions/"+id+"/approve", map[string]any{})
	if code != 200 {
		t.Fatalf("phê duyệt: %d %v", code, b)
	}
	return b
}

// R-56: người đặt không tự phê duyệt được, kể cả khi có cả hai vai.
func TestSelfApprovalBlocked(t *testing.T) {
	f := newFixture(t)
	tid := ids.New()
	both := f.caller(tid, port.RoleParamSetter, port.RoleParamApprover)
	id := propose(t, both, "cgh.norm_ha_per_machine:MGD:thu_hoach", "90", "2026-01-01", nil)

	code, b := both.adm("POST", "/v1/masterdata/params/versions/"+id+"/approve", map[string]any{})
	if code != 403 || b["code"] != "self_approval_forbidden" {
		t.Fatalf("tự phê duyệt phải bị chặn: %d %v", code, b)
	}
	code, b = both.adm("POST", "/v1/masterdata/params/versions/"+id+"/reject", map[string]any{"note": "Tự từ chối cũng không được"})
	if code != 403 || b["code"] != "self_approval_forbidden" {
		t.Fatalf("tự từ chối phải bị chặn: %d %v", code, b)
	}
	// Chưa duyệt → chưa có giá trị.
	if _, err := f.mod.Params().At(both.ctx(), "cgh.norm_ha_per_machine:MGD:thu_hoach", day("2026-05-01")); !errors.Is(err, params.ErrParamNotFound) {
		t.Fatalf("chưa duyệt không được có giá trị: %v", err)
	}
	// Người thiếu vai param_approver → 403 forbidden.
	setter2 := f.caller(tid, port.RoleParamSetter)
	if code, b := setter2.adm("POST", "/v1/masterdata/params/versions/"+id+"/approve", map[string]any{}); code != 403 || b["code"] != "forbidden" {
		t.Fatalf("thiếu vai: %d %v", code, b)
	}
	// Người duyệt khác → được.
	approver := f.caller(tid, port.RoleParamApprover)
	b = approve(t, approver, id)
	if b["version"].(map[string]any)["status"] != "approved" || b["approved"].(map[string]any)["value"] != "90" {
		t.Fatalf("duyệt: %v", b)
	}
	// Đã duyệt thì bất biến: duyệt lại / từ chối → 409.
	if code, b := approver.adm("POST", "/v1/masterdata/params/versions/"+id+"/reject", map[string]any{"note": "Đổi ý sau khi duyệt"}); code != 409 {
		t.Fatalf("từ chối bản đã duyệt: %d %v", code, b)
	}
	// Và ở DB: vai ứng dụng không sửa được phiên bản đã duyệt, không sửa/xóa được sổ.
	pool := f.env.Pool(t, migrations.PF, masterdata.DBRole)
	for _, q := range []string{
		"UPDATE pf_masterdata.param_version SET source_doc = 'sửa lén' WHERE status = 'approved'",
		"UPDATE pf_masterdata.param_approved SET value_num = 1",
		"DELETE FROM pf_masterdata.param_approved",
		"DELETE FROM pf_masterdata.param_version",
	} {
		err := tenant.WithTenantTx(context.Background(), pool, tid, func(tx pgx.Tx) error { _, e := tx.Exec(context.Background(), q); return e })
		if err == nil {
			t.Errorf("phải thất bại: %s", q)
		}
	}
}

// CGH QT-01 / AC-02: vụ cũ tính bằng định mức hiệu lực lúc đó, vụ mới bằng định mức mới.
func TestParamLookupAtEventTime(t *testing.T) {
	f := newFixture(t)
	tid := ids.New()
	setter := f.caller(tid, port.RoleParamSetter)
	approver := f.caller(tid, port.RoleParamApprover)
	key := port.NormKey("MGD", "thu_hoach")

	approve(t, approver, propose(t, setter, key, "90", "2025-01-01", nil))
	approve(t, approver, propose(t, setter, key, "100.5", "2026-01-01", nil))
	// Bản nháp mới hơn chưa duyệt không được dùng.
	propose(t, setter, key, "999", "2026-06-01", nil)

	prm := f.mod.Params()
	for _, c := range []struct {
		at   string
		want string
		ver  int
	}{
		{"2025-03-15", "90", 1},    // Đông Xuân 2024–2025
		{"2025-12-31", "90", 1},    // ngày cuối trước hiệu lực mới (giờ Việt Nam)
		{"2026-01-01", "100.5", 2}, // 00:00 giờ Việt Nam ngày hiệu lực
		{"2026-09-01", "100.5", 2}, // nháp 999 chưa duyệt
	} {
		pv, err := prm.At(setter.ctx(), key, day(c.at))
		if err != nil || !pv.Value.Equal(decimal.RequireFromString(c.want)) || pv.Version != c.ver {
			t.Errorf("At(%s) = %v v%d, %v; muốn %s v%d", c.at, pv.Value, pv.Version, err, c.want, c.ver)
		}
	}
	if _, err := prm.At(setter.ctx(), key, day("2024-06-01")); !errors.Is(err, params.ErrParamNotFound) {
		t.Fatalf("trước mọi phiên bản: %v", err)
	}
	// Application API.
	reader := f.caller(tid)
	code, b := reader.get("/v1/masterdata/params/value?key=" + key + "&at=2025-06-01")
	if code != 200 || b["value"] != "90" || b["version"] != 1.0 {
		t.Fatalf("HTTP value: %d %v", code, b)
	}
	code, b = reader.get("/v1/masterdata/params/value?key=cgh.khong_co&at=2025-06-01")
	if code != 404 || b["code"] != "masterdata.param_not_found" {
		t.Fatalf("không có: %d %v", code, b)
	}
	// Lịch sử phiên bản (mọi trạng thái), phân trang.
	code, b = setter.adm("GET", "/v1/masterdata/params/versions?limit=2&key="+key, nil)
	if code != 200 || len(b["items"].([]any)) != 2 || b["next_cursor"] == nil {
		t.Fatalf("lịch sử trang 1: %d %v", code, b)
	}
	code, b = setter.adm("GET", "/v1/masterdata/params/versions?limit=2&key="+key+"&cursor="+b["next_cursor"].(string), nil)
	if code != 200 || len(b["items"].([]any)) != 1 || b["items"].([]any)[0].(map[string]any)["status"] != "submitted" {
		t.Fatalf("lịch sử trang 2: %d %v", code, b)
	}
	// Sự kiện pf.masterdata.param.approved: đúng 2.
	pub := f.env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	var n int
	if err := pub.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox
	    WHERE tenant_id = $1 AND event_type = 'pf.masterdata.param.approved'`, tid).Scan(&n); err != nil || n != 2 {
		t.Fatalf("outbox: %d %v", n, err)
	}
}

// Sửa sai định mức đã duyệt bằng bản ghi bù: cùng effective_from, bắt buộc lý do,
// bản mới trỏ supersedes_id về bản cũ; bản cũ vẫn nằm trong sổ.
func TestParamCorrectionSupersedes(t *testing.T) {
	f := newFixture(t)
	tid := ids.New()
	setter := f.caller(tid, port.RoleParamSetter)
	approver := f.caller(tid, port.RoleParamApprover)
	key := port.NormKey("MLD", "lam_dat")
	first := approve(t, approver, propose(t, setter, key, "120", "2026-01-01", nil))

	body := map[string]any{"key": key, "value": "125", "unit": "ha/máy/vụ", "effective_from": "2026-01-01", "source_doc": "QĐ đính chính"}
	if code, b := setter.adm("POST", "/v1/masterdata/params/versions", body); code != 422 || b["code"] != "masterdata.param_correction_required" {
		t.Fatalf("thiếu lý do bù: %d %v", code, b)
	}
	reason := "Đính chính số liệu theo văn bản 15/QĐ-KTHT"
	second := approve(t, approver, propose(t, setter, key, "125", "2026-01-01", &reason))
	sup := second["approved"].(map[string]any)["supersedes_id"]
	if sup == nil {
		t.Fatalf("bản ghi bù phải có supersedes_id: %v", second)
	}
	pv, err := f.mod.Params().At(setter.ctx(), key, day("2026-02-01"))
	if err != nil || pv.Value.String() != "125" || pv.Version != 2 {
		t.Fatalf("sau bản ghi bù: %+v %v", pv, err)
	}
	pool := f.env.Pool(t, migrations.PF, masterdata.DBRole)
	err = tenant.WithTenantTx(context.Background(), pool, tid, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_masterdata.param_approved WHERE param_key = $1", key).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			t.Errorf("sổ phải giữ cả hai bản, có %d", n)
		}
		r, err := ledger.Verify(context.Background(), tx, "pf_masterdata.param_approved", tid, "param:"+key)
		if err != nil {
			return err
		}
		if !r.OK || r.RowsChecked != 2 {
			t.Errorf("chuỗi băm: %+v", r)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = first
}

// Danh mục: ngừng dùng không xóa, lịch sử giữ đủ trước → sau; không xóa được ở DB.
func TestCatalogDeactivateKeepsHistory(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	reader := f.caller(tid)

	code, b := admin.adm("POST", "/v1/masterdata/catalogs/rice_variety/items", map[string]any{
		"code": "OM380", "name": "OM380", "attrs": map[string]string{"growth_days": "95", "expected_yield_t_per_ha": "6.2"}})
	if code != 201 || b["active"] != true {
		t.Fatalf("thêm giống: %d %v", code, b)
	}
	if code, b := admin.adm("PUT", "/v1/masterdata/catalogs/rice_variety/items/OM380", map[string]any{
		"name": "OM380 (chịu mặn)", "attrs": map[string]string{"growth_days": "96", "expected_yield_t_per_ha": "6.2"}}); code != 200 || b["revision"] != 2.0 {
		t.Fatalf("sửa: %d %v", code, b)
	}
	if code, b := admin.adm("POST", "/v1/masterdata/catalogs/rice_variety/items/OM380/deactivate", map[string]any{"reason": "ngắn"}); code != 422 {
		t.Fatalf("lý do quá ngắn: %d %v", code, b)
	}
	code, b = admin.adm("POST", "/v1/masterdata/catalogs/rice_variety/items/OM380/deactivate", map[string]any{"reason": "Giống thoái hóa, ngừng khuyến cáo từ vụ Hè Thu 2026"})
	if code != 200 || b["active"] != false || b["deactivation_reason"] == nil {
		t.Fatalf("ngừng dùng: %d %v", code, b)
	}
	if code, b := admin.adm("POST", "/v1/masterdata/catalogs/rice_variety/items/OM380/deactivate", map[string]any{"reason": "Ngừng dùng lần thứ hai"}); code != 409 || b["code"] != "masterdata.catalog_item_state" {
		t.Fatalf("ngừng lần hai: %d %v", code, b)
	}

	// Application API: danh sách mặc định chỉ mục đang dùng; mục vẫn đọc được.
	_, list := reader.get("/v1/masterdata/catalogs/rice_variety?limit=200")
	for _, it := range list["items"].([]any) {
		if it.(map[string]any)["code"] == "OM380" {
			t.Fatal("mục ngừng dùng không được trong danh sách mặc định")
		}
	}
	_, all := reader.get("/v1/masterdata/catalogs/rice_variety?limit=200&include_inactive=true")
	if len(all["items"].([]any)) != len(list["items"].([]any))+1 {
		t.Fatalf("include_inactive phải thấy mục đã ngừng: %v", all)
	}
	if code, it := reader.get("/v1/masterdata/catalogs/rice_variety/OM380"); code != 200 || it["active"] != false {
		t.Fatalf("mục vẫn còn: %d %v", code, it)
	}
	pi, err := f.mod.Catalogs().Get(reader.ctx(), port.CatalogRiceVariety, "OM380")
	if err != nil || pi.Active {
		t.Fatalf("port: %+v %v", pi, err)
	}

	// Lịch sử: created → updated → deactivated, có trước → sau và lý do.
	code, h := admin.adm("GET", "/v1/masterdata/catalogs/rice_variety/items/OM380/history", nil)
	items, _ := h["items"].([]any)
	if code != 200 || len(items) != 3 {
		t.Fatalf("lịch sử: %d %v", code, h)
	}
	actions := []string{}
	for _, it := range items {
		actions = append(actions, it.(map[string]any)["action"].(string))
	}
	if !slices.Equal(actions, []string{"created", "updated", "deactivated"}) {
		t.Fatalf("hành động: %v", actions)
	}
	upd := items[1].(map[string]any)
	if upd["before"].(map[string]any)["name"] != "OM380" || upd["after"].(map[string]any)["name"] != "OM380 (chịu mặn)" {
		t.Fatalf("trước → sau: %v", upd)
	}

	// Không xóa cứng ở DB; sổ lịch sử có chuỗi băm nguyên vẹn.
	pool := f.env.Pool(t, migrations.PF, masterdata.DBRole)
	err = tenant.WithTenantTx(context.Background(), pool, tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "DELETE FROM pf_masterdata.catalog_item WHERE code = 'OM380'")
		return e
	})
	if err == nil {
		t.Fatal("xóa mục danh mục phải thất bại")
	}
	err = tenant.WithTenantTx(context.Background(), pool, tid, func(tx pgx.Tx) error {
		r, e := ledger.Verify(context.Background(), tx, "pf_masterdata.catalog_change", tid, "catalog:rice_variety:OM380")
		if e == nil && (!r.OK || r.RowsChecked != 3) {
			t.Errorf("chuỗi băm: %+v", r)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}

	// Loại máy phải gắn khâu đang dùng.
	if code, b := admin.adm("POST", "/v1/masterdata/catalogs/machine_type/items", map[string]any{
		"code": "MXX", "name": "Máy lạ", "attrs": map[string]string{"farm_stage": "khong_co"}}); code != 422 || b["code"] != "masterdata.farm_stage_invalid" {
		t.Fatalf("khâu không tồn tại: %d %v", code, b)
	}
	// Cờ thí điểm có trong dữ liệu nền.
	st, err := f.mod.Catalogs().Get(reader.ctx(), port.CatalogFarmStage, "thu_hoach")
	if err != nil || st.Attrs["pilot"] != "true" {
		t.Fatalf("cờ thí điểm: %+v %v", st, err)
	}
	// Không có vai → 403.
	if code, _ := reader.adm("POST", "/v1/masterdata/catalogs/pest/items", map[string]any{"code": "x", "name": "x", "attrs": map[string]string{"kind": "sau_hai"}}); code != 403 {
		t.Fatalf("thiếu vai: %d", code)
	}
}

// RB-2 / R-16: tenant khác không thấy dữ liệu chủ, tham số hay danh mục của nhau.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	a := f.seeded()
	b := ids.New()
	adminA := f.caller(a, port.RoleMasterdataAdmin)
	if code, body := adminA.adm("POST", "/v1/masterdata/catalogs/pest/items", map[string]any{
		"code": "sau_duc_than", "name": "Sâu đục thân", "attrs": map[string]string{"kind": "sau_hai"}}); code != 201 {
		t.Fatalf("tạo: %d %v", code, body)
	}
	rb := f.caller(b)
	if code, body := rb.get("/v1/masterdata/catalogs/pest/sau_duc_than"); code != 404 || body["code"] != "masterdata.catalog_item_not_found" {
		t.Fatalf("tenant B đọc mục của A: %d %v", code, body)
	}
	if _, body := rb.get("/v1/masterdata/catalogs/pest"); len(body["items"].([]any)) != 0 {
		t.Fatalf("tenant B thấy danh mục của A: %v", body)
	}
	if code, _ := rb.get("/v1/masterdata/admin-units/91?at=2026-01-01"); code != 404 {
		t.Fatalf("tenant B thấy đơn vị hành chính của A: %d", code)
	}
	if _, err := f.mod.Params().At(rb.ctx(), port.ParamCGHCoverageSufficient, day("2026-06-01")); !errors.Is(err, params.ErrParamNotFound) {
		t.Fatalf("tenant B thấy tham số của A: %v", err)
	}
	// Tenant A thấy ngưỡng QT-02 đã nạp sẵn.
	ra := f.caller(a)
	for key, want := range map[string]string{
		port.ParamCGHCoverageSufficient: "85", port.ParamCGHCoverageAttention: "60", port.ParamCGHCoverageSurplusFrom: "120",
	} {
		pv, err := f.mod.Params().At(ra.ctx(), key, day("2026-06-01"))
		if err != nil || pv.Value.String() != want || pv.Unit != "%" {
			t.Errorf("%s = %+v %v; muốn %s", key, pv, err, want)
		}
	}
	// Idempotency-Key của tenant A không đụng tenant B: cùng khóa, tenant khác → tạo mới.
	key := "k-" + ids.New().String()
	adminB := f.caller(b, port.RoleMasterdataAdmin)
	body := map[string]any{"code": "chuot", "name": "Chuột", "attrs": map[string]string{"kind": "dong_vat_hai"}}
	if code, _ := adminB.do(f.admin, "POST", "/v1/masterdata/catalogs/pest/items", key, body); code != 201 {
		t.Fatalf("B tạo: %d", code)
	}
	if code, _ := adminA.do(f.admin, "POST", "/v1/masterdata/catalogs/pest/items", key, body); code != 409 {
		t.Fatalf("A có sẵn 'chuot' từ dữ liệu nền nên phải 409, nhận %d", code)
	}
}

// R-51: gọi lặp POST cùng Idempotency-Key → cùng phản hồi, một bản ghi, kể cả song song.
func TestIdempotentPost(t *testing.T) {
	f := newFixture(t)
	tid := ids.New()
	setter := f.caller(tid, port.RoleParamSetter)
	body := map[string]any{"key": "gis.basemap.source", "text": "OSM Việt Nam", "effective_from": "2026-01-01", "source_doc": "BRD GIS v1.3"}
	key := "k-" + ids.New().String()

	code1, b1 := setter.do(f.admin, "POST", "/v1/masterdata/params/versions", key, body)
	code2, b2 := setter.do(f.admin, "POST", "/v1/masterdata/params/versions", key, body)
	if code1 != 201 || code2 != 201 || b1["id"] != b2["id"] || b1["version"] != 1.0 {
		t.Fatalf("gọi lặp: %d %v / %d %v", code1, b1, code2, b2)
	}
	// Song song với khóa khác nhau nhưng cùng tham số: số phiên bản không trùng.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			setter.do(f.admin, "POST", "/v1/masterdata/params/versions", "k-"+ids.New().String(), body)
		}()
	}
	wg.Wait()
	pool := f.env.Pool(t, migrations.PF, masterdata.DBRole)
	err := tenant.WithTenantTx(context.Background(), pool, tid, func(tx pgx.Tx) error {
		var n, distinct int
		if err := tx.QueryRow(context.Background(), `SELECT count(*), count(DISTINCT version) FROM pf_masterdata.param_version
		    WHERE param_key = 'gis.basemap.source'`).Scan(&n, &distinct); err != nil {
			return err
		}
		if n != 6 || distinct != 6 {
			t.Errorf("phải có 6 phiên bản khác số, có %d/%d", n, distinct)
		}
		var m int
		if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_masterdata.param_version WHERE idem_key = $1", key).Scan(&m); err != nil {
			return err
		}
		if m != 1 {
			t.Errorf("khóa lặp phải chỉ có 1 bản ghi, có %d", m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Thiếu Idempotency-Key → 400.
	if code, b := setter.do(f.admin, "POST", "/v1/masterdata/params/versions", "", body); code != 400 || b["code"] != "idempotency_key_required" {
		t.Fatalf("thiếu khóa: %d %v", code, b)
	}
}

// Bề mặt Admin bắt buộc MFA (mục 11) — token không có yếu tố thứ hai bị chặn.
func TestAdminRequiresMFA(t *testing.T) {
	f := newFixture(t)
	c := f.caller(ids.New(), port.RoleMasterdataAdmin)
	c.token = f.iss.Sign(t, authztest.Token{Subject: c.actor, TenantID: c.tid, Roles: []string{port.RoleMasterdataAdmin}})
	if code, b := c.adm("POST", "/v1/masterdata/seed", map[string]any{}); code != 403 || b["code"] != "mfa_required" {
		t.Fatalf("không MFA: %d %v", code, b)
	}
}

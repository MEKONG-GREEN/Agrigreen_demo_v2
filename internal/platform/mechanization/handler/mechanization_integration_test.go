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
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	app    *httptest.Server
	admin  *httptest.Server
	mod    *mechanization.Module
	pool   *pgxpool.Pool
	clk    *clock.Fixed
	orgs   *fakeOrgs
	params *fakeParams
	scope  *fakeScope
	tenant uuid.UUID
	// Tổ chức của người gọi và hai HTX mẫu.
	dcrd, adminOrg, provOrg uuid.UUID
	c1, c2                  uuid.UUID // c1: xã 30001 (An Giang); c2: xã 31001 (Cần Thơ)
}

func newFixture(t *testing.T, now time.Time) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, mechanization.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	f := &fixture{
		t: t, iss: iss, pool: pool, clk: clock.NewFixed(now), tenant: ids.New(),
		orgs: &fakeOrgs{orgs: map[uuid.UUID]partyport.Org{}}, params: &fakeParams{vals: map[string][]mdport.ParamValue{}},
		dcrd: ids.New(), adminOrg: ids.New(), provOrg: ids.New(), c1: ids.New(), c2: ids.New(),
	}
	f.scope = &fakeScope{byOrg: map[uuid.UUID]authz.Scope{
		f.dcrd: {All: true}, f.adminOrg: {All: true}, f.provOrg: {Units: []string{"92", "31001"}},
	}}
	f.orgs.put(partyport.Org{Ref: f.c1, Kind: partyport.OrgCooperative, Code: "HTX000001", Name: "HTX Lúa Xanh", AdminUnitCode: "30001", Active: true})
	f.orgs.put(partyport.Org{Ref: f.c2, Kind: partyport.OrgCooperative, Code: "HTX000002", Name: "HTX Cần Thơ Xanh", AdminUnitCode: "31001", Active: true})
	f.mod = mechanization.New(pool, f.clk, mechanization.Deps{
		Scope: f.scope, Orgs: f.orgs, Params: f.params, Catalogs: newFakeCatalogs(), AdminUnits: newFakeUnits(), Vault: clienttest.New(),
	})
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	appEng.Use(logErrors(t))
	f.mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 1<<20)
	adminEng.Use(logErrors(t))
	f.mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	f.app, f.admin = httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(f.app.Close)
	t.Cleanup(f.admin.Close)
	// Ngưỡng QT-02 theo BRD và định mức làm đất, có hiệu lực từ 2025.
	f.params.approve(mdport.ParamCGHCoverageSufficient, "85", "2025-01-01")
	f.params.approve(mdport.ParamCGHCoverageAttention, "60", "2025-01-01")
	f.params.approve(mdport.ParamCGHCoverageSurplusFrom, "120", "2025-01-01")
	f.params.approve(mdport.NormKey("MLD", "lam_dat"), "100", "2025-01-01")
	return f
}

// logErrors in nguyên nhân lỗi 5xx vào log kiểm thử (không lộ ra client).
func logErrors(t *testing.T) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		for _, e := range c.Errors {
			t.Logf("%s %s: %v", c.Request.Method, c.FullPath(), e.Err)
		}
	}
}

var defaultNow = time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC)

type caller struct {
	f     *fixture
	tid   uuid.UUID
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})
	return caller{f: f, tid: tid, token: tok, actor: actor}
}

func (f *fixture) adminUser() caller  { return f.as(f.tenant, f.adminOrg, port.RoleCGHAdmin) }
func (f *fixture) viewerUser() caller { return f.as(f.tenant, f.dcrd, port.RoleCGHViewer) }

type resp struct {
	status int
	hdr    http.Header
	body   map[string]any
}

func (c caller) call(srv *httptest.Server, method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	req.Header.Set("Content-Type", ctype)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, hdr: r.Header, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) do(srv *httptest.Server, method, path string, body any) resp {
	c.f.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	idem := ""
	if method == http.MethodPost {
		idem = key()
	}
	return c.call(srv, method, path, idem, "application/json", b)
}

func (c caller) get(path string) resp {
	return c.do(c.f.app, http.MethodGet, "/v1/mechanization"+path, nil)
}
func (c caller) post(path string, body any) resp {
	return c.do(c.f.admin, http.MethodPost, "/v1/mechanization"+path, body)
}

func str(m map[string]any, path ...string) string {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	switch v := cur.(type) {
	case string:
		return v
	case float64:
		return decimal.NewFromFloat(v).String()
	case bool:
		if v {
			return "true"
		}
		return "false"
	}
	return ""
}

func num(m map[string]any, path ...string) int {
	n, _ := decimal.NewFromString(str(m, path...))
	return int(n.IntPart())
}

func list(m map[string]any, k string) []map[string]any {
	raw, _ := m[k].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

func (c caller) mustOK(r resp, want int, what string) resp {
	c.f.t.Helper()
	if r.status != want {
		c.f.t.Fatalf("%s: muốn %d, nhận %d %v", what, want, r.status, r.body)
	}
	return r
}

// owner tạo chủ sở hữu loại "Thành viên HTX" của HTX coop và trả id.
func (c caller) owner(coop uuid.UUID, phone string) string {
	c.f.t.Helper()
	r := c.mustOK(c.post("/owners", map[string]any{
		"owner_type": "thanh_vien_htx", "cooperative_ref": coop.String(), "name": "Nguyễn Văn Máy", "phone": phone,
	}), http.StatusCreated, "tạo chủ sở hữu")
	return str(r.body, "owner", "id")
}

func (c caller) machine(coop uuid.UUID, owner, typ, serial, ownedSince string) map[string]any {
	c.f.t.Helper()
	r := c.mustOK(c.post("/machines", map[string]any{
		"cooperative_ref": coop.String(), "machine_type": typ, "owner_id": owner, "brand": "Kubota", "model": "L4018",
		"serial_no": serial, "owned_since": ownedSince, "status": "Hoạt động", "year_made": 2020, "capacity_ha": "12.5",
	}), http.StatusCreated, "tạo máy "+serial)
	return r.body["machine"].(map[string]any)
}

func (c caller) area(coop uuid.UUID, season string, year int, ha string, lock bool) map[string]any {
	c.f.t.Helper()
	r := c.mustOK(c.post("/season-areas", map[string]any{
		"cooperative_ref": coop.String(), "season_code": season, "season_year": year, "area_ha": ha, "lock": lock,
	}), http.StatusCreated, "nhập diện tích")
	return r.body["area"].(map[string]any)
}

func (c caller) compute(season string, year int) map[string]any {
	c.f.t.Helper()
	r := c.mustOK(c.post("/balance-runs", map[string]any{"season_code": season, "season_year": year}), http.StatusCreated, "cân đối")
	return r.body["run"].(map[string]any)
}

// results trả kết quả đã lưu theo (HTX, chủng loại).
func (c caller) results(runID string) map[string]map[string]any {
	c.f.t.Helper()
	r := c.mustOK(c.get("/balance-runs/"+runID), http.StatusOK, "đọc kết quả")
	out := map[string]map[string]any{}
	for _, it := range list(r.body, "results") {
		out[it["cooperative_ref"].(string)+"|"+it["machine_type"].(string)] = it
	}
	return out
}

// ---------------------------------------------------------------------------

// FN-04/FN-05 BR-02, ADR-0009: mã CSH-/MAY-<tỉnh>-00001 cấp một lần; HTX/máy đổi tỉnh không đổi mã.
func TestCodesAssignedOnceAndStableOnProvinceMove(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()

	// FN-04 BR-04: Thành viên HTX phải liên kết đúng một HTX.
	r := adm.post("/owners", map[string]any{"owner_type": "Thành viên HTX", "name": "Trần Văn Bình", "phone": "0901234567"})
	if r.status != http.StatusUnprocessableEntity || str(r.body, "code") != "validation_failed" {
		t.Fatalf("thiếu HTX liên kết phải 422: %d %v", r.status, r.body)
	}
	o1 := adm.owner(f.c1, "0901234567")
	got := adm.mustOK(adm.get("/owners/"+o1), http.StatusOK, "đọc chủ sở hữu").body
	if str(got, "code") != "CSH-AG-00001" || str(got, "person_ref") == "" || strings.Contains(str(got, "label"), "Nguyễn Văn Máy") {
		t.Fatalf("CSH-AG-00001, định danh ở kho, nhãn đã che: %v", got)
	}
	// Doanh nghiệp không gắn HTX → CSH-XX-.
	dn := adm.mustOK(adm.post("/owners", map[string]any{"owner_type": "doanh_nghiep", "name": "Công ty Cơ giới Mekong", "phone": "0912000111"}),
		http.StatusCreated, "doanh nghiệp").body
	if str(dn, "owner", "code") != "CSH-XX-00001" {
		t.Fatalf("doanh nghiệp không gắn HTX: %v", dn)
	}
	// Chống trùng theo SĐT qua kho định danh.
	dup := adm.post("/owners", map[string]any{"owner_type": "khac", "name": "Tên khác", "phone": "090 123 4567"})
	if dup.status != http.StatusConflict || str(dup.body, "code") != "mechanization.owner_duplicate" || str(dup.body, "existing_code") != "CSH-AG-00001" {
		t.Fatalf("trùng SĐT phải 409: %d %v", dup.status, dup.body)
	}

	m1 := adm.machine(f.c1, o1, "MLD", "sn-001", "2025-06-01")
	m2 := adm.machine(f.c1, o1, "MLD", "SN-002", "2025-06-01")
	if m1["code"] != "MAY-AG-00001" || m2["code"] != "MAY-AG-00002" {
		t.Fatalf("mã máy theo tỉnh HTX: %v %v", m1["code"], m2["code"])
	}
	// HTX c1 chuyển sang Cần Thơ (sáp nhập địa giới): máy cũ giữ mã, máy mới lấy nhãn tỉnh mới.
	f.orgs.move(f.c1, "31001")
	if g := adm.get("/machines/" + m1["ref"].(string)); str(g.body, "code") != "MAY-AG-00001" {
		t.Fatalf("mã máy không đổi khi HTX đổi tỉnh: %v", g.body)
	}
	m3 := adm.machine(f.c1, o1, "MGD", "SN-003", "2025-06-01")
	if m3["code"] != "MAY-CT-00001" {
		t.Fatalf("máy mới sau khi đổi tỉnh: %v", m3["code"])
	}
	// Máy chuyển sang HTX tỉnh khác: vẫn giữ mã.
	upd := adm.mustOK(adm.post("/machines/"+m2["ref"].(string)+"/update", map[string]any{"cooperative_ref": f.c2.String()}), http.StatusOK, "chuyển HTX").body
	if str(upd, "machine", "code") != "MAY-AG-00002" || str(upd, "machine", "cooperative_ref") != f.c2.String() || str(upd, "event", "action") != "updated" {
		t.Fatalf("chuyển HTX giữ mã: %v", upd)
	}
}

// FN-05 BR-01/BR-03, AC-01/AC-03: Số máy/SN duy nhất trong tenant; khâu suy ra từ chủng loại.
func TestSerialUniqueAndStageDerived(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o := adm.owner(f.c1, "0903000001")
	m := adm.machine(f.c1, o, "MGD", "ABC-123", "2025-01-10")
	if m["farm_stage"] != "thu_hoach" || m["serial_no"] != "ABC-123" {
		t.Fatalf("khâu tự gán theo chủng loại: %v", m)
	}
	o2 := adm.owner(f.c2, "0903000002")
	r := adm.post("/machines", map[string]any{
		"cooperative_ref": f.c2.String(), "machine_type": "MLD", "owner_id": o2, "brand": "Yanmar", "model": "X", "serial_no": " abc-123 ", "owned_since": "2025-01-01",
	})
	if r.status != http.StatusConflict || str(r.body, "code") != "mechanization.serial_taken" || str(r.body, "existing_code") != m["code"] ||
		!strings.Contains(str(r.body["errors"].([]any)[0].(map[string]any), "reason"), "HTX Lúa Xanh") {
		t.Fatalf("trùng SN (kể cả khác HTX, khác hoa thường) phải 409 kèm HTX: %d %v", r.status, r.body)
	}
	r = adm.post("/machines", map[string]any{
		"cooperative_ref": f.c1.String(), "machine_type": "MLD", "farm_stage": "thu_hoach", "owner_id": o, "serial_no": "Z-1", "owned_since": "2025-01-01",
	})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("không nhập khâu độc lập: %d %v", r.status, r.body)
	}
	r = adm.post("/machines", map[string]any{"cooperative_ref": f.c1.String(), "machine_type": "XYZ", "owner_id": o, "serial_no": "Z-2", "owned_since": "2025-01-01"})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("chủng loại ngoài danh mục: %d %v", r.status, r.body)
	}
	r = adm.post("/machines", map[string]any{"cooperative_ref": f.c1.String(), "machine_type": "MLD", "owner_id": o, "serial_no": "Z-3", "owned_since": "2026-12-01"})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("Ngày HTX sở hữu ở tương lai: %d %v", r.status, r.body)
	}
	// Đổi chủng loại → khâu đổi theo.
	u := adm.mustOK(adm.post("/machines/"+m["ref"].(string)+"/update", map[string]any{"machine_type": "MLD"}), http.StatusOK, "đổi chủng loại").body
	if str(u, "machine", "farm_stage") != "lam_dat" {
		t.Fatalf("khâu theo chủng loại mới: %v", u)
	}
}

// FN-05 BR-09/BR-11, US-MAC-03: số máy tại T = sở hữu ≤ T và (chưa vô hiệu hóa hoặc vô hiệu hóa > T);
// tình trạng tại T đọc từ sổ.
func TestMachineCountAtT(t *testing.T) {
	f := newFixture(t, time.Date(2026, 4, 20, 3, 0, 0, 0, time.UTC))
	adm := f.adminUser()
	o := adm.owner(f.c1, "0904000001")
	a := adm.machine(f.c1, o, "MLD", "T-1", "2026-03-15")
	b := adm.machine(f.c1, o, "MGD", "T-2", "2026-01-10")
	adm.mustOK(adm.post("/machines/"+b["ref"].(string)+"/deactivate", map[string]any{"deactivated_on": "2026-04-01", "reason": "Bán máy cho HTX khác"}),
		http.StatusOK, "vô hiệu hóa")
	bad := adm.post("/machines/"+a["ref"].(string)+"/deactivate", map[string]any{"deactivated_on": "2026-03-01", "reason": "Ngày sai trước sở hữu"})
	if bad.status != http.StatusUnprocessableEntity {
		t.Fatalf("Ngày vô hiệu hóa trước Ngày HTX sở hữu phải 422: %d", bad.status)
	}
	f.clk.Advance(160 * 24 * time.Hour) // 27/09/2026
	adm.mustOK(adm.post("/machines/"+a["ref"].(string)+"/status", map[string]any{"status": "Hỏng", "effective_at": "2026-05-01T00:00:00+07:00"}),
		http.StatusOK, "đổi tình trạng")

	count := func(at string) (int, int) {
		r := adm.mustOK(adm.get("/cooperatives/"+f.c1.String()+"/machine-count?at="+at), http.StatusOK, "số máy tại "+at)
		return num(r.body, "total"), num(r.body, "active")
	}
	cases := []struct {
		at            string
		total, active int
	}{
		{"2026-01-09", 0, 0}, // trước mọi ngày sở hữu
		{"2026-03-01", 1, 1}, // chỉ máy B
		{"2026-03-20", 2, 2}, // A và B
		{"2026-04-01", 1, 1}, // B vô hiệu hóa ngày 01/04 → không còn tính ngày đó
		{"2026-04-30", 1, 1}, // A còn Hoạt động
		{"2026-05-01", 1, 0}, // A Hỏng từ 01/05 — không tính năng lực
	}
	for _, c := range cases {
		if total, active := count(c.at); total != c.total || active != c.active {
			t.Errorf("T=%s: tổng %d, hoạt động %d; muốn %d, %d", c.at, total, active, c.total, c.active)
		}
	}
	hist := adm.mustOK(adm.do(f.admin, http.MethodGet, "/v1/mechanization/machines/"+a["ref"].(string)+"/events", nil), http.StatusOK, "lịch sử").body
	if evs := list(hist, "items"); len(evs) != 2 || evs[0]["action"] != "created" || evs[1]["action"] != "status_changed" {
		t.Fatalf("lịch sử biến động máy: %v", evs)
	}
}

// US-MAC-02, FN-05 BR-10: "Cập nhật" không đụng máy vắng mặt; "Thay thế toàn bộ" vô hiệu hóa
// máy vắng mặt bằng SỰ KIỆN kèm Ngày vô hiệu hóa — không xóa; kết quả theo từng dòng.
func TestImportUpdateAndReplace(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o := adm.owner(f.c1, "0905000001")
	ownerCode := str(adm.get("/owners/"+o).body, "code")
	a1 := adm.machine(f.c1, o, "MLD", "A1", "2025-02-01")
	a2 := adm.machine(f.c1, o, "MLD", "A2", "2025-02-01")

	importCSV := func(mode, csv string) resp {
		return adm.call(f.admin, http.MethodPost, "/v1/mechanization/machines/import?cooperative_ref="+f.c1.String()+"&mode="+mode, key(), "text/csv", []byte(csv))
	}
	header := "Chủng loại,Hãng,Model,Năm SX,Nhiên liệu,Công suất,Năng suất,Mã CSH,Số máy,Số khung,Ngày sở hữu,Tình trạng\n"
	csv := header +
		"MLD,Iseki,NT540,2021,Dầu,40,15,{o},A1,,01/02/2025,Hoạt động\n" + // cập nhật hãng
		"MGD,Kubota,DC70,2022,Dầu,70,20,{o},A3,,15/03/2025,Bảo trì\n" + // mới
		"MLD,Kubota,X,2020,Dầu,30,10,{o},A3,,01/01/2025,Hoạt động\n" + // trùng trong tệp
		"XYZ,Kubota,X,2020,Dầu,30,10,{o},A9,,01/01/2025,Hoạt động\n" // chủng loại sai
	r := adm.mustOK(importCSV("update", strings.ReplaceAll(csv, "{o}", ownerCode)), http.StatusOK, "nhập Cập nhật").body
	if num(r, "created") != 1 || num(r, "updated") != 1 || num(r, "rejected") != 2 || len(list(r, "deactivated")) != 0 {
		t.Fatalf("kết quả nhập Cập nhật: %v", r)
	}
	rows := list(r, "rows")
	if rows[2]["status"] != "rejected" || !strings.Contains(rows[2]["reason"].(string), "trùng với dòng 3") {
		t.Fatalf("lý do dòng trùng: %v", rows[2])
	}
	if g := adm.get("/machines/" + a1["ref"].(string)).body; str(g, "brand") != "Iseki" {
		t.Fatalf("A1 được cập nhật: %v", g)
	}
	if g := adm.get("/machines/" + a2["ref"].(string)).body; g["active"] != true {
		t.Fatalf("chế độ Cập nhật không đụng A2: %v", g)
	}
	// Sai mẫu: có cột Mã máy → từ chối cả tệp.
	if bad := importCSV("update", "Mã máy,"+header); bad.status != http.StatusUnprocessableEntity || str(bad.body, "code") != "mechanization.import_invalid" {
		t.Fatalf("tệp có cột Mã máy phải bị từ chối: %d %v", bad.status, bad.body)
	}

	// Thay thế toàn bộ: tệp chỉ còn A1 → A2, A3 chuyển Inactive bằng sự kiện.
	r = adm.mustOK(importCSV("replace", header+"MLD,Iseki,NT540,2021,Dầu,40,15,"+ownerCode+",A1,,01/02/2025,Hoạt động\n"), http.StatusOK, "Thay thế toàn bộ").body
	if num(r, "unchanged") != 1 || len(list(r, "deactivated")) != 2 {
		t.Fatalf("kết quả Thay thế toàn bộ: %v", r)
	}
	all := adm.mustOK(adm.get("/machines?cooperative_ref="+f.c1.String()+"&include_inactive=true"), http.StatusOK, "liệt kê").body
	if items := list(all, "items"); len(items) != 3 {
		t.Fatalf("không xóa máy nào: %v", items)
	}
	g := adm.get("/machines/" + a2["ref"].(string)).body
	if g["active"] != false || str(g, "deactivated_on") != "2026-09-27" {
		t.Fatalf("A2 Inactive kèm Ngày vô hiệu hóa: %v", g)
	}
	hist := list(adm.do(f.admin, http.MethodGet, "/v1/mechanization/machines/"+a2["ref"].(string)+"/events", nil).body, "items")
	last := hist[len(hist)-1]
	if last["action"] != "deactivated" || last["source"] != "import" || !strings.Contains(last["reason"].(string), "Thay thế toàn bộ") {
		t.Fatalf("vô hiệu hóa là sự kiện trong sổ: %v", last)
	}
	if act := list(adm.get("/machines?cooperative_ref="+f.c1.String()).body, "items"); len(act) != 1 {
		t.Fatalf("chỉ còn A1 đang hoạt động: %v", act)
	}
}

// FN-09 BR-01/BR-05, US-CFG-02 AC-2 (QT-01): vụ cũ dùng định mức hiệu lực tại vụ đó kể cả
// sau khi định mức mới được duyệt; FN-08 BR-06: chỉ khâu thí điểm; QT-02 không cắt ở 100%.
func TestBalanceUsesNormAtSeasonAndPilotStages(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o1, o2 := adm.owner(f.c1, "0906000001"), adm.owner(f.c2, "0906000002")
	for _, sn := range []string{"L1", "L2", "L3"} {
		adm.machine(f.c1, o1, "MLD", sn, "2025-06-01")
	}
	adm.machine(f.c1, o1, "MSL", "DRY-1", "2025-06-01") // khâu sau thu hoạch — không thí điểm
	for _, sn := range []string{"K1", "K2", "K3", "K4", "K5", "K6"} {
		adm.machine(f.c2, o2, "MLD", sn, "2025-06-01")
	}
	adm.area(f.c1, "DX", 2025, "500", false)
	adm.area(f.c2, "DX", 2025, "500", false)

	run1 := adm.compute("DX", 2025)
	if run1["reference_date"] != "2025-11-01" || run1["rule_version"] != "cgh.balance/v1" {
		t.Fatalf("T = ngày bắt đầu vụ: %v", run1)
	}
	if nm, _ := run1["missing_norms"].([]any); len(nm) != 1 || nm[0] != "cgh.norm_ha_per_machine:MGD:thu_hoach" {
		t.Fatalf("thiếu định mức máy gặt: %v", run1["missing_norms"])
	}
	res := adm.results(run1["id"].(string))
	if len(res) != 4 { // 2 HTX × 2 chủng loại thí điểm (MLD, MGD); MSL bị loại
		t.Fatalf("chỉ khâu thí điểm: %d dòng %v", len(res), res)
	}
	for k := range res {
		if strings.HasSuffix(k, "|MSL") {
			t.Fatal("khâu không thí điểm không được cân đối")
		}
	}
	r1 := res[f.c1.String()+"|MLD"]
	if str(r1, "required") != "5" || num(r1, "capacity") != 3 || str(r1, "coverage_pct") != "60" || r1["status"] != "attention" ||
		num(r1, "norm", "version") != 1 || str(r1, "area_source_label") != "Nhập tay" {
		t.Fatalf("HTX c1: 500 ha ÷ 100 = 5 máy, 3 hoạt động → 60%% Cần chú ý: %v", r1)
	}
	if r2 := res[f.c2.String()+"|MLD"]; str(r2, "coverage_pct") != "120" || r2["status"] != "surplus" {
		t.Fatalf("HTX c2: 6/5 = 120%% Thừa (không cắt ở 100): %v", r2)
	}
	if rg := res[f.c1.String()+"|MGD"]; rg["status"] != "no_norm" || rg["norm"] != nil {
		t.Fatalf("máy gặt chưa có định mức: %v", rg)
	}

	// DCRD ban hành định mức mới 50 ha/máy/vụ từ 01/06/2026.
	f.params.approve(mdport.NormKey("MLD", "lam_dat"), "50", "2026-06-01")
	again := adm.results(adm.compute("DX", 2025)["id"].(string))[f.c1.String()+"|MLD"]
	if num(again, "norm", "version") != 1 || str(again, "coverage_pct") != "60" {
		t.Fatalf("vụ DX-2025 vẫn dùng định mức cũ: %v", again)
	}
	adm.area(f.c1, "DX", 2026, "500", false)
	newer := adm.results(adm.compute("DX", 2026)["id"].(string))[f.c1.String()+"|MLD"]
	if num(newer, "norm", "version") != 2 || str(newer, "required") != "10" || str(newer, "coverage_pct") != "30" || newer["status"] != "shortage" {
		t.Fatalf("vụ DX-2026 dùng định mức mới: %v", newer)
	}

	// Bản đồ theo tỉnh và dashboard đọc kết quả đã lưu.
	cov := adm.mustOK(adm.get("/coverage?season_code=DX&season_year=2025&level=province"), http.StatusOK, "bản đồ tỉnh").body
	items := list(cov, "items")
	if len(items) != 2 || items[0]["province_code"] != "89" || str(items[0], "overall", "status") != "attention" || items[0]["name"] != "An Giang" {
		t.Fatalf("mức đáp ứng theo tỉnh: %v", items)
	}
	dash := adm.mustOK(adm.get("/dashboard?season_code=DX&season_year=2025"), http.StatusOK, "dashboard").body
	if num(dash, "kpis", "total_machines") != 10 || num(dash, "kpis", "total_cooperatives") != 2 || num(dash, "kpis", "cooperatives_sufficient") != 1 {
		t.Fatalf("4 thẻ chỉ số: %v", dash["kpis"])
	}
	if pr := list(dash, "priority"); len(pr) != 2 || pr[0]["cooperative_ref"] != f.c1.String() {
		t.Fatalf("HTX cần ưu tiên — mức thấp nhất trước: %v", pr)
	}
	trend := list(adm.mustOK(adm.get("/trend?season_code=DX&farm_stage=lam_dat"), http.StatusOK, "xu hướng").body, "items")
	if len(trend) != 2 || str(trend[0], "run", "season_year") != "2025" || str(trend[1], "summary", "coverage_pct") != "30" {
		t.Fatalf("xu hướng nhiều năm theo vụ đã lưu: %v", trend)
	}
	panel := adm.mustOK(adm.get("/cooperatives/"+f.c1.String()+"/panel?season_code=DX&season_year=2025"), http.StatusOK, "panel").body
	ms := list(panel, "machines")
	if len(ms) != 4 || ms[0]["owner_code"] == "" || strings.Contains(str(ms[0], "owner_label"), "Nguyễn Văn Máy") || str(panel, "coverage", "overall", "status") != "attention" {
		t.Fatalf("panel HTX: %v", panel)
	}
}

// QT-03, FN-06 BR-03: bản ghi nhập tay đã khóa không bị đồng bộ App HTX ghi đè; mở khóa thì App HTX ưu tiên trở lại.
func TestLockedManualAreaNotOverwritten(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	a := adm.area(f.c1, "HT", 2026, "120.5", true)
	if a["source"] != "manual" || a["locked"] != true || a["source_label"] != "Nhập tay" {
		t.Fatalf("nhập tay + khóa: %v", a)
	}
	feed := f.mod.AreaFeed()
	ctx := tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{
		ActorRef: ids.New(), TenantID: f.tenant, Roles: []string{port.RoleCGHSync},
	}), f.tenant)
	sync := func(ha string) port.AreaFeedResult {
		out, err := feed.ApplySyncedArea(ctx, port.SyncedArea{
			CooperativeRef: f.c1, SeasonCode: "HT", SeasonYear: 2026, AreaHa: decimal.RequireFromString(ha), IdemKey: key(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := sync("300"); out.Applied || out.Reason == "" {
		t.Fatalf("bản ghi khóa phải giữ nguyên: %+v", out)
	}
	cur := list(adm.get("/season-areas?season_code=HT&season_year=2026").body, "items")
	if len(cur) != 1 || str(cur[0], "area_ha") != "120.5" || cur[0]["source"] != "manual" {
		t.Fatalf("giá trị nhập tay còn nguyên: %v", cur)
	}
	adm.mustOK(adm.post("/season-areas/"+a["id"].(string)+"/unlock", map[string]any{"reason": "App HTX đã có dữ liệu"}), http.StatusOK, "mở khóa")
	if out := sync("300"); !out.Applied {
		t.Fatalf("mở khóa → App HTX ưu tiên trở lại: %+v", out)
	}
	cur = list(adm.get("/season-areas?season_code=HT&season_year=2026").body, "items")
	if str(cur[0], "area_ha") != "300" || cur[0]["source_label"] != "App HTX" {
		t.Fatalf("nhãn nguồn [App HTX]: %v", cur)
	}
	hist := list(adm.do(f.admin, http.MethodGet, "/v1/mechanization/season-areas/"+a["id"].(string)+"/events", nil).body, "items")
	if len(hist) != 3 || hist[0]["action"] != "recorded" || hist[1]["action"] != "unlocked" || hist[2]["action"] != "synced" {
		t.Fatalf("lịch sử thay đổi giữ giá trị nhập tay: %v", hist)
	}
	if r := adm.post("/season-areas", map[string]any{"cooperative_ref": f.c1.String(), "season_code": "HT", "season_year": 2026, "area_ha": "10",
		"calendar": []map[string]any{{"stage": "lam_dat", "start": "2026-04-10", "end": "2026-04-05"}}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("ngày kết thúc trước ngày bắt đầu: %d", r.status)
	}
}

// FN-09 BR-05, FN-10 BR-04: kết quả đã lưu bất biến, chuỗi băm xác minh được; báo cáo không tính lại.
func TestSavedResultsImmutableAndChainVerifies(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o := adm.owner(f.c1, "0907000001")
	adm.machine(f.c1, o, "MLD", "IM-1", "2025-01-01")
	adm.area(f.c1, "DX", 2025, "100", false)
	run := adm.compute("DX", 2025)
	runID := uuid.MustParse(run["id"].(string))

	ctx := context.Background()
	err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE pf_mechanization.balance_result SET capacity = 99 WHERE run_id = $1", runID)
		return err
	})
	if !errors.Is(ledger.Classify(err), ledger.ErrImmutable) {
		t.Fatalf("sửa kết quả đã lưu phải bị chặn: %v", err)
	}
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		for _, c := range []struct{ table, stream string }{
			{"pf_mechanization.balance_result", "run:" + runID.String()},
			{"pf_mechanization.balance_run", "season:DX-2025"},
		} {
			v, err := ledger.Verify(ctx, tx, c.table, f.tenant, c.stream)
			if err != nil {
				return err
			}
			if !v.OK || v.RowsChecked == 0 {
				t.Errorf("chuỗi %s: %+v", c.stream, v)
			}
		}
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM shared.outbox WHERE event_type = $1", port.EventBalanceComputed).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Errorf("sự kiện pf.mechanization.balance.computed: %d", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Diện tích đổi sau khi tính: báo cáo vẫn đọc số đã lưu.
	adm.area(f.c1, "DX", 2025, "1000", false)
	cov := adm.mustOK(adm.get("/coverage?run_id="+runID.String()+"&level=cooperative"), http.StatusOK, "báo cáo").body
	if it := list(cov, "items"); len(it) != 1 || str(it[0], "overall", "coverage_pct") != "100" {
		t.Fatalf("báo cáo đọc kết quả đã lưu, không tính lại: %v", it)
	}
}

// FN-01 BR-01, ma trận mục VI: Cục KTHT xem toàn vùng, không ghi được (403).
func TestViewerReadsAllButCannotWrite(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o1, o2 := adm.owner(f.c1, "0908000001"), adm.owner(f.c2, "0908000002")
	adm.machine(f.c1, o1, "MLD", "V-1", "2025-01-01")
	adm.machine(f.c2, o2, "MLD", "V-2", "2025-01-01")
	adm.area(f.c1, "DX", 2025, "100", false)
	adm.compute("DX", 2025)

	viewer := f.viewerUser()
	if items := list(viewer.mustOK(viewer.get("/machines"), http.StatusOK, "Cục xem máy").body, "items"); len(items) != 2 {
		t.Fatalf("Cục thấy toàn vùng: %v", items)
	}
	if items := list(viewer.mustOK(viewer.get("/coverage?season_code=DX&season_year=2025&level=cooperative"), http.StatusOK, "Cục xem bản đồ").body, "items"); len(items) != 2 {
		t.Fatalf("Cục xem mức đáp ứng mọi HTX: %v", items)
	}
	viewer.mustOK(viewer.get("/owners"), http.StatusOK, "Cục xem chủ sở hữu")
	for path, body := range map[string]any{
		"/owners":       map[string]any{"owner_type": "khac", "name": "X", "phone": "0909000009"},
		"/machines":     map[string]any{"cooperative_ref": f.c1.String(), "machine_type": "MLD"},
		"/season-areas": map[string]any{"cooperative_ref": f.c1.String(), "season_code": "DX", "season_year": 2025, "area_ha": "1"},
		"/balance-runs": map[string]any{"season_code": "DX", "season_year": 2025},
	} {
		if r := viewer.post(path, body); r.status != http.StatusForbidden {
			t.Errorf("Cục ghi %s phải 403, nhận %d", path, r.status)
		}
	}
	// Phạm vi hẹp (tỉnh 92) chỉ thấy HTX trong tỉnh.
	prov := f.as(f.tenant, f.provOrg, port.RoleCGHAdmin)
	cov := list(prov.mustOK(prov.get("/coverage?season_code=DX&season_year=2025&level=cooperative"), http.StatusOK, "tỉnh").body, "items")
	if len(cov) != 1 || cov[0]["cooperative_ref"] != f.c2.String() {
		t.Fatalf("phạm vi tỉnh 92: %v", cov)
	}
	if r := prov.post("/season-areas", map[string]any{"cooperative_ref": f.c1.String(), "season_code": "DX", "season_year": 2025, "area_ha": "5"}); r.status != http.StatusForbidden {
		t.Fatalf("ghi ngoài phạm vi phải 403: %d", r.status)
	}
}

// RB-2: tenant khác không thấy dữ liệu.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o := adm.owner(f.c1, "0910000001")
	m := adm.machine(f.c1, o, "MLD", "ISO-1", "2025-01-01")
	adm.area(f.c1, "DX", 2025, "100", false)
	run := adm.compute("DX", 2025)

	other := f.as(ids.New(), f.adminOrg, port.RoleCGHAdmin)
	if items := list(other.get("/machines").body, "items"); len(items) != 0 {
		t.Fatalf("tenant khác không thấy máy: %v", items)
	}
	if r := other.get("/machines/" + m["ref"].(string)); r.status != http.StatusNotFound {
		t.Fatalf("máy tenant khác → 404: %d", r.status)
	}
	if r := other.get("/balance-runs/" + run["id"].(string)); r.status != http.StatusNotFound {
		t.Fatalf("kết quả tenant khác → 404: %d", r.status)
	}
	if items := list(other.get("/owners").body, "items"); len(items) != 0 {
		t.Fatalf("tenant khác không thấy chủ sở hữu: %v", items)
	}
	// Cùng Số máy/SN ở tenant khác không trùng (duy nhất theo tenant).
	o2 := other.owner(f.c1, "0910000001")
	if mm := other.machine(f.c1, o2, "MLD", "ISO-1", "2025-01-01"); mm["code"] != "MAY-AG-00001" {
		t.Fatalf("tenant khác có bộ đếm mã riêng: %v", mm["code"])
	}
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng kết quả lần đầu, không ghi thêm.
func TestIdempotentPost(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o := adm.owner(f.c1, "0911000001")
	body, _ := json.Marshal(map[string]any{
		"cooperative_ref": f.c1.String(), "machine_type": "MLD", "owner_id": o, "serial_no": "IDEM-1", "owned_since": "2025-01-01",
	})
	k := key()
	first := adm.call(f.admin, http.MethodPost, "/v1/mechanization/machines", k, "application/json", body)
	second := adm.call(f.admin, http.MethodPost, "/v1/mechanization/machines", k, "application/json", body)
	if first.status != http.StatusCreated || second.status != http.StatusCreated || second.hdr.Get("Idempotent-Replayed") != "true" ||
		str(first.body, "machine", "ref") != str(second.body, "machine", "ref") {
		t.Fatalf("gọi lặp: %d %d %v", first.status, second.status, second.body)
	}
	if items := list(adm.get("/machines").body, "items"); len(items) != 1 {
		t.Fatalf("chỉ một máy: %v", items)
	}
	adm.area(f.c1, "DX", 2025, "100", false)
	rb, _ := json.Marshal(map[string]any{"season_code": "DX", "season_year": 2025})
	k2 := key()
	r1 := adm.call(f.admin, http.MethodPost, "/v1/mechanization/balance-runs", k2, "application/json", rb)
	r2 := adm.call(f.admin, http.MethodPost, "/v1/mechanization/balance-runs", k2, "application/json", rb)
	if str(r1.body, "run", "id") == "" || str(r1.body, "run", "id") != str(r2.body, "run", "id") {
		t.Fatalf("cân đối gọi lặp: %v %v", r1.body, r2.body)
	}
	if runs := list(adm.get("/balance-runs?season_code=DX&season_year=2025").body, "items"); len(runs) != 1 {
		t.Fatalf("một lần cân đối: %v", runs)
	}
	if short := adm.post("/owners/"+o+"/deactivate", map[string]any{"reason": "ngắn"}); short.status != http.StatusUnprocessableEntity {
		t.Fatalf("lý do vô hiệu hóa quá ngắn: %d", short.status)
	}
	if r := adm.post("/owners/"+o+"/deactivate", map[string]any{"reason": "Chủ máy chuyển nơi khác"}); r.status != http.StatusConflict ||
		str(r.body, "code") != "mechanization.owner_has_machines" || num(r.body, "active_machines") != 1 {
		t.Fatalf("FN-04 BR-05: chủ còn máy → 409: %d %v", r.status, r.body)
	}
	r := adm.mustOK(adm.post("/owners/"+o+"/deactivate", map[string]any{"reason": "Chủ máy chuyển nơi khác", "with_machines": true}), http.StatusOK, "vô hiệu hóa kèm máy").body
	if len(list(r, "deactivated_machines")) != 1 || str(r, "owner", "active") != "false" {
		t.Fatalf("vô hiệu hóa kèm máy: %v", r)
	}
}

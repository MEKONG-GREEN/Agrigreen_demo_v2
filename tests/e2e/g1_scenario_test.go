package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	mrvport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Địa bàn của kịch bản (mã chính thức QĐ 19/2025/QĐ-TTg trong bộ seed ĐBSCL của pf/masterdata).
const (
	provCT     = "92"    // TP Cần Thơ
	communeNK  = "31135" // Phường Ninh Kiều
	provAG     = "91"    // An Giang — "tỉnh khác"
	communeLX  = "30307" // Phường Long Xuyên
	nkLat      = 10.03
	nkLon      = 105.77
	seasonYear = 2026 // vụ Đông Xuân 2026–2027: ngày tham chiếu 01/11/2026, sau hiệu lực tham số CGH (01/01/2026)
)

// Định danh tổng hợp của kịch bản (R-95): dải SĐT 09990xxxxx, không có người thật.
// Chúng CHỈ được nằm trong kho định danh — kiểm ở cuối (pf, log, kho đối tượng).
var (
	pii = []string{
		"Nguyễn Văn Tổng Hợp", "0999000001", "Trần Thị Mô Phỏng", "0999000002", "Lê Văn Giả Lập", "0999000003",
		"079099000001", "Ấp Tổng Hợp 1, Ninh Kiều", "Phạm Văn Đại Diện", "0999000009", "Võ Văn Chủ Máy", "0999000010",
	}
)

// g1 là trạng thái dùng chung giữa các bước của kịch bản.
type g1 struct {
	t   *testing.T
	s   *harness.Stack
	tid uuid.UUID

	root, officer, officer91, provAdmin, nationalViewer *harness.User

	orgNational, orgP92, orgTeamNK, orgP91, orgTeam91, coopNK, coop91 string
	households                                                        []string // h1, h2, h3
	plots                                                             []string
	procedureRef, supplementRef, communeGuideRef                      string
	taskID                                                            string
	deviceID                                                          string
	pubkeys                                                           map[string][]byte // household_ref → khóa công khai kéo về
}

// TestG1DongXuanCanTho là kịch bản chấp nhận G1 — một vụ Đông Xuân ở Cần Thơ.
// Các bước chạy tuần tự trên một tenant; bước sau dùng dữ liệu của bước trước.
func TestG1DongXuanCanTho(t *testing.T) {
	s := harness.New(t)
	g := &g1{t: t, s: s, tid: harness.NewTenant(), pubkeys: map[string][]byte{}}
	g.root = s.User(harness.UserSpec{Tenant: g.tid, MFA: true,
		Roles: []string{"platform_admin", "masterdata_admin", "registry_admin", "vault_registrar"}})

	steps := []struct {
		name string
		fn   func()
	}{
		{"01_masterdata_seed", g.stepSeed},
		{"02_cay_to_chuc_va_HTX", g.stepOrgTree},
		{"03_nong_ho_qua_vault_chong_trung", g.stepHouseholds},
		{"04_dong_y_mrv_evidence", g.stepConsent},
		{"05_thua_va_pham_vi", g.stepPlots},
		{"06_thu_vien_quy_trinh", g.stepContent},
		{"07_nhiem_vu_SLA_leo_thang", g.stepTaskEscalation},
		{"08_bao_cao_gieo_sa_va_dashboard", g.stepFieldReportDashboard},
		{"09_CGH_can_doi", g.stepMechanization},
		{"10_MRV_danh_muc", g.stepMRVCatalog},
		{"11_dong_bo_pull_push_file", g.stepSync},
		{"12_thu_hoi_thiet_bi", g.stepRevoke},
		{"13_xuat_goi_MRV", g.stepMRVExport},
		{"14_khong_PII_ngoai_vault", g.stepNoPII},
	}
	for _, st := range steps {
		if !t.Run(st.name, func(t *testing.T) { g.t = t; st.fn() }) {
			t.Fatalf("bước %s hỏng — dừng kịch bản", st.name)
		}
	}
}

// ---------- trợ giúp kiểm ----------

func (g *g1) want(r harness.Resp, status int, what string) harness.Resp {
	g.t.Helper()
	if r.Status != status {
		g.t.Fatalf("%s: muốn %d, nhận %d %s", what, status, r.Status, trim(r.Raw))
	}
	return r
}

// wantProblem kiểm lỗi trả application/problem+json có code và trace_id (R-15, R-63).
func (g *g1) wantProblem(r harness.Resp, status int, code, what string) {
	g.t.Helper()
	if r.Status != status || r.Code() != code || !r.IsProblem() {
		g.t.Fatalf("%s: muốn problem+json %d %s, nhận %d %q %s", what, status, code, r.Status, r.Header.Get("Content-Type"), trim(r.Raw))
	}
}

func trim(b []byte) string {
	if len(b) > 600 {
		return string(b[:600]) + "…"
	}
	return string(b)
}

func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }
func str(m map[string]any, k string) string         { v, _ := m[k].(string); return v }
func num(m map[string]any, k string) float64        { v, _ := m[k].(float64); return v }

func dec(t *testing.T, v any) decimal.Decimal {
	t.Helper()
	switch x := v.(type) {
	case string:
		d, err := decimal.NewFromString(x)
		if err != nil {
			t.Fatalf("không đọc được số thập phân %q", x)
		}
		return d
	case float64:
		return decimal.NewFromFloat(x)
	case nil:
		return decimal.Zero
	}
	t.Fatalf("kiểu số lạ %T", v)
	return decimal.Zero
}

func vnPeriod() string { return time.Now().In(clock.Zone()).Format("2006-01") }

// outboxCount đếm sự kiện trong shared.outbox của tenant (vai ag_outbox_publisher).
func (g *g1) outboxCount(eventType, field, value string) int {
	g.t.Helper()
	var n int
	err := g.s.PF(dbadmin.OutboxPublisher).QueryRow(context.Background(),
		"SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2 AND payload->>$3 = $4",
		g.tid, eventType, field, value).Scan(&n)
	if err != nil {
		g.t.Fatal(err)
	}
	return n
}

// ---------- các bước ----------

// 01 — nạp dữ liệu chủ (đơn vị hành chính sau sáp nhập 2025, danh mục, tham số CGH).
func (g *g1) stepSeed() {
	noKey := g.root.DoRaw(harness.Admin, http.MethodPost, "/v1/masterdata/seed", http.Header{"Content-Type": {"application/json"}}, strings.NewReader("{}"))
	g.wantProblem(noKey, 400, "idempotency_key_required", "POST thiếu Idempotency-Key (R-51)")

	noMFA := g.s.User(harness.UserSpec{Tenant: g.tid, Roles: []string{"platform_admin"}})
	g.wantProblem(noMFA.Do(harness.Admin, http.MethodPost, "/v1/masterdata/seed", "", map[string]any{}), 403, "mfa_required", "Admin API không MFA")

	key := harness.NewIdem()
	r := g.want(g.root.Do(harness.Admin, http.MethodPost, "/v1/masterdata/seed", key, map[string]any{}), 200, "seed")
	if num(obj(r.Body, "admin_units"), "created") < 100 || num(r.Body, "params_created") < 9 {
		g.t.Fatalf("seed thiếu dữ liệu: %s", trim(r.Raw))
	}
	again := g.want(g.root.Do(harness.Admin, http.MethodPost, "/v1/masterdata/seed", key, map[string]any{}), 200, "seed gọi lặp")
	if again.Header.Get("Idempotent-Replayed") != "true" || string(again.Raw) != string(r.Raw) {
		g.t.Fatalf("gọi lặp cùng khóa phải trả đúng phản hồi cũ")
	}
	u := g.want(g.root.Do(harness.App, http.MethodGet, "/v1/masterdata/admin-units?parent="+provCT, "", nil), 200, "xã của Cần Thơ")
	found := false
	for _, it := range u.Items() {
		found = found || str(it, "code") == communeNK
	}
	if !found {
		g.t.Fatalf("không thấy %s trong xã của %s", communeNK, provCT)
	}
}

func (g *g1) createOrg(body map[string]any) string {
	g.t.Helper()
	if body["kind"] == "cooperative" {
		n := map[string]any{"name": body["name"], "admin_unit_code": body["admin_unit_code"], "parent_ref": body["parent_ref"]}
		r := g.want(g.root.Do(harness.Admin, http.MethodPost, "/v1/htx-anchors", "", map[string]any{"created_by": "mkg", "new_cooperative": n}), 201, "tạo HTX qua Neo")
		return str(r.Body, "org_ref")
	}
	r := g.want(g.root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", body), 201, "tạo tổ chức "+fmt.Sprint(body["name"]))
	return str(obj(r.Body, "org"), "ref")
}

// 02 — cây TTKN QG → TTKN tỉnh → tổ KN xã; HTX (người đại diện vào vault).
func (g *g1) stepOrgTree() {
	g.orgNational = g.createOrg(map[string]any{"kind": "ttkn_national", "code": "TTKNQG", "name": "Trung tâm Khuyến nông Quốc gia"})
	g.orgP92 = g.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN92", "name": "TTKN TP Cần Thơ", "admin_unit_code": provCT, "parent_ref": g.orgNational})
	g.orgTeamNK = g.createOrg(map[string]any{"kind": "extension_team", "code": "TKN-NINHKIEU", "name": "Tổ KNCĐ Ninh Kiều", "admin_unit_code": communeNK, "parent_ref": g.orgP92})
	g.orgP91 = g.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN91", "name": "TTKN An Giang", "admin_unit_code": provAG, "parent_ref": g.orgNational})
	g.orgTeam91 = g.createOrg(map[string]any{"kind": "extension_team", "code": "TKN-LONGXUYEN", "name": "Tổ KNCĐ Long Xuyên", "admin_unit_code": communeLX, "parent_ref": g.orgP91})

	r := g.want(g.root.Do(harness.Admin, http.MethodPost, "/v1/htx-anchors", "", map[string]any{
		"created_by": "mkg", "new_cooperative": map[string]any{
			"name": "HTX Nông nghiệp Tổng Hợp Ninh Kiều", "admin_unit_code": communeNK, "parent_ref": g.orgTeamNK,
			"representative": map[string]any{"full_name": "Phạm Văn Đại Diện", "phone": "0999000009"},
			"profile":        map[string]any{"area_ha": "180.5", "established_on": "2019-03-01", "location": map[string]any{"lat": "10.0341", "lon": "105.7702"}},
		},
	}), 201, "tạo HTX qua Neo")
	g.coopNK = str(r.Body, "org_ref")
	coop := g.want(g.root.Do(harness.App, http.MethodGet, "/v1/cooperatives/"+g.coopNK, "", nil), 200, "đọc hồ sơ HTX mới")
	if str(coop.Body, "representative_ref") == "" {
		g.t.Fatalf("HTX phải có đại diện ở vault: %s", trim(coop.Raw))
	}
	g.coop91 = g.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Tổng Hợp Long Xuyên", "admin_unit_code": communeLX, "parent_ref": g.orgTeam91})

	// Con không cùng địa bàn với cha → vi phạm cây phân cấp.
	bad := g.root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", map[string]any{
		"kind": "extension_team", "code": "TKN-SAI", "name": "Tổ sai tỉnh", "admin_unit_code": communeLX, "parent_ref": g.orgP92})
	g.wantProblem(bad, 422, "party.invalid_hierarchy", "tổ xã An Giang dưới TTKN Cần Thơ")

	uuidOf := func(s string) uuid.UUID { return uuid.MustParse(s) }
	g.officer = g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuidOf(g.orgTeamNK), Roles: []string{
		"kn_officer", "plot_editor", "plot_viewer", "vault_registrar", "vault_pubkey_sync", "consent_writer", "mrv_collector"}})
	g.officer91 = g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuidOf(g.orgTeam91), Roles: []string{"kn_officer", "plot_editor", "plot_viewer"}})
	g.provAdmin = g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuidOf(g.orgP92), Roles: []string{"kn_province_admin", "plot_viewer"}})
	g.nationalViewer = g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuidOf(g.orgNational), Roles: []string{"kn_national_admin", "plot_viewer"}})
}

// 03 — nông hộ: định danh vào vault, pf chỉ giữ ref; SĐT trùng → chống trùng qua chỉ mục mù.
func (g *g1) stepHouseholds() {
	ids := []map[string]any{
		{"full_name": "Nguyễn Văn Tổng Hợp", "phone": "0999000001", "national_id": "079099000001", "address": "Ấp Tổng Hợp 1, Ninh Kiều"},
		{"full_name": "Trần Thị Mô Phỏng", "phone": "0999000002"},
		{"full_name": "Lê Văn Giả Lập", "phone": "0999000003"},
	}
	for i, id := range ids {
		r := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/households", "", map[string]any{
			"cooperative_ref": g.coopNK, "cultivated_area_ha": fmt.Sprintf("%d.5", i+1), "member_since": "2023-05-01", "identity": id,
		}), 201, fmt.Sprintf("tạo hộ %d", i+1))
		if r.Body["duplicate"] != false {
			g.t.Fatalf("hộ %d không được báo trùng: %s", i+1, trim(r.Raw))
		}
		g.households = append(g.households, str(obj(r.Body, "household"), "household_ref"))
	}
	// Cùng SĐT với hộ 1 (khác tên) → 200 duplicate, trả lại đúng ref cũ.
	dup := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/households", "", map[string]any{
		"cooperative_ref": g.coopNK, "identity": map[string]any{"full_name": "Nguyễn Văn Tổng Hợp", "phone": "0999 000 001"},
	}), 200, "hộ trùng SĐT")
	if dup.Body["duplicate"] != true || str(obj(dup.Body, "household"), "household_ref") != g.households[0] {
		g.t.Fatalf("trùng SĐT phải trả duplicate + ref của hộ 1: %s", trim(dup.Raw))
	}
	// Tỉnh khác không ghi được hộ vào HTX Cần Thơ.
	g.wantProblem(g.officer91.Do(harness.App, http.MethodPost, "/v1/households", "", map[string]any{
		"cooperative_ref": g.coopNK, "identity": map[string]any{"full_name": "Người Ngoài Tỉnh", "phone": "0999000099"},
	}), 403, "party.out_of_scope", "cán bộ An Giang ghi hộ Cần Thơ")

	list := g.want(g.officer.Do(harness.App, http.MethodGet, "/v1/cooperatives/"+g.coopNK+"/households", "", nil), 200, "hộ của HTX")
	items := list.Items()
	if len(items) != 3 {
		g.t.Fatalf("HTX phải có đúng 3 hộ, có %d", len(items))
	}
	for _, it := range items {
		label := str(it, "label")
		if label == "" || it["label_unavailable"] == true {
			g.t.Fatalf("thiếu nhãn hiển thị tối thiểu: %v", it)
		}
		for _, p := range pii {
			if label == p || strings.Contains(str(it, "masked_phone"), p) {
				g.t.Fatalf("nhãn hiển thị phải che bớt, nhận %q", label)
			}
		}
	}
}

// 04 — đồng ý theo mục đích mrv.evidence cho hộ 1 (mục 10.5).
func (g *g1) stepConsent() {
	r := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/consents", "", map[string]any{
		"subject_ref": g.households[0], "purpose": mrvport.ConsentPurpose, "text_version": "2026-11", "status": "granted",
		"channel": "field_app", "event_time": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	}), 201, "ghi đồng ý")
	if g.outboxCount("pf.party.consent.recorded", "consent_id", str(r.Body, "id")) != 1 {
		g.t.Fatal("thiếu sự kiện pf.party.consent.recorded")
	}
}

// 05 — cán bộ xã vẽ 2 thửa; TTKN tỉnh thấy; cán bộ tỉnh khác không thấy, không ghi được.
func (g *g1) stepPlots() {
	shapes := []map[string]any{
		harness.Rect(nkLat, nkLon, 0, 0, 100, 100),  // ≈ 1 ha
		harness.Rect(nkLat, nkLon, 300, 0, 150, 80), // ≈ 1,2 ha, cách thửa 1 200 m
	}
	for i, geom := range shapes {
		r := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", map[string]any{
			"name": fmt.Sprintf("Thửa bờ kênh %d", i+1), "commune_code": communeNK, "cooperative_ref": g.coopNK,
			"household_ref": g.households[i], "geometry": geom, "gps_accuracy_m": "3.5", "source": "kn_web",
		}), 201, fmt.Sprintf("vẽ thửa %d", i+1))
		props := obj(obj(r.Body, "plot"), "properties")
		area := dec(g.t, props["area_ha"])
		if area.LessThan(decimal.NewFromFloat(0.9)) || area.GreaterThan(decimal.NewFromFloat(1.3)) || str(props, "status") != "active" {
			g.t.Fatalf("thửa %d: diện tích geodesic/trạng thái sai: %v", i+1, props)
		}
		g.plots = append(g.plots, str(props, "ref"))
	}
	bbox := "/v1/plots?bbox=105.75,10.01,105.80,10.06"
	seen := g.want(g.provAdmin.Do(harness.App, http.MethodGet, bbox, "", nil), 200, "TTKN tỉnh xem thửa")
	if n := len(seen.Items("features")); n != 2 {
		g.t.Fatalf("TTKN Cần Thơ phải thấy 2 thửa, thấy %d", n)
	}
	other := g.want(g.officer91.Do(harness.App, http.MethodGet, bbox, "", nil), 200, "tỉnh khác xem thửa")
	if n := len(other.Items("features")); n != 0 {
		g.t.Fatalf("cán bộ An Giang không được thấy thửa Cần Thơ, thấy %d", n)
	}
	g.wantProblem(g.officer91.Do(harness.App, http.MethodGet, "/v1/plots/"+g.plots[0], "", nil), 404, "geo.plot_not_found", "tỉnh khác đọc thửa")
	g.wantProblem(g.officer91.Do(harness.App, http.MethodPost, "/v1/plots", "", map[string]any{
		"commune_code": communeNK, "geometry": harness.Rect(nkLat, nkLon, 0, 600, 50, 50),
	}), 403, "geo.out_of_scope", "tỉnh khác vẽ thửa Cần Thơ")
	g.wantProblem(g.officer.Do(harness.App, http.MethodPost, "/v1/plots", "", map[string]any{
		"commune_code": communeNK, "geometry": map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.77, 10.03}, {105.771, 10.03}, {105.772, 10.03}, {105.77, 10.03}}}},
	}), 422, "geo.too_few_points", "3 điểm thẳng hàng (A2: tam giác thật hợp lệ)")
}

func (g *g1) publish(reviewer *harness.User, ref string) harness.Resp {
	g.t.Helper()
	return reviewer.Do(harness.Admin, http.MethodPost, "/v1/documents/"+ref+"/publish", "", map[string]any{"version_no": 1})
}

// 06 — quy trình quốc gia do người duyệt ≠ người soạn công bố; hướng dẫn bổ sung của tỉnh.
func (g *g1) stepContent() {
	org := func(s string) uuid.UUID { return uuid.MustParse(s) }
	natEditor := g.s.User(harness.UserSpec{Tenant: g.tid, Org: org(g.orgNational), MFA: true, Roles: []string{"content_editor_national", "content_reviewer"}})
	reviewer := g.s.User(harness.UserSpec{Tenant: g.tid, Org: org(g.orgNational), MFA: true, Roles: []string{"content_reviewer"}})
	provEditor := g.s.User(harness.UserSpec{Tenant: g.tid, Org: org(g.orgP92), MFA: true, Roles: []string{"content_editor_province"}})

	r := g.want(natEditor.Do(harness.Admin, http.MethodPost, "/v1/documents", "", map[string]any{
		"kind": "procedure", "audience": map[string]any{"level": "national"}, "title": "Quy trình canh tác lúa Đông Xuân giảm phát thải",
		"summary": "Quy trình chuẩn quốc gia", "body": "# Quy trình\n\nBước 1: làm đất, san phẳng mặt ruộng.", "tags": []string{"rice_variety:OM18", "farm_stage:gieo_sa", "season:DX"},
	}), 201, "soạn quy trình quốc gia")
	g.procedureRef = str(obj(r.Body, "document"), "ref")
	g.want(natEditor.Do(harness.Admin, http.MethodPost, "/v1/documents/"+g.procedureRef+"/submit", "", map[string]any{"version_no": 1}), 200, "gửi duyệt")
	g.wantProblem(g.publish(natEditor, g.procedureRef), 403, "self_approval_forbidden", "người soạn tự công bố (R-56)")
	pub := g.want(g.publish(reviewer, g.procedureRef), 200, "người duyệt công bố")
	if str(obj(pub.Body, "document"), "status") != "published" || str(obj(pub.Body, "document"), "publisher_ref") != reviewer.Actor.String() {
		g.t.Fatalf("công bố sai: %s", trim(pub.Raw))
	}
	if g.outboxCount("pf.content.document.published", "document_ref", g.procedureRef) != 1 {
		g.t.Fatal("thiếu sự kiện pf.content.document.published")
	}

	// Tỉnh không soạn được quy trình quốc gia.
	g.wantProblem(provEditor.Do(harness.Admin, http.MethodPost, "/v1/documents", "", map[string]any{
		"kind": "procedure", "audience": map[string]any{"level": "national"}, "title": "Không được", "body": "x", "tags": []string{"season:DX"},
	}), 403, "content.kind_not_allowed", "tỉnh soạn quy trình")

	sup := g.want(provEditor.Do(harness.Admin, http.MethodPost, "/v1/documents", "", map[string]any{
		"kind": "supplement", "audience": map[string]any{"level": "province", "units": []string{provCT}}, "parent_ref": g.procedureRef,
		"title": "Bổ sung Cần Thơ: xử lý đất phèn trước khi sạ", "body": "Bón vôi 300–500 kg/ha trước khi sạ 7 ngày.",
	}), 201, "soạn hướng dẫn bổ sung tỉnh")
	g.supplementRef = str(obj(sup.Body, "document"), "ref")
	g.want(provEditor.Do(harness.Admin, http.MethodPost, "/v1/documents/"+g.supplementRef+"/submit", "", map[string]any{"version_no": 1}), 200, "gửi duyệt bổ sung")
	g.want(g.publish(reviewer, g.supplementRef), 200, "công bố bổ sung")

	// Tài liệu cấp xã dùng mã xã của bộ seed (có "-", dài hơn 10 ký tự).
	cg := g.want(provEditor.Do(harness.Admin, http.MethodPost, "/v1/documents", "", map[string]any{
		"kind": "alert", "audience": map[string]any{"level": "commune", "units": []string{communeNK}},
		"title": "Cảnh báo rầy nâu Ninh Kiều", "body": "Mật độ rầy cao ở cánh đồng phía đông, thăm đồng hằng ngày.",
	}), 201, "soạn cảnh báo cấp xã")
	g.communeGuideRef = str(obj(cg.Body, "document"), "ref")
	g.want(provEditor.Do(harness.Admin, http.MethodPost, "/v1/documents/"+g.communeGuideRef+"/submit", "", map[string]any{"version_no": 1}), 200, "gửi duyệt cảnh báo")
	g.want(g.publish(reviewer, g.communeGuideRef), 200, "công bố cảnh báo")

	// Cán bộ xã đọc quy trình kèm bổ sung của tỉnh; cán bộ An Giang không thấy bổ sung Cần Thơ.
	doc := g.want(g.officer.Do(harness.App, http.MethodGet, "/v1/documents/"+g.procedureRef, "", nil), 200, "cán bộ xã đọc quy trình")
	sups := doc.Items("supplements")
	if len(sups) != 1 || str(sups[0], "ref") != g.supplementRef {
		g.t.Fatalf("quy trình phải kèm đúng 1 hướng dẫn bổ sung Cần Thơ: %s", trim(doc.Raw))
	}
	other := g.want(g.officer91.Do(harness.App, http.MethodGet, "/v1/documents/"+g.procedureRef, "", nil), 200, "An Giang đọc quy trình")
	if len(other.Items("supplements")) != 0 {
		g.t.Fatal("cán bộ An Giang không được thấy bổ sung của Cần Thơ")
	}
	g.wantProblem(g.officer91.Do(harness.App, http.MethodGet, "/v1/documents/"+g.supplementRef, "", nil), 404, "content.document_not_found", "An Giang đọc bổ sung Cần Thơ")
}

// 07 — nhiệm vụ có SLA 24 giờ, giao việc, job leo thang chạy với đồng hồ giả quá hạn.
func (g *g1) stepTaskEscalation() {
	g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/extension/staff", "", map[string]any{
		"actor_ref": g.officer.Actor.String(), "unit_code": communeNK, "role_title": "Khuyến nông viên xã", "display_name": "KNV Ninh Kiều",
	}), 200, "đăng ký danh bạ trực")
	created := time.Now()
	r := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", "", map[string]any{
		"commune_code": communeNK, "category": "sau_benh", "priority": "high", "cooperative_ref": g.coopNK,
		"description": "Rầy nâu mật độ cao ở cánh đồng phía đông, cần hướng dẫn phun thuốc",
	}), 201, "tạo nhiệm vụ")
	task := r.Body
	g.taskID = str(task, "task_id")
	due, err := time.Parse(time.RFC3339Nano, str(task, "due_at"))
	if err != nil || str(task, "sla_hours") != "24" || due.Sub(created) < 23*time.Hour || due.Sub(created) > 25*time.Hour {
		g.t.Fatalf("SLA mặc định 24 giờ (KN US-TASK-02): sla=%v due=%v", task["sla_hours"], task["due_at"])
	}
	if str(task, "escalation_org_ref") != g.orgP92 {
		g.t.Fatalf("nơi nhận leo thang phải là TTKN tỉnh %s, nhận %v", g.orgP92, task["escalation_org_ref"])
	}
	a := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks/"+g.taskID+"/assign", "", map[string]any{
		"assignee_ref": g.officer.Actor.String(), "note": "Tự nhận xử lý",
	}), 200, "giao nhiệm vụ")
	if str(obj(a.Body, "task"), "status") != "assigned" {
		g.t.Fatalf("trạng thái sau giao: %s", trim(a.Raw))
	}
	g.wantProblem(g.officer91.Do(harness.App, http.MethodGet, "/v1/extension/tasks/"+g.taskID, "", nil), 404, "extension.task_not_found", "tỉnh khác đọc nhiệm vụ")

	// Worker: module extension dựng như cmd/worker, đồng hồ giả đã qua hạn 1 giờ.
	fake := clock.NewFixed(due.Add(time.Hour))
	m := g.s.Mods
	worker := extension.New(g.s.PF(extension.DBRole), fake, extension.Deps{
		Scope: m.Party.ScopeResolver(), Orgs: m.Party.Orgs(), Units: m.MasterData.AdminUnits(), Params: m.MasterData.Params(), Catalogs: m.MasterData.Catalogs(),
	})
	jobs := worker.Jobs(extension.OverdueTenants(g.s.PF(dbadmin.OpsJobs)), nil)
	if len(jobs) != 1 || jobs[0].Name != extension.JobEscalation {
		g.t.Fatalf("job leo thang: %v", jobs)
	}
	for i := 0; i < 2; i++ { // chạy hai lần: job idempotent
		if err := jobs[0].Run(context.Background()); err != nil {
			g.t.Fatalf("job leo thang lần %d: %v", i+1, err)
		}
	}
	if n := g.outboxCount("pf.extension.task.escalated", "task_id", g.taskID); n != 1 {
		g.t.Fatalf("phải có đúng 1 sự kiện pf.extension.task.escalated, có %d", n)
	}
	got := g.want(g.provAdmin.Do(harness.App, http.MethodGet, "/v1/extension/tasks/"+g.taskID, "", nil), 200, "TTKN tỉnh đọc nhiệm vụ")
	tk := obj(got.Body, "task")
	if tk["escalated_at"] == nil || str(tk, "escalated_unit") != provCT {
		g.t.Fatalf("nhiệm vụ phải leo thang lên tỉnh %s: %s", provCT, trim(got.Raw))
	}
	escalations := 0
	for _, e := range got.Items("events") {
		if str(e, "action") == "escalated" {
			escalations++
		}
	}
	if escalations != 1 {
		g.t.Fatalf("phải có đúng 1 sự kiện leo thang trong lịch sử, có %d", escalations)
	}
}

// 08 — báo cáo tiến độ gieo sạ hai tỉnh; dashboard toàn quốc = tổng các tỉnh.
func (g *g1) stepFieldReportDashboard() {
	period := vnPeriod()
	report := func(u *harness.User, commune, coop, area string) {
		g.want(u.Do(harness.App, http.MethodPost, "/v1/extension/field-reports", "", map[string]any{
			"kind": "sowing_progress", "commune_code": commune, "cooperative_ref": coop, "period": period,
			"season_code": "DX", "variety_code": "OM5451", "sown_area_ha": area, "sown_on": time.Now().In(clock.Zone()).Format("2006-01-02"),
		}), 201, "báo cáo gieo sạ "+commune)
	}
	report(g.officer, communeNK, g.coopNK, "12.5")
	report(g.officer91, communeLX, g.coop91, "8.25")
	g.wantProblem(g.officer91.Do(harness.App, http.MethodPost, "/v1/extension/field-reports", "", map[string]any{
		"kind": "sowing_progress", "commune_code": communeNK, "cooperative_ref": g.coopNK, "period": period,
		"season_code": "DX", "variety_code": "OM5451", "sown_area_ha": "1",
	}), 403, "extension.out_of_scope", "tỉnh khác báo cáo cho Cần Thơ")

	nat := g.want(g.nationalViewer.Do(harness.App, http.MethodGet, "/v1/extension/dashboard?period="+period, "", nil), 200, "dashboard toàn quốc")
	if str(nat.Body, "level") != "national" {
		g.t.Fatalf("cấp dashboard: %v", nat.Body["level"])
	}
	total := obj(nat.Body, "total")
	children := nat.Items("children")
	if len(children) < 2 {
		g.t.Fatalf("dashboard toàn quốc phải có ít nhất 2 tỉnh con: %s", trim(nat.Raw))
	}
	sum := map[string]decimal.Decimal{}
	var p92 map[string]any
	for _, c := range children {
		k := obj(c, "kpi")
		for _, f := range []string{"sown_area_ha", "cooperatives_reporting", "open_tasks", "overdue_tasks", "directive_targets", "directive_acks", "expected_communes", "reported_communes"} {
			sum[f] = sum[f].Add(dec(g.t, k[f]))
		}
		if str(c, "unit") == provCT {
			p92 = k
		}
	}
	for f, v := range sum {
		if !dec(g.t, total[f]).Equal(v) {
			g.t.Fatalf("KPI %s: toàn quốc %v ≠ tổng các tỉnh %v", f, total[f], v)
		}
	}
	if !dec(g.t, total["sown_area_ha"]).Equal(decimal.RequireFromString("20.75")) {
		g.t.Fatalf("tổng diện tích gieo sạ phải 20,75 ha, nhận %v", total["sown_area_ha"])
	}
	prov := g.want(g.provAdmin.Do(harness.App, http.MethodGet, "/v1/extension/dashboard?unit="+provCT+"&period="+period, "", nil), 200, "dashboard tỉnh")
	if p92 == nil || !dec(g.t, obj(prov.Body, "total")["sown_area_ha"]).Equal(dec(g.t, p92["sown_area_ha"])) {
		g.t.Fatalf("dashboard tỉnh phải khớp ô tỉnh của dashboard toàn quốc: %s", trim(prov.Raw))
	}
	g.wantProblem(g.officer91.Do(harness.App, http.MethodGet, "/v1/extension/dashboard?unit="+provCT+"&period="+period, "", nil), 403, "extension.out_of_scope", "tỉnh khác xem dashboard Cần Thơ")
}

// 09 — CGH: chủ máy (vault), 3 máy, diện tích vụ nhập tay, chạy cân đối → kết quả lưu, phân loại theo ngưỡng.
func (g *g1) stepMechanization() {
	org := uuid.MustParse(g.orgNational)
	admin := g.s.User(harness.UserSpec{Tenant: g.tid, Org: org, MFA: true, Roles: []string{"cgh_admin", "vault_registrar"}})
	viewer := g.s.User(harness.UserSpec{Tenant: g.tid, Org: org, Roles: []string{"cgh_viewer"}})

	ow := g.want(admin.Do(harness.Admin, http.MethodPost, "/v1/mechanization/owners", "", map[string]any{
		"owner_type": "thanh_vien_htx", "cooperative_ref": g.coopNK, "name": "Võ Văn Chủ Máy", "phone": "0999000010",
	}), 201, "tạo chủ máy")
	owner := obj(ow.Body, "owner")
	if str(owner, "person_ref") == "" || !strings.HasPrefix(str(owner, "code"), "CSH-") {
		g.t.Fatalf("chủ máy phải có person_ref ở vault và mã CSH-: %s", trim(ow.Raw))
	}
	g.wantProblem(admin.Do(harness.Admin, http.MethodPost, "/v1/mechanization/owners", "", map[string]any{
		"owner_type": "thanh_vien_htx", "cooperative_ref": g.coopNK, "name": "Võ Văn Chủ Máy", "phone": "0999 000 010",
	}), 409, "mechanization.owner_duplicate", "chủ máy trùng SĐT")
	for i := 1; i <= 3; i++ {
		g.want(admin.Do(harness.Admin, http.MethodPost, "/v1/mechanization/machines", "", map[string]any{
			"cooperative_ref": g.coopNK, "machine_type": "MLD", "owner_id": str(owner, "id"), "brand": "Kubota", "model": "L4018",
			"serial_no": fmt.Sprintf("TH-SN-%03d", i), "year_made": 2021, "fuel": "Dầu", "power_hp": "40", "owned_since": "2025-06-01", "status": "Hoạt động",
		}), 201, fmt.Sprintf("tạo máy %d", i))
	}
	g.want(admin.Do(harness.Admin, http.MethodPost, "/v1/mechanization/season-areas", "", map[string]any{
		"cooperative_ref": g.coopNK, "season_code": "DX", "season_year": seasonYear, "area_ha": "500", "source": "manual",
		"calendar": []map[string]any{{"stage": "lam_dat", "start": "2026-11-05", "end": "2026-11-30"}},
	}), 201, "diện tích vụ nhập tay")
	g.wantProblem(viewer.Do(harness.App, http.MethodPost, "/v1/mechanization/balance-runs", "", map[string]any{"season_code": "DX", "season_year": seasonYear}),
		405, "method_not_allowed", "ghi qua Application API") // route ghi chỉ ở Admin API (MFA)

	run := g.want(admin.Do(harness.Admin, http.MethodPost, "/v1/mechanization/balance-runs", "", map[string]any{"season_code": "DX", "season_year": seasonYear}), 201, "chạy cân đối")
	runID := str(obj(run.Body, "run"), "id")
	th := obj(obj(run.Body, "run"), "thresholds")
	if str(obj(th, "sufficient"), "value") != "85" || str(obj(th, "attention"), "value") != "60" {
		g.t.Fatalf("ngưỡng QT-02 phải 85/60: %s", trim(run.Raw))
	}
	res := g.want(viewer.Do(harness.App, http.MethodGet, "/v1/mechanization/balance-runs/"+runID+"?cooperative_ref="+g.coopNK, "", nil), 200, "đọc kết quả đã lưu")
	var mld map[string]any
	for _, r := range res.Items("results") {
		if str(r, "machine_type") == "MLD" {
			mld = r
		}
	}
	// 3 máy × 120 ha/máy ÷ 500 ha = 72 % → "Cần chú ý" (60 ≤ 72 < 85).
	if mld == nil || !dec(g.t, mld["coverage_pct"]).Equal(decimal.NewFromInt(72)) || str(mld, "status") != "attention" || str(mld, "status_label") != "Cần chú ý" || num(mld, "capacity") != 3 {
		g.t.Fatalf("kết quả MLD/làm đất sai: %v", mld)
	}
	for _, r := range res.Items("results") {
		if str(r, "machine_type") != "MLD" && str(r, "status") != "shortage" && str(r, "status") != "no_norm" {
			g.t.Fatalf("khâu không có máy phải là Thiếu: %v", r)
		}
	}
	dash := g.want(viewer.Do(harness.App, http.MethodGet, fmt.Sprintf("/v1/mechanization/dashboard?season_code=DX&season_year=%d", seasonYear), "", nil), 200, "dashboard CGH")
	if num(obj(dash.Body, "kpis"), "total_machines") != 3 {
		g.t.Fatalf("dashboard CGH: %s", trim(dash.Raw))
	}
}

// 10 — danh mục trường MRV [CHỜ-1]: seed → gửi → người khác duyệt.
func (g *g1) stepMRVCatalog() {
	setter := g.s.User(harness.UserSpec{Tenant: g.tid, MFA: true, Roles: []string{"param_setter"}})
	approver := g.s.User(harness.UserSpec{Tenant: g.tid, MFA: true, Roles: []string{"param_approver", "param_setter"}})
	seed := g.want(setter.Do(harness.Admin, http.MethodPost, "/v1/mrv/catalog/seed", "", nil), 200, "seed danh mục MRV")
	for _, it := range seed.Items() {
		v := obj(it, "version")
		if v == nil {
			continue
		}
		id := str(v, "id")
		g.want(setter.Do(harness.Admin, http.MethodPost, "/v1/mrv/catalog/versions/"+id+"/submit", "", nil), 200, "gửi duyệt danh mục")
		g.wantProblem(setter.Do(harness.Admin, http.MethodPost, "/v1/mrv/catalog/versions/"+id+"/approve", "", map[string]any{}), 403, "forbidden", "setter không có vai duyệt")
		g.want(approver.Do(harness.Admin, http.MethodPost, "/v1/mrv/catalog/versions/"+id+"/approve", "", map[string]any{"note": "Duyệt tạm G1"}), 200, "duyệt danh mục")
	}
}

type pullChange struct {
	Entity string          `json:"entity"`
	ID     string          `json:"id"`
	Op     string          `json:"op"`
	Data   json.RawMessage `json:"data"`
}

// pullAll kéo hết các trang từ đầu, trả thay đổi theo entity.
func (g *g1) pullAll(u *harness.User) map[string][]pullChange {
	g.t.Helper()
	out := map[string][]pullChange{}
	cursor := ""
	for page := 0; page < 20; page++ {
		path := "/v1/sync/pull?limit=50"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		r := g.want(u.Do(harness.App, http.MethodGet, path, "", nil), 200, "pull")
		var body struct {
			Changes    []pullChange `json:"changes"`
			NextCursor string       `json:"next_cursor"`
			HasMore    bool         `json:"has_more"`
		}
		if err := json.Unmarshal(r.Raw, &body); err != nil {
			g.t.Fatal(err)
		}
		for _, c := range body.Changes {
			out[c.Entity] = append(out[c.Entity], c)
		}
		if !body.HasMore {
			return out
		}
		cursor = body.NextCursor
	}
	g.t.Fatal("pull không kết thúc sau 20 trang")
	return nil
}

// 11 — đồng bộ: đăng ký thiết bị, pull (thửa, tài liệu, nhiệm vụ, khóa công khai),
// tải ảnh mã hóa bằng khóa công khai của hộ, push lô hỗn hợp → kết quả theo từng bản ghi.
func (g *g1) stepSync() {
	g.deviceID = uuid.Must(uuid.NewV7()).String()
	g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/devices", "devreg:"+g.deviceID, map[string]any{
		"device_id": g.deviceID, "platform": "android", "app_version": "1.0.0+1",
	}), 201, "đăng ký thiết bị")
	g.wantProblem(g.officer.Do(harness.App, http.MethodGet, "/v1/sync/pull", "", nil), 403, "sync.device_unregistered", "pull thiếu X-Device-Id")
	g.officer.DeviceID = g.deviceID

	ch := g.pullAll(g.officer)
	has := func(entity, id string) bool {
		for _, c := range ch[entity] {
			if c.ID == id && c.Op == "upsert" {
				return true
			}
		}
		return false
	}
	for _, p := range g.plots {
		if !has("plot", p) {
			g.t.Fatalf("pull thiếu thửa %s", p)
		}
	}
	for _, d := range []string{g.procedureRef, g.supplementRef, g.communeGuideRef} {
		if !has("document", d) {
			g.t.Fatalf("pull thiếu tài liệu %s", d)
		}
	}
	if !has("task", g.taskID) {
		g.t.Fatal("pull thiếu nhiệm vụ được giao")
	}
	for _, c := range ch["pubkey"] {
		if c.Op != "upsert" {
			continue
		}
		var d struct {
			Ref       string `json:"ref"`
			AreaCode  string `json:"area_code"`
			PublicKey []byte `json:"public_key"`
		}
		if err := json.Unmarshal(c.Data, &d); err != nil {
			g.t.Fatal(err)
		}
		if d.AreaCode != communeNK || len(d.PublicKey) != envelope.KeySize {
			g.t.Fatalf("khóa công khai sai địa bàn/độ dài: %s", c.Data)
		}
		g.pubkeys[d.Ref] = d.PublicKey
	}
	for _, h := range g.households {
		if g.pubkeys[h] == nil {
			g.t.Fatalf("pull thiếu khóa công khai của hộ %s", h)
		}
	}

	// Ảnh minh chứng: mã hóa NGAY trên máy bằng khóa công khai của hộ 1 (envelope v1).
	pub, err := envelope.Key32(g.pubkeys[g.households[0]])
	if err != nil {
		g.t.Fatal(err)
	}
	plain := append([]byte("\xff\xd8\xff\xe0JFIF-anh-ruong-tong-hop-"), []byte(strings.Repeat("x", 512))...)
	sealed, err := envelope.Seal(plain, pub)
	if err != nil {
		g.t.Fatal(err)
	}
	meta := envelope.NewMetadata(g.households[0], sealed, "image/jpeg", time.Now().Add(-10*time.Minute).Format(time.RFC3339))
	meta.GPS = &envelope.GPS{Lat: nkLat, Lon: nkLon, AccuracyM: 5}
	mb, _ := json.Marshal(meta)
	fileRef := uuid.Must(uuid.NewV7()).String()
	up := g.want(g.officer.Multipart(harness.App, "/v1/files", "file:"+fileRef, map[string]string{"meta": string(mb), "file_ref": fileRef}, "blob", sealed.Ciphertext), 201, "tải tệp mã hóa")
	if str(up.Body, "sha256_ciphertext") != meta.SHA256Ciphertext {
		g.t.Fatalf("máy chủ phải kiểm và trả sha256 bản mã: %s", trim(up.Raw))
	}
	// Kho đối tượng chỉ giữ bản mã (R-40).
	rc, _, err := g.s.Store.Get(context.Background(), "files/"+g.tid.String()+"/"+fileRef)
	if err != nil {
		g.t.Fatal(err)
	}
	stored := make([]byte, len(sealed.Ciphertext)+1)
	n, _ := rc.Read(stored)
	_ = rc.Close()
	if strings.Contains(string(stored[:n]), "JFIF-anh-ruong") {
		g.t.Fatal("kho đối tượng chứa bản rõ")
	}
	bad := meta
	bad.SHA256Ciphertext = strings.Repeat("0", 64)
	bb, _ := json.Marshal(bad)
	g.wantProblem(g.officer.Multipart(harness.App, "/v1/files", "", map[string]string{"meta": string(bb)}, "blob", sealed.Ciphertext), 422, "sync.file_checksum_mismatch", "sai sha256")

	// Người xem minh chứng (có mục đích + đồng ý) đọc được bản rõ qua vault mở DEK.
	viewer := g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuid.MustParse(g.orgP92), Roles: []string{"vault_evidence_viewer"}})
	h := http.Header{"X-Purpose": {mrvport.ConsentPurpose}}
	content := viewer.DoRaw(harness.App, http.MethodGet, "/v1/files/"+fileRef+"/content", h, nil)
	if content.Status != 200 || string(content.Raw) != string(plain) || content.Header.Get("Cache-Control") != "no-store" {
		g.t.Fatalf("đọc bản rõ qua vault: %d %s", content.Status, content.Header.Get("Cache-Control"))
	}
	g.wantProblem(viewer.DoRaw(harness.App, http.MethodGet, "/v1/files/"+fileRef+"/content", nil, nil), 400, "sync.purpose_required", "đọc tệp thiếu mục đích")

	// Lô push hỗn hợp.
	seq := 0
	now := time.Now().Add(-5 * time.Minute).Format(time.RFC3339)
	rec := func(entity string, payload any) map[string]any {
		seq++
		return map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "idem_key": fmt.Sprintf("dev:%s:%d", g.deviceID, seq),
			"entity": entity, "op": "create", "event_time": now, "payload": payload}
	}
	newPlot := uuid.Must(uuid.NewV7()).String()
	evidence := func(household string) map[string]any {
		return map[string]any{"evidence_type": "awd_log", "admin_unit_code": communeNK, "plot_ref": g.plots[0], "season_code": fmt.Sprintf("%d-DX", seasonYear),
			"values":    map[string]any{"stage": "de_nhanh", "awd_water_level_cm": "-12.5", "awd_dry_days": 5},
			"file_refs": []map[string]any{{"file_ref": fileRef, "sha256": meta.SHA256Ciphertext}},
			"gps":       map[string]any{"lat": "10.0300000", "lon": "105.7700000", "accuracy_m": "5"}, "household_ref": household}
	}
	records := []map[string]any{
		rec("plot_boundary", map[string]any{"plot_ref": newPlot, "commune_code": communeNK, "cooperative_ref": g.coopNK, "household_ref": g.households[2],
			"geometry": harness.Rect(nkLat, nkLon, 0, 300, 90, 90), "gps_accuracy_m": "4.0"}),
		rec("task_update", map[string]any{"task_id": g.taskID, "action": "accepted", "note": "Nhận xử lý ngoài đồng"}),
		rec("field_report", map[string]any{"kind": "growth_pest", "commune_code": communeNK, "period": vnPeriod(), "growth_stage": "de_nhanh",
			"pest_code": "ray_nau", "severity": "trung_binh", "affected_area_ha": "1.2", "urgent": true, "note": "Rầy nâu tuổi 2–3"}),
		rec("evidence", evidence(g.households[0])),
		rec("evidence", evidence(g.households[1])), // hộ 2 chưa đồng ý mrv.evidence
		rec("plot_boundary", map[string]any{"plot_ref": uuid.Must(uuid.NewV7()).String(), "commune_code": communeNK,
			"geometry": map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.77, 10.04}, {105.771, 10.04}, {105.772, 10.04}, {105.77, 10.04}}}}}), // 3 điểm thẳng hàng
		rec("invoice", map[string]any{"x": 1}),
	}
	want := []struct{ status, code string }{
		{"accepted", ""}, {"accepted", ""}, {"accepted", ""}, {"accepted", ""},
		{"rejected", "mrv.consent_missing"}, {"rejected", "geo.too_few_points"}, {"rejected", "sync.unknown_entity"},
	}
	batch := "batch:" + uuid.Must(uuid.NewV7()).String()
	body := map[string]any{"device_id": g.deviceID, "records": records}
	r := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/sync/push", batch, body), 200, "push lô hỗn hợp")
	results := r.Items("results")
	if len(results) != len(want) {
		g.t.Fatalf("kết quả phải theo từng bản ghi (%d), nhận %s", len(want), trim(r.Raw))
	}
	for i, w := range want {
		if str(results[i], "id") != records[i]["id"] || str(results[i], "status") != w.status || str(results[i], "code") != w.code {
			g.t.Fatalf("bản ghi %d (%s): muốn %s %s, nhận %v", i, records[i]["entity"], w.status, w.code, results[i])
		}
	}
	replay := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/sync/push", batch, body), 200, "gọi lặp lô")
	if replay.Header.Get("Idempotent-Replayed") != "true" {
		g.t.Fatal("gọi lặp cùng Idempotency-Key phải trả phản hồi cũ")
	}
	again := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/sync/push", "", map[string]any{"device_id": g.deviceID, "records": records[:1]}), 200, "gửi lại bản ghi trong lô khác")
	if st := str(again.Items("results")[0], "status"); st != "duplicate" {
		g.t.Fatalf("bản ghi gửi lại phải duplicate theo idem_key, nhận %s", st)
	}
	// Hiệu quả ở module nhận.
	tk := g.want(g.officer.Do(harness.App, http.MethodGet, "/v1/extension/tasks/"+g.taskID, "", nil), 200, "nhiệm vụ sau push")
	if str(obj(tk.Body, "task"), "status") != "accepted" {
		g.t.Fatalf("task_update phải chuyển nhiệm vụ sang accepted: %s", trim(tk.Raw))
	}
	g.want(g.provAdmin.Do(harness.App, http.MethodGet, "/v1/plots/"+newPlot, "", nil), 200, "thửa vẽ ngoại tuyến")
}

// 12 — thu hồi thiết bị → mọi lời gọi đồng bộ trả 403 sync.device_revoked.
func (g *g1) stepRevoke() {
	admin := g.s.User(harness.UserSpec{Tenant: g.tid, MFA: true, Roles: []string{"device_admin"}})
	r := g.want(admin.Do(harness.Admin, http.MethodPost, "/v1/devices/"+g.deviceID+"/revoke", "", map[string]any{"reason": "Cán bộ báo mất máy ngày 02/12"}), 200, "thu hồi thiết bị")
	if r.Body["revoked"] != true {
		g.t.Fatalf("thu hồi: %s", trim(r.Raw))
	}
	g.wantProblem(g.officer.Do(harness.App, http.MethodGet, "/v1/sync/pull", "", nil), 403, "sync.device_revoked", "pull sau thu hồi")
	g.wantProblem(g.officer.Do(harness.App, http.MethodPost, "/v1/sync/push", "", map[string]any{"device_id": g.deviceID, "records": []any{}}), 403, "sync.device_revoked", "push sau thu hồi")
	g.wantProblem(g.officer.Do(harness.App, http.MethodPost, "/v1/devices", "", map[string]any{"device_id": g.deviceID, "platform": "android", "app_version": "1.0.0+2"}),
		403, "sync.device_revoked", "đăng ký lại mã đã thu hồi")
	if g.outboxCount("pf.sync.device.revoked", "device_id", g.deviceID) != 1 {
		g.t.Fatal("thiếu sự kiện pf.sync.device.revoked")
	}
	g.officer.DeviceID = ""
}

// 13 — xuất gói MRV: 202 + job, handler inbox của worker dựng gói, băm khớp.
func (g *g1) stepMRVExport() {
	exp := g.s.User(harness.UserSpec{Tenant: g.tid, Org: uuid.MustParse(g.orgP92), Roles: []string{"mrv_exporter", "mrv_viewer"}})
	r := g.want(exp.Do(harness.App, http.MethodPost, "/v1/mrv/exports", "", map[string]any{"filters": map[string]any{"evidence_type": "awd_log"}}), 202, "yêu cầu xuất gói")
	id := str(r.Body, "id")
	g.wantProblem(exp.Do(harness.App, http.MethodGet, "/v1/mrv/exports/"+id+"/package", "", nil), 409, "mrv.export_not_ready", "gói chưa sẵn sàng")

	// Worker: đọc sự kiện từ outbox như publisher, xử lý qua inbox.Process (R-61).
	ctx := context.Background()
	pool := g.s.PF("mrv_app")
	var env outbox.Envelope
	err := tenant.WithTenantTx(ctx, pool, g.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id, tenant_id, event_type, schema_version, occurred_at, recorded_at, producer, payload
		  FROM shared.outbox WHERE event_type = $1 AND payload->>'export_id' = $2`, mrvport.EventExportRequested, id).
			Scan(&env.EventID, &env.TenantID, &env.EventType, &env.SchemaVersion, &env.OccurredAt, &env.RecordedAt, &env.Producer, &env.Payload)
	})
	if err != nil {
		g.t.Fatalf("không thấy sự kiện yêu cầu xuất gói: %v", err)
	}
	h := g.s.Mods.MRV.ExportHandler()
	if ok, err := inbox.Process(ctx, pool, mrvport.ExportConsumer, env, h); err != nil || !ok {
		g.t.Fatalf("xử lý lần 1: processed=%v err=%v", ok, err)
	}
	if ok, err := inbox.Process(ctx, pool, mrvport.ExportConsumer, env, h); err != nil || ok {
		g.t.Fatalf("giao lặp phải bỏ qua: processed=%v err=%v", ok, err)
	}
	job := g.want(exp.Do(harness.App, http.MethodGet, "/v1/mrv/exports/"+id, "", nil), 200, "đọc job")
	if str(job.Body, "status") != "completed" || num(job.Body, "evidence_count") != 1 {
		g.t.Fatalf("job xuất gói phải completed với 1 minh chứng: %s", trim(job.Raw))
	}
	v := g.want(exp.Do(harness.App, http.MethodGet, "/v1/mrv/exports/"+id+"/verify", "", nil), 200, "kiểm gói")
	if v.Body["match"] != true || str(v.Body, "rebuilt_sha256") != str(job.Body, "snapshot_sha256") {
		g.t.Fatalf("băm ảnh chụp dữ liệu phải khớp: %s", trim(v.Raw))
	}
	pkg := g.want(exp.Do(harness.App, http.MethodGet, "/v1/mrv/exports/"+id+"/package", "", nil), 200, "lấy gói")
	rc, _, err := g.s.Store.Get(ctx, str(job.Body, "object_key"))
	if err != nil {
		g.t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	raw, err := readAll(rc)
	if err != nil {
		g.t.Fatal(err)
	}
	if objstore.SHA256Hex(raw) != str(pkg.Body, "package_sha256") || str(pkg.Body, "package_sha256") != str(job.Body, "package_sha256") {
		g.t.Fatal("SHA-256 của gói trong kho đối tượng không khớp package_sha256")
	}
}

// 14 — không có dữ liệu cá nhân ngoài vault (R-04, R-14): lược đồ pf, dữ liệu pf,
// log của cả ba server, kho đối tượng.
func (g *g1) stepNoPII() {
	ctx := context.Background()
	admin := g.s.AdminPF()
	rows, err := admin.Query(ctx, piiColumnQuery)
	if err != nil {
		g.t.Fatal(err)
	}
	var bad []string
	for rows.Next() {
		var s, tb, c string
		if err := rows.Scan(&s, &tb, &c); err != nil {
			g.t.Fatal(err)
		}
		bad = append(bad, s+"."+tb+"."+c)
	}
	rows.Close()
	if len(bad) > 0 {
		g.t.Fatalf("cụm pf có cột mang tên dữ liệu cá nhân (phải ở vault): %v", bad)
	}

	// Quét giá trị: mọi bảng thường của pf_* và shared (kể cả outbox, idempotency).
	trows, err := admin.Query(ctx, `SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname) FROM pg_class c
	  JOIN pg_namespace n ON n.oid = c.relnamespace
	  WHERE c.relkind IN ('r', 'p') AND NOT c.relispartition AND (n.nspname LIKE 'pf\_%' OR n.nspname = 'shared')`)
	if err != nil {
		g.t.Fatal(err)
	}
	var tables []string
	for trows.Next() {
		var tn string
		if err := trows.Scan(&tn); err != nil {
			g.t.Fatal(err)
		}
		tables = append(tables, tn)
	}
	trows.Close()
	if len(tables) < 10 {
		g.t.Fatalf("quá ít bảng để quét: %v", tables)
	}
	pattern := strings.Join(pii, "|")
	control := 0 // đối chứng: bộ quét phải thấy được văn bản nghiệp vụ không phải PII
	for _, tn := range tables {
		var n, c int
		if err := admin.QueryRow(ctx, "SELECT count(*) FILTER (WHERE x::text ~ $1), count(*) FILTER (WHERE x::text ~ $2) FROM "+tn+" AS x",
			pattern, "Rầy nâu mật độ cao").Scan(&n, &c); err != nil {
			g.t.Fatalf("quét %s: %v", tn, err)
		}
		control += c
		if n > 0 {
			g.t.Errorf("bảng %s chứa dữ liệu cá nhân dạng rõ (%d dòng)", tn, n)
		}
	}
	if control == 0 {
		g.t.Fatal("bộ quét không thấy cả mô tả nhiệm vụ — phép quét không có hiệu lực")
	}

	logs := g.s.Logs.String()
	if !strings.Contains(logs, `"event":"http.request"`) {
		g.t.Fatal("không gom được log truy cập")
	}
	for _, p := range pii {
		if strings.Contains(logs, p) {
			g.t.Errorf("log chứa dữ liệu cá nhân %q (R-14)", p)
		}
	}
	if err := scanDir(g.s.StoreDir, pii); err != nil {
		g.t.Error(err)
	}
	// Kiểm chứng ngược: vault thật sự giữ định danh (tra theo chỉ mục mù).
	lk := g.root.DoRaw(harness.Vault, http.MethodPost, "/v1/subjects/lookup",
		http.Header{"Content-Type": {"application/json"}, "X-Purpose": {"registry.dedup"}},
		strings.NewReader(`{"purpose":"registry.dedup","value":"0999000002"}`))
	if lk.Status != 200 || str(lk.Body, "ref") != g.households[1] {
		g.t.Fatalf("vault phải tìm được hộ 2 theo SĐT: %d %s", lk.Status, trim(lk.Raw))
	}
}

// TestRB3ErasureEvidenceUnreadable là kịch bản RB-3 (ADR-0004): xóa định danh một
// hộ ở kho định danh → ảnh minh chứng của hộ không còn giải mã được qua
// GET /v1/files/{ref}/content (410 vault.erased), trong khi sổ của cụm pf (đồng ý,
// sổ nhận đồng bộ) vẫn nguyên vẹn và chuỗi băm xác minh được.
func TestRB3ErasureEvidenceUnreadable(t *testing.T) {
	s := harness.New(t)
	g := &g1{t: t, s: s, tid: harness.NewTenant(), pubkeys: map[string][]byte{}}
	g.root = s.User(harness.UserSpec{Tenant: g.tid, MFA: true,
		Roles: []string{"platform_admin", "masterdata_admin", "registry_admin", "vault_registrar"}})
	g.want(g.root.Do(harness.Admin, http.MethodPost, "/v1/masterdata/seed", "", map[string]any{}), 200, "seed")
	g.orgNational = g.createOrg(map[string]any{"kind": "ttkn_national", "code": "TTKNQG", "name": "Trung tâm Khuyến nông Quốc gia"})
	g.orgP92 = g.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN92", "name": "TTKN TP Cần Thơ", "admin_unit_code": provCT, "parent_ref": g.orgNational})
	g.orgTeamNK = g.createOrg(map[string]any{"kind": "extension_team", "code": "TKN-NINHKIEU", "name": "Tổ KNCĐ Ninh Kiều", "admin_unit_code": communeNK, "parent_ref": g.orgP92})
	g.coopNK = g.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Tổng Hợp Xóa Định Danh", "admin_unit_code": communeNK, "parent_ref": g.orgTeamNK})
	g.officer = s.User(harness.UserSpec{Tenant: g.tid, Org: uuid.MustParse(g.orgTeamNK), Roles: []string{
		"kn_officer", "vault_registrar", "vault_pubkey_sync", "consent_writer"}})

	// Hộ có đồng ý mrv.evidence.
	r := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/households", "", map[string]any{
		"cooperative_ref": g.coopNK, "identity": map[string]any{"full_name": "Nguyễn Văn Tổng Hợp", "phone": "0999000031"},
	}), 201, "tạo hộ")
	hh := str(obj(r.Body, "household"), "household_ref")
	g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/consents", "", map[string]any{
		"subject_ref": hh, "purpose": mrvport.ConsentPurpose, "text_version": "2026-11", "status": "granted",
		"channel": "field_app", "event_time": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	}), 201, "ghi đồng ý")

	// Máy: đăng ký, kéo khóa công khai, mã hóa ảnh, tải bản mã, gửi thêm một bản ghi đồng ý qua đồng bộ.
	g.deviceID = uuid.Must(uuid.NewV7()).String()
	g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/devices", "devreg:"+g.deviceID, map[string]any{
		"device_id": g.deviceID, "platform": "android", "app_version": "1.0.0+1"}), 201, "đăng ký thiết bị")
	g.officer.DeviceID = g.deviceID
	var pub []byte
	for _, c := range g.pullAll(g.officer)["pubkey"] {
		var d struct {
			Ref       string `json:"ref"`
			PublicKey []byte `json:"public_key"`
		}
		if c.Op == "upsert" && json.Unmarshal(c.Data, &d) == nil && d.Ref == hh {
			pub = d.PublicKey
		}
	}
	key, err := envelope.Key32(pub)
	if err != nil {
		t.Fatalf("khóa công khai của hộ: %v", err)
	}
	plain := []byte("\xff\xd8\xff\xe0JFIF-anh-minh-chung-rb3")
	sealed, err := envelope.Seal(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	meta := envelope.NewMetadata(hh, sealed, "image/jpeg", time.Now().Add(-10*time.Minute).Format(time.RFC3339))
	mb, _ := json.Marshal(meta)
	fileRef := uuid.Must(uuid.NewV7()).String()
	g.want(g.officer.Multipart(harness.App, "/v1/files", "file:"+fileRef, map[string]string{"meta": string(mb), "file_ref": fileRef}, "blob", sealed.Ciphertext), 201, "tải bản mã")
	push := g.want(g.officer.Do(harness.App, http.MethodPost, "/v1/sync/push", "batch:"+uuid.Must(uuid.NewV7()).String(), map[string]any{
		"device_id": g.deviceID, "records": []map[string]any{{"id": uuid.Must(uuid.NewV7()).String(), "idem_key": "dev:" + g.deviceID + ":1",
			"entity": "consent", "op": "create", "event_time": time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			"payload": map[string]any{"subject_ref": hh, "purpose": "extension.field_support", "text_version": "2026-11", "status": "granted"}}},
	}), 200, "push đồng ý")
	if st := str(push.Items("results")[0], "status"); st != "accepted" {
		t.Fatalf("bản ghi đồng ý qua đồng bộ: %s", trim(push.Raw))
	}

	viewer := s.User(harness.UserSpec{Tenant: g.tid, Org: uuid.MustParse(g.orgP92), Roles: []string{"vault_evidence_viewer"}})
	purpose := http.Header{"X-Purpose": {mrvport.ConsentPurpose}}
	if c := viewer.DoRaw(harness.App, http.MethodGet, "/v1/files/"+fileRef+"/content", purpose, nil); c.Status != 200 || string(c.Raw) != string(plain) {
		t.Fatalf("trước khi xóa phải đọc được bản rõ: %d", c.Status)
	}

	// Xóa định danh (RB-3): vai vault_eraser + MFA.
	g.wantProblem(viewer.Do(harness.Vault, http.MethodPost, "/v1/subjects/"+hh+"/erasure", "", map[string]any{"reason": "Chủ hộ yêu cầu xóa dữ liệu"}),
		403, "forbidden", "không có vai xóa định danh")
	eraser := s.User(harness.UserSpec{Tenant: g.tid, MFA: true, Roles: []string{"vault_eraser"}})
	er := g.want(eraser.Do(harness.Vault, http.MethodPost, "/v1/subjects/"+hh+"/erasure", "", map[string]any{"reason": "Chủ hộ yêu cầu xóa dữ liệu"}), 200, "xóa định danh")
	if er.Body["erased"] != true {
		t.Fatalf("xóa định danh: %s", trim(er.Raw))
	}

	// Ảnh không còn giải mã được: khóa bí mật đã hủy.
	g.wantProblem(viewer.DoRaw(harness.App, http.MethodGet, "/v1/files/"+fileRef+"/content", purpose, nil), 410, "vault.erased", "đọc ảnh sau khi xóa định danh")
	// Bản mã vẫn còn ở kho đối tượng (không xóa cứng sổ/bản mã), nhưng vô dụng khi thiếu khóa.
	if rc, _, err := s.Store.Get(context.Background(), "files/"+g.tid.String()+"/"+fileRef); err != nil {
		t.Fatalf("bản mã phải còn: %v", err)
	} else {
		_ = rc.Close()
	}
	// Nhãn hiển thị của hộ đã thành "đã xóa định danh".
	for _, it := range g.want(g.officer.Do(harness.App, http.MethodGet, "/v1/cooperatives/"+g.coopNK+"/households", "", nil), 200, "hộ sau xóa").Items() {
		if str(it, "household_ref") == hh && it["label_unavailable"] != true {
			t.Fatalf("nhãn của hộ đã xóa phải không còn: %v", it)
		}
	}

	// Sổ pf nguyên vẹn: dòng đồng ý và dòng sổ nhận đồng bộ vẫn còn, chuỗi băm hợp lệ.
	ctx := context.Background()
	verify := func(role, table, where string, arg any, minRows int) {
		t.Helper()
		pool := s.PF(role)
		err := tenant.WithTenantTx(ctx, pool, g.tid, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "SELECT DISTINCT stream_key FROM "+table+" WHERE "+where, arg)
			if err != nil {
				return err
			}
			streams, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			if len(streams) == 0 {
				t.Fatalf("%s: không còn dòng sổ", table)
			}
			var total int64
			for _, st := range streams {
				res, err := ledger.Verify(ctx, tx, table, g.tid, st)
				if err != nil {
					return err
				}
				if !res.OK {
					t.Fatalf("%s %s: chuỗi băm hỏng %+v", table, st, res)
				}
				total += res.RowsChecked
			}
			if total < int64(minRows) {
				t.Fatalf("%s: chỉ còn %d dòng", table, total)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	verify("party_app", "pf_party.consent", "subject_ref = $1::uuid", hh, 2)
	verify("sync_app", "pf_sync.inbound", "device_id = $1", g.deviceID, 1)
	verify("party_app", "pf_party.org_event", "org_ref = $1::uuid", g.coopNK, 1)
}

// ADR-0054: phone/address của thẻ đơn vị là thông tin công vụ, không phải cá nhân.
// Ngoại lệ đúng hai cột trên bảng unit và các partition thực sự của nó. Quét giá trị
// PII vẫn bao gồm mọi bảng, kể cả unit; không miễn toàn schema hay theo tiền tố tên.
//
// PRICE-SURVEY-PII-NAME (#150): pf_price.survey_form.has_phone (W11.78) là cờ boolean "dòng có SĐT"
// để chống trùng; SĐT/họ tên nông hộ ở kho định danh (farmer_ref, mục đích price.cost_survey —
// docs/kn/gia-dinh/W11.78.md Q-005). Không đổi tên được: survey_form là bảng sổ, row_hash băm
// to_jsonb(dòng) nên RENAME COLUMN đứt chuỗi băm (ADR-0012, migratecheck CheckLedgerColumns), và
// migration đã áp không sửa (R-24). Miễn đúng cột này, đúng cây partition của survey_form và chỉ
// khi kiểu là boolean (không thể chứa SĐT). PRICE-SURVEY-PII-GUARD-001 chứng minh không che gì khác.
const piiColumnQuery = `SELECT table_schema, table_name, column_name FROM information_schema.columns
	  WHERE (table_schema LIKE 'pf\_%' OR table_schema = 'shared')
	    AND column_name ~* '(full_?name|phone|national_?id|cccd|address|dia_chi|ho_ten)'
	    AND data_type <> 'boolean' -- cờ có/không (vd pf_price.survey_form.has_phone) không chứa được dữ liệu cá nhân
      AND NOT (table_schema = 'pf_contact' AND column_name IN ('phone', 'address')
       AND table_name IN (SELECT c.relname FROM pg_partition_tree('pf_contact.unit'::regclass) p
        JOIN pg_class c ON c.oid = p.relid))
      AND NOT (table_schema = 'pf_price' AND column_name = 'has_phone' AND data_type = 'boolean'
       AND table_name IN (SELECT c.relname FROM pg_partition_tree('pf_price.survey_form'::regclass) p
        JOIN pg_class c ON c.oid = p.relid))`

// ORG-PII-GUARD-001: ngoại lệ công vụ không che cột PII khác hay bảng tên gần giống.
func TestContactPublicColumnsDoNotHidePII_ORGUI(t *testing.T) {
	s := harness.New(t)
	ctx := context.Background()
	tx, err := s.AdminPF().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `ALTER TABLE pf_contact.unit ADD COLUMN full_name text; CREATE TABLE pf_contact.unit_person_probe (phone text)`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, piiColumnQuery)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var rootName, probePhone bool
	for rows.Next() {
		var schema, table, col string
		if err := rows.Scan(&schema, &table, &col); err != nil {
			t.Fatal(err)
		}
		rootName = rootName || (schema == "pf_contact" && table == "unit" && col == "full_name")
		probePhone = probePhone || (schema == "pf_contact" && table == "unit_person_probe" && col == "phone")
		if schema == "pf_contact" && table == "unit" && (col == "phone" || col == "address") {
			t.Fatal("ngoại lệ công vụ không được nhận diện")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !rootName || !probePhone {
		t.Fatal("ngoại lệ đã che dữ liệu cá nhân ngoài phạm vi")
	}
}

// PRICE-SURVEY-PII-GUARD-001 (#150): ngoại lệ cờ has_phone của phiếu điều tra giá thành không che cột PII
// khác trên survey_form, bảng tên gần giống, hay has_phone đổi sang kiểu chứa được SĐT.
func TestPriceSurveyHasPhoneFlagDoesNotHidePII(t *testing.T) {
	s := harness.New(t)
	ctx := context.Background()
	tx, err := s.AdminPF().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	flagged := func(q pgx.Tx) map[string]bool {
		rows, err := q.Query(ctx, piiColumnQuery)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := map[string]bool{}
		for rows.Next() {
			var schema, table, col string
			if err := rows.Scan(&schema, &table, &col); err != nil {
				t.Fatal(err)
			}
			if schema == "pf_price" {
				got[table+"."+col] = true
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return got
	}
	// Cờ boolean trên bảng cha và mọi partition: được miễn.
	var parts int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_partition_tree('pf_price.survey_form'::regclass)`).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if parts < 2 {
		t.Fatalf("survey_form phải có partition để kiểm ngoại lệ lan xuống: %d", parts)
	}
	for k := range flagged(tx) {
		if strings.HasSuffix(k, ".has_phone") {
			t.Fatalf("cờ boolean has_phone của survey_form không được nhận diện: %s", k)
		}
	}

	if _, err = tx.Exec(ctx, `ALTER TABLE pf_price.survey_form ADD COLUMN farmer_phone text;
	  CREATE TABLE pf_price.survey_form_probe (has_phone boolean)`); err != nil {
		t.Fatal(err)
	}
	got := flagged(tx)
	if !got["survey_form.farmer_phone"] || !got["survey_form_default.farmer_phone"] || !got["survey_form_probe.has_phone"] {
		t.Fatalf("ngoại lệ has_phone đã che dữ liệu cá nhân ngoài phạm vi: %v", got)
	}

	if _, err = tx.Exec(ctx, `ALTER TABLE pf_price.survey_form ALTER COLUMN has_phone TYPE text USING has_phone::text`); err != nil {
		t.Fatal(err)
	}
	if got := flagged(tx); !got["survey_form.has_phone"] || !got["survey_form_default.has_phone"] {
		t.Fatalf("has_phone kiểu text phải bị bắt: %v", got)
	}
}

package acceptance

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// Địa bàn dùng chung (mã theo bộ seed ĐBSCL sau sắp xếp 2025 của pf/masterdata).
const (
	provCT    = "92"    // TP Cần Thơ
	communeNK = "31135" // Phường Ninh Kiều
	communeBT = "31186" // Phường Cái Răng (cùng tỉnh, khác xã)
	provAG    = "91"    // An Giang — "tỉnh khác"
	communeLX = "30307" // Phường Long Xuyên
	nkLat     = 10.03
	nkLon     = 105.77
)

// nguon ghi nguồn ca kiểm thử của prototype vào log kiểm thử (UAT S5).
func nguon(t *testing.T, file, name string) {
	t.Helper()
	t.Logf("Nguồn: mekong-green/tests/%s — %q", file, name)
}

// world là một tenant riêng đã nạp dữ liệu chủ và dựng cây tổ chức:
// TTKN QG → TTKN Cần Thơ → tổ KN Ninh Kiều / Cái Răng, TTKN An Giang → tổ KN Long Xuyên;
// HTX NK1, NK2 (xã Ninh Kiều), HTX BT (xã Cái Răng), HTX LX (An Giang).
type world struct {
	t   *testing.T
	s   *harness.Stack
	tid uuid.UUID

	root *harness.User

	orgNational, orgP92, teamNK, teamBT, orgP91, teamLX string
	coopNK, coopNK2, coopBT, coopLX                     string

	officer, officerBT, officer91, provAdmin *harness.User
}

// newWorld dựng stack trong tiến trình và một tenant mới (mỗi kiểm thử một tenant).
func newWorld(t *testing.T) *world {
	t.Helper()
	s := harness.New(t)
	w := &world{t: t, s: s, tid: harness.NewTenant()}
	w.root = s.User(harness.UserSpec{Tenant: w.tid, MFA: true,
		Roles: []string{"platform_admin", "masterdata_admin", "registry_admin", "vault_registrar"}})
	w.want(w.root.Do(harness.Admin, http.MethodPost, "/v1/masterdata/seed", "", map[string]any{}), 200, "nạp dữ liệu chủ")

	w.orgNational = w.createOrg(map[string]any{"kind": "ttkn_national", "code": "TTKNQG", "name": "Trung tâm Khuyến nông Quốc gia"})
	w.orgP92 = w.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN92", "name": "TTKN TP Cần Thơ", "admin_unit_code": provCT, "parent_ref": w.orgNational})
	w.teamNK = w.createOrg(map[string]any{"kind": "extension_team", "code": "TKN-NK", "name": "Tổ KNCĐ Ninh Kiều", "admin_unit_code": communeNK, "parent_ref": w.orgP92})
	w.teamBT = w.createOrg(map[string]any{"kind": "extension_team", "code": "TKN-BT", "name": "Tổ KNCĐ Cái Răng", "admin_unit_code": communeBT, "parent_ref": w.orgP92})
	w.orgP91 = w.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN91", "name": "TTKN An Giang", "admin_unit_code": provAG, "parent_ref": w.orgNational})
	w.teamLX = w.createOrg(map[string]any{"kind": "extension_team", "code": "TKN-LX", "name": "Tổ KNCĐ Long Xuyên", "admin_unit_code": communeLX, "parent_ref": w.orgP91})
	w.coopNK = w.createCoop("HTX Tổng Hợp Ninh Kiều 1", communeNK, w.teamNK)
	w.coopNK2 = w.createCoop("HTX Tổng Hợp Ninh Kiều 2", communeNK, w.teamNK)
	w.coopBT = w.createCoop("HTX Tổng Hợp Cái Răng", communeBT, w.teamBT)
	w.coopLX = w.createCoop("HTX Tổng Hợp Long Xuyên", communeLX, w.teamLX)

	officerRoles := []string{"kn_officer", "plot_editor", "plot_viewer", "vault_registrar"}
	w.officer = w.user(w.teamNK, false, officerRoles...)
	w.officerBT = w.user(w.teamBT, false, officerRoles...)
	w.officer91 = w.user(w.teamLX, false, officerRoles...)
	w.provAdmin = w.user(w.orgP92, false, "kn_province_admin", "plot_viewer", "plot_editor")
	return w
}

// user phát token cho người dùng thuộc đơn vị org (chuỗi rỗng: không có đơn vị).
func (w *world) user(org string, mfa bool, roles ...string) *harness.User {
	spec := harness.UserSpec{Tenant: w.tid, MFA: mfa, Roles: roles}
	if org != "" {
		spec.Org = uuid.MustParse(org)
	}
	return w.s.User(spec)
}

func (w *world) createOrg(body map[string]any) string {
	w.t.Helper()
	r := w.want(w.root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", body), 201, "tạo tổ chức "+fmt.Sprint(body["name"]))
	return str(obj(r.Body, "org"), "ref")
}

func (w *world) createCoop(name, commune, parent string) string {
	return w.createOrg(map[string]any{"kind": "cooperative", "name": name, "admin_unit_code": commune, "parent_ref": parent})
}

// household tạo một nông hộ (định danh vào kho định danh) và trả household_ref.
func (w *world) household(u *harness.User, coop, name, phone string) string {
	w.t.Helper()
	r := w.want(u.Do(harness.App, http.MethodPost, "/v1/households", "", map[string]any{
		"cooperative_ref": coop, "identity": map[string]any{"full_name": name, "phone": phone},
	}), 201, "tạo nông hộ "+name)
	return str(obj(r.Body, "household"), "household_ref")
}

// plot vẽ một thửa hình chữ nhật (w × h mét, lệch east/north mét so với tâm Ninh Kiều).
func (w *world) plot(u *harness.User, coop string, east, north, width, height float64) harness.Resp {
	w.t.Helper()
	body := map[string]any{"commune_code": communeNK, "geometry": harness.Rect(nkLat, nkLon, east, north, width, height)}
	if coop != "" {
		body["cooperative_ref"] = coop
	}
	return u.Do(harness.App, http.MethodPost, "/v1/plots", "", body)
}

// ---------- trợ giúp kiểm ----------

func (w *world) want(r harness.Resp, status int, what string) harness.Resp {
	w.t.Helper()
	return want(w.t, r, status, what)
}

func want(t *testing.T, r harness.Resp, status int, what string) harness.Resp {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%s: muốn %d, nhận %d %s", what, status, r.Status, trim(r.Raw))
	}
	return r
}

// wantProblem kiểm lỗi trả application/problem+json có code và trace_id.
func wantProblem(t *testing.T, r harness.Resp, status int, code, what string) harness.Resp {
	t.Helper()
	if r.Status != status || r.Code() != code || !r.IsProblem() {
		t.Fatalf("%s: muốn problem+json %d %s, nhận %d %q %s", what, status, code, r.Status, r.Header.Get("Content-Type"), trim(r.Raw))
	}
	return r
}

// wantFieldError kiểm lỗi 422 validation_failed có lỗi ở trường field.
func wantFieldError(t *testing.T, r harness.Resp, field, what string) {
	t.Helper()
	wantProblem(t, r, 422, "validation_failed", what)
	for _, e := range r.Items("errors") {
		if str(e, "field") == field {
			return
		}
	}
	t.Fatalf("%s: thiếu lỗi ở trường %q: %s", what, field, trim(r.Raw))
}

func trim(b []byte) string {
	if len(b) > 800 {
		return string(b[:800]) + "…"
	}
	return string(b)
}

func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }
func str(m map[string]any, k string) string         { v, _ := m[k].(string); return v }
func num(m map[string]any, k string) float64        { v, _ := m[k].(float64); return v }

func list(m map[string]any, k string) []map[string]any {
	arr, _ := m[k].([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, v := range arr {
		if mm, ok := v.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}

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

// refsOf trả tập giá trị trường k của các phần tử.
func refsOf(items []map[string]any, k string) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		out[str(it, k)] = true
	}
	return out
}

// containsAny cho biết s chứa một trong các chuỗi needles.
func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

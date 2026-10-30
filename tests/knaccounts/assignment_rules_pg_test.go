package knaccounts_test

// KNSPLIT-2-ASSIGNMENT-RULES-KN (#72): adapter luật lần gán của KN (extension.AssignmentRules) cho cùng phản hồi HTTP
// (mã, trường, câu chữ lỗi, lần gán đã ghi) như luật cũ trong accounts, trên PostgreSQL thật.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

var (
	uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	tsRE   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T`)
)

// normalize bỏ giá trị thay đổi giữa hai lượt (id, thời điểm, email/SĐT sinh ngẫu nhiên); Nút đổi thành tên cố định.
func (f *fixture) normalize(v any) any {
	names := map[string]string{f.national.String(): "<national>", f.p91.String(): "<p91>", f.team30292.String(): "<team30292>",
		f.coop30292.String(): "<coop30292>", f.team31120.String(): "<team31120>"}
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			switch k {
			case "email", "username", "phone", "trace_id", "request_id", "instance":
				continue
			}
			out[k] = f.normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = f.normalize(x[i])
		}
		return out
	case string:
		if n, ok := names[x]; ok {
			return n
		}
		if uuidRE.MatchString(x) {
			return "<uuid>"
		}
		if tsRE.MatchString(x) {
			return "<ts>"
		}
		return strings.ReplaceAll(x, f.tenant.String(), "<tenant>")
	}
	return v
}

type asgCase struct {
	role, org, mgmt string
	depts, communes []string
}

func (c asgCase) key() string {
	return fmt.Sprintf("%s|%s|%s|%s|%s", c.role, c.org, c.mgmt, strings.Join(c.depts, "+"), strings.Join(c.communes, "+"))
}

func (f *fixture) asgBody(c asgCase) map[string]any {
	orgs := map[string]string{"national": f.national.String(), "p91": f.p91.String(), "team30292": f.team30292.String(),
		"coop30292": f.coop30292.String()}
	m := map[string]any{"role": c.role, "department_codes": c.depts, "commune_codes": c.communes, "mgmt_level": c.mgmt}
	if o, ok := orgs[c.org]; ok {
		m["org_ref"] = o
	}
	return m
}

func assignmentCases() []asgCase {
	roles := []string{authz.KNTW, authz.KNTinh, authz.KNXa, authz.KNAdmin, "kn_boss"}
	orgs := []string{"", "national", "p91", "team30292", "coop30292"}
	depts := [][]string{nil, {"trong_trot"}, {"trong_trot", "bao_ve_thuc_vat"}, {"chan_nuoi"}, {"phong_khong_co"}}
	communes := [][]string{nil, {"30292"}, {"31120"}}
	var out []asgCase
	for _, r := range roles {
		for _, o := range orgs {
			for _, d := range depts {
				for _, c := range communes {
					out = append(out, asgCase{role: r, org: o, mgmt: "chuyen_vien", depts: d, communes: c})
				}
			}
		}
		for _, o := range []string{"national", "p91"} {
			for _, m := range []string{"", "lanh_dao_don_vi", "giam_doc", "khong_co"} {
				out = append(out, asgCase{role: r, org: o, mgmt: m, depts: []string{"trong_trot"}})
			}
		}
	}
	return out
}

func observeAssignmentRules(t *testing.T, rules accport.AssignmentRules) map[string]any {
	f := newFixture(t, fixtureDeps{AssignmentRules: rules})
	root, acc := f.as(f.national, authz.RolePlatformAdmin), f.as(f.national, accport.RoleAccountAdmin)
	out := map[string]any{}
	record := func(r resp) map[string]any {
		return map[string]any{"status": r.status, "body": f.normalize(r.body)}
	}
	phone := 0
	create := func(c caller, a map[string]any) resp {
		phone++
		return c.do(http.MethodPost, "/v1/accounts/kn-users", map[string]any{"full_name": "Trần Văn Bình",
			"email": "kn-" + ids.New().String()[24:] + "@vi-du.vn", "phone": fmt.Sprintf("0912%06d", phone), "assignments": []any{a}})
	}
	// 1. Tạo tài khoản KN với một lần gán: ma trận vai × Nút × phòng ban × xã (+ cấp quản lý).
	for _, c := range assignmentCases() {
		out["create/"+c.key()] = record(create(root, f.asgBody(c)))
	}
	// account_admin (không phải platform_admin) gán Admin / vai thường.
	out["create-acc/admin"] = record(create(acc, f.asgBody(asgCase{role: authz.KNAdmin})))
	out["create-acc/tinh"] = record(create(acc, f.asgBody(asgCase{role: authz.KNTinh, org: "p91", depts: []string{"trong_trot"}})))

	// 2. Thêm và sửa lần gán trên tài khoản có sẵn.
	base := create(root, f.asgBody(asgCase{role: authz.KNTinh, org: "p91", depts: []string{"trong_trot"}, mgmt: "chuyen_vien"}))
	if base.status != http.StatusCreated {
		t.Fatalf("base KN user: %d %v", base.status, base.body)
	}
	uid := userID(base)
	list, _ := base.body["assignments"].([]any)
	aid := list[0].(map[string]any)["assignment_id"].(string)
	for _, c := range []asgCase{
		{role: authz.KNXa, org: "p91", depts: []string{"trong_trot"}, communes: []string{"30292"}, mgmt: "chuyen_vien"},
		{role: authz.KNXa, org: "team30292", depts: []string{"trong_trot"}, mgmt: "chuyen_vien"},
		{role: authz.KNTW, org: "p91", depts: []string{"trong_trot"}},
		{role: authz.KNTW, org: "national", depts: []string{"trong_trot"}, mgmt: "giam_doc"},
		{role: authz.KNTinh, org: "p91", depts: []string{"trong_trot"}, communes: []string{"30292"}},
		{role: authz.KNAdmin},
		{role: "kn_boss", org: "p91", depts: []string{"trong_trot"}},
		{role: authz.KNTinh, org: "p91", depts: []string{"bao_ve_thuc_vat"}, mgmt: "khong_co"},
	} {
		body := f.asgBody(c)
		body["reason"] = "Điều chỉnh phân công theo quyết định"
		out["grant/"+c.key()] = record(root.do(http.MethodPost, "/v1/accounts/users/"+uid+"/assignments", body))
		out["change/"+c.key()] = record(root.do(http.MethodPut, "/v1/accounts/users/"+uid+"/assignments/"+aid, body))
	}
	out["list"] = record(root.do(http.MethodGet, "/v1/accounts/users/"+uid+"/assignments", nil))
	return out
}

func TestKNAssignmentRulesHTTPParityWithLegacyOnPostgres(t *testing.T) {
	before, after := observeAssignmentRules(t, nil), observeAssignmentRules(t, extension.AssignmentRules())
	var diffs []string
	for k, b := range before {
		bj, _ := json.Marshal(b)
		aj, _ := json.Marshal(after[k])
		if string(bj) != string(aj) {
			diffs = append(diffs, k+"\n  legacy: "+string(bj)+"\n  kn:     "+string(aj))
		}
	}
	sort.Strings(diffs)
	if len(diffs) > 0 || len(before) != len(after) {
		t.Fatalf("%d observable differences (of %d):\n%s", len(diffs), len(before), strings.Join(diffs, "\n"))
	}
	// Không vô nghĩa: ma trận có cả ca tạo được và ca bị từ chối bởi từng luật của adapter.
	statuses := map[int]int{}
	messages := map[string]bool{}
	for _, v := range after {
		m := v.(map[string]any)
		statuses[m["status"].(int)]++
		b, _ := json.Marshal(m["body"])
		for _, msg := range []string{"vai trò phải là", "cấp quản lý không có", "Admin không chọn phòng ban", "bắt buộc Nút tổ chức",
			"bắt buộc ít nhất một phòng ban", "chỉ Cấp Xã có xã phụ trách", "Nút không đúng cấp của vai trò"} {
			if strings.Contains(string(b), msg) {
				messages[msg] = true
			}
		}
	}
	if statuses[http.StatusCreated] == 0 || statuses[http.StatusUnprocessableEntity] == 0 || len(messages) != 7 {
		t.Fatalf("matrix does not exercise every rule: statuses=%v messages=%v", statuses, messages)
	}
}

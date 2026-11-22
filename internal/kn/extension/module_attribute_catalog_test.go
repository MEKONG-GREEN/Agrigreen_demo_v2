package extension_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

var updateAttributeGolden = flag.Bool("update-attribute-catalog", false, "ghi lại testdata/attribute_catalog.golden.json")

func knAttributeCatalog(t *testing.T) *accport.AttributeCatalog {
	t.Helper()
	c, err := extension.AttributeCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// attributeSnapshot: mọi giá trị quan sát được của catalog — danh mục, nhãn, mã CSV và Capabilities của mọi tổ hợp
// vai × cấp quản lý × nhóm (kể cả rỗng và mã lạ).
func attributeSnapshot(c *accport.AttributeCatalog) map[string]any {
	roles := append(append([]string{}, authz.KNRoles...), "kn_boss", "kn_officer")
	levels := []string{"", "khong_co"}
	for _, l := range c.Levels() {
		levels = append(levels, l.Code)
	}
	groups := []string{"", "nhom_la"}
	for _, g := range c.Groups() {
		groups = append(groups, g.Code)
	}
	caps := map[string]string{}
	for _, r := range roles {
		for _, l := range levels {
			for _, g := range groups {
				v, err := c.Capabilities(r, l, g)
				caps[r+"|"+l+"|"+g] = fmt.Sprintf("%+v err=%v", v, err)
			}
		}
	}
	csv := map[string]string{}
	for _, l := range c.Levels() {
		for _, cell := range []string{l.Code, l.Label, strings.ToUpper(l.Label), "  " + l.Label + " ", strings.ReplaceAll(l.Code, "_", " ")} {
			code, ok := c.ManagementCode(cell)
			csv[cell] = fmt.Sprintf("%s %v", code, ok)
		}
	}
	for _, cell := range []string{"", "  ", "Không có", "giam doc", "Ban Giám đốc (chỉ xem)"} {
		code, ok := c.ManagementCode(cell)
		csv[cell] = fmt.Sprintf("%s %v", code, ok)
	}
	labels := map[string]string{}
	for _, code := range append(levels, groups...) {
		labels["level/"+code], labels["group/"+code] = c.LevelLabel(code), c.GroupLabel(code)
	}
	for _, d := range append(c.Duties(), accport.CatalogItem{Code: "nhiem_vu_la"}) {
		labels["duty/"+d.Code] = c.DutyLabel(d.Code)
	}
	return map[string]any{"version": c.Version(), "provisional": c.Provisional(), "levels": c.Levels(), "groups": c.Groups(),
		"duties": c.Duties(), "roles": c.Roles(), "capabilities": caps, "csv": csv, "labels": labels}
}

// Golden "trước/sau": bridge LegacyAttributeCatalog (trước) và catalog KN (sau) cho cùng snapshot, khớp tệp golden.
func TestKNAttributeCatalogParityWithLegacyGolden(t *testing.T) {
	b, _ := json.MarshalIndent(attributeSnapshot(accport.LegacyAttributeCatalog()), "", "  ")
	a, _ := json.MarshalIndent(attributeSnapshot(knAttributeCatalog(t)), "", "  ")
	if string(a) != string(b) {
		t.Fatalf("KN attribute catalog differs from legacy bridge\nlegacy:\n%s\nkn:\n%s", b, a)
	}
	path := filepath.Join("testdata", "attribute_catalog.golden.json")
	if *updateAttributeGolden {
		if err := os.WriteFile(path, append(a, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(a)+"\n" {
		t.Fatalf("effective attribute catalog changed vs %s:\n%s", path, a)
	}
}

// Quy tắc chủ đã chốt: Manager không suy ra đọc mọi phòng; Ban Giám đốc chỉ xem (cả phòng mình), không quyền lãnh
// đạo; Lãnh đạo đơn vị đọc chéo phòng; nhóm/quyền quản lý chỉ ở Cấp Tỉnh; mã lạ bị từ chối.
func TestKNAttributeCatalogOwnerRules(t *testing.T) {
	c := knAttributeCatalog(t)
	capOf := func(role, level, group string) accport.AttributeCapabilities {
		t.Helper()
		v, err := c.Capabilities(role, level, group)
		if err != nil {
			t.Fatalf("%s/%s/%s: %v", role, level, group, err)
		}
		return v
	}
	if v := capOf(authz.KNTinh, "truong_phong", ""); !v.Manager || v.ReadsAllDepartments || v.ForceReadOnly {
		t.Fatalf("manager implied reads-all: %+v", v)
	}
	if v := capOf(authz.KNTinh, "chuyen_vien", "ban_giam_doc"); v.Manager || !v.ReadsAllDepartments || !v.ForceReadOnly {
		t.Fatalf("board group: %+v", v)
	}
	if v := capOf(authz.KNTinh, "lanh_dao_don_vi", ""); !v.Manager || !v.ReadsAllDepartments || v.ForceReadOnly {
		t.Fatalf("unit leader: %+v", v)
	}
	if v := capOf(authz.KNTinh, "chuyen_vien", "lanh_dao_phong"); !v.Manager || v.ReadsAllDepartments {
		t.Fatalf("department leader group: %+v", v)
	}
	for _, role := range []string{authz.KNTW, authz.KNXa, authz.KNAdmin} {
		if v := capOf(role, "giam_doc", ""); v.Manager || v.ReadsAllDepartments {
			t.Fatalf("%s gained management permissions: %+v", role, v)
		}
		if _, err := c.Capabilities(role, "", "lanh_dao_phong"); err == nil {
			t.Fatalf("%s accepted a province group", role)
		}
	}
	if !capOf(authz.KNXa, "", "").ScopedTargets || capOf(authz.KNTinh, "", "").ScopedTargets {
		t.Fatal("scoped targets flag wrong")
	}
	for _, in := range [][3]string{{"kn_officer", "", ""}, {"", "", ""}, {authz.KNTinh, "khong_co", ""}, {authz.KNTinh, "", "nhom_la"}} {
		if _, err := c.Capabilities(in[0], in[1], in[2]); err == nil {
			t.Fatalf("unknown code accepted: %v", in)
		}
	}
}

// Instance độc lập, getter trả bản sao; mã cấp quản lý của catalog trùng danh mục luật lần gán #72.
func TestKNAttributeCatalogIsolationAndAssignmentConsistency(t *testing.T) {
	a, b := knAttributeCatalog(t), knAttributeCatalog(t)
	if a == b {
		t.Fatal("shared instance")
	}
	levels, groups := a.Levels(), a.Groups()
	levels[0].Label, groups[4].ReadOnly = "mutated", false
	if a.LevelLabel("chuyen_vien") != "Chuyên viên - Cán bộ" || !a.Groups()[4].ReadOnly {
		t.Fatal("catalog exposes mutable state")
	}
	rules := extension.AssignmentRules()
	for _, l := range a.Levels() {
		if err := rules.ValidateAssignment(accport.AssignmentRuleValues{Role: authz.KNAdmin, MgmtLevel: l.Code}); err != nil {
			t.Fatalf("assignment rules reject catalog level %s: %v", l.Code, err)
		}
	}
	for _, r := range a.Roles() {
		if err := rules.ValidateAssignment(accport.AssignmentRuleValues{Role: r.Role}); err != nil && strings.Contains(err.Error(), "role:") {
			t.Fatalf("assignment rules reject catalog role %s", r.Role)
		}
	}
	if err := rules.ValidateAssignment(accport.AssignmentRuleValues{Role: authz.KNAdmin, MgmtLevel: "khong_co"}); err == nil {
		t.Fatal("assignment rules accept a level outside the catalog")
	}
}

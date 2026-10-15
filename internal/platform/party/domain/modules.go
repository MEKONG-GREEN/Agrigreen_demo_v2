package domain

// Bật module theo đơn vị (W1.4, R-MOD-08): quy tắc của sổ module_enablement_event và ánh xạ màn menu →
// module để menu không hiện màn của module đã tắt.

import "strings"

// ModuleEnablementRuleVersion là rule_version của sổ module_enablement_event.
const ModuleEnablementRuleVersion = "pf.party.module_enablement/v1"

// menuModules ánh xạ tiền tố đường dẫn màn (web-ops) → module per_tenant sở hữu màn. Khớp tiền tố DÀI
// NHẤT; đường dẫn không khớp thuộc module lõi (pf/party, pf/accounts — luôn hiện). Màn Admin theo phòng
// ban (/kn/phong-ban/<mã>/<màn>) tra ở menuDeptModules theo phần <màn>.
var menuModules = catalogValue[map[string]string]("menuModules")

// menuDeptModules: màn Admin theo phòng ban (/kn/phong-ban/{department}/<màn>) → module.
var menuDeptModules = catalogValue[map[string]string]("menuDeptModules")

// MenuModule trả module per_tenant sở hữu màn route ("" = lõi, luôn hiện).
func MenuModule(route string) string {
	if rest, ok := strings.CutPrefix(route, "/kn/phong-ban/"); ok {
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			return menuDeptModules[rest[i:]]
		}
		return ""
	}
	best, mod := -1, ""
	for p, m := range menuModules {
		if (route == p || strings.HasPrefix(route, p+"/")) && len(p) > best {
			best, mod = len(p), m
		}
	}
	return mod
}

package domain

// Bật module theo đơn vị (W1.4, R-MOD-08): quy tắc của sổ module_enablement_event và ánh xạ màn menu →
// module để menu không hiện màn của module đã tắt.

import "strings"

// ModuleEnablementRuleVersion là rule_version của sổ module_enablement_event.
const ModuleEnablementRuleVersion = "pf.party.module_enablement/v1"

// menuModules ánh xạ tiền tố đường dẫn màn (web-ops) → module per_tenant sở hữu màn. Khớp tiền tố DÀI
// NHẤT; đường dẫn không khớp thuộc module lõi (pf/party, pf/accounts — luôn hiện). Màn Admin theo phòng
// ban (/kn/phong-ban/<mã>/<màn>) tra ở menuDeptModules theo phần <màn>.
var menuModules = map[string]string{
	"/kn/dashboard":                  "pf/extension",
	"/kn/nhiem-vu":                   "pf/extension",
	"/kn/bao-cao":                    "pf/extension",
	"/kn/bao-cao/chi-cuc":            "pf/reports", // gửi báo cáo Chi cục (dispatch)
	"/kn/du-lieu/may-moc":            "pf/mechanization",
	"/kn/du-lieu/ve-thua":            "pf/geo",
	"/kn/du-lieu/vat-tu":             "pf/extension",
	"/kn/quan-tri/don-vi-hanh-chinh": "pf/masterdata",
	"/kn/bao-gia":                    "pf/price", // Báo giá nông sản (W11.72)
	"/kn/cnts/danh-muc":              "pf/masterdata", // W11.41
	"/quan-tri/tham-so":              "pf/masterdata",
	"/quan-tri/mau-bao-cao":          "pf/reports",
	"/quan-tri/xung-dot":             "pf/sync",
	"/quan-tri/giam-sat-dong-bo":     "pf/sync",
	"/dong-bo":                       "pf/sync",
	"/quan-tri/su-dung-api":          "pf/metering",
	"/quan-tri/bang-ke":              "pf/metering",
	"/quan-tri/nhat-ky-xuat":         "pf/metering",
}

// menuDeptModules: màn Admin theo phòng ban (/kn/phong-ban/{department}/<màn>) → module.
var menuDeptModules = map[string]string{
	"/theo-doi-nop":    "pf/extension",
	"/tong-hop":        "pf/extension",
	"/du-lieu-bao-cao": "pf/extension",
	"/thu-thap":        "pf/extension",
	"/danh-muc":        "pf/masterdata",
	"/danh-muc-cnts":   "pf/masterdata", // W11.41
	"/mau-bao-cao":     "pf/reports",
	"/bao-gia-lich":    "pf/price", // W11.74 lịch báo giá (W11.72 mở menu)
}

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

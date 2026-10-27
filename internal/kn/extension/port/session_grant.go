package port

// Phạm vi quyền CNTS FN-14 trong phiên chủ KN (#71 KN-GRANT-OWNER, W11.47, BRD CNTS v3.7 UR-TINH-09, FN-14 AC-01…AC-03).
//
// Chủ KN (extension/cntsmodel) đọc sổ cnts_grant ở server cho đúng người được cấp, tenant và xã của lần gán đã chọn, rồi
// gắn vào payload chủ. Shared (pf/accounts, cap/forms, pf/sync) không giải mã payload này: cap/forms nhận closure qua
// ResolvedActor; dấu quyền authz.Session.AuthorizationStamp đổi khi phạm vi đổi (idempotency, con trỏ đồng bộ).

import (
	"slices"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// CNTSGrant là một quyền FN-14 đang hiệu lực (theo ngày, tính cả hai đầu) chiếu vào phiên.
type CNTSGrant struct {
	Ref        uuid.UUID
	Seq        int64  // phiên bản sổ — sửa quyền đổi dấu quyền của phiên
	Role       string // tang_cuong | ksgm | theo_doi_mo_hinh
	Communes   []string
	Facilities []uuid.UUID
	Sites      []uuid.UUID
	Households []uuid.UUID
	Forms      []string // BM-01…BM-19
	ValidFrom  string   // YYYY-MM-DD
	ValidTo    string   // YYYY-MM-DD; rỗng = không hạn
}

// CNTSGrantScope là phạm vi tường minh: danh sách quyền đang hiệu lực (có thể rỗng = chặn) và hộ mô hình được bao.
type CNTSGrantScope struct {
	Grants []CNTSGrant
	// Households: hộ mô hình còn tham gia được bao — giao trực tiếp hoặc thuộc điểm được giao.
	Households []uuid.UUID
}

// CNTSFormOfTemplate đổi mã biểu cap/forms của phòng CNTS ("CNTS-BM01", "CNTS-BM05-I") sang mã biểu FN-14 ("BM-01",
// "BM-05"). Mã khác → false (phạm vi tường minh chặn biểu không xác định được).
func CNTSFormOfTemplate(code string) (string, bool) {
	const prefix = "CNTS-BM"
	if len(code) < len(prefix)+2 || code[:len(prefix)] != prefix {
		return "", false
	}
	d := code[len(prefix) : len(prefix)+2]
	rest := code[len(prefix)+2:]
	if d[0] < '0' || d[0] > '1' || d[1] < '0' || d[1] > '9' || d == "00" || (rest != "" && rest[0] != '-') || (rest == "-") {
		return "", false
	}
	return "BM-" + d, true
}

// cntsExplicit: phiên Cấp Xã có phạm vi quyền FN-14 tường minh.
func (k SessionContext) cntsExplicit() bool {
	return k.Role == SessionRoleCommune && k.CNTSGrant != nil
}

// AllowsCNTSForm cho biết biểu cap/forms code (profile kn_cnts) thuộc biểu được giao. Không tường minh → mọi biểu.
func (k SessionContext) AllowsCNTSForm(code string) bool {
	if !k.cntsExplicit() {
		return true
	}
	form, ok := CNTSFormOfTemplate(code)
	if !ok {
		return false
	}
	for _, g := range k.CNTSGrant.Grants {
		if slices.Contains(g.Forms, form) {
			return true
		}
	}
	return false
}

// AllowsCNTSFacility: BR-02/AC-03 — phạm vi cơ sở KSGM là danh sách cơ sở được cấp ở FN-14 (nguồn duy nhất). Không
// tường minh → giới hạn W11.07 cũ (AllowsFacility).
func (k SessionContext) AllowsCNTSFacility(ref uuid.UUID) bool {
	if !k.cntsExplicit() {
		return k.AllowsFacility(ref)
	}
	for _, g := range k.CNTSGrant.Grants {
		if slices.Contains(g.Facilities, ref) {
			return true
		}
	}
	return false
}

// AllowsCNTSHousehold: hộ mô hình được bao bởi quyền theo dõi mô hình. Không tường minh → AllowsHousehold cũ.
func (k SessionContext) AllowsCNTSHousehold(ref uuid.UUID) bool {
	if !k.cntsExplicit() {
		return k.AllowsHousehold(ref)
	}
	return slices.Contains(k.CNTSGrant.Households, ref)
}

// CNTSScope là phạm vi xã cho biểu CNTS: không tường minh → Scope của phiên; tường minh → các xã của quyền đang hiệu lực
// GIAO với Scope (chỉ thu hẹp).
func (k SessionContext) CNTSScope() authz.Scope {
	if !k.cntsExplicit() {
		return authz.Scope{All: k.Scope.All, Units: slices.Clone(k.Scope.Units)}
	}
	units := []string{}
	for _, g := range k.CNTSGrant.Grants {
		for _, c := range g.Communes {
			if k.Scope.Allows(c) && !slices.Contains(units, c) {
				units = append(units, c)
			}
		}
	}
	slices.Sort(units)
	return authz.Scope{Units: units}
}

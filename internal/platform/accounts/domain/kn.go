package domain

// KN v7.7 gói P1: quy tắc gán vai trò (FN-AD-04.04) và ngữ cảnh phiên (FN-CH-00.01). Hàm thuần — không
// đọc đồng hồ (R-12), không phụ thuộc hạ tầng.

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// KNRuleVersion ghi vào sổ gán vai trò, phiên hỗ trợ, nhật ký hỗ trợ, sổ chỉnh sửa tài khoản.
const KNRuleVersion = "accounts.kn/1"

// Thời hạn ngữ cảnh — TẠM, chờ khách (FN-CH-00.01: phiên Web 4 giờ không thao tác, Mobile 24 giờ do
// Keycloak giữ; ngữ cảnh chỉ là lựa chọn vai trò + phòng ban trong phiên đó, nên lấy trần 24 giờ).
const (
	ContextTTL = 24 * time.Hour
	// SupportTTL: phiên hỗ trợ của Admin tự hết hạn (FN-AD-04.07) — tạm, chờ khách.
	SupportTTL = 4 * time.Hour
	// MinKNReason: lý do thu hồi vai trò, bắt đầu hỗ trợ, thao tác hỗ trợ.
	MinKNReason = 10
)

// Sự kiện sổ gán vai trò.
const (
	AsgGranted = "granted"
	AsgChanged = "changed"
	AsgRevoked = "revoked"
)

// Thông điệp kiểm dữ liệu KN.
const (
	MsgKNRole        = "vai trò phải là kn_tw | kn_tinh | kn_xa | admin"
	MsgKNOrgRequired = "bắt buộc Nút tổ chức của vai trò"
	MsgKNAdminScope  = "Admin không chọn phòng ban, không gán xã"
	MsgKNDeptMissing = "bắt buộc ít nhất một phòng ban"
	MsgKNDeptCode    = "mã phòng ban không hợp lệ"
	MsgKNCommunes    = "chỉ Cấp Xã có xã phụ trách"
	MsgKNCommuneCode = "mã xã không hợp lệ"
	MsgKNMgmtLevel   = "cấp quản lý không có trong danh mục (tạm, chờ khách Q-05)"
	MsgKNValidity    = "valid_to phải sau valid_from"
	MsgKNReason      = "lý do phải có ít nhất 10 ký tự"
	MsgKNOrgKind     = "Nút không đúng cấp của vai trò"
)

var (
	deptCodeRe    = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
	communeCodeRe = regexp.MustCompile(`^[0-9]{2,10}$`)
)

// AssignmentInput là dữ liệu gán/sửa một vai trò.
type AssignmentInput struct {
	Role        string
	OrgRef      uuid.UUID
	Departments []string
	MgmtLevel   string
	Communes    []string
	ValidFrom   time.Time // zero → lúc ghi
	ValidTo     *time.Time
}

func dedupe(in []string, re *regexp.Regexp, field, msg string) ([]string, error) {
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !re.MatchString(s) {
			return nil, FieldError{Field: field, Msg: msg}
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out, nil
}

// NormalizeAssignment chuẩn hóa và kiểm ràng buộc không phụ thuộc dữ liệu (UR-AD-04): Admin không phòng
// ban, không xã; vai khác có Nút và ≥ 1 phòng ban; chỉ Cấp Xã có xã phụ trách (được phép rỗng — lưu kèm
// cảnh báo, chưa thấy dữ liệu nghiệp vụ). now dùng khi ValidFrom rỗng.
func NormalizeAssignment(in AssignmentInput, now time.Time) (AssignmentInput, error) {
	in.Role = strings.TrimSpace(in.Role)
	in.MgmtLevel = strings.TrimSpace(in.MgmtLevel)
	if !slices.Contains(port.KNRolesAll, in.Role) {
		return in, FieldError{Field: "role", Msg: MsgKNRole}
	}
	var err error
	if in.Departments, err = dedupe(in.Departments, deptCodeRe, "department_codes", MsgKNDeptCode); err != nil {
		return in, err
	}
	if in.Communes, err = dedupe(in.Communes, communeCodeRe, "commune_codes", MsgKNCommuneCode); err != nil {
		return in, err
	}
	if in.MgmtLevel != "" && port.MgmtLevelLabel(in.MgmtLevel) == "" {
		return in, FieldError{Field: "mgmt_level", Msg: MsgKNMgmtLevel}
	}
	switch {
	case in.Role == port.KNAdmin && (len(in.Departments) > 0 || len(in.Communes) > 0):
		return in, FieldError{Field: "department_codes", Msg: MsgKNAdminScope}
	case in.Role != port.KNAdmin && in.OrgRef == uuid.Nil:
		return in, FieldError{Field: "org_ref", Msg: MsgKNOrgRequired}
	case in.Role != port.KNAdmin && len(in.Departments) == 0:
		return in, FieldError{Field: "department_codes", Msg: MsgKNDeptMissing}
	case in.Role != port.KNXa && len(in.Communes) > 0:
		return in, FieldError{Field: "commune_codes", Msg: MsgKNCommunes}
	}
	if in.ValidFrom.IsZero() {
		in.ValidFrom = now
	}
	in.ValidFrom = in.ValidFrom.UTC().Truncate(time.Microsecond)
	if in.ValidTo != nil {
		t := in.ValidTo.UTC().Truncate(time.Microsecond)
		if !t.After(in.ValidFrom) {
			return in, FieldError{Field: "valid_to", Msg: MsgKNValidity}
		}
		in.ValidTo = &t
	}
	return in, nil
}

// KNOrgKinds: loại Nút hợp lệ của từng vai (plan P1 mục 1). Cấp Xã gắn Nút TỈNH + danh sách xã phụ trách
// (Hình 25b). Admin: Nút tùy chọn, mọi loại.
var KNOrgKinds = map[string][]string{
	port.KNTW:   {"ttkn_national"},
	port.KNTinh: {"ttkn_province"},
	port.KNXa:   {"ttkn_province"},
}

// KNRoleFitsOrg kiểm loại Nút của vai.
func KNRoleFitsOrg(role, orgKind string) error {
	kinds, ok := KNOrgKinds[role]
	if ok && !slices.Contains(kinds, orgKind) {
		return FieldError{Field: "org_ref", Msg: MsgKNOrgKind + " (" + strings.Join(kinds, ", ") + ")"}
	}
	return nil
}

// CheckKNReason kiểm lý do ≥ 10 ký tự.
func CheckKNReason(field, reason string) error {
	if utf8.RuneCountInString(strings.TrimSpace(reason)) < MinKNReason {
		return FieldError{Field: field, Msg: MsgKNReason}
	}
	return nil
}

// DefaultRoute: trang mặc định theo vai (FN-CH-00.01) — đường dẫn của bề mặt KN web-ops (P6).
func DefaultRoute(role string) string {
	switch role {
	case port.KNTW, port.KNTinh:
		return "/kn/dashboard"
	case port.KNXa:
		return "/kn/nhiem-vu/cong-viec"
	case port.KNAdmin:
		return "/kn/tong-quan"
	}
	return "/"
}

// AssignmentSnapshot là ảnh chụp phẳng (không PII) của một lần gán cho khung Log.
type AssignmentSnapshot struct {
	Role        string  `json:"role"`
	OrgRef      string  `json:"org_ref"`
	Departments string  `json:"department_codes"`
	MgmtLevel   string  `json:"mgmt_level"`
	Communes    string  `json:"commune_codes"`
	ValidFrom   string  `json:"valid_from"`
	ValidTo     *string `json:"valid_to"`
	Active      bool    `json:"active"`
}

// SnapshotOf dựng ảnh chụp của lần gán.
func SnapshotOf(a port.Assignment) AssignmentSnapshot {
	s := AssignmentSnapshot{Role: a.Role, Departments: strings.Join(a.Departments, ","), MgmtLevel: a.MgmtLevel,
		Communes: strings.Join(a.Communes, ","), ValidFrom: a.ValidFrom.UTC().Format(time.RFC3339), Active: a.Active}
	if a.OrgRef != uuid.Nil {
		s.OrgRef = a.OrgRef.String()
	}
	if a.ValidTo != nil {
		v := a.ValidTo.UTC().Format(time.RFC3339)
		s.ValidTo = &v
	}
	return s
}

// FieldChange là một trường đổi trong khung Log (cũ → mới; nil = không có).
type FieldChange struct {
	Field string  `json:"field"`
	Old   *string `json:"old"`
	New   *string `json:"new"`
}

// Diff so hai ảnh chụp JSON phẳng; before nil → mọi trường của after là mới. Sắp theo tên trường.
func Diff(before, after []byte) []FieldChange {
	b, a := flat(before), flat(after)
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	slices.Sort(names)
	out := []FieldChange{}
	for _, k := range names {
		o, n := b[k], a[k]
		if (o == nil && n == nil) || (o != nil && n != nil && *o == *n) {
			continue
		}
		out = append(out, FieldChange{Field: k, Old: o, New: n})
	}
	return out
}

func flat(raw []byte) map[string]*string {
	out := map[string]*string{}
	if len(raw) == 0 {
		return out
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return out
	}
	for k, v := range m {
		var s string
		switch {
		case string(v) == "null":
			out[k] = nil
			continue
		case json.Unmarshal(v, &s) == nil:
		default:
			s = string(v) // số, bool giữ nguyên dạng JSON
		}
		if s == "" {
			out[k] = nil
			continue
		}
		out[k] = &s
	}
	return out
}

package domain

// W11.07 — phân quyền trong vai (D4 X3, D3 H0/C3–C5): thuộc tính của lần gán (nhóm Cấp Tỉnh, cờ nhiệm vụ,
// đơn vị nội bộ, phạm vi Cấp Xã theo cơ sở/hộ) và ủy quyền có hạn. Hàm thuần — không đọc đồng hồ (R-12).

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// AttrRuleVersion ghi vào sổ thuộc tính vai và sổ ủy quyền.
const AttrRuleVersion = "accounts.kn-attrs/1"

// Tham số ủy quyền có hạn — TẠM (BRD CNTS III.3.0 chỉ nói "có thời hạn"; chờ khách). Đổi = tăng
// AttrRuleVersion.
const (
	// DelegationMaxDays: thời hạn tối đa một lần ủy quyền.
	DelegationMaxDays = 30
	// MaxScopeTargets: số cơ sở/hộ tối đa trong phạm vi một lần gán Cấp Xã.
	MaxScopeTargets = 500
)

// Thông điệp kiểm dữ liệu.
const (
	MsgAttrGroupRole    = "nhóm Cấp Tỉnh và cờ nhiệm vụ chỉ dành cho vai Cấp Tỉnh"
	MsgAttrGroup        = "nhóm Cấp Tỉnh không có trong danh mục (tạm, chờ khách Q-059)"
	MsgAttrDuty         = "cờ nhiệm vụ không có trong danh mục"
	MsgAttrUnitCode     = "mã đơn vị nội bộ không hợp lệ"
	MsgAttrTargetsRole  = "giới hạn cơ sở/hộ chỉ dành cho vai Cấp Xã"
	MsgAttrTargetsMax   = "quá nhiều cơ sở/hộ trong phạm vi"
	MsgAttrTargetNil    = "mã tham chiếu cơ sở/hộ không hợp lệ"
	MsgDelegPower       = "quyền không có trong danh mục ủy quyền"
	MsgDelegWindow      = "ủy quyền phải có hạn: valid_to sau valid_from và không quá 30 ngày"
	MsgDelegSelf        = "không tự ủy quyền cho chính mình"
	MsgDelegDepartment  = "bắt buộc phòng ban"
	MsgDelegGranteeNull = "bắt buộc người nhận ủy quyền"
)

var unitCodeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// AttrsInput là dữ liệu đặt thuộc tính vai.
type AttrsInput struct {
	ProvinceGroup string
	Duties        []string
	InternalUnit  string
	Facilities    []uuid.UUID
	Households    []uuid.UUID
}

func dedupeRefs(in []uuid.UUID) ([]uuid.UUID, error) {
	out := []uuid.UUID{}
	for _, r := range in {
		if r == uuid.Nil {
			return nil, FieldError{Field: "facility_refs", Msg: MsgAttrTargetNil}
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out, nil
}

// NormalizeAttrs chuẩn hóa và kiểm thuộc tính theo vai của lần gán: nhóm + cờ chỉ Cấp Tỉnh, cơ sở/hộ chỉ Cấp
// Xã, mã thuộc danh mục có phiên bản (port.ProvinceGroups, port.Duties).
func NormalizeAttrs(role string, in AttrsInput) (AttrsInput, error) {
	return NormalizeAttrsWithCatalog(role, in, port.LegacyAttributeCatalog())
}

// NormalizeAttrsWithCatalog keeps structural/target safeguards native, with
// membership and role capabilities from the same immutable catalog as readers.
func NormalizeAttrsWithCatalog(role string, in AttrsInput, catalog *port.AttributeCatalog) (AttrsInput, error) {
	if !catalog.Valid() {
		return in, port.ErrAttributeCatalog
	}
	roleAttrs, known := catalog.Role(role)
	if !known {
		return in, FieldError{Field: "role", Msg: MsgKNRole}
	}
	in.ProvinceGroup = strings.TrimSpace(in.ProvinceGroup)
	in.InternalUnit = strings.TrimSpace(in.InternalUnit)
	duties := []string{}
	for _, d := range in.Duties {
		d = strings.TrimSpace(d)
		if d == "" || slices.Contains(duties, d) {
			continue
		}
		if catalog.DutyLabel(d) == "" {
			return in, FieldError{Field: "duties", Msg: MsgAttrDuty}
		}
		duties = append(duties, d)
	}
	slices.Sort(duties)
	in.Duties = duties
	if in.ProvinceGroup != "" && catalog.GroupLabel(in.ProvinceGroup) == "" {
		return in, FieldError{Field: "province_group", Msg: MsgAttrGroup}
	}
	if !roleAttrs.GroupAndDuties && (in.ProvinceGroup != "" || len(in.Duties) > 0) {
		return in, FieldError{Field: "province_group", Msg: MsgAttrGroupRole}
	}
	if in.InternalUnit != "" && !unitCodeRe.MatchString(in.InternalUnit) {
		return in, FieldError{Field: "internal_unit_code", Msg: MsgAttrUnitCode}
	}
	var err error
	if in.Facilities, err = dedupeRefs(in.Facilities); err != nil {
		return in, err
	}
	if in.Households, err = dedupeRefs(in.Households); err != nil {
		return in, FieldError{Field: "household_refs", Msg: MsgAttrTargetNil}
	}
	if !roleAttrs.ScopedTargets && (len(in.Facilities) > 0 || len(in.Households) > 0) {
		return in, FieldError{Field: "facility_refs", Msg: MsgAttrTargetsRole}
	}
	if len(in.Facilities)+len(in.Households) > MaxScopeTargets {
		return in, FieldError{Field: "facility_refs", Msg: MsgAttrTargetsMax}
	}
	return in, nil
}

// AttrsSnapshot là ảnh chụp phẳng (không PII) của thuộc tính vai cho khung Log.
type AttrsSnapshot struct {
	ProvinceGroup string `json:"province_group"`
	Duties        string `json:"duties"`
	InternalUnit  string `json:"internal_unit_code"`
	Facilities    string `json:"facility_refs"`
	Households    string `json:"household_refs"`
}

func joinRefs(rs []uuid.UUID) string {
	s := make([]string, 0, len(rs))
	for _, r := range rs {
		s = append(s, r.String())
	}
	return strings.Join(s, ",")
}

// AttrsSnapshotOf dựng ảnh chụp.
func AttrsSnapshotOf(a port.AssignmentAttrs) AttrsSnapshot {
	return AttrsSnapshot{ProvinceGroup: a.ProvinceGroup, Duties: strings.Join(a.Duties, ","), InternalUnit: a.InternalUnit,
		Facilities: joinRefs(a.Facilities), Households: joinRefs(a.Households)}
}

// ViewOnlyDepartment cho biết ngữ cảnh của lần gán trên phòng ban dept là CHỈ XEM, và ok = lần gán được chọn
// phòng ban đó. Quy tắc (Q-059, đề xuất của dev): phòng ban trong lần gán → chọn được; Lãnh đạo đơn vị
// (ReadsAllDepartments) chọn được mọi phòng ban khác của đơn vị ở chế độ chỉ xem; nhóm Ban Giám đốc luôn chỉ
// xem (và đọc được mọi phòng ban như Lãnh đạo đơn vị).
func ViewOnlyDepartment(a port.Assignment, group, dept string) (viewOnly, ok bool) {
	viewOnly, ok, _ = ViewOnlyDepartmentWithCatalog(a, group, dept, port.LegacyAttributeCatalog())
	return
}

// ViewOnlyDepartmentWithCatalog never grants cross-department writes. Unknown
// stored codes/configuration return an error rather than default capabilities.
func ViewOnlyDepartmentWithCatalog(a port.Assignment, group, dept string, catalog *port.AttributeCatalog) (viewOnly, ok bool, err error) {
	caps, err := catalog.Capabilities(a.Role, a.MgmtLevel, group)
	if err != nil {
		return false, false, err
	}
	if slices.Contains(a.Departments, dept) {
		return caps.ForceReadOnly, true, nil
	}
	if caps.ReadsAllDepartments && dept != "" {
		return true, true, nil
	}
	return false, false, nil
}

// ReadsAllDepartments keeps the legacy public helper during owner migration.
func ReadsAllDepartments(a port.Assignment, group string) bool {
	caps, err := port.LegacyAttributeCatalog().Capabilities(a.Role, a.MgmtLevel, group)
	return err == nil && caps.ReadsAllDepartments
}

// DelegationInput là dữ liệu tạo ủy quyền.
type DelegationInput struct {
	GranteeRef uuid.UUID
	Department string
	Power      string
	ValidFrom  time.Time // zero → lúc ghi
	ValidTo    time.Time
	Reason     string
}

// NormalizeDelegation kiểm ràng buộc không phụ thuộc dữ liệu: quyền trong danh mục, có hạn (≤ 30 ngày), có
// lý do ≥ 10 ký tự, không tự ủy quyền.
func NormalizeDelegation(grantor uuid.UUID, in DelegationInput, now time.Time) (DelegationInput, error) {
	in.Department, in.Power, in.Reason = strings.TrimSpace(in.Department), strings.TrimSpace(in.Power), strings.TrimSpace(in.Reason)
	switch {
	case in.GranteeRef == uuid.Nil:
		return in, FieldError{Field: "grantee_ref", Msg: MsgDelegGranteeNull}
	case in.GranteeRef == grantor:
		return in, FieldError{Field: "grantee_ref", Msg: MsgDelegSelf}
	case in.Department == "":
		return in, FieldError{Field: "department_code", Msg: MsgDelegDepartment}
	case port.CatalogLabel(port.Powers, in.Power) == "":
		return in, FieldError{Field: "power", Msg: MsgDelegPower}
	}
	if err := CheckKNReason("reason", in.Reason); err != nil {
		return in, err
	}
	if in.ValidFrom.IsZero() || in.ValidFrom.Before(now) {
		in.ValidFrom = now
	}
	in.ValidFrom = in.ValidFrom.UTC().Truncate(time.Microsecond)
	in.ValidTo = in.ValidTo.UTC().Truncate(time.Microsecond)
	if !in.ValidTo.After(in.ValidFrom) || in.ValidTo.Sub(in.ValidFrom) > DelegationMaxDays*24*time.Hour {
		return in, FieldError{Field: "valid_to", Msg: MsgDelegWindow}
	}
	return in, nil
}

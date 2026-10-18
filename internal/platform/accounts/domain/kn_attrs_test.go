package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// W11.07: thuộc tính vai theo vai của lần gán.
func TestW1107_NormalizeAttrs(t *testing.T) {
	f := uuid.New()
	in, err := domain.NormalizeAttrs(port.KNTinh, domain.AttrsInput{ProvinceGroup: " tong_hop ", Duties: []string{"phu_trach_bieu", "qtnv_bao_gia", "qtnv_bao_gia"}})
	if err != nil || in.ProvinceGroup != "tong_hop" || len(in.Duties) != 2 || in.Duties[0] != "phu_trach_bieu" {
		t.Fatalf("Cấp Tỉnh: %+v %v", in, err)
	}
	cases := map[string]struct {
		role string
		in   domain.AttrsInput
	}{
		"nhóm ở Cấp Xã":     {port.KNXa, domain.AttrsInput{ProvinceGroup: "tong_hop"}},
		"cờ ở Cấp TW":       {port.KNTW, domain.AttrsInput{Duties: []string{"qtnv_bao_gia"}}},
		"cờ lạ":             {port.KNTinh, domain.AttrsInput{Duties: []string{"sieu_quyen"}}},
		"nhóm lạ":           {port.KNTinh, domain.AttrsInput{ProvinceGroup: "giam_doc_so"}},
		"cơ sở ở Cấp Tỉnh":  {port.KNTinh, domain.AttrsInput{Facilities: []uuid.UUID{f}}},
		"hộ ở Admin":        {port.KNAdmin, domain.AttrsInput{Households: []uuid.UUID{f}}},
		"mã đơn vị sai":     {port.KNXa, domain.AttrsInput{InternalUnit: "Tram 1"}},
		"mã tham chiếu Nil": {port.KNXa, domain.AttrsInput{Facilities: []uuid.UUID{uuid.Nil}}},
	}
	for name, c := range cases {
		if _, err := domain.NormalizeAttrs(c.role, c.in); err == nil {
			t.Errorf("%s: phải lỗi", name)
		}
	}
	if x, err := domain.NormalizeAttrs(port.KNXa, domain.AttrsInput{Facilities: []uuid.UUID{f, f}, InternalUnit: "tram_1"}); err != nil || len(x.Facilities) != 1 {
		t.Fatalf("Cấp Xã: %+v %v", x, err)
	}
}

// W11.07 (Q-059): Lãnh đạo đơn vị chọn mọi phòng ban (ngoài lần gán: chỉ xem); BGĐ luôn chỉ xem; người khác chỉ
// phòng ban của mình.
func TestW1107_ViewOnlyDepartment(t *testing.T) {
	boss := port.Assignment{Role: port.KNTinh, Departments: []string{"trong_trot"}, MgmtLevel: port.MgmtLanhDaoDonVi}
	head := port.Assignment{Role: port.KNTinh, Departments: []string{"trong_trot"}, MgmtLevel: port.MgmtTruongPhong}
	plain := port.Assignment{Role: port.KNTinh, Departments: []string{"trong_trot"}}
	xa := port.Assignment{Role: port.KNXa, Departments: []string{"trong_trot"}, MgmtLevel: port.MgmtLanhDaoDonVi}
	cases := []struct {
		a        port.Assignment
		group    string
		dept     string
		ro, okay bool
	}{
		{boss, "", "trong_trot", false, true},
		{boss, "", "chan_nuoi_thuy_san", true, true},
		{boss, "", "", false, false},
		{head, "", "chan_nuoi_thuy_san", false, false},
		{plain, port.GroupBanGiamDoc, "trong_trot", true, true},
		{plain, port.GroupBanGiamDoc, "ke_hoach_tai_chinh", true, true},
		{xa, "", "chan_nuoi_thuy_san", false, false},
	}
	for _, c := range cases {
		ro, ok := domain.ViewOnlyDepartment(c.a, c.group, c.dept)
		if ro != c.ro || ok != c.okay {
			t.Errorf("%+v %s %s: có (%v, %v), muốn (%v, %v)", c.a, c.group, c.dept, ro, ok, c.ro, c.okay)
		}
	}
	if !port.IsManager(port.MgmtPhuTrach, "") || !port.IsManager("", port.GroupLanhDaoPhong) || port.IsManager(port.MgmtChuyenVien, port.GroupTongHop) {
		t.Fatal("IsManager")
	}
}

// W11.07: ủy quyền có hạn — tối đa 30 ngày, có lý do, quyền trong danh mục, không tự ủy quyền.
func TestW1107_NormalizeDelegation(t *testing.T) {
	now := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	me, other := uuid.New(), uuid.New()
	valid := domain.DelegationInput{GranteeRef: other, Department: "trong_trot", Power: "mo_lai_ky", ValidTo: now.Add(7 * 24 * time.Hour),
		Reason: "đi công tác một tuần"}
	d, err := domain.NormalizeDelegation(me, valid, now)
	if err != nil || !d.ValidFrom.Equal(now) {
		t.Fatalf("hợp lệ: %+v %v", d, err)
	}
	with := func(mut func(*domain.DelegationInput)) domain.DelegationInput {
		x := valid
		mut(&x)
		return x
	}
	cases := map[string]domain.DelegationInput{
		"tự ủy quyền": with(func(x *domain.DelegationInput) { x.GranteeRef = me }),
		"quá 30 ngày": with(func(x *domain.DelegationInput) { x.ValidTo = now.Add(31 * 24 * time.Hour) }),
		"hết hạn rồi": with(func(x *domain.DelegationInput) { x.ValidTo = now.Add(-time.Hour) }),
		"lý do ngắn":  with(func(x *domain.DelegationInput) { x.Reason = "ngắn" }),
		"quyền lạ":    with(func(x *domain.DelegationInput) { x.Power = "xoa_du_lieu" }),
		"thiếu phòng": with(func(x *domain.DelegationInput) { x.Department = " " }),
		"thiếu người": with(func(x *domain.DelegationInput) { x.GranteeRef = uuid.Nil }),
	}
	for name, in := range cases {
		if _, err := domain.NormalizeDelegation(me, in, now); err == nil {
			t.Errorf("%s: phải lỗi", name)
		}
	}
}

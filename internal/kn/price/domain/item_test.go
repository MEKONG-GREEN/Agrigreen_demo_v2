package domain

import (
	"errors"
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func iv(code string, seq int64, from, status, kind, group string, sort int) Ver[Item] {
	return Ver[Item]{Key: code, Seq: seq, ValidFrom: d(from), Status: status, V: Item{Code: code, Name: "Xoài", Variety: code, GroupCode: group,
		UnitCode: "kg", Kind: kind, SortOrder: sort, Status: status, ValidFrom: d(from)}}
}

func TestValidateItem_W1173(t *testing.T) {
	ok := Item{Code: "MH00001", Name: "Xoài", Variety: "cát Hòa Lộc loại 1", GroupCode: "rau_mau", UnitCode: "kg", Kind: ItemBasic,
		SortOrder: 10, Status: StatusActive, ValidFrom: d("2026-10-13")}
	if err := ValidateItem(ok); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Item){
		"không giống (AC-06)": func(i *Item) { i.Variety = "  " },
		"tên rỗng":            func(i *Item) { i.Name = "" },
		"khoảng trắng đầu":    func(i *Item) { i.Name = " Xoài" },
		"loại sai":            func(i *Item) { i.Kind = "other" },
		"nhóm sai":            func(i *Item) { i.GroupCode = "Rau Màu" },
		"thay thế chính nó":   func(i *Item) { i.ReplacesCode = "MH00001" },
		"thiếu ngày":          func(i *Item) { i.ValidFrom = time.Time{} },
	} {
		it := ok
		mut(&it)
		if err := ValidateItem(it); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if ItemCode(42) != "MH00042" || !ValidItemCode("MH00042") || ValidItemCode("MH42") {
		t.Fatal("mã mặt hàng")
	}
	if !ValidPointCode("30280") || !ValidPointCode("xa_my_hoa-1") || ValidPointCode("a:b") || ValidPointCode("") {
		t.Fatal("mã xã")
	}
	a := ok
	a.Name, a.Variety = "XOÀI ", "Cát  hòa lộc loại 1"
	if IdentityKey(a) != IdentityKey(ok) {
		t.Fatal("khóa trùng phải bỏ hoa thường, khoảng trắng thừa")
	}
}

// Ngưng "Lúa Jasmine 85" hiệu lực đến 30/09 → từ 01/10 không còn; ngày 30/09 vẫn hoạt động, có hiệu lực đến 30/09 (AC-02).
// Mặt hàng hiệu lực từ 05/10 → ngày 04/10 "chờ hiệu lực", từ 05/10 hoạt động (AC-03).
func TestItemStates_W1173(t *testing.T) {
	vs := []Ver[Item]{
		iv("MH00001", 1, "2026-09-01", StatusActive, ItemBasic, "lua_gao", 10),
		iv("MH00001", 2, "2026-10-01", StatusRetired, ItemBasic, "lua_gao", 10),
		iv("MH00002", 1, "2026-10-05", StatusActive, ItemBasic, "rau_mau", 10),
		iv("MH00002", 2, "2026-10-05", StatusActive, ItemBasic, "rau_mau", 20), // sửa cùng ngày: seq lớn thắng
	}
	s := ItemStates(vs, d("2026-09-30"))
	if st := s["MH00001"]; st.State != ItemStateActive || !st.ValidTo.Equal(d("2026-09-30")) || !st.Since.Equal(d("2026-09-01")) {
		t.Fatalf("30/09: %+v", st)
	}
	s = ItemStates(vs, d("2026-10-04"))
	if st := s["MH00001"]; st.State != ItemStateRetired || !st.ValidTo.Equal(d("2026-09-30")) {
		t.Fatalf("04/10 ngưng: %+v", st)
	}
	if st := s["MH00002"]; st.State != ItemStatePending || st.Version != 2 || st.SortOrder != 20 {
		t.Fatalf("04/10 chờ: %+v", st)
	}
	if st := ItemStates(vs, d("2026-10-05"))["MH00002"]; st.State != ItemStateActive || !st.ValidTo.IsZero() {
		t.Fatalf("05/10: %+v", st)
	}
	if _, ok := ItemStates(vs, d("2026-08-01"))["MH00001"]; !ok {
		t.Fatal("trước hiệu lực vẫn thấy dạng chờ")
	}
}

// Danh sách mặt hàng của xã (AC-04, BR-03, BR-08): Cơ bản ở mọi xã, Địa phương theo gán, bộ theo vụ tắt thì ẩn, thứ tự
// nhóm rồi thứ tự mặt hàng.
func TestPointItems_W1173(t *testing.T) {
	vs := []Ver[Item]{
		iv("MH00001", 1, "2026-10-01", StatusActive, ItemBasic, "rau_mau", 20),
		iv("MH00002", 1, "2026-10-01", StatusActive, ItemBasic, "lua_gao", 10),
		iv("MH00003", 1, "2026-10-01", StatusActive, ItemLocal, "rau_mau", 10),
		iv("MH00004", 1, "2026-10-01", StatusActive, ItemBasic, "rau_mau", 30), // thuộc bộ Hè Thu
		iv("MH00005", 1, "2026-10-01", StatusActive, ItemLocal, "rau_mau", 40), // gán xã khác
	}
	as := []Ver[Assignment]{
		{Key: AssignKey("X1", "MH00003"), Seq: 1, ValidFrom: d("2026-10-01"), Status: StatusActive, V: Assignment{"X1", "MH00003"}},
		{Key: AssignKey("X2", "MH00005"), Seq: 1, ValidFrom: d("2026-10-01"), Status: StatusActive, V: Assignment{"X2", "MH00005"}},
		{Key: AssignKey("X1", "MH00003"), Seq: 2, ValidFrom: d("2026-10-10"), Status: StatusRetired, V: Assignment{"X1", "MH00003"}},
	}
	sets := []Ver[ItemSet]{
		{Key: "he_thu", Seq: 1, ValidFrom: d("2026-10-01"), Status: StatusActive, V: ItemSet{Code: "he_thu", ItemCodes: []string{"MH00004"}, Enabled: true}},
		{Key: "he_thu", Seq: 2, ValidFrom: d("2026-10-08"), Status: StatusActive, V: ItemSet{Code: "he_thu", ItemCodes: []string{"MH00004"}, Enabled: false}},
	}
	order := map[string]int{"lua_gao": 10, "rau_mau": 20}
	codes := func(day string, point string) []string {
		at := d(day)
		var out []string
		for _, it := range PointItems(ItemStates(vs, at), EffectiveAt(as, at), Gate(EffectiveAt(sets, at)), point, order) {
			out = append(out, it.Code)
		}
		return out
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%v, muốn %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%v, muốn %v", got, want)
			}
		}
	}
	eq(codes("2026-10-05", "X1"), "MH00002", "MH00003", "MH00001", "MH00004")
	eq(codes("2026-10-05", "X2"), "MH00002", "MH00001", "MH00004", "MH00005")
	eq(codes("2026-10-08", "X1"), "MH00002", "MH00003", "MH00001") // bộ Hè Thu tắt
	eq(codes("2026-10-10", "X1"), "MH00002", "MH00001")            // bỏ gán từ 10/10
}

package domain

import (
	"strings"
	"testing"
	"time"
)

func fieldsOf(es Errors) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Field)
	}
	return out
}

func TestNormalizeFields(t *testing.T) {
	f := Fields{CommuneCode: " 30001 ", Hamlet: "  Ấp   Mỹ  Hòa ", LivestockType: TypeFarmSmall,
		Animals: []Animal{{Species: " GA ", Heads: 120}, {Species: "VIT", Heads: 0}}}
	if es := f.Normalize(); len(es) != 0 {
		t.Fatalf("hợp lệ: %v", es)
	}
	if f.CommuneCode != "30001" || f.Hamlet != "Ấp Mỹ Hòa" || f.Animals[0].Species != "GA" {
		t.Fatalf("chuẩn hóa: %+v", f)
	}
	bad := Fields{CommuneCode: "", Hamlet: strings.Repeat("a", 201), LivestockType: "trang_trai",
		Animals: []Animal{{Species: "GA", Heads: -1}, {Species: "GA", Heads: MaxHeads + 1}}}
	got := strings.Join(fieldsOf(bad.Normalize()), ",")
	for _, w := range []string{"commune_code", "hamlet", "livestock_type", "animals[0].head_count", "animals[1].species_code", "animals[1].head_count"} {
		if !strings.Contains(got, w) {
			t.Errorf("thiếu lỗi %s trong %s", w, got)
		}
	}
	empty := Fields{CommuneCode: "30001", LivestockType: TypeHousehold}
	if es := empty.Normalize(); len(es) != 1 || es[0].Field != FieldAnimals {
		t.Fatalf("thiếu vật nuôi: %v", es)
	}
	many := Fields{CommuneCode: "30001", LivestockType: TypeHousehold, Animals: make([]Animal, 31)}
	for i := range many.Animals {
		many.Animals[i] = Animal{Species: strings.Repeat("x", i+1)}
	}
	if es := many.Normalize(); len(es) != 1 || es[0].Field != FieldAnimals {
		t.Fatalf("31 dòng: %v", es)
	}
}

func TestAccuracyAndCheckin(t *testing.T) {
	for _, s := range []string{"12", "12.5", "0.25", "99999999.99"} {
		if _, ok := ParseAccuracy(s); !ok {
			t.Errorf("%q phải hợp lệ", s)
		}
	}
	for _, s := range []string{"", "-1", "1e100000", "1e-100000", "12.345", "123456789", " 12", "NaN", strings.Repeat("9", 5000)} {
		if _, ok := ParseAccuracy(s); ok {
			t.Errorf("%q phải bị từ chối", s)
		}
	}
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if es := ValidCheckin(Checkin{Lat: 10.3, Lon: 105.4, CapturedAt: now}, now); len(es) != 0 {
		t.Fatalf("check-in hợp lệ: %v", es)
	}
	if es := ValidCheckin(Checkin{Lat: 48, Lon: 2, CapturedAt: now.Add(time.Hour)}, now); len(es) != 2 {
		t.Fatalf("check-in sai: %v", es)
	}
}

func TestCodeNameChanged(t *testing.T) {
	if Code(1) != "HCN0000001" || Code(MaxCode) != "HCN9999999" {
		t.Fatal("mã hộ")
	}
	if !SameName(" nguyễn  văn AN", "Nguyễn Văn An") || SameName("Nguyễn Văn An", "Nguyễn Văn Ân") {
		t.Fatal("so họ tên")
	}
	if !SameHamlet("ấp 1", " Ấp  1") {
		t.Fatal("so ấp")
	}
	if !ValidPhoneDigits("0912000123") || ValidPhoneDigits("0912 000") || ValidPhoneDigits("+84912") {
		t.Fatal("SĐT chỉ chữ số")
	}
	a := Fields{CommuneCode: "1", Hamlet: "A", LivestockType: TypeHousehold, Animals: []Animal{{"GA", 1}}}
	b := a
	b.LivestockType, b.Animals = TypeFarmSmall, []Animal{{"GA", 2}}
	if got := strings.Join(Changed(a, b), ","); got != "animals,livestock_type" {
		t.Fatalf("changed: %s", got)
	}
}

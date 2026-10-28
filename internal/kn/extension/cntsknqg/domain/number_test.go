package domain

import (
	"runtime"
	"strings"
	"testing"
)

// Review #29 [P1]: ô số chỉ nhận dạng thập phân thường; dạng khoa học, dấu phẩy, khoảng trắng, quá nhiều chữ số đều 422
// với lý do của từng ô, và bị chặn TRƯỚC khi dựng decimal (không rescale theo số mũ).
func TestCellNumberSyntax(t *testing.T) {
	const syntax, tooFrac, notInt = "số không âm (dấu chấm thập phân)", "tối đa 4 số lẻ", "số nguyên"
	cases := []struct {
		kind, in, want, bad string
	}{
		{KindNumber, "120.50", "120.5", ""},
		{KindNumber, "  007.2500 ", "7.25", ""},
		{KindNumber, "+5", "5", ""},
		{KindNumber, "0", "0", ""},
		{KindNumber, "0.0000", "0", ""},
		{KindNumber, "999999999999999999.9999", "", "số quá dài"}, // 24 ký tự > MaxNumberLen
		{KindNumber, "999999999999999999.99", "999999999999999999.99", ""},
		{KindNumber, "1000000000000000000", "", syntax}, // 10^18: 19 chữ số phần nguyên
		{KindNumber, "0001000000000000000", "1000000000000000", ""},
		{KindNumber, "1.23456", "", tooFrac},
		{KindNumber, "1.50000", "", tooFrac},
		{KindInt, "12", "12", ""},
		{KindInt, "2.0", "2", ""},
		{KindInt, "2.5", "", notInt},
		{KindInt, "1e3", "", syntax},
		{KindInt, "+1E+3", "", syntax},
		{KindNumber, "1e-100000", "", syntax},
		{KindNumber, "1e100000", "", syntax},
		{KindNumber, "1E-2147483648", "", syntax},
		{KindNumber, "1.5e2", "", syntax},
		{KindNumber, "-1", "", syntax},
		{KindNumber, "-0", "", syntax},
		{KindNumber, "--1", "", syntax},
		{KindNumber, "+-1", "", syntax},
		{KindNumber, "++1", "", syntax},
		{KindNumber, "1,5", "", syntax}, // dấu phẩy thập phân kiểu VN: web và máy chủ cùng từ chối
		{KindNumber, "1.000,5", "", syntax},
		{KindNumber, "1 000", "", syntax},
		{KindNumber, "1. 5", "", syntax},
		{KindNumber, ".5", "", syntax},
		{KindNumber, "5.", "", syntax},
		{KindNumber, "1.2.3", "", syntax},
		{KindNumber, "0x10", "", syntax},
		{KindNumber, "Inf", "", syntax},
		{KindNumber, "NaN", "", syntax},
		{KindNumber, "１２", "", syntax}, // chữ số toàn khổ
		{KindNumber, "+", "", syntax},
		{KindNumber, strings.Repeat("9", 23), "", "số quá dài"},
	}
	for _, c := range cases {
		got, bad := cell(c.kind, nil, c.in)
		if bad != c.bad || (bad == "" && got != c.want) {
			t.Errorf("cell(%s, %q) = %q, %q; muốn %q, %q", c.kind, c.in, got, bad, c.want, c.bad)
		}
	}
}

// allocs đo số byte cấp phát của f (một luồng, sau GC).
func allocs(f func()) uint64 {
	runtime.GC()
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

func TestCellNumberRejectsExponentWithinBudget(t *testing.T) {
	for _, kind := range []string{KindNumber, KindInt} {
		for _, v := range []string{"1e-100000", "1e100000", "1E-999999999", "9e+2147483647", "+1e-2147483648"} {
			var bad string
			used := allocs(func() { _, bad = cell(kind, nil, v) })
			if bad == "" || used > 4<<10 {
				t.Fatalf("cell(%s, %q): lỗi %q, cấp phát %d byte (phải bị chặn trước khi dựng decimal)", kind, v, bad, used)
			}
		}
	}
}

func TestNormalizeAndWarningsRejectExponentWithinBudget(t *testing.T) {
	s, _ := Lookup(Form01)
	c := Content{Tables: map[string][]Row{"vat_tu": {{"ten": "Vật tư", "ke_hoach": "1e-100000", "da_cap": "1e100000"}}}}
	var es Errors
	used := allocs(func() { es = s.Normalize(&c) })
	if len(es) != 2 || used > 64<<10 {
		t.Fatalf("Normalize: %v, cấp phát %d byte", es, used)
	}
	for _, e := range es {
		if e.Reason != "số không âm (dấu chấm thập phân)" {
			t.Fatalf("lý do theo ô: %v", es)
		}
	}
	// Warnings đọc nội dung đã lưu; giá trị ngoài cú pháp (dữ liệu cũ/hỏng) bị bỏ qua, không dựng decimal.
	raw := Content{Tables: map[string][]Row{"vat_tu": {{"ten": "X", "ke_hoach": "1e-100000", "da_cap": "1e100000"}}}}
	var w []string
	used = allocs(func() { w = s.Warnings(raw) })
	if len(w) != 0 || used > 16<<10 {
		t.Fatalf("Warnings: %v, cấp phát %d byte", w, used)
	}
	ok := Content{Tables: map[string][]Row{"vat_tu": {{"ten": "X", "ke_hoach": "10", "da_cap": "10.5"}}}}
	if w := s.Warnings(ok); len(w) != 1 || !strings.Contains(w[0], "đã cấp 10.5 vượt kế hoạch 10") {
		t.Fatalf("Warnings hợp lệ: %v", w)
	}
}

func FuzzCellNumber(f *testing.F) {
	for _, s := range []string{"1", "1.5", "1e5", "+0.0001", "1,5", "-3"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, kind := range []string{KindNumber, KindInt} {
			got, bad := cell(kind, nil, s)
			if bad != "" || got == "" {
				continue
			}
			// Hợp lệ → dạng chuẩn ổn định và parsePlainDecimal đọc lại được.
			if again, bad2 := cell(kind, nil, got); bad2 != "" || again != got {
				t.Fatalf("%s %q → %q → %q %q", kind, s, got, again, bad2)
			}
			if _, ok := parsePlainDecimal(got); !ok {
				t.Fatalf("%s %q → %q không đọc lại được", kind, s, got)
			}
		}
	})
}

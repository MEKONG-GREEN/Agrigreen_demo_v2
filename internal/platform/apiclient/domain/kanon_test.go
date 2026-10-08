package domain

import (
	"fmt"
	"slices"
	"testing"
)

func units(prefix string, n int) map[string]struct{} {
	u := map[string]struct{}{}
	for i := range n {
		u[fmt.Sprintf("%s%d", prefix, i)] = struct{}{}
	}
	return u
}

func cell(key string, n int) KCell {
	return KCell{Key: key, Group: "89:2026-HT", WithheldKey: "89:2026-HT", Units: units(key+"/", n)}
}

var rule = KRule{K: 10, Hysteresis: 2}

// ADR-0035 §3: ô < k bị bỏ; dòng withheld = hợp các ô ẩn, phát khi ≥ k.
func TestApplyKPrimaryAndWithheld_ADR0035(t *testing.T) {
	r := ApplyK([]KCell{cell("89001:2026-HT", 12), cell("89002:2026-HT", 6), cell("89003:2026-HT", 5)}, rule, nil)
	if !slices.Equal(r.Published, []string{"89001:2026-HT"}) {
		t.Fatalf("chỉ ô ≥ k được phát: %v", r.Published)
	}
	if len(r.Withheld) != 1 || r.Withheld[0].Key != "89:2026-HT" || !slices.Equal(r.Withheld[0].Members, []string{"89002:2026-HT", "89003:2026-HT"}) {
		t.Fatalf("withheld gom hai ô ẩn (6 + 5 = 11 ≥ k): %+v", r.Withheld)
	}
	if r.Marks["89002:2026-HT"].Reason != SuppressBelowK || r.Marks["89001:2026-HT"].Suppressed || r.Suppressed != 2 {
		t.Fatalf("đánh dấu: %+v, suppressed %d", r.Marks, r.Suppressed)
	}
}

// Dòng withheld dưới k cũng bị ẩn; đơn vị đếm là HỢP (trùng nông hộ không đếm hai lần).
func TestApplyKWithheldBelowKUsesDistinctUnion(t *testing.T) {
	a, b := cell("89002:2026-HT", 6), cell("89003:2026-HT", 5)
	b.Units = units("89002:2026-HT/", 5) // cùng 5 nông hộ với a → hợp = 6 < k
	r := ApplyK([]KCell{cell("89001:2026-HT", 12), a, b}, rule, nil)
	if len(r.Withheld) != 0 || r.Marks["89:2026-HT"].Reason != SuppressWithheldBelowK || r.Suppressed != 3 {
		t.Fatalf("hợp 6 < k → withheld ẩn: %+v %+v", r.Withheld, r.Marks)
	}
}

// Ẩn ô bù: một ô ẩn duy nhất + withheld phát được → lộ ô đó; ẩn thêm ô phát nhỏ nhất của nhóm.
func TestApplyKComplementarySuppression(t *testing.T) {
	// Ô 89002 có 11 đơn vị nhưng đã ẩn ở phiên bản trước → cần ≥ 12 (trễ ẩn) → vẫn ẩn; withheld = 11 ≥ k nhưng
	// chỉ gom một ô → ẩn bù ô nhỏ nhất đang phát (89003, 14 < 20).
	prev := map[string]bool{"89002:2026-HT": true}
	r := ApplyK([]KCell{cell("89001:2026-HT", 20), cell("89002:2026-HT", 11), cell("89003:2026-HT", 14)}, rule, prev)
	if !slices.Equal(r.Published, []string{"89001:2026-HT"}) {
		t.Fatalf("ô bù phải bị ẩn: %v", r.Published)
	}
	if r.Marks["89003:2026-HT"].Reason != SuppressComplementary {
		t.Fatalf("lý do ẩn bù: %+v", r.Marks)
	}
	if len(r.Withheld) != 1 || len(r.Withheld[0].Members) != 2 {
		t.Fatalf("withheld phải gom ≥ 2 ô: %+v", r.Withheld)
	}
	// Không còn ô nào để ẩn bù → withheld cũng ẩn.
	r = ApplyK([]KCell{cell("89002:2026-HT", 11)}, rule, prev)
	if len(r.Withheld) != 0 || len(r.Published) != 0 || r.Marks["89:2026-HT"].Reason != SuppressComplementary {
		t.Fatalf("một ô ẩn, không ô bù → không phát gì: %+v", r)
	}
}

// Trễ ẩn: ô đã ẩn chỉ phát lại khi ≥ k + hysteresis; ô đang phát giữ ngưỡng k.
func TestApplyKHysteresis(t *testing.T) {
	c := cell("89001:2026-HT", 11)
	if r := ApplyK([]KCell{c}, rule, map[string]bool{"89001:2026-HT": true}); len(r.Published) != 0 {
		t.Fatalf("đã ẩn, 11 < k + 2 → vẫn ẩn: %v", r.Published)
	}
	c.Units = units("x", 12)
	if r := ApplyK([]KCell{c}, rule, map[string]bool{"89001:2026-HT": true}); !slices.Equal(r.Published, []string{"89001:2026-HT"}) {
		t.Fatalf("đã ẩn, 12 = k + 2 → phát lại: %v", r.Published)
	}
	c.Units = units("x", 10)
	if r := ApplyK([]KCell{c}, rule, map[string]bool{"89001:2026-HT": false}); !slices.Equal(r.Published, []string{"89001:2026-HT"}) {
		t.Fatalf("đang phát, 10 = k → vẫn phát: %v", r.Published)
	}
}

// Nhóm độc lập (tỉnh × vụ), kết quả tất định.
func TestApplyKGroupsDeterministic(t *testing.T) {
	cs := []KCell{cell("89001:2026-HT", 12),
		{Key: "92001:2026-HT", Group: "92:2026-HT", WithheldKey: "92:2026-HT", Units: units("q", 3)}}
	a, b := ApplyK(cs, rule, nil), ApplyK([]KCell{cs[1], cs[0]}, rule, nil)
	if !slices.Equal(a.Published, b.Published) || a.Suppressed != b.Suppressed || a.Suppressed != 2 {
		t.Fatalf("tất định theo thứ tự đầu vào: %+v / %+v", a, b)
	}
}

package domain

import "testing"

func cell(p, c, s, m string, hh, rolls int64) VolumeCell {
	return VolumeCell{ProvinceCode: p, CommuneCode: c, SeasonCode: s, PeriodMonth: m, Households: hh, Rolls: rolls}
}

func hidden(cells []VolumeCell) map[string]bool {
	out := map[string]bool{}
	for _, c := range cells {
		if c.Suppressed {
			out[c.Key()] = true
		}
	}
	return out
}

// G5 D-01 (C-3, publicdata.kanon.v1): ô < k nông hộ bị ẩn; ô ≥ k giữ khi không suy ra được.
func TestSuppressPrimary_G5_D01(t *testing.T) {
	cells := []VolumeCell{
		cell("89", "30001", "2026-HT", "2026-06", 10, 100), // đúng k → giữ
		cell("89", "30002", "2026-HT", "2026-06", 9, 90),   // < k → ẩn
		cell("89", "30003", "2026-HT", "2026-06", 0, 5),    // không nông hộ → ẩn
		cell("89", "30004", "2026-HT", "2026-06", 25, 300),
	}
	if n := Suppress(cells, 10); n != 2 {
		t.Fatalf("ẩn %d ô, muốn 2: %+v", n, cells)
	}
	h := hidden(cells)
	if !h["89|30002|2026-HT|2026-06"] || !h["89|30003|2026-HT|2026-06"] || h["89|30001|2026-HT|2026-06"] || h["89|30004|2026-HT|2026-06"] {
		t.Fatalf("ô bị ẩn: %v", h)
	}
}

// G5 D-01: một ô ẩn duy nhất trong hàng (tỉnh × vụ × tháng) → ẩn bổ sung ô công bố nhỏ nhất của hàng,
// để tổng tỉnh − các ô công bố không lộ ô ẩn.
func TestSuppressComplementaryRow_G5_D01(t *testing.T) {
	cells := []VolumeCell{
		cell("89", "30001", "2026-HT", "2026-06", 40, 400),
		cell("89", "30002", "2026-HT", "2026-06", 3, 30), // ẩn sơ cấp
		cell("89", "30003", "2026-HT", "2026-06", 12, 50),
		cell("89", "30004", "2026-HT", "2026-06", 12, 80),
	}
	if n := Suppress(cells, 10); n != 2 {
		t.Fatalf("ẩn %d ô, muốn 2: %v", n, hidden(cells))
	}
	// Nhỏ nhất: 12 nông hộ, 50 cuộn → 30003.
	if h := hidden(cells); !h["89|30003|2026-HT|2026-06"] {
		t.Fatalf("ô bổ sung phải là ô nhỏ nhất: %v", h)
	}
}

// G5 D-01: ẩn bổ sung lan sang chiều cột (xã × vụ — các tháng) tới điểm bất động; không ô nào còn là
// ô ẩn duy nhất của một nhóm có ô công bố.
func TestSuppressComplementaryFixedPoint_G5_D01(t *testing.T) {
	cells := []VolumeCell{
		// tháng 06: hai xã
		cell("89", "30001", "2026-HT", "2026-06", 4, 10), // ẩn sơ cấp
		cell("89", "30002", "2026-HT", "2026-06", 20, 200),
		// tháng 07: hai xã
		cell("89", "30001", "2026-HT", "2026-07", 15, 150),
		cell("89", "30002", "2026-HT", "2026-07", 30, 300),
	}
	Suppress(cells, 10)
	h := hidden(cells)
	// hàng 06 → ẩn 30002/06; cột 30001 (06 ẩn, 07 công bố) → ẩn 30001/07; cột 30002 (06 ẩn) → ẩn 30002/07.
	if len(h) != 4 {
		t.Fatalf("điểm bất động phải ẩn cả bốn ô: %v", h)
	}
	for _, grouping := range []func(VolumeCell) string{
		func(c VolumeCell) string { return c.ProvinceCode + c.SeasonCode + c.PeriodMonth },
		func(c VolumeCell) string { return c.CommuneCode + c.SeasonCode },
	} {
		count := map[string][2]int{}
		for _, c := range cells {
			v := count[grouping(c)]
			if c.Suppressed {
				v[0]++
			} else {
				v[1]++
			}
			count[grouping(c)] = v
		}
		for g, v := range count {
			if v[0] == 1 && v[1] > 0 {
				t.Fatalf("nhóm %s còn đúng một ô ẩn: %v", g, v)
			}
		}
	}
}

// Không có ô dưới k → không ẩn gì (không ẩn bổ sung vô cớ).
func TestSuppressNothing_G5_D01(t *testing.T) {
	cells := []VolumeCell{cell("89", "30001", "2026-HT", "2026-06", 10, 1), cell("89", "30002", "2026-HT", "2026-06", 11, 1)}
	if n := Suppress(cells, 10); n != 0 {
		t.Fatalf("không được ẩn: %v", hidden(cells))
	}
}

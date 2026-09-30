package domain

import "testing"

func intp(n int) *int { return &n }

// RB-6 chiều mua (G3-2): quy tắc phản hồi của HTX trùng với mkg_fieldops.htx_confirmation.
func TestCheckResponse_RB6_BuySide(t *testing.T) {
	cases := []struct {
		name        string
		outcome     string
		htx         *int
		reason      string
		superseding bool
		ok          bool
		field       string
	}{
		{"xác nhận", "confirmed", nil, "", false, true, ""},
		{"xác nhận kèm số bằng", "confirmed", intp(40), "", false, true, ""},
		{"xác nhận số khác", "confirmed", intp(39), "", false, false, "htx_rolls"},
		{"tranh chấp đủ", "disputed", intp(38), "Đếm lại thiếu 2 cuộn", false, true, ""},
		{"tranh chấp thiếu số", "disputed", nil, "Đếm lại thiếu 2 cuộn", false, false, "htx_rolls"},
		{"tranh chấp số bằng", "disputed", intp(40), "Đếm lại thiếu 2 cuộn", false, false, "htx_rolls"},
		{"tranh chấp thiếu lý do", "disputed", intp(38), "", false, false, "reason"},
		{"lý do ngắn", "disputed", intp(38), "ngắn", false, false, "reason"},
		{"lý do có SĐT", "disputed", intp(38), "gọi 0912345678 nhé", false, false, "reason"},
		{"thay phản hồi thiếu lý do", "confirmed", nil, "", true, false, "reason"},
		{"thay phản hồi có lý do", "confirmed", nil, "Đếm lại, số MKG đúng", true, true, ""},
		{"kết quả lạ", "maybe", nil, "", false, false, "outcome"},
	}
	for _, c := range cases {
		r, err := CheckResponse(c.outcome, 40, c.htx, c.reason, c.superseding)
		if c.ok != (err == nil) {
			t.Fatalf("%s: err = %v", c.name, err)
		}
		if err != nil {
			if fe, _ := AsFieldError(err); fe.Field != c.field {
				t.Fatalf("%s: trường %q, muốn %q", c.name, fe.Field, c.field)
			}
			continue
		}
		if c.outcome == "confirmed" && r.HTXRolls != 40 {
			t.Fatalf("%s: htx_rolls = %d", c.name, r.HTXRolls)
		}
	}
}

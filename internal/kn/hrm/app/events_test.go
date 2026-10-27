package app

import "testing"

// NOTIFY-HCTH-KN (#50): job hằng ngày chỉ phát sự kiện khi có việc cần Phòng HCTH xử lý VÀ số đếm khác lần trước.
func TestPromotionDueNotice_NotifyHCTH(t *testing.T) {
	months := []int{1, 3, 6}
	cur := map[string]int{"due_1": 0, "due_3": 2, "due_6": 4, "fail": 1, "missing": 0, "active_vien_chuc": 5}
	p, ok := PromotionDueNotice(months, cur, nil, false)
	if !ok || len(p.Windows) != 3 || p.Windows[1].Months != 3 || p.Windows[1].Count != 2 || p.Fail != 1 {
		t.Fatalf("lần đầu: %+v %v", p, ok)
	}
	// Cùng số đếm (kể cả khác active_vien_chuc — không thuộc nội dung thông báo) → không phát.
	prev := map[string]int{"due_1": 0, "due_3": 2, "due_6": 4, "fail": 1, "active_vien_chuc": 6}
	if _, ok := PromotionDueNotice(months, cur, prev, true); ok {
		t.Fatal("số đếm không đổi vẫn phát")
	}
	// Số đếm đổi → phát.
	prev["due_6"] = 3
	if _, ok := PromotionDueNotice(months, cur, prev, true); !ok {
		t.Fatal("số đếm đổi không phát")
	}
	// Không có việc → không phát.
	if _, ok := PromotionDueNotice(months, map[string]int{"due_1": 0, "due_3": 0, "due_6": 0, "active_vien_chuc": 5}, nil, false); ok {
		t.Fatal("không việc vẫn phát")
	}
	// Chỉ quá hạn/thiếu mốc cũng là việc cần xử lý.
	if p, ok := PromotionDueNotice(months, map[string]int{"overdue": 1}, nil, false); !ok || p.Overdue != 1 || len(p.Windows) != 3 {
		t.Fatalf("quá hạn: %+v %v", p, ok)
	}
}

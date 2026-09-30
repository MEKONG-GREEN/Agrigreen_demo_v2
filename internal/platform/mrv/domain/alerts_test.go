package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func vals(t *testing.T, m map[string]any) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	for k, v := range m {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out[k] = b
	}
	return out
}

// KN US-MRV-04: vượt ngưỡng nguy hiểm cấu hình trước → tự bật "Cảnh báo khẩn".
func TestPestUrgentRuleKNUSMRV04(t *testing.T) {
	cats := SplitList(" ray_nau, dao_on,,lu_lut,ray_nau ")
	if len(cats) != 3 {
		t.Fatalf("SplitList: %v", cats)
	}
	five := decimal.NewFromInt(5)
	cases := []struct {
		name string
		v    map[string]any
		want bool
	}{
		{"cấp 4 loại trong danh sách", map[string]any{"category": "ray_nau", "severity_level": 4, "affected_area_ha": "0.5"}, true},
		{"cấp 3 nhưng diện tích ≥ ngưỡng", map[string]any{"category": "lu_lut", "severity_level": 3, "affected_area_ha": "5"}, true},
		{"cấp 3, diện tích dưới ngưỡng", map[string]any{"category": "dao_on", "severity_level": 3, "affected_area_ha": "4.99"}, false},
		{"cấp 5 nhưng loại không kích hoạt", map[string]any{"category": "khac", "severity_level": 5, "affected_area_ha": "50"}, false},
		{"thiếu cấp và diện tích", map[string]any{"category": "ray_nau"}, false},
		{"thiếu loại", map[string]any{"severity_level": 5}, false},
	}
	for _, c := range cases {
		got, d := PestUrgent(vals(t, c.v), cats, 4, five)
		if got != c.want {
			t.Errorf("%s: khẩn = %v, mong %v (%+v)", c.name, got, c.want, d)
		}
	}
	// Tham số khác phiên bản → kết quả khác (quy tắc đọc ngưỡng, không cứng trong mã).
	if got, _ := PestUrgent(vals(t, map[string]any{"category": "dao_on", "severity_level": 3}), cats, 3, five); !got {
		t.Fatal("min_severity = 3 thì cấp 3 phải khẩn")
	}
	if got, _ := PestUrgent(vals(t, map[string]any{"category": "dao_on", "severity_level": 5}), []string{"ray_nau"}, 4, five); got {
		t.Fatal("loại ngoài danh sách phiên bản mới không được khẩn")
	}
}

// KN FR-08: ghi AWD nhưng vùng chưa đủ hạ tầng → cờ.
func TestAWDNeedsInfrastructureKNFR08(t *testing.T) {
	awd := []string{"tuoi_nuoc"}
	if !AWDNeedsInfrastructure("tuoi_nuoc", true, awd, false) {
		t.Fatal("AWD không có hạ tầng phải bị cờ")
	}
	if AWDNeedsInfrastructure("tuoi_nuoc", true, awd, true) {
		t.Fatal("có hạ tầng đạt: không cờ")
	}
	if AWDNeedsInfrastructure("bon_phan", true, awd, false) {
		t.Fatal("hoạt động không phải AWD: không cờ")
	}
	if AWDNeedsInfrastructure("tuoi_nuoc", false, awd, false) {
		t.Fatal("bản nhật ký không thành hiện hành: không cờ")
	}
}

// KN US-MRV-06: quá hạn xử lý → mức 1 (người duyệt), quá hạn leo thang → mức 2 (tỉnh).
func TestOverdueLevelKNUSMRV06(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		age       time.Duration
		sla, esc  int
		wantLevel int
	}{
		{71 * time.Hour, 72, 120, 0},
		{72 * time.Hour, 72, 120, 1},
		{119 * time.Hour, 72, 120, 1},
		{120 * time.Hour, 72, 120, 2},
		{80 * time.Hour, 72, 24, 2}, // hạn leo thang < SLA → coi bằng SLA
		{999 * time.Hour, 0, 120, 0},
	} {
		if got := OverdueLevel(t0, t0.Add(c.age), c.sla, c.esc); got != c.wantLevel {
			t.Errorf("tuổi %v, SLA %d, leo thang %d: mức %d, mong %d", c.age, c.sla, c.esc, got, c.wantLevel)
		}
	}
}

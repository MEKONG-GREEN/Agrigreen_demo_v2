package extension

import (
	"encoding/json"
	"testing"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// KN-PARAMS-CNTS-DIARY-OWNER (#85): đăng ký RIÊNG hai tham số nhật ký CNTS (W11.48) — không đổi đăng ký KN cũ
// (MasterdataParams, golden 16 spec/13 seed), không trùng khóa, seed đúng mặc định đang chạy, trả bản sao độc lập.
func TestCNTSDiaryMasterdataParams(t *testing.T) {
	reg := CNTSDiaryMasterdataParams()
	specs := map[string]mdport.ParamSpec{}
	for _, sp := range reg.Specs {
		specs[sp.Key] = sp
	}
	if len(reg.Specs) != 2 || len(specs) != 2 {
		t.Fatalf("muốn đúng 2 spec: %+v", reg.Specs)
	}
	for key, want := range map[string][2]string{
		extport.ParamCNTSDiaryEditWindowDays:   {"0", "30"},
		extport.ParamCNTSDiaryGPSRetentionDays: {"1", "3650"},
	} {
		sp, ok := specs[key]
		if !ok || sp.Kind != mdport.ParamKindDecimal || sp.Unit != "ngày" || sp.Dimensional || sp.Name == "" ||
			sp.Min != want[0] || sp.Max != want[1] {
			t.Fatalf("spec %s: %+v", key, sp)
		}
	}
	seeds := map[string]mdport.ParamSeed{}
	for _, s := range reg.Seeds {
		seeds[s.Key] = s
	}
	for key, v := range map[string]string{extport.ParamCNTSDiaryEditWindowDays: "7", extport.ParamCNTSDiaryGPSRetentionDays: "365"} {
		s, ok := seeds[key]
		if !ok || s.Value == nil || *s.Value != v || s.Text != nil || s.Unit != "ngày" || s.EffectiveFrom != "2026-01-01" || s.SourceDoc == "" {
			t.Fatalf("seed %s: %+v", key, s)
		}
	}
	if len(reg.Seeds) != 2 {
		t.Fatalf("muốn đúng 2 seed: %+v", reg.Seeds)
	}

	legacy := MasterdataParams()
	for _, sp := range legacy.Specs {
		if _, dup := specs[sp.Key]; dup {
			t.Fatalf("trùng khóa với đăng ký KN cũ: %s", sp.Key)
		}
	}
	if len(legacy.Specs) != 16 || len(legacy.Seeds) != 13 {
		t.Fatalf("đăng ký KN cũ phải giữ 16 spec/13 seed: %d/%d", len(legacy.Specs), len(legacy.Seeds))
	}

	want, _ := json.Marshal(CNTSDiaryMasterdataParams())
	reg.Specs[0].Name, reg.Specs[0].Max = "corrupt", "1"
	*reg.Seeds[0].Value = "999"
	reg.Seeds[1].SourceDoc = "corrupt"
	if got, _ := json.Marshal(CNTSDiaryMasterdataParams()); string(got) != string(want) {
		t.Fatal("đăng ký chia sẻ trạng thái có thể sửa")
	}
}

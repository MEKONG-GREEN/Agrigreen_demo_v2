package app

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/store"
	obdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/domain"
)

// FN-18 BR-02, FN-11 BR-03: số hộ, diện tích bị ảnh hưởng của vùng = tổng phiên bản mới nhất của từng ổ dịch; ổ dịch cũ
// một hộ (v1, DB-15 trống) tính 1 hộ; DB-16 trống tính 0.
func TestImpactOf(t *testing.T) {
	n := func(v int) *int { return &v }
	d := func(s string) *decimal.Decimal { x := decimal.RequireFromString(s); return &x }
	obs := []OutbreakView{
		{Outbreak: store.Outbreak{RuleVersion: obdomain.RuleVersion, AffectedHouseholds: n(3), AquaFarmedHa: d("2.00"), AquaWildHa: d("0.50")}},
		{Outbreak: store.Outbreak{RuleVersion: obdomain.RuleVersionV1}},
		{Outbreak: store.Outbreak{RuleVersion: obdomain.RuleVersion, AffectedHouseholds: n(4), AquaFarmedHa: d("0.75")}},
	}
	im := impactOf(obs)
	if im.Outbreaks != 3 || im.Households != 8 || im.AquaFarmedHa.StringFixed(2) != "2.75" || im.AquaWildHa.StringFixed(2) != "0.50" {
		t.Fatalf("impact: %+v", im)
	}
	if !obs[1].Legacy() || obs[0].Legacy() {
		t.Fatal("cờ dữ liệu cũ theo rule_version")
	}
	if z := impactOf(nil); z.Outbreaks != 0 || z.Households != 0 || !z.AquaFarmedHa.IsZero() {
		t.Fatalf("vùng không có ổ dịch: %+v", z)
	}
}

package handler_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// G5 (collection_volume): PlotFacts trả xã/HTX/nông hộ/diện tích của nhiều thửa theo tenant trong
// context; thửa lạ và tenant khác không có trong kết quả.
func TestPlotFacts_G5(t *testing.T) {
	f := newFixture(t)
	p1, _ := f.national().createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	pf := f.mod.PlotFacts()
	got, err := pf.PlotFacts(tenant.WithID(context.Background(), f.tenant), []uuid.UUID{p1, ids.New()})
	if err != nil || len(got) != 1 {
		t.Fatalf("PlotFacts: %+v %v", got, err)
	}
	if p := got[p1]; p.CommuneCode != "31117" || p.CooperativeRef != f.coopA || !p.AreaHa.IsPositive() {
		t.Fatalf("dữ kiện thửa: %+v", p)
	}
	if other, err := pf.PlotFacts(tenant.WithID(context.Background(), ids.New()), []uuid.UUID{p1}); err != nil || len(other) != 0 {
		t.Fatalf("tenant khác: %+v %v", other, err)
	}
	if _, err := pf.PlotFacts(context.Background(), []uuid.UUID{p1}); err == nil {
		t.Fatal("thiếu tenant trong context phải lỗi")
	}
}

package app

import (
	"context"
	"testing"
	"time"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

type catalogStub struct{ items []mdport.CatalogItem }

func (c catalogStub) Get(context.Context, string, string) (mdport.CatalogItem, error) {
	return mdport.CatalogItem{}, mdport.ErrCatalogItemNotFound
}

func (c catalogStub) List(context.Context, string) ([]mdport.CatalogItem, error) { return c.items, nil }

// GIS US-WATER-01 (rà E2E G4): danh mục vessel_class của pf/masterdata đặt tải trọng ở thuộc tính capacity_t
// (lược đồ domain.catalogSchemas + seeddata/catalogs.json) — dựng mạng phải đọc được, không 422 params_invalid.
func TestVesselClassesReadMasterdataCapacityT(t *testing.T) {
	s := &Service{Catalogs: catalogStub{items: []mdport.CatalogItem{
		{Catalog: "vessel_class", Code: "ghe_100", Active: true, Attrs: map[string]string{"capacity_t": "100", "beam_m": "5.5", "draft_m": "1.4",
			"air_draft_m": "3.5", "min_width_m": "8.25", "min_depth_m": "1.8", "min_clearance_m": "4"}},
		{Catalog: "vessel_class", Code: "cu", Active: true, Attrs: map[string]string{"tons": "50", "beam_m": "4", "draft_m": "1", "air_draft_m": "3"}},
		{Catalog: "vessel_class", Code: "ngung", Active: false, Attrs: map[string]string{}},
	}}}
	vs, err := s.vessels(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("danh mục chuẩn của masterdata phải đọc được: %v", err)
	}
	if len(vs) != 2 || vs[0].Code != "ghe_100" || vs[0].Tons.String() != "100" || vs[1].Tons.String() != "50" {
		t.Fatalf("lớp phương tiện: %+v", vs)
	}
	bad := &Service{Catalogs: catalogStub{items: []mdport.CatalogItem{{Catalog: "vessel_class", Code: "x", Active: true,
		Attrs: map[string]string{"beam_m": "4", "draft_m": "1", "air_draft_m": "3"}}}}}
	if _, err := bad.vessels(context.Background(), time.Now()); err == nil {
		t.Fatal("thiếu tải trọng phải lỗi params_invalid")
	}
}

package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/simtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

var examplePath = filepath.Join("..", "..", "..", "..", "contracts", "optimizer", "examples", "snapshot.example.json")

func exampleSnapshot(t *testing.T) []byte {
	t.Helper()
	at := time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)
	u := func(s string) uuid.UUID { return uuid.MustParse(s) }
	supply := &simtest.Supply{Data: port.SupplyData{
		Cooperatives: []port.CooperativeSupply{
			simtest.Coop(u("0192a000-0000-7000-8000-00000000c001"), 10.42, 105.47, 120, "9000", "8000", "5000"),
			simtest.Coop(u("0192a000-0000-7000-8000-00000000c002"), 10.30, 105.40, 80, "6000", "5500", "3000"),
			simtest.Coop(u("0192a000-0000-7000-8000-00000000c003"), 10.47, 105.66, 95, "7000", "6500", "0"),
		},
		Seasons:      simtest.Seasons,
		Observations: map[string][]port.HarvestObservation{"DX": {{DayOfYear: 62, Tons: decimal.NewFromInt(4000)}, {DayOfYear: 69, Tons: decimal.NewFromInt(9000)}}},
	}}
	barge := decimal.NewFromInt(2000)
	sc := domain.Scenario{ID: u("0192a000-0000-7000-8000-0000000000a1"), Code: "KB-0001", PlantLocation: port.LatLng{Lat: 10.9, Lng: 106.7},
		LifecycleYears: 10, BaselineMode: domain.BaselineNoHub, ReferenceYear: 2026,
		Hubs: []domain.ScenarioHub{
			{ID: u("0192a000-0000-7000-8000-0000000000b1"), HubID: u("0192a000-0000-7000-8000-0000000000d1"), HubCode: "HUB-0001",
				HubLocation: port.LatLng{Lat: 10.38, Lng: 105.435}, ServiceRadiusKm: decimal.NewFromInt(25), DesignCapacityTons: decimal.NewFromInt(30000),
				BargePayloadTons: &barge, ModeFieldHub: "auto", ModeHubPlant: "waterway", LandMode: "thue", SortOrder: 1},
			{ID: u("0192a000-0000-7000-8000-0000000000b2"), HubID: u("0192a000-0000-7000-8000-0000000000d2"), HubCode: "HUB-0002",
				HubLocation: port.LatLng{Lat: 10.46, Lng: 105.64}, ServiceRadiusKm: decimal.NewFromInt(20), DesignCapacityTons: decimal.NewFromInt(20000),
				ModeFieldHub: "auto", ModeHubPlant: "auto", LandMode: "mua", SortOrder: 2},
		}}
	svc := &app.Service{Clock: clock.NewFixed(at), Params: &simtest.Params{ApproveAll: true}, Supply: supply, Router: &simtest.Router{Version: "ww-example-1", WaterwayFound: true}}
	snap, err := svc.BuildSnapshot(context.Background(), app.BuildRequest{
		TenantID:   u("0192a000-0000-7000-8000-0000000000e1"),
		Simulation: domain.Simulation{ID: u("0192a000-0000-7000-8000-0000000000f1"), RequestedAt: at, Options: domain.SnapOptions{Mode: "evaluate", SensitivityCodes: []string{"loss_ratio", "road_freight_rate"}}},
		Scenario:   sc, SnapshotID: u("0192a000-0000-7000-8000-0000000000f2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := snap.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Hợp đồng liên ngôn ngữ: ảnh chụp mẫu do Go dựng nằm ở contracts/optimizer/examples và được
// optimizer/tests/test_contract.py kiểm bằng JSON Schema rồi tính thật. Đổi cấu trúc ảnh chụp
// → chạy lại với AG_UPDATE_EXAMPLES=1 và chạy pytest.
func TestExampleSnapshotIsUpToDate(t *testing.T) {
	raw := exampleSnapshot(t)
	if os.Getenv("AG_UPDATE_EXAMPLES") == "1" {
		if err := os.MkdirAll(filepath.Dir(examplePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(examplePath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("thiếu %s — chạy với AG_UPDATE_EXAMPLES=1: %v", examplePath, err)
	}
	if !bytes.Equal(bytes.TrimSpace(want), raw) {
		t.Fatal("contracts/optimizer/examples/snapshot.example.json lệch với ảnh chụp Go dựng — chạy lại với AG_UPDATE_EXAMPLES=1")
	}
}

// Dựng ảnh chụp không có cổng nguồn cung → lỗi phụ thuộc (việc hỏng, không treo).
func TestBuildSnapshotWithoutSupplySource(t *testing.T) {
	svc := &app.Service{Clock: clock.NewFixed(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}
	_, err := svc.BuildSnapshot(context.Background(), app.BuildRequest{})
	if err == nil {
		t.Fatal("phải báo thiếu SupplySource")
	}
}

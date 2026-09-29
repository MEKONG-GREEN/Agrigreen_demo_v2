// Package simtest cung cấp cổng giả (ParamSet, SupplySource, Router) và phong bì
// kết quả giả cho kiểm thử pf/simulation — không bao giờ import từ mã chạy thật.
// Go KHÔNG có engine: kiểm thử nộp kết quả giả theo hợp đồng contracts/optimizer.
package simtest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
)

// Params là ParamSet giả: ApproveAll → mọi khóa của danh mục có phiên bản phê duyệt (= giá trị danh mục).
type Params struct {
	mu         sync.Mutex
	ApproveAll bool
	Values     map[string]decimal.Decimal // khóa → giá trị đã phê duyệt (ghi đè)
}

// Approved hiện thực port.ParamSet.
func (p *Params) Approved(_ context.Context, _ uuid.UUID, key string, _ time.Time) (port.ApprovedParam, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := p.Values[key]; ok {
		return port.ApprovedParam{Value: v, Version: 3}, true, nil
	}
	if !p.ApproveAll {
		return port.ApprovedParam{}, false, nil
	}
	for _, d := range domain.Catalog {
		if domain.ParamKey(d.Code, "") == key && d.Base != "" {
			return port.ApprovedParam{Value: decimal.RequireFromString(d.Base), Version: 1}, true, nil
		}
		for _, dim := range d.Dims {
			if domain.ParamKey(d.Code, dim.Key) == key {
				return port.ApprovedParam{Value: decimal.RequireFromString(dim.Base), Version: 1}, true, nil
			}
		}
	}
	return port.ApprovedParam{}, false, nil
}

// SetApproveAll đổi chế độ.
func (p *Params) SetApproveAll(v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ApproveAll = v
}

// Supply là SupplySource giả với dữ liệu cố định.
type Supply struct {
	Data  port.SupplyData
	Calls int
	mu    sync.Mutex
}

// Supply hiện thực port.SupplySource.
func (s *Supply) Supply(_ context.Context, _ uuid.UUID, _ port.SupplyQuery) (port.SupplyData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls++
	return s.Data, nil
}

// Coop tạo một HTX giả với sản lượng ba vụ.
func Coop(ref uuid.UUID, lat, lng float64, members int, paddy ...string) port.CooperativeSupply {
	c := port.CooperativeSupply{CooperativeRef: ref, MemberCount: members}
	if lat != 0 || lng != 0 {
		c.Location = &port.LatLng{Lat: lat, Lng: lng}
	}
	for i, p := range paddy {
		c.Seasons = append(c.Seasons, port.CoopSeason{SeasonKey: []string{"DX-2026", "HT-2026", "TD-2026"}[i],
			PaddyTons: decimal.RequireFromString(p), PlantedAreaHa: decimal.RequireFromString(p).Div(decimal.NewFromInt(6)).Round(1)})
	}
	return c
}

// Seasons là lịch mùa vụ mẫu.
var Seasons = []port.SeasonDef{
	{Key: "DX-2026", Type: "DX", EndMonth: 3, SortOrder: 1},
	{Key: "HT-2026", Type: "HT", EndMonth: 8, SortOrder: 2},
	{Key: "TD-2026", Type: "TD", EndMonth: 12, SortOrder: 3},
}

// Router là Router giả: đường thủy "tìm thấy" = 1,4 × chim bay khi WaterwayFound.
type Router struct {
	Version       string
	WaterwayFound bool
	Calls         int
}

// NetworkVersion hiện thực port.Router.
func (r *Router) NetworkVersion(context.Context, uuid.UUID) (string, error) { return r.Version, nil }

// Route hiện thực port.Router.
func (r *Router) Route(_ context.Context, _ uuid.UUID, _ string, reqs []port.RouteRequest) ([]port.RouteResult, error) {
	r.Calls++
	out := make([]port.RouteResult, len(reqs))
	for i, q := range reqs {
		if q.Mode == port.ModeWaterway && r.WaterwayFound {
			km := decimal.NewFromFloat(domain.HaversineKm(q.From, q.To) * 1.4).Round(3)
			out[i] = port.RouteResult{Found: true, DistanceKm: km, Source: port.SourceDigitized}
		}
	}
	return out, nil
}

// ResultEnvelope dựng phong bì kết quả giả đúng hợp đồng result.v1 (phần Go đọc).
// tcoPerTon điều khiển thứ hạng so sánh; lifecycle/discounted để thử FN-14 BR-02.
func ResultEnvelope(simID, snapshotSHA string, tcoPerTon int64, lifecycle int, discounted bool) []byte {
	result := map[string]any{
		"engine_version": "2026.09.24", "scenario_id": uuid.Nil.String(), "scenario_code": "KB-0001", "status": "tham_khao",
		"parameter_set_version": "ps-1", "plant": map[string]any{"plant_ref": nil, "annual_demand_tons": 240000},
		"hubs": []any{map[string]any{"capacity_warning": map[string]any{"shortfall_pct": 40.5, "message": "dư"}}}, "hub_count": 1,
		"seasons": []any{}, "total_available_tons": 50000, "collectable_tons": 22500, "delivered_tons": 21375.5,
		"costs": map[string]any{"collection_cost": 5062500000, "warehouse_cost": 1800000000, "handling_cost": 2500000000,
			"transportation_cost": 3000000000, "transportation_field_hub": 1000000000, "transportation_hub_plant": 2000000000,
			"total_delivered_logistics_cost": 12362500000, "cost_per_ton": 578361, "cost_per_ton_note": "chưa gồm khấu hao đầu tư"},
		"capex": map[string]any{"total": 30000000000}, "opex": map[string]any{"total": 4300000000},
		"financial": map[string]any{"lifecycle_years": lifecycle, "discounted": discounted, "tco": 73000000000, "tco_per_ton": tcoPerTon,
			"lifecycle_cost_per_ton": tcoPerTon + 500000, "roi_pct": nil, "payback_years": nil, "payback_status": "khong_hoan_von"},
		"baseline": map[string]any{"available": true, "delivered_tons": 21375.5, "collection_cost": 5062500000, "transportation_cost": 9000000000,
			"total_delivered_logistics_cost": 14062500000, "baseline_cost_per_ton": 657895},
		"plant_demand_coverage_pct": 8.91, "unapproved_parameters": []any{}, "can_be_marked_official": true, "warnings": []any{}, "notes": []any{},
	}
	env := map[string]any{
		"schema_version": 1, "simulation_id": simID, "snapshot_id": nil, "snapshot_sha256": snapshotSHA,
		"algorithm_version": domain.AlgorithmVersion, "engine_version": "2026.09.24", "mode": "evaluate", "status": "completed", "error": nil,
		"result": result, "sensitivity": map[string]any{"high_sensitivity": true, "swing_pct": 23.456, "rows": []any{}}, "selection": nil,
	}
	b, err := json.Marshal(env)
	if err != nil {
		panic(fmt.Sprint(err))
	}
	return b
}

// FailedEnvelope dựng phong bì lỗi của optimizer.
func FailedEnvelope(simID, snapshotSHA string) []byte {
	b, _ := json.Marshal(map[string]any{
		"schema_version": 1, "simulation_id": simID, "snapshot_id": nil, "snapshot_sha256": snapshotSHA, "algorithm_version": domain.AlgorithmVersion,
		"engine_version": "2026.09.24", "mode": "evaluate", "status": "failed",
		"error": map[string]any{"code": "optimizer.snapshot_incomplete", "message": "thiếu tuyến"}, "result": nil, "sensitivity": nil, "selection": nil,
	})
	return b
}

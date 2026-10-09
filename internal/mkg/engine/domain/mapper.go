package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/port"
)

// MapperVersion ghi vào mỗi quan sát (tái lập: đổi quy tắc ánh xạ → tăng phiên bản).
const MapperVersion = "engine-obs.v1"

// Mã căn cứ độ bất định (uncertainty_basis).
const (
	BasisQuantization        = "quantization"          // làm tròn của nguồn: nửa đơn vị chữ số lẻ cuối
	BasisKSuppressedResidual = "k_suppressed_residual" // dòng province_withheld: phần dư của các xã bị ẩn dưới k (ADR-0035)
	BasisClamped             = "clamped"               // giá trị dẫn xuất âm được kẹp về 0, cận nới tương ứng
	BasisMissingValue        = "missing_value"         // hợp đồng cho phép vắng giá trị
	BasisLocationMissing     = "location_missing"      // HTX chưa có tọa độ
	BasisPendingC9           = "pending_c9"            // kế thừa đơn vị hành chính chưa có nguồn chính thức (C-9)
	BasisForecast            = "forecast"              // ngày gặt dự kiến, vụ chưa xong
	BasisGrantRevoked        = "grant_revoked"         // rút do bên cấp thu hồi cấp quyền (403 party.share_grant_missing)
)

// Draft là quan sát dẫn xuất từ MỘT mục feed, chưa gắn nguồn gốc / thời điểm.
type Draft struct {
	Kind      string
	Retracted bool
	Value     *Measured // nil → quan sát không có giá trị số
	Unit      string
	Basis     []string
}

type mapFn func(rec json.RawMessage) ([]Draft, error)

type feedMapper struct {
	kinds []string // kind feed sinh ra (retract → một quan sát rút cho mỗi kind)
	fn    mapFn
}

// mappers: feed của cổng tích hợp (contracts/integration/feeds) mà MKG được đọc → kind quan sát.
// collection_volume_by_commune không có: MKG không là bên nhận của hợp đồng đó.
var mappers = map[string]feedMapper{
	"cooperative_directory":       {kinds: []string{port.KindCooperative}, fn: mapCooperative},
	"plot_directory":              {kinds: []string{port.KindPlot}, fn: mapPlot},
	"harvest_progress_by_commune": {kinds: []string{port.KindHarvestShare, port.KindHarvestAreaRemaining}, fn: mapHarvestProgress},
	"admin_units":                 {kinds: []string{port.KindAdminUnit}, fn: mapAdminUnit},
	"season_harvest_dates":        {kinds: []string{port.KindSeasonHarvestWindow}, fn: mapSeasonHarvest},
	"waterway_routes":             {kinds: []string{port.KindWaterwayNetwork}, fn: mapWaterway},
}

// Feeds trả tên mọi feed có bộ ánh xạ (đã sắp).
func Feeds() []string {
	out := make([]string, 0, len(mappers))
	for f := range mappers {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

// Kinds trả mọi kind quan sát (đã sắp).
func Kinds() []string {
	var out []string
	for _, m := range mappers {
		out = append(out, m.kinds...)
	}
	slices.Sort(out)
	return out
}

// FeedKinds trả kind mà feed sinh ra (nil nếu feed không có bộ ánh xạ).
func FeedKinds(feed string) []string {
	m, ok := mappers[feed]
	if !ok {
		return nil
	}
	return slices.Clone(m.kinds)
}

// Map ánh xạ một mục feed (op upsert | retract) thành quan sát. Lỗi → không áp trang (bản ghi lệch hợp đồng).
func Map(feed, op string, rec json.RawMessage) ([]Draft, error) {
	m, ok := mappers[feed]
	if !ok {
		return nil, fmt.Errorf("feed %q không có bộ ánh xạ quan sát", feed)
	}
	switch op {
	case "retract":
		return Retractions(feed, nil), nil
	case "upsert":
		if len(bytes.TrimSpace(rec)) == 0 || string(bytes.TrimSpace(rec)) == "null" {
			return nil, fmt.Errorf("%s: upsert thiếu record", feed)
		}
		ds, err := m.fn(rec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", feed, err)
		}
		for i := range ds {
			slices.Sort(ds[i].Basis)
			ds[i].Basis = slices.Compact(ds[i].Basis)
		}
		return ds, nil
	}
	return nil, fmt.Errorf("%s: op %q lạ", feed, op)
}

// Retractions trả một quan sát rút cho mỗi kind của feed, với căn cứ basis (vd BasisGrantRevoked).
func Retractions(feed string, basis []string) []Draft {
	var out []Draft
	for _, k := range mappers[feed].kinds {
		out = append(out, Draft{Kind: k, Retracted: true, Basis: slices.Clone(basis)})
	}
	return out
}

func decodeRecord(rec json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(rec))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("bản ghi không đọc được: %w", err)
	}
	return nil
}

func valued(kind, unit string, m Measured, basis ...string) Draft {
	return Draft{Kind: kind, Value: &m, Unit: unit, Basis: append([]string{BasisQuantization}, basis...)}
}

func mapCooperative(rec json.RawMessage) ([]Draft, error) {
	var r struct {
		Lon *string `json:"lon"`
		Lat *string `json:"lat"`
	}
	if err := decodeRecord(rec, &r); err != nil {
		return nil, err
	}
	d := Draft{Kind: port.KindCooperative}
	if r.Lon == nil || r.Lat == nil {
		d.Basis = []string{BasisLocationMissing}
	}
	return []Draft{d}, nil
}

func mapPlot(rec json.RawMessage) ([]Draft, error) {
	var r struct {
		AreaHa *string `json:"area_ha"`
	}
	if err := decodeRecord(rec, &r); err != nil {
		return nil, err
	}
	if r.AreaHa == nil {
		return []Draft{{Kind: port.KindPlot, Basis: []string{BasisMissingValue}}}, nil
	}
	a, err := Quantized(*r.AreaHa)
	if err != nil {
		return nil, fmt.Errorf("area_ha: %w", err)
	}
	if a.Value.Sign() < 0 {
		return nil, fmt.Errorf("area_ha âm")
	}
	return []Draft{valued(port.KindPlot, port.UnitHectare, a)}, nil
}

// mapHarvestProgress: tỷ lệ đã gặt (làm tròn 4 chữ số của nguồn) và diện tích còn chờ gặt = gieo − đã gặt (cận cộng
// hai sai số làm tròn). Dòng province_withheld (ADR-0035 §3) là phần dư của các xã bị ẩn dưới k: giá trị đúng của
// nhóm, nhưng không phân bổ được về xã — căn cứ k_suppressed_residual để engine dùng như ước tính vùng.
func mapHarvestProgress(rec json.RawMessage) ([]Draft, error) {
	var r struct {
		AreaLevel string `json:"area_level"`
		Sown      string `json:"area_ha_sown"`
		Harvested string `json:"area_ha_harvested"`
		Share     string `json:"harvested_share"`
	}
	if err := decodeRecord(rec, &r); err != nil {
		return nil, err
	}
	sown, err := Quantized(r.Sown)
	if err != nil {
		return nil, fmt.Errorf("area_ha_sown: %w", err)
	}
	harv, err := Quantized(r.Harvested)
	if err != nil {
		return nil, fmt.Errorf("area_ha_harvested: %w", err)
	}
	share, err := Quantized(r.Share)
	if err != nil {
		return nil, fmt.Errorf("harvested_share: %w", err)
	}
	var extra []string
	if r.AreaLevel == "province_withheld" {
		extra = append(extra, BasisKSuppressedResidual)
	}
	remaining := sown.Sub(harv)
	remBasis := slices.Clone(extra)
	if remaining.Value.Sign() < 0 {
		// Đã gặt > đã gieo (lệch nguồn): kẹp về 0, nới cận đủ phủ giá trị gốc.
		remaining = Measured{Value: decimal.Zero, U: remaining.U.Add(remaining.Value.Abs())}
		remBasis = append(remBasis, BasisClamped)
	}
	return []Draft{
		valued(port.KindHarvestShare, port.UnitRatio, share, extra...),
		valued(port.KindHarvestAreaRemaining, port.UnitHectare, remaining, remBasis...),
	}, nil
}

func mapAdminUnit(rec json.RawMessage) ([]Draft, error) {
	var r struct {
		Successors []struct {
			BasisStatus string `json:"basis_status"`
		} `json:"successors"`
	}
	if err := decodeRecord(rec, &r); err != nil {
		return nil, err
	}
	d := Draft{Kind: port.KindAdminUnit}
	for _, s := range r.Successors {
		if s.BasisStatus == "pending_c9" {
			d.Basis = []string{BasisPendingC9}
		}
	}
	return []Draft{d}, nil
}

func mapSeasonHarvest(rec json.RawMessage) ([]Draft, error) {
	var r struct {
		Status string `json:"status"`
	}
	if err := decodeRecord(rec, &r); err != nil {
		return nil, err
	}
	d := Draft{Kind: port.KindSeasonHarvestWindow}
	if r.Status == "open" {
		d.Basis = []string{BasisForecast}
	}
	return []Draft{d}, nil
}

func mapWaterway(rec json.RawMessage) ([]Draft, error) {
	var r struct {
		NetworkVersionRef string `json:"network_version_ref"`
	}
	if err := decodeRecord(rec, &r); err != nil {
		return nil, err
	}
	if r.NetworkVersionRef == "" {
		return nil, fmt.Errorf("thiếu network_version_ref")
	}
	return []Draft{{Kind: port.KindWaterwayNetwork}}, nil
}

// Package store là kho dữ liệu của pf/cooperative.
//
// W3.2 (store tạm, không đổi DB): dữ liệu HTX vẫn ở pf_party/pf_geo/pf_cropcycle. Legacy giữ các
// nguồn đã chuyển dạng sang kiểu của cooperative/port — gốc lắp ráp dựng chúng từ port cũ của
// party/geo/cropcycle qua bộ chuyển cmd/internal/coopwire (module này không import port cũ để
// giữ pf/cooperative là đỉnh lá của đồ thị module, docs/design/cooperative.md mục 3).
// W3.3 thay Legacy bằng store SQL trên schema pf_coop (pf 000094–000096).
package store

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"

// Legacy là các nguồn cũ; trường nil = chưa nối (thao tác cần nó trả port.ErrUnavailable).
type Legacy struct {
	Cooperatives port.Cooperatives // pf/party (CooperativeStats, CooperativeSites) + pf/geo (CoopCentroids)
	Households   port.Households   // pf/party (Households) + pf/geo (PlotHouseholds)
	Plots        port.Plots        // pf/geo (Plots, PlotFacts, PlotStats)
	Seasons      port.Seasons      // pf/cropcycle (Stats, SupplyStats, ReportData)
	Diary        port.Diary        // chưa có nguồn cũ (W3.3)
}

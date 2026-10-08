package app

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"

// Danh sách job tổng hợp đã khai (W6.4, ADR-0031 §4): đăng ký lúc biên dịch. Thêm job hoặc đổi BẤT KỲ phần nào
// của định nghĩa = phiên bản mới, đi qua Agg.Propose → Agg.Decide (bốn mắt); bộ chạy từ chối định nghĩa có băm
// khác phiên bản đã duyệt mới nhất.

// HarvestByCommune: sản lượng và diện tích khai thu hoạch (bản ghi mới nhất mỗi chuỗi) theo xã × vụ trên mọi đơn
// vị — đầu vào tập dữ liệu dựng sẵn theo xã (ADR-0035) khi k được ký (C-4).
var HarvestByCommune = domain.AggJob{
	ID:      "cc.harvest_by_commune",
	Version: 1,
	Views:   []string{"pf_cropcycle_pub.v1_harvest_declaration"},
	Query: `WITH last AS (
	    SELECT DISTINCT ON (stream_key) commune_code, season_year, season_code, mass_kg, area_ha
	      FROM pf_cropcycle_pub.v1_harvest_declaration ORDER BY stream_key, seq DESC)
	  SELECT commune_code, season_year::text, upper(season_code), sum(mass_kg)::text, sum(area_ha)::text
	    FROM last GROUP BY 1, 2, 3`,
	Dims:     []string{"commune_code", "season_year", "season_code"},
	Measures: []string{"mass_kg", "area_ha"},
	Purpose:  "Sản lượng, diện tích thu hoạch theo xã và vụ trên mọi đơn vị, ẩn nhóm dưới k (ADR-0035)",
	KMin:     domain.KFloorDefault,
}

// AggJobs là mọi job tổng hợp đã khai.
var AggJobs = []domain.AggJob{HarvestByCommune}

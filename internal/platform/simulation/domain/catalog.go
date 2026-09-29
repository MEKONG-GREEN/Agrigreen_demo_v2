// Package domain: quy tắc thuần của pf/simulation — danh mục 57 tham số (FN-01),
// ảnh chụp đầu vào và băm của nó, khoảng cách chim bay, so sánh kịch bản (FN-14).
// Không phụ thuộc hạ tầng; không time.Now() (R-12).
package domain

import "github.com/shopspring/decimal"

// Classification là phân loại nguồn của tham số (FN-01 AC-03).
type Classification string

// Các phân loại.
const (
	ClassMarket     Classification = "thi_truong" // có thể lấy từ nguồn thị trường
	ClassAssumption Classification = "gia_dinh"   // cần input giả định — phải có người phê duyệt
	ClassOther      Classification = "khac"
)

// ParamDef là một tham số của danh mục. Base/Min/Max rỗng = chưa chốt (không tự suy).
// Dims: tham số nhiều giá trị (#10 theo phương thức, #26 theo loại máy).
type ParamDef struct {
	Number         int
	Code           string
	Name           string
	Unit           string
	Classification Classification
	Base, Min, Max string
	Dims           []ParamDim
	// ManagedElsewhere: dữ liệu nằm ở module khác (HTX, toạ độ, sản lượng, mạng…), không phải số cấu hình.
	ManagedElsewhere bool
}

// ParamDim là một chiều của tham số nhiều giá trị.
type ParamDim struct {
	Key  string
	Base string
}

// Catalog là 49 tham số BRD Supply Chain Hub Simulation v1.4 (#1–#49) và nhóm mở
// rộng mô hình luồng rơm (#50–#57) — theo mekong-green src/erp/params/catalog.ts.
var Catalog = []ParamDef{
	{Number: 1, Code: "admin_boundaries", Name: "Ranh giới tỉnh/thành sau sáp nhập (2025)", Unit: "Polygon", Classification: ClassMarket, ManagedElsewhere: true},
	{Number: 2, Code: "htx_directory", Name: "Danh sách hợp tác xã (tên, địa bàn)", Unit: "Danh mục", Classification: ClassMarket, ManagedElsewhere: true},
	{Number: 3, Code: "htx_coordinates", Name: "Toạ độ GPS của từng HTX/vùng nguyên liệu", Unit: "Độ", Classification: ClassMarket, ManagedElsewhere: true},
	{Number: 4, Code: "planted_area", Name: "Diện tích gieo trồng theo HTX/vùng", Unit: "ha", Classification: ClassMarket, ManagedElsewhere: true},
	{Number: 5, Code: "paddy_output", Name: "Sản lượng lúa theo HTX/vùng/mùa vụ", Unit: "tấn/mùa vụ", Classification: ClassMarket, ManagedElsewhere: true},
	{Number: 6, Code: "farmer_count", Name: "Số nông hộ liên kết theo HTX", Unit: "hộ", Classification: ClassMarket, ManagedElsewhere: true},
	{Number: 7, Code: "season_calendar", Name: "Lịch mùa vụ (Đông Xuân / Hè Thu / Thu Đông)", Unit: "Khoảng thời gian", Classification: ClassMarket, ManagedElsewhere: true},
	{Number: 8, Code: "leg_distances", Name: "Khoảng cách Field→Hub, Hub→Plant và Field→Plant", Unit: "km", Classification: ClassMarket, ManagedElsewhere: true},
	{Number: 9, Code: "waterway_network", Name: "Mạng lưới đường thủy/kênh rạch lưu thông được cho sà lan", Unit: "Polyline", Classification: ClassAssumption, ManagedElsewhere: true},
	{Number: 10, Code: "distance_correction", Name: "Hệ số hiệu chỉnh khoảng cách (Haversine → thực tế) theo phương thức", Unit: "lần", Classification: ClassAssumption, Base: "1.3", Min: "1.15", Max: "1.5",
		Dims: []ParamDim{{Key: "road", Base: "1.3"}, {Key: "waterway", Base: "1.45"}}},
	{Number: 11, Code: "plant_coordinates", Name: "Toạ độ GPS Nhà máy VFT", Unit: "Độ", Classification: ClassOther, ManagedElsewhere: true},
	{Number: 12, Code: "plant_annual_demand", Name: "Nhu cầu rơm hàng năm của Nhà máy VFT", Unit: "tấn/năm", Classification: ClassMarket, Base: "240000", Min: "180000", Max: "300000"},
	{Number: 13, Code: "default_service_radius_km", Name: "Bán kính phục vụ mặc định cho Hub mới", Unit: "km", Classification: ClassAssumption, Base: "30", Min: "20", Max: "45"},
	{Number: 14, Code: "default_design_capacity_tons", Name: "Công suất thiết kế mặc định cho Hub mới", Unit: "tấn", Classification: ClassAssumption, Base: "60000", Min: "40000", Max: "90000"},
	{Number: 15, Code: "distance_error_threshold_pct", Name: "Ngưỡng sai số khoảng cách chấp nhận được", Unit: "%", Classification: ClassAssumption, Base: "10", Min: "5", Max: "15"},
	{Number: 16, Code: "hub_coordinates", Name: "Toạ độ vị trí Hub ứng viên", Unit: "Độ", Classification: ClassOther, ManagedElsewhere: true},
	{Number: 17, Code: "straw_to_paddy_ratio", Name: "Hệ số rơm/lúa (tỷ lệ phụ phẩm)", Unit: "tấn rơm/tấn lúa", Classification: ClassAssumption, Base: "1", Min: "0.9", Max: "1.35"},
	{Number: 18, Code: "collectable_ratio", Name: "Hệ số thu gom khả thi", Unit: "%", Classification: ClassAssumption, Base: "45", Min: "30", Max: "60"},
	{Number: 19, Code: "loss_ratio", Name: "Hệ số hao hụt/tổn thất rơm trong lưu kho & vận chuyển", Unit: "%", Classification: ClassAssumption, Base: "5", Min: "3", Max: "10"},
	{Number: 20, Code: "collection_labour_price", Name: "Đơn giá nhân công thu gom", Unit: "VNĐ/tấn", Classification: ClassAssumption, Base: "180000", Min: "140000", Max: "240000"},
	{Number: 21, Code: "collection_consumable_price", Name: "Đơn giá công cụ/dụng cụ tiêu hao thu gom", Unit: "VNĐ/tấn", Classification: ClassAssumption, Base: "45000", Min: "30000", Max: "70000"},
	{Number: 22, Code: "road_freight_rate", Name: "Đơn giá vận chuyển đường bộ", Unit: "đ/tấn/km", Classification: ClassMarket, Base: "2200", Min: "1800", Max: "2900"},
	{Number: 23, Code: "waterway_freight_rate", Name: "Đơn giá vận chuyển đường thủy", Unit: "đ/tấn/km", Classification: ClassMarket, Base: "900", Min: "650", Max: "1250"},
	{Number: 24, Code: "warehouse_build_price", Name: "Đơn giá xây dựng kho", Unit: "triệu đ/m²", Classification: ClassMarket, Base: "3.5", Min: "2.8", Max: "4.6"},
	{Number: 25, Code: "yard_build_price", Name: "Đơn giá xây dựng sân bãi", Unit: "triệu đ/m²", Classification: ClassMarket, Base: "0.9", Min: "0.6", Max: "1.4"},
	{Number: 26, Code: "equipment_price", Name: "Đơn giá thiết bị (máy ép kiện, xe nâng)", Unit: "triệu đ/máy", Classification: ClassMarket, Base: "850", Min: "650", Max: "1100",
		Dims: []ParamDim{{Key: "bale_press", Base: "850"}, {Key: "forklift", Base: "620"}}},
	{Number: 27, Code: "road_feasible_distance_km", Name: "Ngưỡng cự ly khả thi cho vận chuyển đường bộ", Unit: "km", Classification: ClassAssumption, Base: "120", Min: "80", Max: "180"},
	{Number: 28, Code: "land_lease_price", Name: "Đơn giá thuê đất/mặt bằng Hub", Unit: "VNĐ/m²/năm", Classification: ClassAssumption, Base: "45000", Min: "25000", Max: "80000"},
	{Number: 29, Code: "warehouse_area_norm", Name: "Định mức diện tích kho", Unit: "m²/tấn", Classification: ClassMarket, Base: "0.85", Min: "0.6", Max: "1.2"},
	{Number: 30, Code: "yard_area_norm", Name: "Định mức diện tích sân bãi", Unit: "m²/tấn", Classification: ClassMarket, Base: "0.6", Min: "0.4", Max: "0.9"},
	{Number: 31, Code: "bale_press_capacity", Name: "Công suất máy ép kiện", Unit: "tấn/máy/năm", Classification: ClassMarket, Base: "18000", Min: "12000", Max: "25000"},
	{Number: 32, Code: "forklift_capacity", Name: "Công suất xe nâng", Unit: "tấn/máy/năm", Classification: ClassMarket, Base: "25000", Min: "18000", Max: "35000"},
	{Number: 33, Code: "truck_payload_tons", Name: "Tải trọng bình quân xe tải", Unit: "tấn/chuyến", Classification: ClassMarket, Base: "15", Min: "10", Max: "22"},
	{Number: 34, Code: "barge_payload_tons", Name: "Tải trọng bình quân sà lan", Unit: "tấn/chuyến", Classification: ClassMarket, Base: "300", Min: "150", Max: "600"},
	{Number: 35, Code: "peak_inventory_factor", Name: "Hệ số tồn kho cao điểm", Unit: "lần", Classification: ClassAssumption, Base: "0.3", Min: "0.2", Max: "0.45"},
	{Number: 36, Code: "road_speed_kmh", Name: "Tốc độ bình quân đường bộ", Unit: "km/h", Classification: ClassMarket, Base: "40", Min: "30", Max: "55"},
	{Number: 37, Code: "waterway_speed_kmh", Name: "Tốc độ bình quân đường thủy", Unit: "km/h", Classification: ClassMarket, Base: "12", Min: "8", Max: "16"},
	{Number: 38, Code: "handling_hours_per_trip", Name: "Thời gian bốc/xếp bình quân mỗi chuyến", Unit: "giờ/chuyến", Classification: ClassMarket, Base: "3.5", Min: "2", Max: "6"},
	{Number: 39, Code: "capex_other", Name: "CAPEX ngoài xây dựng/thiết bị (đất, pháp lý, dự phòng…)", Unit: "triệu đ", Classification: ClassAssumption, Base: "12000", Min: "8000", Max: "20000"},
	{Number: 40, Code: "staff_count", Name: "Số nhân sự vận hành Hub (định biên)", Unit: "người", Classification: ClassAssumption, Base: "18", Min: "12", Max: "28"},
	{Number: 41, Code: "staff_salary_month", Name: "Lương bình quân nhân sự vận hành", Unit: "VNĐ/người/tháng", Classification: ClassAssumption, Base: "9500000", Min: "8000000", Max: "13000000"},
	{Number: 42, Code: "utility_cost_year", Name: "Chi phí điện/nhiên liệu vận hành Hub", Unit: "VNĐ/năm", Classification: ClassAssumption, Base: "1800000000", Min: "1200000000", Max: "2800000000"},
	{Number: 43, Code: "maintenance_rate_pct", Name: "Tỷ lệ bảo trì thiết bị hàng năm", Unit: "%", Classification: ClassAssumption, Base: "6", Min: "4", Max: "10"},
	{Number: 44, Code: "discount_rate_pct", Name: "Tỷ lệ chiết khấu (discount rate) cho TCO", Unit: "%/năm", Classification: ClassAssumption, Base: "10", Min: "8", Max: "14"},
	{Number: 45, Code: "opex_escalation_pct", Name: "Tỷ lệ tăng chi phí OPEX hàng năm (escalation)", Unit: "%/năm", Classification: ClassAssumption, Base: "5", Min: "3", Max: "8"},
	{Number: 46, Code: "ramp_up_pct", Name: "Tỷ lệ sản lượng năm đầu so với mức ổn định (ramp-up)", Unit: "%", Classification: ClassAssumption, Base: "60", Min: "40", Max: "85"},
	{Number: 47, Code: "lifecycle_years", Name: "Số năm vòng đời so sánh", Unit: "năm", Classification: ClassAssumption, Base: "10", Min: "7", Max: "15"},
	{Number: 48, Code: "min_roi_pct", Name: "Ngưỡng ROI tối thiểu để khuyến nghị đầu tư", Unit: "%", Classification: ClassAssumption},
	{Number: 49, Code: "max_payback_years", Name: "Ngưỡng Payback Period tối đa chấp nhận được", Unit: "năm", Classification: ClassAssumption},
	{Number: 50, Code: "boat_registered_tons", Name: "Tải trọng đăng ký của ghe chở rơm", Unit: "tấn/chuyến", Classification: ClassAssumption, Base: "100", Min: "60", Max: "150"},
	{Number: 51, Code: "boat_straw_payload_tons", Name: "Khối lượng rơm thực chở mỗi chuyến ghe", Unit: "tấn/chuyến", Classification: ClassAssumption, Base: "90", Min: "50", Max: "140"},
	{Number: 52, Code: "barge_small_payload_tons", Name: "Tải trọng sà lan nhỏ (ngoài vụ)", Unit: "tấn/chuyến", Classification: ClassAssumption, Base: "1000", Min: "500", Max: "1500"},
	{Number: 53, Code: "barge_large_payload_tons", Name: "Tải trọng sà lan lớn (ngoài vụ)", Unit: "tấn/chuyến", Classification: ClassAssumption, Base: "2000", Min: "1500", Max: "3000"},
	{Number: 54, Code: "plant_operating_days", Name: "Số ngày vận hành nhà máy trong năm", Unit: "ngày/năm", Classification: ClassAssumption, Base: "330", Min: "300", Max: "365"},
	{Number: 55, Code: "hub_processing_cost", Name: "Chi phí băm và nén rơm tại Hub", Unit: "VNĐ/tấn", Classification: ClassAssumption, Base: "120000", Min: "70000", Max: "200000"},
	{Number: 56, Code: "hub_processing_loss_pct", Name: "Hao hụt khi băm và nén tại Hub", Unit: "%", Classification: ClassAssumption, Base: "2", Min: "0", Max: "6"},
	{Number: 57, Code: "harvest_window_days", Name: "Số ngày thu hoạch rộ của một vụ", Unit: "ngày", Classification: ClassAssumption, Base: "45", Min: "20", Max: "90"},
}

// BRDParamCount là số tham số BRD v1.4 (FN-01 AC-03); phần còn lại là nhóm mở rộng.
const BRDParamCount = 49

// ParamByCode tra một tham số của danh mục.
func ParamByCode(code string) (ParamDef, bool) {
	for _, p := range Catalog {
		if p.Code == code {
			return p, true
		}
	}
	return ParamDef{}, false
}

// CatalogDecimal đọc giá trị danh mục; rỗng → false.
func CatalogDecimal(s string) (decimal.Decimal, bool) {
	if s == "" {
		return decimal.Zero, false
	}
	d, err := decimal.NewFromString(s)
	return d, err == nil
}

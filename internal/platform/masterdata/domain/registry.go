package domain

import (
	"sort"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// catalogNames là tên tiếng Việt của danh mục (G-06).
var catalogNames = map[string]string{
	port.CatalogRiceVariety: "Giống lúa",
	port.CatalogFarmStage:   "Khâu sản xuất",
	port.CatalogMachineType: "Chủng loại máy",
	port.CatalogSeason:      "Mùa vụ",
	port.CatalogPest:        "Sâu bệnh, dịch hại",
	port.CatalogVesselClass: "Lớp phương tiện thủy",
}

// CatalogName trả tên tiếng Việt của danh mục; không biết → chính mã.
func CatalogName(c string) string {
	if n, ok := catalogNames[c]; ok {
		return n
	}
	return c
}

// CatalogAttrs trả tên các thuộc tính của danh mục (theo bộ kiểm attrSpecs), sắp theo tên.
func CatalogAttrs(c string) []string {
	out := make([]string, 0, len(attrSpecs[c]))
	for k := range attrSpecs[c] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Kiểu giá trị của tham số.
const (
	ParamKindDecimal = "decimal"
	ParamKindText    = "text"
	// ParamKindColor: tham số chuỗi là mã màu HEX "#RRGGBB" (GIS US-CFG-01 AC-2).
	ParamKindColor = "color"
)

// ParamSpec là mô tả của một khóa tham số trong sổ đăng ký (G-07). Min/Max là
// khoảng hợp lệ gợi ý cho giao diện (số thập phân dạng chuỗi), rỗng = không giới hạn.
type ParamSpec struct {
	Key         string // khóa, hoặc tiền tố với khóa có chiều (Dimensional)
	Name        string
	Unit        string
	Kind        string
	Min, Max    string
	Dimensional bool // khóa có chiều: Key + ":<chiều>…"
}

// ParamRegistry là các khóa tham số hệ thống đang dùng (mọi module). Khóa có
// chiều (vd định mức theo loại máy × khâu) chỉ xuất hiện trong sổ khi đã có
// phiên bản.
var ParamRegistry = []ParamSpec{
	{Key: port.ParamCGHCoverageSufficient, Name: "Ngưỡng Đủ máy (mức đáp ứng)", Unit: "%", Kind: ParamKindDecimal, Min: "0", Max: "500"},
	{Key: port.ParamCGHCoverageAttention, Name: "Ngưỡng Cần chú ý", Unit: "%", Kind: ParamKindDecimal, Min: "0", Max: "500"},
	{Key: port.ParamCGHCoverageSurplusFrom, Name: "Ngưỡng Thừa máy", Unit: "%", Kind: ParamKindDecimal, Min: "0", Max: "1000"},
	{Key: port.ParamCGHNormPrefix, Name: "Định mức máy", Unit: "ha/máy/vụ", Kind: ParamKindDecimal, Min: "0", Dimensional: true},
	{Key: "extension.task_sla_hours", Name: "SLA xử lý nhiệm vụ hỗ trợ", Unit: "giờ", Kind: ParamKindDecimal, Min: "1", Max: "720"},
	{Key: "extension.duty_status_ttl_hours", Name: "Thời hạn trạng thái trực", Unit: "giờ", Kind: ParamKindDecimal, Min: "1", Max: "168"},
	{Key: "extension.target_sown_area_ha", Name: "Mục tiêu diện tích gieo sạ", Unit: "ha", Kind: ParamKindDecimal, Min: "0", Dimensional: true},
	{Key: "geo.overlap_tolerance_pct", Name: "Ngưỡng chồng lấn ranh giới thửa", Unit: "%", Kind: ParamKindDecimal, Min: "0", Max: "100"},
	{Key: "geo.area_deviation_max_pct", Name: "Ngưỡng lệch diện tích khai báo", Unit: "%", Kind: ParamKindDecimal, Min: "0", Max: "100"},
	{Key: "sync.conflict_sla_hours", Name: "SLA xử lý xung đột đồng bộ", Unit: "giờ", Kind: ParamKindDecimal, Min: "1", Max: "720"},
	// G2 (migration 000027, seeddata/params.json).
	{Key: port.ParamGPSMatchTolerance, Name: "Ngưỡng khớp GPS nhật ký ↔ thửa", Unit: "m", Kind: ParamKindDecimal, Min: "0", Max: "1000"},
	{Key: port.ParamSowingWindowBefore, Name: "Cửa sổ gieo sạ trước ngày kế hoạch", Unit: "ngày", Kind: ParamKindDecimal, Min: "0", Max: "120"},
	{Key: port.ParamSowingWindowAfter, Name: "Cửa sổ gieo sạ sau ngày kế hoạch", Unit: "ngày", Kind: ParamKindDecimal, Min: "0", Max: "120"},
	{Key: port.ParamYieldAnomalyMinPrefix, Name: "Năng suất thấp bất thường", Unit: "t/ha", Kind: ParamKindDecimal, Min: "0", Max: "30", Dimensional: true},
	{Key: port.ParamYieldAnomalyMaxPrefix, Name: "Năng suất cao bất thường", Unit: "t/ha", Kind: ParamKindDecimal, Min: "0", Max: "30", Dimensional: true},
	{Key: port.ParamHubCapacityWarn, Name: "Ngưỡng cảnh báo sức chứa hub/kho", Unit: "%", Kind: ParamKindDecimal, Min: "0", Max: "100"},
	{Key: port.ParamHubCapacityFull, Name: "Ngưỡng đã đầy sức chứa hub/kho", Unit: "%", Kind: ParamKindDecimal, Min: "0", Max: "100"},
	{Key: port.ParamPlotLayerMinZoom, Name: "Mức zoom tối thiểu hiện polygon lô/thửa", Unit: "zoom", Kind: ParamKindDecimal, Min: "0", Max: "22"},
	// Quy chuẩn hiển thị GIS (GIS US-CFG-01/02).
	{Key: port.ParamGISStageColorPrefix, Name: "Màu heatmap giai đoạn mùa vụ", Kind: ParamKindColor, Dimensional: true},
	{Key: port.ParamGISCapacityColorPrefix, Name: "Màu mức lấp đầy kho/Hub", Kind: ParamKindColor, Dimensional: true},
	{Key: port.ParamGISRipeningOutline, Name: "Màu viền vùng chín rộ", Kind: ParamKindColor},
	{Key: port.ParamCGHAreaFeedStaleHours, Name: "Ngưỡng độ tươi diện tích vụ từ App HTX", Unit: "giờ", Kind: ParamKindDecimal, Min: "1", Max: "720"},
	{Key: port.ParamWaterwaySnapTolerance, Name: "Dung sai bắt điểm mạng đường thủy", Unit: "m", Kind: ParamKindDecimal, Min: "0", Max: "1000"},
	{Key: port.ParamWaterwayAutoJoin, Name: "Khoảng tự nối đoạn đường thủy", Unit: "m", Kind: ParamKindDecimal, Min: "0", Max: "5000"},
	{Key: port.ParamWaterwayGapReport, Name: "Ngưỡng báo khoảng hở mạng đường thủy", Unit: "m", Kind: ParamKindDecimal, Min: "0", Max: "100000"},
	{Key: port.ParamHaversineFactor, Name: "Hệ số hiệu chỉnh Haversine → cự ly thực tế", Unit: "lần", Kind: ParamKindDecimal, Min: "1", Max: "3", Dimensional: true},
	{Key: port.ParamActivationGrantLifespan, Name: "Hạn liên kết/QR kích hoạt tài khoản nông dân", Unit: "giờ", Kind: ParamKindDecimal, Min: "1", Max: "720"},
	// G3 pf/mrv (seeddata/params.json) — hệ số CO2 TẠM, chờ khách ký phương pháp (C-1).
	{Key: "mrv.co2.open_burning_kg_per_t", Name: "Hệ số phát thải đốt rơm lộ thiên (tạm, chờ ký phương pháp)", Unit: "kgCO2e/t", Kind: ParamKindDecimal, Min: "0", Max: "10000"},
	{Key: "mrv.co2.transport_road_kg_per_tkm", Name: "Hệ số phát thải vận chuyển đường bộ (tạm)", Unit: "kgCO2e/t·km", Kind: ParamKindDecimal, Min: "0", Max: "10"},
	{Key: "mrv.co2.transport_waterway_kg_per_tkm", Name: "Hệ số phát thải vận chuyển đường thủy (tạm)", Unit: "kgCO2e/t·km", Kind: ParamKindDecimal, Min: "0", Max: "10"},
	{Key: "mrv.anomaly_no_record_days", Name: "Số ngày không có bản ghi mới → cảnh báo bất thường", Unit: "ngày", Kind: ParamKindDecimal, Min: "1", Max: "120"},
	{Key: "mrv.compare_window_days", Name: "Cửa sổ ghép nhật ký HTX ↔ minh chứng cán bộ", Unit: "ngày", Kind: ParamKindDecimal, Min: "0", Max: "30"},
}

var stageNames = map[string]string{
	"lam_dat": "làm đất", "gieo_sa": "gieo sạ/cấy", "cham_soc": "chăm sóc", "thu_hoach": "thu hoạch", "sau_thu_hoach": "sau thu hoạch",
}

// dimNames là nhãn tiếng Việt của chiều bảng màu GIS.
var dimNames = map[string]string{
	"chuan_bi": "Chuẩn bị đất", "sinh_truong": "Sinh trưởng", "sap_thu": "Sắp thu hoạch", "chin_ro": "Chín rộ / đang thu hoạch",
	"da_thu": "Đã thu hoạch", "none": "Chưa có vụ",
	"available": "Còn chỗ", "near_full": "Sắp đầy", "full": "Đã đầy", "unknown": "Chưa có số liệu",
}

// SpecOf trả mô tả của một khóa cụ thể: khóa đã biết, khóa có chiều của một tiền
// tố đã biết (tên kèm chiều), hoặc khóa lạ (tên = khóa, kiểu suy ra là decimal).
func SpecOf(key string) ParamSpec {
	base, dims, _ := strings.Cut(key, ":")
	for _, sp := range ParamRegistry {
		if sp.Key != base {
			continue
		}
		out := sp
		out.Key = key
		if sp.Dimensional && dims != "" {
			out.Name = sp.Name + " — " + dimLabel(base, dims)
		}
		return out
	}
	return ParamSpec{Key: key, Name: key, Kind: ParamKindDecimal}
}

func dimLabel(base, dims string) string {
	parts := strings.Split(dims, ":")
	if base == port.ParamCGHNormPrefix && len(parts) == 2 {
		st := stageNames[parts[1]]
		if st == "" {
			st = parts[1]
		}
		return parts[0] + " × khâu " + st
	}
	if n, ok := dimNames[dims]; ok && (base == port.ParamGISStageColorPrefix || base == port.ParamGISCapacityColorPrefix) {
		return n
	}
	return strings.Join(parts, " × ")
}

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
}

var stageNames = map[string]string{
	"lam_dat": "làm đất", "gieo_sa": "gieo sạ/cấy", "cham_soc": "chăm sóc", "thu_hoach": "thu hoạch", "sau_thu_hoach": "sau thu hoạch",
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
	return strings.Join(parts, " × ")
}

package domain

import "time"

// Giai đoạn mùa vụ cho lớp bản đồ nhiệt GIS (GIS US-SEASON-01 / FN-13 B2: nâu → xanh → vàng → đỏ → xám).
// Suy từ ngày của vụ (ngày sạ, cổng thu hoạch và ngày thu hoạch dự kiến của SOP đã ghim) và trạng thái vụ,
// không tự tính lại số liệu App HTX (BR-48).
const (
	StagePrep       = "chuan_bi"    // nâu: làm đất, chưa tới ngày sạ
	StageGrowing    = "sinh_truong" // xanh: đã gieo sạ, chưa tới cổng thu hoạch
	StageHarvestOK  = "sap_thu"     // vàng: được thu hoạch (đã qua cổng thu hoạch)
	StageHarvesting = "chin_ro"     // đỏ: tới/qua ngày thu hoạch dự kiến, vụ chưa đóng
	StageDone       = "da_thu"      // xám: đã thu hoạch, đang nghỉ
)

// Stages theo thứ tự hiển thị chú giải.
var Stages = []string{StagePrep, StageGrowing, StageHarvestOK, StageHarvesting, StageDone}

var stageLabels = map[string]string{
	StagePrep:       "Chuẩn bị đất",
	StageGrowing:    "Sinh trưởng",
	StageHarvestOK:  "Sắp thu hoạch",
	StageHarvesting: "Chín rộ / đang thu hoạch",
	StageDone:       "Đã thu hoạch",
}

// StageLabel trả nhãn tiếng Việt của giai đoạn.
func StageLabel(code string) string { return stageLabels[code] }

// Cửa sổ cảnh báo chín rộ (GIS US-YIELD-01 / FN-14): thu hoạch dự kiến trong 1–3 ngày tới.
const (
	RipeningFromDays = 1
	RipeningToDays   = 3
)

// DoneWindowDays: vụ đã hoàn thành trong chừng này ngày gần nhất vẫn hiện màu "Đã thu hoạch".
const DoneWindowDays = 45

// SeasonStage suy giai đoạn của một vụ tại ngày day (ngày nghiệp vụ, 00:00 UTC). Vụ hủy → "".
func SeasonStage(status string, sowing, harvestOpen, expectedHarvest, day time.Time) string {
	switch status {
	case "completed":
		return StageDone
	case "open":
	default:
		return ""
	}
	switch {
	case day.Before(sowing):
		return StagePrep
	case day.Before(harvestOpen):
		return StageGrowing
	case day.Before(expectedHarvest):
		return StageHarvestOK
	default:
		return StageHarvesting
	}
}

// Ripening cho biết vụ đang mở có ngày thu hoạch dự kiến trong [day+1, day+3] (GIS US-YIELD-01).
func Ripening(status string, expectedHarvest, day time.Time) bool {
	if status != "open" {
		return false
	}
	d := DaysBetween(day, expectedHarvest)
	return d >= RipeningFromDays && d <= RipeningToDays
}

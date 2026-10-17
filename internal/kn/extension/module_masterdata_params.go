package extension

import (
	_ "embed"
	"encoding/json"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

//go:embed masterdata_params.json
var masterdataParamSeeds []byte

// MasterdataParams owns KN parameter metadata and the initial approved values.
func MasterdataParams() mdport.ParamRegistration {
	var seeds []mdport.ParamSeed
	if err := json.Unmarshal(masterdataParamSeeds, &seeds); err != nil {
		panic(err)
	}
	return mdport.ParamRegistration{Specs: []mdport.ParamSpec{
		{Key: "extension.task_sla_hours", Name: "SLA xử lý nhiệm vụ hỗ trợ", Unit: "giờ", Kind: mdport.ParamKindDecimal, Min: "1", Max: "720"},
		{Key: "extension.duty_status_ttl_hours", Name: "Thời hạn trạng thái trực", Unit: "giờ", Kind: mdport.ParamKindDecimal, Min: "1", Max: "168"},
		{Key: "extension.target_sown_area_ha", Name: "Mục tiêu diện tích gieo sạ", Unit: "ha", Kind: mdport.ParamKindDecimal, Min: "0", Dimensional: true},
		{Key: "extension.report_period_rule", Name: "Quy tắc chia kỳ báo cáo (lịch tuần; \":<mã phòng>\" = lịch riêng của phòng)", Kind: mdport.ParamKindText},
		{Key: "extension.ksgm_week_starts", Name: "Ranh giới 4 tuần KSGM của tháng (vd 1,8,15,22)", Kind: mdport.ParamKindText, Dimensional: true},
		{Key: mdport.ParamCNTSIndicatorMinPrefix, Name: "Ngưỡng dưới chỉ tiêu theo dõi CNTS", Kind: mdport.ParamKindDecimal, Dimensional: true},
		{Key: mdport.ParamCNTSIndicatorMaxPrefix, Name: "Ngưỡng trên chỉ tiêu theo dõi CNTS", Kind: mdport.ParamKindDecimal, Dimensional: true},
		{Key: mdport.ParamCNTSHerdChangePct, Name: "Biến động tổng đàn BM-04 so kỳ gần nhất", Unit: "%", Kind: mdport.ParamKindDecimal, Min: "0", Max: "1000"},
		{Key: mdport.ParamCNTSFishingChangePct, Name: "Biến động khai thác thủy sản BM-17 so kỳ gần nhất", Unit: "%", Kind: mdport.ParamKindDecimal, Min: "0", Max: "1000"},
		{Key: mdport.ParamCNTSSlaughterSpikeFactor, Name: "KSGM bất thường: hệ số so trung bình 4 tuần", Unit: "lần", Kind: mdport.ParamKindDecimal, Min: "1", Max: "100"},
		{Key: mdport.ParamCNTSSlaughterSpikeMinDays, Name: "KSGM bất thường: số ngày có số liệu tối thiểu", Unit: "ngày", Kind: mdport.ParamKindDecimal, Min: "1", Max: "28"},
		{Key: mdport.ParamCNTSLookbackPeriods, Name: "Kỳ gần nhất: số kỳ tìm lùi tối đa", Unit: "kỳ", Kind: mdport.ParamKindDecimal, Min: "1", Max: "12"},
		{Key: mdport.ParamCNTSLocationDistanceM, Name: "Khoảng cách lần ghi tới điểm mô hình", Unit: "m", Kind: mdport.ParamKindDecimal, Min: "0", Max: "10000"},
		{Key: mdport.ParamCNTSGPSAccuracyM, Name: "Sai số GPS tối đa của lần ghi", Unit: "m", Kind: mdport.ParamKindDecimal, Min: "0", Max: "1000"},
		{Key: mdport.ParamCNTSLogOverdueDays, Name: "Quá hạn ghi nhật ký: số ngày thêm sau tần suất", Unit: "ngày", Kind: mdport.ParamKindDecimal, Min: "0", Max: "30"},
		{Key: mdport.ParamCNTSMortalityPctPrefix, Name: "Tỷ lệ chết tối đa theo loại mô hình", Unit: "%", Kind: mdport.ParamKindDecimal, Min: "0", Max: "100", Dimensional: true},
	}, Seeds: seeds}
}

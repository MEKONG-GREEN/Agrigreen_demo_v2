package port

// Tham số có phiên bản của CNTS (ngưỡng mặc định, BRD Mục III.5 / UR-TINH-13). Cấp Tỉnh sửa qua quy trình tham số
// (người đặt ≠ người duyệt); ngưỡng mới áp dụng cho số liệu có ngày sự việc từ effective_from.
const (
	// ParamCNTSIndicatorMinPrefix / MaxPrefix + ":<mã chỉ tiêu>"[":<mã loại mô hình>"] — ngưỡng dưới/trên của chỉ
	// tiêu theo dõi (vd pH 7,5–8,5; oxy ≥ 4 mg/l). Khóa có loại mô hình ghi đè khóa chỉ có chỉ tiêu.
	ParamCNTSIndicatorMinPrefix = "cnts.indicator_min"
	ParamCNTSIndicatorMaxPrefix = "cnts.indicator_max"
	// ParamCNTSHerdChangePct: tổng đàn BM-04 tăng/giảm quá % so kỳ gần nhất → cảnh báo (15).
	ParamCNTSHerdChangePct = "cnts.herd_change_pct"
	// ParamCNTSFishingChangePct: khai thác BM-17 tăng/giảm quá % so kỳ gần nhất → cảnh báo (50).
	ParamCNTSFishingChangePct = "cnts.fishing_change_pct"
	// ParamCNTSSlaughterSpikeFactor / MinDays: KSGM ngày > hệ số × trung bình 4 tuần trước (3), cần đủ số ngày có số liệu (7).
	ParamCNTSSlaughterSpikeFactor  = "cnts.slaughter_spike_factor"
	ParamCNTSSlaughterSpikeMinDays = "cnts.slaughter_spike_min_days"
	// ParamCNTSLookbackPeriods: "kỳ gần nhất" tìm lùi tối đa n kỳ (4).
	ParamCNTSLookbackPeriods = "cnts.lookback_periods"
	// ParamCNTSLocationDistanceM / GPSAccuracyM: lần ghi cách điểm mô hình quá m (200) hoặc sai số GPS quá m (50).
	ParamCNTSLocationDistanceM = "cnts.location_distance_m"
	ParamCNTSGPSAccuracyM      = "cnts.gps_accuracy_m"
	// ParamCNTSLogOverdueDays: quá tần suất ghi nhật ký thêm n ngày → cảnh báo (1).
	ParamCNTSLogOverdueDays = "cnts.log_overdue_grace_days"
	// ParamCNTSMortalityPctPrefix + ":<mã loại mô hình>" — tỷ lệ chết vượt % → cảnh báo Khẩn (Cấp Tỉnh cấu hình).
	ParamCNTSMortalityPctPrefix = "cnts.mortality_max_pct"
)

// IndicatorKey dựng khóa ngưỡng chỉ tiêu: prefix + ":<chỉ tiêu>"[":<loại mô hình>"].
func IndicatorKey(prefix, indicator, modelType string) string {
	k := prefix + ":" + indicator
	if modelType != "" {
		k += ":" + modelType
	}
	return k
}

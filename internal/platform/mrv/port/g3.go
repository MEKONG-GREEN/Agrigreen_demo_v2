package port

// G3 (g3-conventions §2, pf 000038): liên kết bằng chứng từ nguồn ngoài module và
// ước tính CO2 TẠM — "chờ khách ký phương pháp" (C-1). pf/mrv G3 KHÔNG công bố
// tCO2e chính thức trước khi phương pháp và tham số được duyệt.

// Loại minh chứng G3 có danh mục mẫu (seed, tạm chờ C-1).
const (
	// EvidenceHTXFarmLog: nhật ký canh tác HTX được liên kết làm tham chiếu bằng
	// chứng (KN FR-06/US-MRV-02). Liên kết ghim phiên bản danh mục ĐÃ DUYỆT của loại này.
	EvidenceHTXFarmLog = "htx_farm_log"
	// EvidenceStrawHandling: cán bộ KN ghi nhận xử lý rơm rạ tại thửa (KN FR-07).
	EvidenceStrawHandling = "straw_handling"
)

// Consumer của worker (shared.inbox, vai mrv_app).
const (
	// FarmLogLinkConsumer liên kết nhật ký HTX và tham chiếu vụ (pf/cropcycle).
	FarmLogLinkConsumer = "pf.mrv.farm_log_link"
	// CollectionEvidenceConsumer nhận van mrv_collection_evidence của mkg/measurement.
	CollectionEvidenceConsumer = "pf.mrv.collection_evidence"
)

// Sự kiện pf/cropcycle mà FarmLogLinkConsumer đăng ký (tên trùng cropcycle/port;
// khai lại ở đây để không phụ thuộc chiều ngược).
const (
	EventFarmLogRecorded   = "pf.cropcycle.farm_log.recorded"
	EventFarmLogReviewed   = "pf.cropcycle.farm_log.reviewed"
	EventSeasonOpened      = "pf.cropcycle.season.opened"
	EventSeasonCorrected   = "pf.cropcycle.season.corrected"
	EventSeasonCompleted   = "pf.cropcycle.season.completed"
	EventMKGCollectionEvid = "mkg.measurement.mrv_collection_evidence.published"
)

// FarmLogLinkEvents là các sự kiện FarmLogLinkConsumer đăng ký.
var FarmLogLinkEvents = []string{EventFarmLogRecorded, EventFarmLogReviewed, EventSeasonOpened, EventSeasonCorrected, EventSeasonCompleted}

// CollectionEvidenceEvents là các sự kiện CollectionEvidenceConsumer đăng ký.
var CollectionEvidenceEvents = []string{EventMKGCollectionEvid}

// ContractCollectionEvidence là hợp đồng van (contracts/publication/mrv_collection_evidence.json).
const ContractCollectionEvidence = "mrv_collection_evidence"

// Tham số masterdata (có phiên bản + phê duyệt; seed ở masterdata seeddata/params.json).
// Chưa có phiên bản đã duyệt → dùng mặc định trong mã và ước tính KHÔNG đủ điều kiện chính thức.
const (
	// ParamCO2OpenBurning: hệ số phát thải đốt rơm lộ thiên (kgCO2e/t rơm) — mặc định 1460 (nhãn IPCC EFDB, prototype).
	ParamCO2OpenBurning = "mrv.co2.open_burning_kg_per_t"
	// ParamCO2TransportRoad: phát thải vận chuyển đường bộ (kgCO2e/t·km) — mặc định 0,062.
	ParamCO2TransportRoad = "mrv.co2.transport_road_kg_per_tkm"
	// ParamCO2TransportWaterway: phát thải vận chuyển đường thủy (kgCO2e/t·km) — mặc định 0,021.
	ParamCO2TransportWaterway = "mrv.co2.transport_waterway_kg_per_tkm"
	// ParamAnomalyNoRecordDays: số ngày không có bản ghi mới → cờ bất thường (KN FR-08) — mặc định 14.
	ParamAnomalyNoRecordDays = "mrv.anomaly_no_record_days"
	// ParamCompareWindowDays: cửa sổ ± ngày ghép nhật ký HTX với minh chứng cán bộ (KN US-MRV-02) — mặc định 3.
	ParamCompareWindowDays = "mrv.compare_window_days"
)

// RuleEstimate là phiên bản quy tắc ước tính CO2 G3 (TẠM, chờ khách ký phương pháp):
// tấn tính = cận dưới bậc khối lượng (bảo thủ) của bằng chứng thu gom hiện hành,
// chỉ khi thửa/vụ có minh chứng cán bộ KN đã "Đạt chuẩn" (KN US-MRV-05);
// tránh đốt = tấn × ParamCO2OpenBurning; vận chuyển theo chặng nếu có dữ liệu chặng.
// Chưa có net, baseline, leakage, chất khô.
const RuleEstimate = "mrv.co2.estimate.v1"

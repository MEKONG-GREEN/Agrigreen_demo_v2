package port

// Cảnh báo MRV (G3, pf 000045): sổ pf_mrv.alert chỉ ghi thêm + sự kiện pf.mrv.alert.raised
// → pf/notify. Ba quy tắc, mọi ngưỡng là tham số masterdata có phiên bản + phê duyệt
// (người đặt ≠ người duyệt); mặc định trong mã là TẠM, chờ khách.

// Loại minh chứng G3 bổ sung (danh mục mẫu seed, tạm chờ C-1).
const (
	// EvidencePestDisaster: cán bộ xã ghi nhận sâu bệnh/thiên tai — loại, cấp gây hại 1–5,
	// diện tích ảnh hưởng (KN US-MRV-04).
	EvidencePestDisaster = "pest_disaster"
	// EvidenceIrrigationInfra: cán bộ xã xác nhận hạ tầng tưới tiêu chủ động đủ cho AWD
	// (ống quan trắc, bờ bao, cống điều tiết) của một thửa, hoặc cả xã khi không gắn thửa (KN FR-08).
	EvidenceIrrigationInfra = "irrigation_infrastructure"
)

// Loại cảnh báo (cột kind của pf_mrv.alert, trường kind của pf.mrv.alert.raised).
const (
	// AlertPestUrgent: nhãn "Cảnh báo khẩn" tự bật khi minh chứng sâu bệnh/thiên tai vượt
	// ngưỡng nguy hiểm (KN US-MRV-04). Nhãn gắn với ĐÚNG phiên bản minh chứng.
	AlertPestUrgent = "pest_urgent"
	// AlertAWDNoInfrastructure: nhật ký App HTX ghi AWD nhưng thửa/xã chưa có minh chứng hạ
	// tầng đạt (KN FR-08) → cờ + thông báo cán bộ xã phụ trách.
	AlertAWDNoInfrastructure = "awd_no_infrastructure"
	// AlertReviewOverdue: hồ sơ MRV chờ duyệt quá hạn xử lý (KN US-MRV-06). Level 1 → người
	// duyệt (TTKN tỉnh, mrv_reviewer); level 2 → quản trị TTKN tỉnh (kn_province_admin).
	AlertReviewOverdue = "review_overdue"
)

// Mức leo thang của AlertReviewOverdue.
const (
	OverdueLevelReviewer      = 1
	OverdueLevelProvinceAdmin = 2
)

// Phiên bản quy tắc (cột rule_version của pf_mrv.alert).
const (
	RulePestUrgent     = "mrv.pest_urgent.v1"
	RuleAWDInfra       = "mrv.awd_infra.v1"
	RuleReviewOverdue  = "mrv.review_overdue.v1"
	EventAlertRaised   = "pf.mrv.alert.raised"
	JobReviewEscalator = "mrv_review_escalation"
)

// Tham số masterdata của cảnh báo (seed ở masterdata seeddata/params.json; TẠM, chờ khách).
const (
	// ParamUrgentCategories: danh sách (tham số CHUỖI, phân tách bằng dấu phẩy) các loại sâu
	// bệnh/thiên tai có thể bật "Cảnh báo khẩn". Đổi danh sách = phiên bản tham số mới có duyệt.
	ParamUrgentCategories = "mrv.urgent.categories"
	// ParamUrgentMinSeverity: cấp gây hại (1–5) từ đó bật khẩn — mặc định 4.
	ParamUrgentMinSeverity = "mrv.urgent.min_severity"
	// ParamUrgentMinAreaHa: diện tích ảnh hưởng (ha) từ đó bật khẩn dù cấp thấp hơn — mặc định 5.
	ParamUrgentMinAreaHa = "mrv.urgent.min_area_ha"
	// ParamAWDActivityCodes: mã hoạt động nhật ký HTX được coi là ghi AWD (tham số CHUỖI, dấu phẩy) —
	// mặc định "tuoi_nuoc" (danh mục htx_farm_log G3 chưa có mã AWD riêng).
	ParamAWDActivityCodes = "mrv.awd.activity_codes"
	// ParamReviewSLAHours: thời hạn xử lý hồ sơ chờ duyệt (giờ, tính từ lúc hồ sơ vào hệ thống) —
	// quá hạn → leo thang mức 1 (người duyệt). Mặc định 72 (BRD chưa có số).
	ParamReviewSLAHours = "mrv.review_sla_hours"
	// ParamReviewEscalateHours: quá hạn này (giờ, từ lúc vào hệ thống) → leo thang mức 2 (quản trị
	// TTKN tỉnh). Mặc định 120; nhỏ hơn SLA thì coi bằng SLA.
	ParamReviewEscalateHours = "mrv.review_escalate_hours"
)

// AlertRaisedV1 là payload của pf.mrv.alert.raised: một cảnh báo MRV đã ghi sổ. Không có dữ
// liệu cá nhân, không có giá trị minh chứng (chỉ mã tham chiếu); pf/notify dựng thông báo.
//
//ag:event pf.mrv.alert.raised v1
type AlertRaisedV1 struct {
	AlertID       string  `json:"alert_id"`
	Kind          string  `json:"kind"`
	Level         int     `json:"level"`
	Severity      string  `json:"severity"`
	AdminUnitCode string  `json:"admin_unit_code"`
	SubjectType   string  `json:"subject_type"`
	SubjectID     string  `json:"subject_id"`
	SubjectRootID string  `json:"subject_root_id"`
	PlotRef       *string `json:"plot_ref"`
	SeasonRef     *string `json:"season_ref"`
	RuleVersion   string  `json:"rule_version"`
	EventTime     string  `json:"event_time"`
}

// Loại chủ thể của cảnh báo.
const (
	AlertSubjectEvidence = "evidence" // subject_id = phiên bản minh chứng, subject_root_id = gốc chuỗi
	AlertSubjectFarmLog  = "farm_log" // subject_id = log_id (phiên bản nhật ký), subject_root_id = log_ref
)

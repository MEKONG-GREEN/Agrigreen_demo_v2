// Package port là giao diện công khai của pf/masterdata (R-02): danh mục, đơn vị
// hành chính có hiệu lực theo thời gian, tham số có phiên bản và phê duyệt.
//
// Quy tắc mục 16: định mức và tham số tra THEO THỜI ĐIỂM CỦA SỰ VIỆC, không theo
// thời điểm hiện tại — vụ đã qua tính bằng giá trị có hiệu lực lúc đó.
//
// Tenant lấy từ context (tenant.WithID do middleware xác thực gắn).
package port

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Level là cấp đơn vị hành chính. Sau sắp xếp 2025: chỉ còn tỉnh và xã.
type Level string

// Các cấp.
const (
	LevelProvince Level = "province"
	LevelCommune  Level = "commune"
	// LevelDistrict chỉ còn trong dữ liệu lịch sử (hiệu lực trước 01/07/2025).
	LevelDistrict Level = "district"
)

// AdminUnit là một đơn vị hành chính tại một thời điểm.
type AdminUnit struct {
	Code       string // mã theo danh mục của Tổng cục Thống kê / Bộ Nội vụ
	Name       string
	Level      Level
	ParentCode string     // tỉnh của xã; rỗng với tỉnh
	ValidFrom  time.Time  // ngày có hiệu lực (theo giờ Việt Nam, 00:00)
	ValidTo    *time.Time // nil = còn hiệu lực; khác nil = thời điểm hết hiệu lực (không bao gồm)
	// SuccessorCodes: đơn vị kế thừa khi bị sáp nhập/chia tách (đóng hiệu lực, không xóa).
	SuccessorCodes []string
}

// ErrUnitNotFound: không có đơn vị với mã đó tại thời điểm tra.
var ErrUnitNotFound = errors.New("masterdata: không có đơn vị hành chính tại thời điểm tra")

// AdminUnits tra đơn vị hành chính tại thời điểm at.
type AdminUnits interface {
	Get(ctx context.Context, code string, at time.Time) (AdminUnit, error)
	// Descendants trả mã của chính đơn vị và mọi đơn vị con có hiệu lực tại at.
	Descendants(ctx context.Context, code string, at time.Time) ([]string, error)
	// Current trả mã hiện hành tương ứng với một mã cũ (đi theo SuccessorCodes).
	Current(ctx context.Context, code string, at time.Time) ([]string, error)
}

// ParamValue là giá trị của một tham số tại thời điểm tra.
type ParamValue struct {
	Key           string
	Value         decimal.Decimal // tham số số học dùng decimal (R-10)
	Text          string          // tham số chuỗi (vd tên nguồn bản đồ nền)
	Unit          string
	Version       int
	EffectiveFrom time.Time
	SourceDoc     string // số văn bản ban hành (vd văn bản của DCRD)
}

// ErrParamNotFound: không có phiên bản ĐÃ PHÊ DUYỆT có hiệu lực tại thời điểm tra.
var ErrParamNotFound = errors.New("masterdata: không có tham số đã phê duyệt có hiệu lực")

// Params tra tham số đã phê duyệt có hiệu lực tại thời điểm của sự việc.
// Khóa tham số có thể mang chiều, vd "cgh.norm_ha_per_machine:<loại máy>:<khâu>".
type Params interface {
	At(ctx context.Context, key string, at time.Time) (ParamValue, error)
}

// NormKey dựng khóa định mức CGH QT-01 theo chủng loại máy × khâu.
func NormKey(machineType, farmStage string) string {
	return ParamCGHNormPrefix + ":" + machineType + ":" + farmStage
}

// Khóa tham số có sẵn (seed) của Bản đồ CGH.
const (
	ParamCGHNormPrefix          = "cgh.norm_ha_per_machine"         // + ":<loại máy>:<khâu>", ha/máy/vụ (QT-01)
	ParamCGHCoverageSufficient  = "cgh.coverage.sufficient_min_pct" // Đủ ≥ giá trị (QT-02)
	ParamCGHCoverageAttention   = "cgh.coverage.attention_min_pct"  // Cần chú ý ≥ giá trị và < Đủ; dưới là Thiếu
	ParamCGHCoverageSurplusFrom = "cgh.coverage.surplus_min_pct"    // Thừa ≥ giá trị
)

// Khóa tham số G2 (seed ở seeddata/params.json, migration 000027). Module khác
// đọc qua Params.At với ĐÚNG các khóa này.
const (
	// ParamGPSMatchTolerance: ngưỡng khớp GPS nhật ký ↔ thửa (m) — cropcycle.
	ParamGPSMatchTolerance = "cropcycle.gps_match_tolerance_m"
	// ParamSowingWindowBefore / After: cửa sổ gieo sạ quanh ngày kế hoạch (ngày) — cropcycle.
	ParamSowingWindowBefore = "cropcycle.sowing_window_before_days"
	ParamSowingWindowAfter  = "cropcycle.sowing_window_after_days"
	// ParamYieldAnomalyMinPrefix / MaxPrefix: ngưỡng năng suất bất thường (t/ha). Khóa
	// theo giống = tiền tố + ":<mã giống>" (YieldAnomalyKey); khóa không chiều là mặc
	// định khi giống chưa có ngưỡng (nguyên mẫu: min = yield_min × 0,4; max = yield_max × 1,25).
	ParamYieldAnomalyMinPrefix = "cropcycle.yield_anomaly_min_t_per_ha"
	ParamYieldAnomalyMaxPrefix = "cropcycle.yield_anomaly_max_t_per_ha"
	// ParamHubCapacityWarn / Full: ngưỡng % lấp đầy hub/kho — "sắp đầy" / "đã đầy" (GIS US-HUB-01,
	// US-CFG-01). Cùng khóa pf/geo đọc (geo/port.ParamHubCapacityWarnPct / FullPct); phải tăng dần
	// trong 0–100 % (kiểm lúc đề xuất và lúc duyệt). Khóa cũ "geo.hub_capacity_critical_pct" của
	// seed 000027 không module nào đọc — thay bằng full_pct (seed chỉ thêm khóa mới).
	ParamHubCapacityWarn = "geo.hub_capacity_warn_pct"
	ParamHubCapacityFull = "geo.hub_capacity_full_pct"
	// ParamPlotLayerMinZoom: mức zoom tối thiểu để lớp bản đồ trả polygon thửa (GIS BR-24) — geo.
	ParamPlotLayerMinZoom = "geo.plot_layer_min_zoom"
	// ParamWaterwaySnapTolerance, AutoJoin, GapReport: dựng mạng đường thủy (m) — waterway.
	ParamWaterwaySnapTolerance = "waterway.snap_tolerance_m"
	ParamWaterwayAutoJoin      = "waterway.autojoin_m"
	ParamWaterwayGapReport     = "waterway.gap_report_m"
	// ParamHaversineFactor: hệ số hiệu chỉnh Haversine → cự ly thực tế (nguyên mẫu tham số #10).
	// Theo phương thức: ParamHaversineFactor + ":road" | ":waterway".
	ParamHaversineFactor = "distance.haversine_factor"
	// ParamActivationGrantLifespan: hạn liên kết/QR kích hoạt tài khoản nông dân (giờ) — accounts.
	ParamActivationGrantLifespan = "activation.grant_lifespan_hours"
)

// Quy chuẩn hiển thị dùng chung của bản đồ GIS (GIS US-CFG-01/02, FN-03): tham số chuỗi mã
// màu HEX "#RRGGBB", có phiên bản và phê duyệt như mọi tham số (R-56: người đặt ≠ người duyệt,
// nên "áp dụng ngay" của AC = áp dụng NGAY KHI ĐƯỢC DUYỆT). Web đọc qua
// GET /v1/masterdata/display-config.
const (
	// ParamGISStageColorPrefix + ":<giai đoạn>" — màu heatmap giai đoạn mùa vụ (FN-13 BR-11):
	// chuan_bi, sinh_truong, sap_thu, chin_ro, da_thu, none (chưa có vụ).
	ParamGISStageColorPrefix = "gis.stage_color"
	// ParamGISCapacityColorPrefix + ":<mức>" — màu mức lấp đầy kho/Hub: available, near_full, full, unknown.
	ParamGISCapacityColorPrefix = "gis.capacity_color"
	// ParamGISRipeningOutline: viền nổi bật vùng chín rộ 1–3 ngày tới (GIS US-YIELD-01).
	ParamGISRipeningOutline = "gis.ripening_outline_color"
	// ParamCGHAreaFeedStaleHours: ngưỡng độ tươi dữ liệu diện tích vụ từ App HTX (giờ) —
	// quá ngưỡng thì Admin CGH được cảnh báo và màn CGH báo đang dùng dữ liệu dự phòng (CGH FN-07 AC-03).
	ParamCGHAreaFeedStaleHours = "cgh.area_feed_stale_hours"
)

// StageCodes / CapacityClasses là các chiều của bảng màu GIS.
var (
	StageCodes      = []string{"chuan_bi", "sinh_truong", "sap_thu", "chin_ro", "da_thu", "none"}
	CapacityClasses = []string{"available", "near_full", "full", "unknown"}
)

// YieldAnomalyKey dựng khóa ngưỡng năng suất bất thường theo giống.
func YieldAnomalyKey(prefix, varietyCode string) string { return prefix + ":" + varietyCode }

// CatalogItem là một mục danh mục dùng chung (giống lúa, khâu, loại máy, vụ...).
type CatalogItem struct {
	Catalog string // vd "rice_variety", "farm_stage", "machine_type", "season"
	Code    string
	Name    string
	Attrs   map[string]string
	Active  bool
}

// Decimal đọc một thuộc tính số (vd expected_yield_t_per_ha) dạng decimal (R-10).
func (c CatalogItem) Decimal(attr string) (decimal.Decimal, error) {
	return decimal.NewFromString(c.Attrs[attr])
}

// ErrCatalogItemNotFound: không có mục danh mục.
var ErrCatalogItemNotFound = errors.New("masterdata: không có mục danh mục")

// Catalogs tra danh mục. Mục không bị xóa — chỉ ngừng dùng (Active = false).
type Catalogs interface {
	Get(ctx context.Context, catalog, code string) (CatalogItem, error)
	List(ctx context.Context, catalog string) ([]CatalogItem, error)
}

// Tên danh mục.
const (
	CatalogRiceVariety = "rice_variety" // attrs: growth_days, expected_yield_t_per_ha (decimal)
	CatalogFarmStage   = "farm_stage"   // attrs: pilot ("true"/"false" — cờ thí điểm CGH BR-06), description
	CatalogMachineType = "machine_type" // attrs: farm_stage (khâu tự gán theo chủng loại, CGH BR-02)
	CatalogSeason      = "season"       // attrs: start_month, end_month (tùy chọn)
	CatalogPest        = "pest"         // attrs: kind (sau_hai | benh_hai | dong_vat_hai)
	// CatalogVesselClass: lớp phương tiện thủy (G2 waterway/simulation). Mã: ghe_100,
	// sa_lan_1000, sa_lan_2000. attrs (decimal, m hoặc tấn): capacity_t, beam_m, draft_m,
	// air_draft_m và điều kiện tối thiểu của tuyến đã cộng biên an toàn: min_width_m
	// (= beam × 1,5), min_depth_m (= draft + 0,4), min_clearance_m (= air_draft + 0,5).
	CatalogVesselClass = "vessel_class"
)

// Vai nghiệp vụ của module (tên vai trong Keycloak).
const (
	// RoleMasterdataAdmin quản trị danh mục và đơn vị hành chính (Admin API).
	RoleMasterdataAdmin = "masterdata_admin"
	// RoleParamSetter đề xuất và gửi duyệt phiên bản tham số (R-56).
	RoleParamSetter = authz.RoleParamSetter
	// RoleParamApprover phê duyệt/từ chối; không được là người đặt (R-56).
	RoleParamApprover = authz.RoleParamApprover
)

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventParamApproved    = "pf.masterdata.param.approved"
	EventAdminUnitChanged = "pf.masterdata.admin_unit.changed"
)

// ParamApprovedV1 là payload của pf.masterdata.param.approved, schema_version 1.
//
//ag:event pf.masterdata.param.approved v1
type ParamApprovedV1 struct {
	ParamKey       string  `json:"param_key"`
	Version        int     `json:"version"`
	ParamVersionID string  `json:"param_version_id"`
	Value          *string `json:"value"` // decimal dạng chuỗi; null với tham số chuỗi
	Text           *string `json:"text"`
	Unit           string  `json:"unit"`
	EffectiveFrom  string  `json:"effective_from"`
	SourceDoc      string  `json:"source_doc"`
	SetterRef      string  `json:"setter_ref"`
	ApproverRef    string  `json:"approver_ref"`
	SupersedesID   *string `json:"supersedes_id"`
	Seq            int64   `json:"seq"`
}

// AdminUnitChangedV1 là payload của pf.masterdata.admin_unit.changed, schema_version 1.
// Change: "created" (thêm phiên bản hiệu lực) hoặc "closed" (đóng hiệu lực, có kế thừa).
//
//ag:event pf.masterdata.admin_unit.changed v1
type AdminUnitChangedV1 struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Level          string   `json:"level"`
	ParentCode     *string  `json:"parent_code"`
	ValidFrom      string   `json:"valid_from"`
	ValidTo        *string  `json:"valid_to"`
	SuccessorCodes []string `json:"successor_codes"`
	Change         string   `json:"change"`
}

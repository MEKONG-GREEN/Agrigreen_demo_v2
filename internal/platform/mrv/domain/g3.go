package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RuleVersionG3 ghi vào cột rule_version của các sổ G3 (liên kết, đối chiếu).
const RuleVersionG3 = "pf.mrv/g3-1"

// ---------------------------------------------------------------------------
// Bậc khối lượng (van mrv_collection_evidence v1)

// bucketLowerKg: cận dưới (kg) của từng bậc — dùng cận dưới cho ước tính (bảo thủ).
var bucketLowerKg = map[string]int64{
	"0-500": 0, "500-1000": 500, "1000-2000": 1000, "2000-5000": 2000, "5000-10000": 5000, "10000+": 10000,
}

// BucketLowerKg trả cận dưới (kg) của bậc; ok = false khi bậc lạ.
func BucketLowerKg(bucket string) (decimal.Decimal, bool) {
	v, ok := bucketLowerKg[bucket]
	return decimal.NewFromInt(v), ok
}

// ---------------------------------------------------------------------------
// Ước tính CO2 (mrv.co2.estimate.v1 — TẠM, chờ khách ký phương pháp)

// Nguồn giá trị tham số.
const (
	ParamSourceApproved = "approved" // bản đã duyệt của masterdata có hiệu lực
	ParamSourceDefault  = "default"  // chưa có bản duyệt → mặc định trong mã (không bao giờ chính thức)
)

// ParamUse là một tham số đã dùng khi tính (ghi vào sổ ước tính và gói xuất).
type ParamUse struct {
	Key     string `json:"key"`
	Version int    `json:"version"` // 0 khi Source = default
	Value   string `json:"value"`
	Unit    string `json:"unit"`
	Source  string `json:"source"`
}

// ParamPin là phiên bản tham số được phương pháp ghim.
type ParamPin struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
}

// Lý do một bằng chứng thu gom không được tính.
const (
	ExcludeNotApproved = "chua_co_minh_chung_dat_chuan" // KN US-MRV-05: thửa/vụ chưa có minh chứng cán bộ "Đạt chuẩn"
	ExcludeBadBucket   = "bac_khong_hop_le"
)

// EstimateInput là một bằng chứng thu gom hiện hành của thửa/vụ.
type EstimateInput struct {
	EvidenceRef string `json:"evidence_ref"`
	Revision    int64  `json:"revision"`
	Bucket      string `json:"collected_kg_bucket"`
	BucketRule  string `json:"bucket_rule_version"`
	CollectedOn string `json:"collected_on"`
	Counted     bool   `json:"counted"`
	Tonnes      string `json:"tonnes"` // cận dưới bậc (t), "0" khi không tính
	Exclusion   string `json:"exclusion,omitempty"`
}

// TransportLeg là một chặng vận chuyển (chế độ road | waterway, cự ly km).
type TransportLeg struct {
	Mode string
	Km   decimal.Decimal
}

// EstimateResult là kết quả tính cho một thửa/vụ.
type EstimateResult struct {
	CountedTonnes   decimal.Decimal
	AvoidedKg       decimal.Decimal
	TransportKg     *decimal.Decimal
	TransportStatus string // computed | no_leg_data
	Inputs          []EstimateInput
	Params          []ParamUse
	InputSHA256     string
}

// Trạng thái phần vận chuyển.
const (
	TransportComputed  = "computed"
	TransportNoLegData = "no_leg_data"
)

// ComputeEstimate tính tất định: tấn tính = Σ cận dưới bậc của bằng chứng được
// tính (approved = thửa/vụ có minh chứng cán bộ KN "Đạt chuẩn"); tránh đốt = tấn ×
// hệ số đốt lộ thiên; vận chuyển = Σ tấn × km × hệ số theo chế độ khi có chặng.
// params theo thứ tự [đốt, đường bộ, đường thủy].
func ComputeEstimate(ruleVersion string, inputs []EstimateInput, approved bool, legs []TransportLeg, burning, road, waterway ParamUse) EstimateResult {
	thousand := decimal.NewFromInt(1000)
	out := EstimateResult{TransportStatus: TransportNoLegData, Params: []ParamUse{burning, road, waterway}}
	sorted := append([]EstimateInput(nil), inputs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].EvidenceRef < sorted[j].EvidenceRef })
	for _, in := range sorted {
		in.Counted, in.Tonnes, in.Exclusion = false, "0", ""
		kg, ok := BucketLowerKg(in.Bucket)
		switch {
		case !ok:
			in.Exclusion = ExcludeBadBucket
		case !approved:
			in.Exclusion = ExcludeNotApproved
		default:
			t := kg.Div(thousand)
			in.Counted, in.Tonnes = true, t.StringFixed(3)
			out.CountedTonnes = out.CountedTonnes.Add(t)
		}
		out.Inputs = append(out.Inputs, in)
	}
	if out.Inputs == nil {
		out.Inputs = []EstimateInput{}
	}
	factor, _ := decimal.NewFromString(burning.Value)
	out.AvoidedKg = out.CountedTonnes.Mul(factor).Round(3)
	if len(legs) > 0 {
		rf, _ := decimal.NewFromString(road.Value)
		wf, _ := decimal.NewFromString(waterway.Value)
		sum := decimal.Zero
		for _, l := range legs {
			f := rf
			if l.Mode == "waterway" {
				f = wf
			}
			sum = sum.Add(out.CountedTonnes.Mul(l.Km).Mul(f))
		}
		sum = sum.Round(3)
		out.TransportKg, out.TransportStatus = &sum, TransportComputed
	}
	h, _ := CanonicalJSON(map[string]any{
		"rule_version": ruleVersion, "approved": approved, "inputs": out.Inputs, "params": out.Params,
		"legs": legKeys(legs),
	})
	sum := sha256.Sum256(h)
	out.InputSHA256 = hex.EncodeToString(sum[:])
	return out
}

func legKeys(legs []TransportLeg) []string {
	out := make([]string, 0, len(legs))
	for _, l := range legs {
		out = append(out, l.Mode+":"+l.Km.String())
	}
	return out
}

// MethodCovers cho biết phương pháp đã duyệt ghim ĐÚNG các phiên bản tham số đã
// dùng và mọi tham số đều là bản đã duyệt (quy tắc "chính thức", như pf/simulation
// FN-01 BR-04: không có số chính thức khi còn tham số chưa duyệt).
func MethodCovers(pins []ParamPin, used []ParamUse) bool {
	want := map[string]int{}
	for _, p := range pins {
		want[p.Key] = p.Version
	}
	for _, u := range used {
		if u.Source != ParamSourceApproved {
			return false
		}
		if v, ok := want[u.Key]; !ok || v != u.Version {
			return false
		}
	}
	return len(used) > 0
}

// ---------------------------------------------------------------------------
// Đối chiếu nhật ký HTX ↔ minh chứng cán bộ (KN US-MRV-02)

// Kết quả đối chiếu.
const (
	CompareMatch      = "match"      // "Khớp"
	CompareMismatch   = "mismatch"   // "Khác biệt"
	CompareUnverified = "unverified" // chưa có minh chứng cán bộ trong cửa sổ
)

// LogItem là bản hiện hành của một nhật ký HTX đã liên kết.
type LogItem struct {
	LogRef       uuid.UUID
	LogID        uuid.UUID
	ActivityCode string
	EventTime    time.Time
}

// OfficerRecord là bản hiện hành của một minh chứng do cán bộ KN thu tại thửa/vụ.
type OfficerRecord struct {
	EvidenceID   uuid.UUID
	RootID       uuid.UUID
	EvidenceType string
	ActivityCode string // values.activity_code nếu danh mục có
	EventTime    time.Time
	ReviewStatus ReviewStatus
}

// activityOfType: hoạt động nhật ký HTX mà một loại minh chứng quan sát được
// (TẠM, chờ C-1). Loại không có trong bảng: coi như quan sát mọi hoạt động.
var activityOfType = map[string]string{"awd_log": "tuoi_nuoc", "straw_handling": "thu_hoach"}

func compatible(log LogItem, r OfficerRecord) bool {
	if r.ActivityCode != "" {
		return r.ActivityCode == log.ActivityCode
	}
	if a, ok := activityOfType[r.EvidenceType]; ok {
		return a == log.ActivityCode
	}
	return true
}

// SuggestMatch gợi ý kết quả đối chiếu tự động: có minh chứng cán bộ cùng hoạt
// động trong cửa sổ ± window → match; có minh chứng trong cửa sổ nhưng khác hoạt
// động → mismatch; không có → unverified. Cán bộ xác nhận bằng bản ghi đối chiếu.
func SuggestMatch(log LogItem, recs []OfficerRecord, window time.Duration) (string, *OfficerRecord) {
	var near *OfficerRecord
	for i := range recs {
		r := recs[i]
		d := r.EventTime.Sub(log.EventTime)
		if d < -window || d > window {
			continue
		}
		if compatible(log, r) {
			return CompareMatch, &recs[i]
		}
		if near == nil {
			near = &recs[i]
		}
	}
	if near != nil {
		return CompareMismatch, near
	}
	return CompareUnverified, nil
}

// ---------------------------------------------------------------------------
// Bất thường nhật ký (KN FR-08: không có bản ghi mới N ngày)

// NoRecordAnomaly: vụ đang mở và từ mốc tham chiếu (bản ghi gần nhất, hoặc lúc mở
// vụ khi chưa có bản ghi) tới now đã quá days ngày → cờ bất thường.
func NoRecordAnomaly(seasonStatus string, reference, now time.Time, days int) (flag bool, daysSince int) {
	if reference.IsZero() {
		return false, 0
	}
	daysSince = int(now.Sub(reference).Hours() / 24)
	return seasonStatus == "open" && days > 0 && daysSince >= days, daysSince
}

// ---------------------------------------------------------------------------
// Ảnh chụp G3 trong gói xuất (mục 16)

// SnapshotFarmLog là liên kết nhật ký HTX hiện hành tại as_of.
type SnapshotFarmLog struct {
	LogRef         string  `json:"log_ref"`
	LogID          string  `json:"log_id"`
	LogVersion     int64   `json:"log_version"`
	SeasonRef      string  `json:"season_ref"`
	PlotRef        string  `json:"plot_ref"`
	CommuneCode    string  `json:"commune_code"`
	ActivityCode   string  `json:"activity_code"`
	LogEventTime   string  `json:"log_event_time"`
	CatalogVersion *int    `json:"catalog_version"`
	ReviewDecision *string `json:"review_decision"`
	RowHash        string  `json:"row_hash"`
}

// SnapshotCollection là bằng chứng thu gom hiện hành (revision lớn nhất) tại as_of.
type SnapshotCollection struct {
	EvidenceRef string  `json:"evidence_ref"`
	Revision    int64   `json:"revision"`
	PlotRef     string  `json:"plot_ref"`
	SeasonRef   *string `json:"season_ref"`
	CollectedOn string  `json:"collected_on"`
	Bucket      string  `json:"collected_kg_bucket"`
	BucketRule  string  `json:"bucket_rule_version"`
	RowHash     string  `json:"row_hash"`
}

// SnapshotComparison là kết quả đối chiếu hiện hành tại as_of.
type SnapshotComparison struct {
	ID           string  `json:"id"`
	EvidenceID   string  `json:"evidence_id"`
	LogRef       string  `json:"log_ref"`
	LogID        string  `json:"log_id"`
	SeasonRef    string  `json:"season_ref"`
	Result       string  `json:"result"`
	Note         *string `json:"note"`
	ComparedBy   string  `json:"compared_by"`
	EventTime    string  `json:"event_time"`
	RowHash      string  `json:"row_hash"`
	SupersedesID *string `json:"supersedes_id"`
}

// SnapshotEstimate là ước tính mới nhất của thửa/vụ tại as_of.
type SnapshotEstimate struct {
	ID                   string          `json:"id"`
	SeasonRef            string          `json:"season_ref"`
	PlotRef              string          `json:"plot_ref"`
	RuleVersion          string          `json:"rule_version"`
	InputSHA256          string          `json:"input_sha256"`
	CountedTonnes        string          `json:"counted_tonnes"`
	AvoidedBurningKgCO2e string          `json:"avoided_burning_kgco2e"`
	TransportKgCO2e      *string         `json:"transport_kgco2e"`
	TransportStatus      string          `json:"transport_status"`
	Inputs               []EstimateInput `json:"inputs"`
	Params               []ParamUse      `json:"params"`
	Official             bool            `json:"official"`
	MethodVersionID      *string         `json:"method_version_id"`
	RowHash              string          `json:"row_hash"`
}

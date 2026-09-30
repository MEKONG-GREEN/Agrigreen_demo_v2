package domain

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ---------------------------------------------------------------------------
// Cảnh báo MRV (G3). Quy tắc thuần, tất định; tham số đến từ masterdata có phiên bản.

// Khóa trường của danh mục mẫu pest_disaster (seed, tạm chờ C-1).
const (
	FieldPestCategory = "category"         // enum loại sâu bệnh / thiên tai
	FieldPestSeverity = "severity_level"   // integer 1–5 (cấp gây hại)
	FieldPestAreaHa   = "affected_area_ha" // decimal (ha)
)

// Khóa trường của danh mục mẫu irrigation_infrastructure: true = hạ tầng đủ cho AWD.
const FieldInfraReady = "infra_ready"

// PestDetail là phần đầu vào đã đánh giá (ghi vào cột detail của sổ cảnh báo).
type PestDetail struct {
	Category     string `json:"category"`
	Severity     int    `json:"severity_level"`
	AreaHa       string `json:"affected_area_ha,omitempty"`
	Listed       bool   `json:"category_listed"`
	BySeverity   bool   `json:"by_severity"`
	ByArea       bool   `json:"by_area"`
	MinSeverity  int    `json:"min_severity"`
	MinAreaHa    string `json:"min_area_ha"`
	CategoryList string `json:"categories"`
}

// PestUrgent đánh giá quy tắc mrv.pest_urgent.v1 trên giá trị minh chứng: "Cảnh báo khẩn" khi
// loại nằm trong danh sách kích hoạt VÀ (cấp gây hại ≥ minSeverity HOẶC diện tích ảnh hưởng ≥
// minArea). Thiếu/ sai kiểu trường → không khẩn (danh mục đã kiểm dạng lúc ghi).
func PestUrgent(values map[string]json.RawMessage, categories []string, minSeverity int, minArea decimal.Decimal) (bool, PestDetail) {
	d := PestDetail{MinSeverity: minSeverity, MinAreaHa: minArea.String(), CategoryList: strings.Join(categories, ",")}
	_ = json.Unmarshal(values[FieldPestCategory], &d.Category)
	var sev json.Number
	if raw, ok := values[FieldPestSeverity]; ok {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		_ = dec.Decode(&sev)
	}
	if n, err := sev.Int64(); err == nil {
		d.Severity = int(n)
	}
	var area *decimal.Decimal
	if raw, ok := values[FieldPestAreaHa]; ok {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			s = strings.TrimSpace(string(raw))
		}
		if a, err := decimal.NewFromString(s); err == nil {
			area = &a
			d.AreaHa = a.String()
		}
	}
	d.Listed = d.Category != "" && slices.Contains(categories, d.Category)
	d.BySeverity = minSeverity > 0 && d.Severity >= minSeverity
	d.ByArea = area != nil && minArea.IsPositive() && area.GreaterThanOrEqual(minArea)
	return d.Listed && (d.BySeverity || d.ByArea), d
}

// SplitList tách tham số danh sách dạng "a, b,c" (bỏ rỗng, bỏ trùng, giữ thứ tự).
func SplitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// AWDNeedsInfrastructure (mrv.awd_infra.v1): nhật ký HTX có mã hoạt động AWD mà thửa/xã chưa có
// minh chứng hạ tầng đạt → cờ bất thường (KN FR-08). Chỉ xét bản nhật ký trở thành hiện hành.
func AWDNeedsInfrastructure(activity string, becameCurrent bool, awdActivities []string, hasInfra bool) bool {
	return becameCurrent && slices.Contains(awdActivities, activity) && !hasInfra
}

// OverdueLevel (mrv.review_overdue.v1): mức leo thang của hồ sơ chờ duyệt vào hệ thống lúc since.
// 0 = chưa quá hạn; 1 = quá SLA (người duyệt); 2 = quá hạn leo thang (quản trị TTKN tỉnh).
// escalateH < slaH được coi bằng slaH (mức 2 không bao giờ tới trước mức 1).
func OverdueLevel(since, now time.Time, slaH, escalateH int) int {
	if slaH <= 0 {
		return 0
	}
	if escalateH < slaH {
		escalateH = slaH
	}
	age := now.Sub(since)
	switch {
	case age >= time.Duration(escalateH)*time.Hour:
		return 2
	case age >= time.Duration(slaH)*time.Hour:
		return 1
	}
	return 0
}

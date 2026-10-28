// Package domain: quy tắc kiểm tra liên biểu và 12 cảnh báo tự động của phòng Chăn nuôi – Thủy sản (W11.43 — D4 CN3,
// BRD CNTS v3.7 FN-13, UR-TINH-08, UR-TINH-13, Mục III.5), vòng đời cảnh báo (Mới → Đã giao → Đang xử lý → Đã đóng;
// quá hạn xử lý → Quá hạn; số liệu sửa hết vi phạm → tự đóng) và cấu hình nhắc nộp theo biểu.
//
// Hàm thuần: không đọc đồng hồ (R-12), không hạ tầng. Số là decimal (R-CODE-01). Ngưỡng truyền vào (tham số có phiên bản
// cnts.* của pf/masterdata — W11.41).
package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// RuleVersion ghi vào cột rule_version của sổ.
const RuleVersion = "pf.extension.cnts_alert/2026.10"

// Department là phòng ban sở hữu cảnh báo.
const Department = "chan_nuoi_thuy_san"

// Mã biểu kỳ Cấp Xã (W11.42, extension/reportcycle/domain) — nhắc lại để gói không phụ thuộc vòng đời báo cáo.
const (
	FormBM01   = "CNTS-BM01"
	FormBM04   = "CNTS-BM04"
	FormBM05I  = "CNTS-BM05-I"
	FormBM05II = "CNTS-BM05-II"
	FormBM17   = "CNTS-BM17"
)

// Forms là các biểu có cấu hình nhắc (thứ tự màn).
var Forms = []struct{ Code, Label string }{
	{FormBM01, "BM-01 Kiểm soát giết mổ hằng ngày"},
	{FormBM04, "BM-04 Nhập liệu tuần chăn nuôi"},
	{FormBM17, "BM-17 Khai thác thủy sản hằng tuần"},
	{FormBM05I, "BM-05 Phần I — Kết quả công việc tuần"},
	{FormBM05II, "BM-05 Phần II — Kế hoạch tuần sau"},
}

// IsForm cho biết code là biểu kỳ CNTS có cấu hình nhắc.
func IsForm(code string) bool {
	return slices.ContainsFunc(Forms, func(f struct{ Code, Label string }) bool { return f.Code == code })
}

// FormLabel trả nhãn biểu (mã nếu không biết).
func FormLabel(code string) string {
	for _, f := range Forms {
		if f.Code == code {
			return f.Label
		}
	}
	return code
}

// Mức cảnh báo.
const (
	LevelUrgent    = "urgent"    // Khẩn
	LevelAttention = "attention" // Cần chú ý
)

// Loại cảnh báo — 12 dòng bảng "Cảnh báo tự động" Mục III.5, cộng trễ nộp liên tiếp (UR-TINH-08, FN-13 AC-06).
const (
	KindHerdChange     = "herd_change"     // 1 Tổng đàn biến động (BM-04)
	KindSlaughterSpike = "slaughter_spike" // 2 KSGM bất thường (BM-01)
	KindSlaughterZero  = "slaughter_zero"  // 3 KSGM bằng 0 khi cơ sở hoạt động (BM-01)
	KindKSGMMismatch   = "ksgm_mismatch"   // 4 KSGM trong BM-04 lệch BM-01 (quy tắc liên biểu số 1)
	KindFishingChange  = "fishing_change"  // 5 Khai thác thủy sản biến động (BM-17)
	KindWaterQuality   = "water_quality"   // 6 Môi trường nước (BM-19)
	KindLowGrowth      = "low_growth"      // 7 Tăng trọng thấp (BM-08, BM-09, BM-19)
	KindMortality      = "mortality"       // 8 Tỷ lệ chết (BM-08, BM-09)
	KindLogLocation    = "log_location"    // 9 Vị trí lần ghi
	KindLogOverdue     = "log_overdue"     // 10 Quá hạn ghi nhật ký
	KindVaccineOverdue = "vaccine_overdue" // 11 Mũi tiêm quá hạn (BM-07; quy tắc liên biểu số 7)
	KindOutbreak       = "outbreak"        // 12 Ổ dịch mới (BM-10), báo nghi dịch đã xác nhận
	KindLateStreak     = "late_streak"     // trễ nộp liên tiếp (UR-TINH-08, FN-13 AC-06)
)

// Người nhận theo bảng Mục III.5 (giải ra tài khoản ở tầng ứng dụng).
const (
	RecipFormOwner   = "form_owner"   // người phụ trách biểu (cờ nhiệm vụ phu_trach_bieu)
	RecipSubmitter   = "submitter"    // người nhập
	RecipCommune     = "commune"      // cán bộ xã phụ trách xã
	RecipManager     = "manager"      // Lãnh đạo phòng
	RecipProvince    = "province"     // cán bộ Cấp Tỉnh của phòng (proxy "cán bộ tỉnh phụ trách dịch")
	RecipProgramLead = "program_lead" // chủ nhiệm chương trình (W11.47/W11.48)
	RecipSiteOfficer = "site_officer" // cán bộ theo dõi điểm (W11.47/W11.48)
	RecipFarmer      = "farmer"       // nông hộ (W11.55)
)

// KindInfo mô tả một loại cảnh báo.
type KindInfo struct {
	Code  string
	No    int // số dòng ở bảng Mục III.5 (0 = bổ sung)
	Label string
	Level string // mức mặc định
	// LevelConfigurable: Cấp Tỉnh đổi mức sang Khẩn được (dòng 6 "mức Khẩn do Cấp Tỉnh cấu hình").
	LevelConfigurable bool
	Recipients        []string
	// Source: nơi sinh cảnh báo — "forms" (bài nộp biểu kỳ, W11.43 chạy), "outbreak" (ổ dịch, W11.43 chạy), "job" (trễ
	// liên tiếp, W11.43 chạy), "log" (nhật ký BM-08/09/19, W11.48 chạy dòng 6–9), "schedule" (lịch tiêm BM-07 — job
	// extension_cnts_alert chạy dòng 11 từ BF-CN-VACC #168).
	Source string
}

// Kinds là bảng loại cảnh báo (thứ tự Mục III.5).
var Kinds = []KindInfo{
	{KindHerdChange, 1, "Tổng đàn biến động", LevelAttention, false, []string{RecipFormOwner}, "forms"},
	{KindSlaughterSpike, 2, "KSGM bất thường", LevelAttention, false, []string{RecipFormOwner}, "forms"},
	{KindSlaughterZero, 3, "KSGM bằng 0 khi cơ sở hoạt động", LevelAttention, false, []string{RecipSubmitter}, "forms"},
	{KindKSGMMismatch, 4, "KSGM trong BM-04 lệch BM-01", LevelAttention, false, []string{RecipCommune, RecipFormOwner}, "forms"},
	{KindFishingChange, 5, "Khai thác thủy sản biến động", LevelAttention, false, []string{RecipFormOwner}, "forms"},
	{KindWaterQuality, 6, "Môi trường nước", LevelAttention, true, []string{RecipProgramLead, RecipSiteOfficer}, "log"},
	{KindLowGrowth, 7, "Tăng trọng thấp", LevelAttention, false, []string{RecipProgramLead}, "log"},
	{KindMortality, 8, "Tỷ lệ chết", LevelUrgent, false, []string{RecipProgramLead, RecipSiteOfficer}, "log"},
	{KindLogLocation, 9, "Vị trí lần ghi", LevelAttention, false, []string{RecipProgramLead}, "log"},
	{KindLogOverdue, 10, "Quá hạn ghi nhật ký", LevelAttention, false, []string{RecipSiteOfficer, RecipProgramLead}, "log"},
	{KindVaccineOverdue, 11, "Mũi tiêm quá hạn", LevelAttention, false, []string{RecipSiteOfficer, RecipFarmer}, "schedule"},
	{KindOutbreak, 12, "Ổ dịch mới", LevelUrgent, false, []string{RecipManager, RecipProvince, RecipProgramLead}, "outbreak"},
	{KindLateStreak, 0, "Trễ nộp liên tiếp", LevelAttention, false, []string{RecipFormOwner, RecipManager}, "job"},
}

// KindOf trả mô tả loại (ok = false nếu không biết).
func KindOf(code string) (KindInfo, bool) {
	for _, k := range Kinds {
		if k.Code == code {
			return k, true
		}
	}
	return KindInfo{}, false
}

// Trạng thái cảnh báo.
const (
	StatusNew        = "new"
	StatusAssigned   = "assigned"
	StatusInProgress = "in_progress"
	StatusClosed     = "closed"
)

// Sự việc trong sổ cảnh báo.
const (
	ActionRaised     = "raised"
	ActionAssigned   = "assigned"
	ActionStarted    = "started"
	ActionClosed     = "closed"
	ActionAutoClosed = "auto_closed"
	ActionOverdue    = "overdue"
)

// ErrTransition: sự việc không hợp lệ ở trạng thái hiện tại.
var ErrTransition = errors.New("trạng thái cảnh báo không cho phép thao tác này")

// MinResultLen là độ dài tối thiểu của kết quả xử lý khi đóng.
const MinResultLen = 10

// Transition tính trạng thái sau sự việc a:
//   - assigned: mọi trạng thái chưa đóng (giao lại được);
//   - started: chỉ khi Đã giao;
//   - closed, auto_closed: mọi trạng thái chưa đóng;
//   - overdue: không đổi trạng thái (cờ Quá hạn).
func Transition(cur, a string) (string, error) {
	if cur == StatusClosed {
		return "", ErrTransition
	}
	switch a {
	case ActionAssigned:
		return StatusAssigned, nil
	case ActionStarted:
		if cur != StatusAssigned {
			return "", ErrTransition
		}
		return StatusInProgress, nil
	case ActionClosed, ActionAutoClosed:
		return StatusClosed, nil
	case ActionOverdue:
		return cur, nil
	}
	return "", ErrTransition
}

// DedupKey: "mỗi đối tượng, loại, kỳ chỉ một cảnh báo" (Mục V). scope là khóa kỳ (hoặc mã ổ dịch, ngày ghi…).
func DedupKey(kind, subject, scope string) string {
	k := kind + ":" + subject
	if scope != "" {
		k += ":" + scope
	}
	if len(k) > 300 {
		k = k[:300]
	}
	return k
}

// ---------------------------------------------------------------------------
// Hàm đánh giá (công thức Mục III.5)
// ---------------------------------------------------------------------------

// Field là một trường số của biểu được so sánh.
type Field struct{ Key, Label string }

// Trường so sánh của từng biểu.
var (
	// HerdFields: CN-01 tổng đàn từng loài (BM-04).
	HerdFields = []Field{{"tong_dan_heo", "Heo"}, {"tong_dan_de", "Dê"}, {"tong_dan_trau", "Trâu"}, {"tong_dan_bo", "Bò"},
		{"tong_dan_vit", "Vịt"}, {"tong_dan_ga", "Gà"}, {"tong_dan_cho_meo", "Chó mèo"}, {"tong_dan_yen", "Yến (nhà)"}}
	// SlaughterFields: số con KSGM từng loài (BM-01).
	SlaughterFields = []Field{{"trau_bo", "Trâu bò"}, {"heo", "Heo"}, {"gia_cam", "Gia cầm"}, {"de", "Dê"}}
	// FishingFields: TS-01…04 sản lượng khai thác từng nhóm (BM-17).
	FishingFields = []Field{{"ca", "Cá các loại"}, {"tom", "Tôm các loại"}, {"muc", "Mực"}, {"khac", "Thủy sản khác"}}
)

// KSGMGroups: CN-06 của BM-04 ↔ tổng BM-01 các cơ sở trong xã (quy tắc liên biểu số 1). Gia súc = trâu bò + heo + dê.
var KSGMGroups = []struct {
	Key, Label string
	Sum        []string
}{
	{"ksgm_gia_suc", "KSGM gia súc", []string{"trau_bo", "heo", "de"}},
	{"ksgm_gia_cam", "KSGM gia cầm", []string{"gia_cam"}},
}

// Finding là một vi phạm của một trường (một loài, một nhóm).
type Finding struct {
	Field    string           `json:"field"`
	Label    string           `json:"label"`
	Current  decimal.Decimal  `json:"current"`
	Compare  *decimal.Decimal `json:"compare,omitempty"`    // kỳ gần nhất / trung bình / tổng BM-01
	Change   *decimal.Decimal `json:"change_pct,omitempty"` // % biến động
	Reason   string           `json:"reason"`
	NewEntry bool             `json:"new_entry,omitempty"` // "Phát sinh mới" (kỳ gần nhất = 0, kỳ này > 0)
}

// Num đọc số của trường (rỗng/sai → 0, ok = false).
func Num(values map[string]string, key string) (decimal.Decimal, bool) {
	s := strings.TrimSpace(values[key])
	if s == "" {
		return decimal.Zero, false
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, false
	}
	return d, true
}

var hundred = decimal.NewFromInt(100)

func fmtNum(d decimal.Decimal) string { return d.String() }

// ChangeFindings: tăng hoặc giảm QUÁ pct % so kỳ gần nhất (dòng 1 tổng đàn ±15 %, dòng 5 khai thác ±50 %). prev = nil
// (không có kỳ gần nhất) → không cảnh báo. Kỳ gần nhất = 0, kỳ này > 0 → "Phát sinh mới".
func ChangeFindings(cur, prev map[string]string, fields []Field, pct decimal.Decimal) []Finding {
	if prev == nil {
		return nil
	}
	var out []Finding
	for _, f := range fields {
		c, okc := Num(cur, f.Key)
		p, okp := Num(prev, f.Key)
		if !okc || !okp {
			continue
		}
		pp := p
		if p.IsZero() {
			if c.IsPositive() {
				out = append(out, Finding{Field: f.Key, Label: f.Label, Current: c, Compare: &pp, NewEntry: true, Reason: "Phát sinh mới (kỳ gần nhất = 0)"})
			}
			continue
		}
		ch := c.Sub(p).Div(p).Mul(hundred).Round(1)
		if ch.Abs().GreaterThan(pct) {
			dir := "tăng"
			if ch.IsNegative() {
				dir = "giảm"
			}
			out = append(out, Finding{Field: f.Key, Label: f.Label, Current: c, Compare: &pp, Change: &ch,
				Reason: fmt.Sprintf("%s %s%% so kỳ gần nhất (%s → %s), ngưỡng %s%%", dir, ch.Abs().String(), fmtNum(p), fmtNum(c), pct.String())})
		}
	}
	return out
}

// SlaughterSpike (dòng 2): số trong ngày > factor × trung bình các ngày có báo cáo trong 4 tuần trước; dưới minDays
// ngày có số liệu → không cảnh báo. history là số liệu của các NGÀY CÓ BÁO CÁO (bài không phát sinh không tính).
func SlaughterSpike(cur map[string]string, history []map[string]string, factor decimal.Decimal, minDays int) []Finding {
	if len(history) < minDays || len(history) == 0 {
		return nil
	}
	n := decimal.NewFromInt(int64(len(history)))
	var out []Finding
	for _, f := range SlaughterFields {
		c, ok := Num(cur, f.Key)
		if !ok || !c.IsPositive() {
			continue
		}
		sum := decimal.Zero
		for _, h := range history {
			v, _ := Num(h, f.Key)
			sum = sum.Add(v)
		}
		avg := sum.Div(n).Round(2)
		if c.GreaterThan(avg.Mul(factor)) {
			a := avg
			out = append(out, Finding{Field: f.Key, Label: f.Label, Current: c, Compare: &a,
				Reason: fmt.Sprintf("%s con > %s × trung bình %s con/ngày (%d ngày có báo cáo trong 4 tuần trước)", fmtNum(c), factor.String(),
					a.String(), len(history))})
		}
	}
	return out
}

// SlaughterZero (dòng 3): mọi loài = 0 và cơ sở Hoạt động (bài không phải "không hoạt động").
func SlaughterZero(cur map[string]string, nilReport bool) bool {
	if nilReport {
		return false
	}
	for _, f := range SlaughterFields {
		v, ok := Num(cur, f.Key)
		if !ok || !v.IsZero() {
			return false
		}
	}
	return true
}

// KSGMMismatch (dòng 4, quy tắc liên biểu số 1): CN-06 của BM-04 ≠ tổng BM-01 của các cơ sở trong xã trong tuần.
// bm01 rỗng (xã chưa có bài BM-01 nào trong tuần) → không so.
func KSGMMismatch(bm04 map[string]string, bm01 []map[string]string) []Finding {
	if bm04 == nil || len(bm01) == 0 {
		return nil
	}
	var out []Finding
	for _, g := range KSGMGroups {
		c, ok := Num(bm04, g.Key)
		if !ok {
			continue
		}
		sum := decimal.Zero
		for _, d := range bm01 {
			for _, k := range g.Sum {
				v, _ := Num(d, k)
				sum = sum.Add(v)
			}
		}
		if !c.Equal(sum) {
			s := sum
			out = append(out, Finding{Field: g.Key, Label: g.Label, Current: c, Compare: &s,
				Reason: fmt.Sprintf("BM-04 ghi %s con, tổng BM-01 các cơ sở trong xã %s con", fmtNum(c), fmtNum(sum))})
		}
	}
	return out
}

// IndicatorOutOfRange (dòng 6): chỉ tiêu môi trường ngoài [min, max] (pH 7,5–8,5; oxy ≥ 4 mg/l — min/max nil = không chặn).
func IndicatorOutOfRange(label string, v decimal.Decimal, min, max *decimal.Decimal) (Finding, bool) {
	switch {
	case min != nil && v.LessThan(*min):
		m := *min
		return Finding{Field: label, Label: label, Current: v, Compare: &m, Reason: fmt.Sprintf("%s %s dưới ngưỡng %s", label, v.String(), m.String())}, true
	case max != nil && v.GreaterThan(*max):
		m := *max
		return Finding{Field: label, Label: label, Current: v, Compare: &m, Reason: fmt.Sprintf("%s %s trên ngưỡng %s", label, v.String(), m.String())}, true
	}
	return Finding{}, false
}

// LowGrowth (dòng 7): trọng lượng dưới chuẩn của mẫu; mẫu chưa có chuẩn → chỉ cảnh báo khi không tăng so lần cân trước.
func LowGrowth(weight decimal.Decimal, standard, previous *decimal.Decimal) (Finding, bool) {
	if standard != nil {
		if weight.LessThan(*standard) {
			s := *standard
			return Finding{Field: "weight", Label: "Trọng lượng", Current: weight, Compare: &s,
				Reason: fmt.Sprintf("trọng lượng %s dưới chuẩn %s", weight.String(), s.String())}, true
		}
		return Finding{}, false
	}
	if previous != nil && !weight.GreaterThan(*previous) {
		p := *previous
		return Finding{Field: "weight", Label: "Trọng lượng", Current: weight, Compare: &p,
			Reason: fmt.Sprintf("trọng lượng %s không tăng so lần cân trước %s", weight.String(), p.String())}, true
	}
	return Finding{}, false
}

// MortalityExceeded (dòng 8): tỷ lệ chết (dead / herd × 100) vượt ngưỡng của mẫu (Cấp Tỉnh cấu hình; nil = chưa có →
// không cảnh báo).
func MortalityExceeded(dead, herd int64, maxPct *decimal.Decimal) (Finding, bool) {
	if maxPct == nil || herd <= 0 {
		return Finding{}, false
	}
	r := decimal.NewFromInt(dead).Div(decimal.NewFromInt(herd)).Mul(hundred).Round(2)
	if r.GreaterThan(*maxPct) {
		m := *maxPct
		return Finding{Field: "mortality", Label: "Tỷ lệ chết", Current: r, Compare: &m,
			Reason: fmt.Sprintf("tỷ lệ chết %s%% vượt ngưỡng %s%% — cân nhắc \"Báo nghi dịch\"", r.String(), m.String())}, true
	}
	return Finding{}, false
}

// LocationIssue (dòng 9): lần ghi cách điểm quá maxDist m hoặc sai số GPS quá maxAcc m; điểm chưa có tọa độ → báo chủ
// nhiệm cập nhật (distance = nil).
func LocationIssue(distance, accuracy *decimal.Decimal, maxDist, maxAcc decimal.Decimal) (Finding, bool) {
	switch {
	case distance == nil:
		return Finding{Field: "location", Label: "Vị trí", Reason: "điểm mô hình chưa có tọa độ — chủ nhiệm cập nhật tọa độ điểm"}, true
	case distance.GreaterThan(maxDist):
		m := maxDist
		return Finding{Field: "distance_m", Label: "Khoảng cách tới điểm", Current: *distance, Compare: &m,
			Reason: fmt.Sprintf("cách điểm %s m (> %s m)", distance.String(), m.String())}, true
	case accuracy != nil && accuracy.GreaterThan(maxAcc):
		m := maxAcc
		return Finding{Field: "accuracy_m", Label: "Sai số GPS", Current: *accuracy, Compare: &m,
			Reason: fmt.Sprintf("sai số GPS %s m (> %s m)", accuracy.String(), m.String())}, true
	}
	return Finding{}, false
}

// LogOverdue (dòng 10): quá tần suất ghi của mẫu thêm grace ngày (ngày, giờ Việt Nam đã quy về 00:00).
func LogOverdue(lastLog, today time.Time, frequencyDays, graceDays int) bool {
	if frequencyDays <= 0 {
		return false
	}
	return today.After(lastLog.AddDate(0, 0, frequencyDays+graceDays))
}

// VaccineOverdue (dòng 11, quy tắc liên biểu số 7): quá ngày dự kiến mà chưa có ngày tiêm thực tế.
func VaccineOverdue(planned time.Time, actual *time.Time, today time.Time) bool {
	return actual == nil && today.After(planned)
}

// LateStreak đếm số kỳ trễ liên tiếp tính từ kỳ mới nhất (late[0] = kỳ mới nhất): trễ = chưa gửi khi quá hạn hoặc gửi sau hạn.
func LateStreak(late []bool) int {
	n := 0
	for _, l := range late {
		if !l {
			break
		}
		n++
	}
	return n
}

// Summary là một dòng mô tả ngắn từ các vi phạm (tiêu đề / nội dung thông báo).
func Summary(fs []Finding) string {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		parts = append(parts, f.Label+": "+f.Reason)
	}
	s := strings.Join(parts, "; ")
	if r := []rune(s); len(r) > 280 {
		s = string(r[:279]) + "…"
	}
	return s
}

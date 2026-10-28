package domain

// Điều tra giá thành sản xuất theo vụ (W11.78 — BRD Báo giá v1.3 FN-TI-02.10…02.12, FN-TW-01.04; Mục II.3 bảng 3.4, II.4
// CT-05, CT-06). Quy tắc thuần: trạng thái đợt / phiếu, công thức giá thành, cờ lệch trung vị, chuẩn hóa tên khi nhập tệp,
// đọc số kiểu Việt Nam. Không float (R-CODE-01): mọi số là decimal.

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/shopspring/decimal"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Nhóm cây của biểu mẫu điều tra (BR-02 FN-TI-02.10: cố định theo mẫu điều tra hiện hành).
const (
	CropRice   = "lua"
	CropVeg    = "rau_mau"
	CropMush   = "nam"
	CropFruit  = "cay_an_trai"
	MinFlagged = 5 // CT-06: chỉ tính trung vị khi mặt hàng có ≥ 5 phiếu trong đợt
)

// Crops là 4 nhóm cây theo thứ tự hiển thị.
var Crops = []string{CropRice, CropVeg, CropMush, CropFruit}

// CropNames là tên hiển thị của nhóm cây.
var CropNames = map[string]string{CropRice: "Lúa", CropVeg: "Rau màu", CropMush: "Nấm", CropFruit: "Cây ăn trái"}

// ValidCrop cho biết c là mã nhóm cây.
func ValidCrop(c string) bool { return slices.Contains(Crops, c) }

// Vụ.
var Seasons = map[string]string{"dong_xuan": "Đông Xuân", "he_thu": "Hè Thu", "thu_dong": "Thu Đông", "khac": "Khác"}

// Trạng thái đợt (bảng 3.4).
const (
	RoundDraft      = "draft"      // Nháp
	RoundCollecting = "collecting" // Đang thu thập
	RoundReviewing  = "reviewing"  // Đang rà soát
	RoundClosed     = "closed"     // Đã chốt
)

// RoundState là trạng thái HIỆU LỰC của đợt tại ngày today (BR-03 FN-TI-02.10: hết thời gian thu thập → Đang rà soát; là
// phép chiếu lúc đọc, không job).
func RoundState(stored string, collectTo, today time.Time) string {
	if stored == RoundCollecting && today.After(collectTo) {
		return RoundReviewing
	}
	return stored
}

// Trạng thái phiếu (bảng 3.4).
const (
	FormImported = "imported" // Đã nhập — chờ rà soát
	FormVerify   = "verify"   // Cần xác minh
	FormVerified = "verified" // Đã xác minh
	FormApproved = "approved" // Đã duyệt
	FormRejected = "rejected" // Loại
	FormDeleted  = "deleted"  // Đã xóa (xóa mềm)
)

// FormPending cho biết phiếu còn chờ rà soát / xác minh (chặn "Chốt đợt" — luồng ngoại lệ FN-TI-02.12).
func FormPending(st string) bool { return st == FormImported || st == FormVerify || st == FormVerified }

// CostKeys là 6 khoản chi phí của biểu mẫu (thứ tự hiển thị) và tên.
var CostKeys = []string{"seed", "fertilizer", "pesticide", "labor", "machine", "other"}

// CostNames là tên hiển thị của khoản chi phí.
var CostNames = map[string]string{"seed": "Giống", "fertilizer": "Phân bón", "pesticide": "Thuốc BVTV", "labor": "Nhân công",
	"machine": "Máy móc", "other": "Chi khác"}

// FormFigures là số liệu gốc của một phiếu (đơn vị: chi phí, thu khác đồng cho cả diện tích; sản lượng kg; giá đ/kg).
type FormFigures struct {
	AreaHa      decimal.Decimal
	OutputKg    decimal.Decimal
	SalePrice   decimal.Decimal // giá bán dùng để tính (giá hiệu chỉnh sau xác minh nếu có)
	OtherIncome decimal.Decimal
	Costs       map[string]decimal.Decimal
}

// Calc là kết quả CT-05 của một phiếu.
type Calc struct {
	TotalCost   decimal.Decimal
	CostPerKg   decimal.Decimal // giá thành 1 kg = tổng chi phí / sản lượng
	YieldPerHa  decimal.Decimal // năng suất kg/ha = sản lượng / diện tích
	Revenue     decimal.Decimal // sản lượng × giá bán + thu khác
	Profit      decimal.Decimal // doanh thu − tổng chi phí
	ProfitPerHa decimal.Decimal
	MarginPct   *decimal.Decimal // lợi nhuận / tổng chi phí × 100 (nil khi tổng chi phí 0)
}

// CT05 tính giá thành, lợi nhuận của phiếu (CT-05). Chi phí là tổng cho cả diện tích điều tra nên "giá thành / kg = tổng chi
// phí / năng suất" quy về tổng chi phí / sản lượng; cây ăn trái và mọi nhóm quy đổi theo ha bằng diện tích. Diện tích,
// sản lượng > 0 đã được kiểm khi nhập (tránh chia cho 0).
func CT05(f FormFigures) Calc {
	var c Calc
	for _, k := range CostKeys {
		c.TotalCost = c.TotalCost.Add(f.Costs[k])
	}
	if f.OutputKg.IsPositive() {
		c.CostPerKg = c.TotalCost.DivRound(f.OutputKg, 4)
	}
	if f.AreaHa.IsPositive() {
		c.YieldPerHa = f.OutputKg.DivRound(f.AreaHa, 4)
	}
	c.Revenue = f.OutputKg.Mul(f.SalePrice).Add(f.OtherIncome)
	c.Profit = c.Revenue.Sub(c.TotalCost)
	if f.AreaHa.IsPositive() {
		c.ProfitPerHa = c.Profit.DivRound(f.AreaHa, 4)
	}
	if c.TotalCost.IsPositive() {
		m := c.Profit.Mul(decimal.NewFromInt(100)).DivRound(c.TotalCost, 4)
		c.MarginPct = &m
	}
	return c
}

// Median trả trung vị (nil khi rỗng). Không đổi thứ tự của vs.
func Median(vs []decimal.Decimal) *decimal.Decimal {
	if len(vs) == 0 {
		return nil
	}
	s := slices.Clone(vs)
	slices.SortFunc(s, func(a, b decimal.Decimal) int { return a.Cmp(b) })
	n := len(s)
	m := s[n/2]
	if n%2 == 0 {
		m = s[n/2-1].Add(s[n/2]).DivRound(decimal.NewFromInt(2), 4)
	}
	return &m
}

// Cờ phiếu điều tra (CT-06).
const (
	FlagHigh = "high" // Cao bất thường
	FlagLow  = "low"  // Thấp bất thường
)

// Deviation là trung vị, % lệch và cờ của một phiếu.
type Deviation struct {
	Median *decimal.Decimal
	Pct    *decimal.Decimal
	Flag   string // "" | high | low
}

// CT06 gắn cờ cho giá bán price so với trung vị giá bán cùng mặt hàng trong đợt prices (CT-06): lệch ≥ +ngưỡng → Cao, ≤
// −ngưỡng → Thấp; dưới MinFlagged phiếu → không trung vị, không cờ (luồng ngoại lệ FN-TI-02.12).
func CT06(price decimal.Decimal, prices []decimal.Decimal, thresholdPct decimal.Decimal) Deviation {
	if len(prices) < MinFlagged {
		return Deviation{}
	}
	med := Median(prices)
	if med == nil || !med.IsPositive() {
		return Deviation{}
	}
	pct := price.Sub(*med).Mul(decimal.NewFromInt(100)).DivRound(*med, 4)
	d := Deviation{Median: med, Pct: &pct}
	switch {
	case pct.GreaterThanOrEqual(thresholdPct):
		d.Flag = FlagHigh
	case pct.LessThanOrEqual(thresholdPct.Neg()):
		d.Flag = FlagLow
	}
	return d
}

// ---------------------------------------------------------------------------------------------------------------------
// Chuẩn hóa khi nhập tệp (B3 FN-TI-02.11)
// ---------------------------------------------------------------------------------------------------------------------

var foldT = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// Fold chuẩn hóa tên để so khớp: bỏ dấu, đ → d, chữ thường, gộp khoảng trắng ("DƯA LEO", "dưa  leo" → "dua leo").
func Fold(s string) string {
	out, _, err := transform.String(foldT, strings.TrimSpace(s))
	if err != nil {
		out = s
	}
	out = strings.NewReplacer("đ", "d", "Đ", "d").Replace(out)
	return strings.Join(strings.Fields(strings.ToLower(out)), " ")
}

var communePrefix = regexp.MustCompile(`^(xa|phuong|thi tran|dac khu|tt|p)\.? `)

// FoldCommune chuẩn hóa tên xã: như Fold, bỏ tiền tố "xã", "phường", "thị trấn", "đặc khu".
func FoldCommune(s string) string { return communePrefix.ReplaceAllString(Fold(s), "") }

// ParseCrop đọc nhóm cây từ chữ trong tệp ("Lúa", "Rau màu", "Nấm", "Cây ăn trái", hoặc mã); "" nếu không nhận ra.
func ParseCrop(s string) string {
	f := Fold(s)
	switch f {
	case "lua", "lua gao", "lua_gao":
		return CropRice
	case "rau mau", "rau", "rau_mau", "hoa mau":
		return CropVeg
	case "nam":
		return CropMush
	case "cay an trai", "cay an qua", "an trai", "cay_an_trai", "trai cay":
		return CropFruit
	}
	return ""
}

var (
	reThousandsDot   = regexp.MustCompile(`^\d{1,3}(\.\d{3})+$`)
	reThousandsComma = regexp.MustCompile(`^\d{1,3}(,\d{3})+$`)
)

// ParseNumber đọc số kiểu Việt Nam hoặc kiểu máy ("25.000.000", "1,2", "1.5", "12 000 đ"). ok = false nếu không phải
// số. Có cả '.' và ',' → ký tự xuất hiện sau cùng là dấu thập phân; chỉ một loại và đúng nhóm 3 chữ số → dấu nghìn.
func ParseNumber(s string) (decimal.Decimal, bool) {
	t := strings.TrimSpace(strings.ToLower(s))
	for _, suf := range []string{"vnđ", "vnd", "đồng", "đ", "kg", "ha"} {
		t = strings.TrimSpace(strings.TrimSuffix(t, suf))
	}
	t = strings.NewReplacer(" ", "", " ", "", "_", "").Replace(t)
	if t == "" {
		return decimal.Zero, false
	}
	dot, comma := strings.LastIndex(t, "."), strings.LastIndex(t, ",")
	switch {
	case dot >= 0 && comma >= 0:
		if dot > comma {
			t = strings.ReplaceAll(t, ",", "")
		} else {
			t = strings.ReplaceAll(strings.ReplaceAll(t, ".", ""), ",", ".")
		}
	case dot >= 0 && reThousandsDot.MatchString(t):
		t = strings.ReplaceAll(t, ".", "")
	case comma >= 0 && reThousandsComma.MatchString(t):
		t = strings.ReplaceAll(t, ",", "")
	case comma >= 0:
		t = strings.ReplaceAll(t, ",", ".")
	}
	d, err := decimal.NewFromString(t)
	if err != nil {
		return decimal.Zero, false
	}
	return d, true
}

var phoneSep = strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "", " ", "")

// NormalizePhone đưa SĐT Việt Nam về 84xxxxxxxxx (cùng quy tắc kho định danh); ok = false nếu không hợp lệ. Dùng để
// chống trùng trong tệp; kho định danh tự chống trùng theo chỉ mục mù khi ghi.
func NormalizePhone(s string) (string, bool) {
	p := phoneSep.Replace(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(p, "+84"):
		p = p[3:]
	case strings.HasPrefix(p, "0084"):
		p = p[4:]
	case strings.HasPrefix(p, "84") && len(p) == 11:
		p = p[2:]
	case strings.HasPrefix(p, "0"):
		p = p[1:]
	case len(p) == 9: // ô số của bảng tính mất số 0 đầu
	default:
		return "", false
	}
	if len(p) != 9 || p[0] == '0' {
		return "", false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return "84" + p, true
}

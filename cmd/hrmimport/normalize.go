package main

// Chuẩn hóa giá trị ô của tệp nhân sự cũ (D3 H10): ngày nhiều định dạng, bậc bị Excel đổi thành ngày, hệ số dạng chữ,
// ghi chú trong họ tên, tên phòng viết nhiều kiểu, số điện thoại mất số 0 đầu. Hàm thuần — không I/O, không đồng hồ.
// Thông báo lỗi KHÔNG chứa giá trị của trường dữ liệu cá nhân (họ tên, SĐT, ngày sinh, email, địa chỉ).

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// fold đưa chữ về khóa so khớp: bỏ dấu, đ → d, chữ thường, bỏ mọi ký tự không phải chữ/số ("Phòng HC-TH" → "phonghcth").
func fold(s string) string { return strings.ReplaceAll(tabular.NormalizeHeader(s), "_", "") }

var (
	reDMY     = regexp.MustCompile(`^(\d{1,2})[/.\-](\d{1,2})[/.\-](\d{4})$`)
	reMY      = regexp.MustCompile(`^(\d{1,2})[/.\-](\d{4})$`)
	reYMD     = regexp.MustCompile(`^(\d{4})[/.\-](\d{1,2})[/.\-](\d{1,2})$`)
	reYM      = regexp.MustCompile(`^(\d{4})[/.\-](\d{1,2})$`)
	reYear    = regexp.MustCompile(`^\d{4}$`)
	reSerial  = regexp.MustCompile(`^\d{5,6}(\.0+)?$`)
	reISODate = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	reFrac    = regexp.MustCompile(`^(\d{1,2})\s*/\s*(\d{1,2})$`)
	reInt     = regexp.MustCompile(`^(\d{1,2})(\.0+)?$`)
	reNote    = regexp.MustCompile(`\([^)]*\)|\[[^\]]*\]|[0-9]+`)
	reSpaces  = regexp.MustCompile(`\s+`)
	reCoeff   = regexp.MustCompile(`^[0-9]+([.,][0-9]+)?$`)
)

// excelEpoch là ngày 0 của số ngày Excel (hệ 1900, đã tính lỗi năm nhuận 1900 của Excel).
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// normDate đọc ngày: YYYY-MM-DD (tabular đã đổi ô định dạng ngày), D/M/YYYY, D-M-YYYY, D.M.YYYY, M/YYYY, YYYY/M/D,
// YYYY-MM, YYYY, hoặc số ngày Excel (ô ngày bị định dạng thành số). Trả chuỗi theo mức chính xác (YYYY-MM-DD | YYYY-MM |
// YYYY) — dạng domain.ParseDate nhận; serial = true khi đọc từ số ngày Excel.
func normDate(s string) (out string, serial bool, ok bool) {
	s = strings.TrimSpace(s)
	day := func(y, m, d int) (string, bool) {
		t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
		if m < 1 || m > 12 || d < 1 || t.Day() != d || y < 1900 || y > 2200 {
			return "", false
		}
		return t.Format(time.DateOnly), true
	}
	atoi := func(x string) int { n, _ := strconv.Atoi(x); return n }
	switch {
	case s == "":
		return "", false, false
	case reYMD.MatchString(s):
		m := reYMD.FindStringSubmatch(s)
		out, ok = day(atoi(m[1]), atoi(m[2]), atoi(m[3]))
		return out, false, ok
	case reDMY.MatchString(s):
		m := reDMY.FindStringSubmatch(s)
		out, ok = day(atoi(m[3]), atoi(m[2]), atoi(m[1]))
		return out, false, ok
	case reYM.MatchString(s), reMY.MatchString(s):
		var y, m int
		if x := reYM.FindStringSubmatch(s); x != nil {
			y, m = atoi(x[1]), atoi(x[2])
		} else {
			x = reMY.FindStringSubmatch(s)
			y, m = atoi(x[2]), atoi(x[1])
		}
		if m < 1 || m > 12 || y < 1900 || y > 2200 {
			return "", false, false
		}
		return fmt.Sprintf("%04d-%02d", y, m), false, true
	case reYear.MatchString(s):
		y := atoi(s)
		if y < 1900 || y > 2200 {
			// 4 chữ số ngoài khoảng năm → có thể là số ngày Excel (1900-01-01 + n).
			break
		}
		return s, false, true
	}
	if reSerial.MatchString(s) {
		n := atoi(strings.SplitN(s, ".", 2)[0])
		if n >= 10000 && n <= 109574 { // 1927-05-18 … 2199-12-31 (4 chữ số dễ nhầm với năm — không nhận)
			return excelEpoch.AddDate(0, 0, n).Format(time.DateOnly), true, true
		}
	}
	return "", false, false
}

// normStep đọc bậc: "3", "3.0", "Bậc 3", "3/9" (bậc/số bậc), hoặc ngày ISO khi Excel đã đổi "3/9" thành ngày (03/09 hay
// 09/03 tùy máy) — bậc là số nhỏ hơn của ngày và tháng, số lớn hơn là số bậc của ngạch. of = số bậc đọc được (0 nếu
// không có); fromDate = ô bị Excel đổi thành ngày.
func normStep(s string) (step, of int, fromDate, ok bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(fold(s), "bac") { // "Bậc 3", "bậc 3/9"
		i := strings.IndexAny(s, "0123456789")
		if i < 0 {
			return 0, 0, false, false
		}
		s = s[i:]
	}
	atoi := func(x string) int { n, _ := strconv.Atoi(x); return n }
	switch {
	case reInt.MatchString(s):
		step = atoi(reInt.FindStringSubmatch(s)[1])
	case reFrac.MatchString(s):
		m := reFrac.FindStringSubmatch(s)
		step, of = atoi(m[1]), atoi(m[2])
	case reISODate.MatchString(s):
		m := reISODate.FindStringSubmatch(s)
		mo, d := atoi(m[2]), atoi(m[3])
		step, of, fromDate = min(mo, d), max(mo, d), true
	default:
		return 0, 0, false, false
	}
	if step < 1 || step > 99 || (of != 0 && of < step) {
		return 0, 0, false, false
	}
	return step, of, fromDate, true
}

// normCoeff đọc hệ số dạng chữ: "2,67", "2.67", "HS 2,67", "hệ số: 3,00" → "2.67" (2 chữ số, 0 < x ≤ 99,99).
func normCoeff(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "-") {
		return "", false
	}
	if i := strings.IndexAny(s, "0123456789"); i > 0 {
		s = strings.TrimSpace(s[i:])
	}
	if !reCoeff.MatchString(s) {
		return "", false
	}
	d, err := decimal.NewFromString(strings.ReplaceAll(s, ",", "."))
	if err != nil || !d.IsPositive() || d.GreaterThan(decimal.RequireFromString("99.99")) {
		return "", false
	}
	return d.Round(2).StringFixed(2), true
}

// normName bỏ ghi chú khỏi họ tên ("Trần Hoàng Anh (19 VC)" → "Trần Hoàng Anh", BR-05); stripped = có phần bị bỏ.
func normName(s string) (name string, stripped bool) {
	n := reNote.ReplaceAllString(s, " ")
	n = strings.Trim(reSpaces.ReplaceAllString(n, " "), " -–,;.:")
	return n, n != reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
}

// normPhone đọc SĐT (HS-12: 10 số, giữ số 0 đầu): bỏ khoảng trắng, dấu chấm, gạch; +84/84 → 0; 9 số không có 0 đầu
// (Excel lưu dạng số làm mất 0) → thêm 0 (addedZero).
func normPhone(s string) (phone string, addedZero, ok bool) {
	var b strings.Builder
	for i, r := range strings.TrimSpace(s) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0, r == ' ', r == '.', r == '-', r == '(', r == ')':
		default:
			return "", false, false
		}
	}
	p := b.String()
	switch {
	case len(p) == 11 && strings.HasPrefix(p, "84"):
		p = "0" + p[2:]
	case len(p) == 9 && p[0] != '0':
		p, addedZero = "0"+p, true
	}
	if len(p) != 10 || p[0] != '0' {
		return "", false, false
	}
	return p, addedZero, true
}

// normGender: Nam/Nữ (HS-03) → nam | nu.
func normGender(s string) (string, bool) {
	switch fold(s) {
	case "nam", "m", "male":
		return "nam", true
	case "nu", "f", "female":
		return "nu", true
	}
	return "", false
}

// builtinValues là bảng đổi chữ → mã dựng sẵn cho trường không có danh mục ở máy chủ (mapping ghi đè/bổ sung).
var builtinValues = map[string]map[string]string{
	"type": {"vienchuc": "vien_chuc", "vc": "vien_chuc", "hopdong": "hop_dong", "hopdonglaodong": "hop_dong", "hdld": "hop_dong",
		"hd": "hop_dong", "hdlddvsn": "hop_dong", "canboxa": "can_bo_xa", "canbokhuyennongcapxa": "can_bo_xa", "cbknxa": "can_bo_xa",
		"khuyennongvienxa": "can_bo_xa", "xa": "can_bo_xa"},
	"degree_form": {"chinhquy": "chinh_quy", "cq": "chinh_quy", "taichuc": "tai_chuc", "tc": "tai_chuc", "chuyentu": "tai_chuc",
		"vualamvuahoc": "tai_chuc", "lienthong": "tai_chuc", "tuxa": "tai_chuc"},
}

// lookupValue tra bảng đổi của trường field: mapping trước, dựng sẵn sau. Khóa so bằng fold.
func lookupValue(m *Mapping, field, raw string) (string, bool) {
	k := fold(raw)
	if v, ok := m.values[field][k]; ok {
		return v, true
	}
	v, ok := builtinValues[field][k]
	return v, ok
}

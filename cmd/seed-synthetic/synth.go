package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"strings"
)

// Bộ sinh giá trị tổng hợp (R-95): KHÔNG có người thật. Tên ghép ngẫu nhiên từ
// họ/đệm/tên phổ biến; SĐT nằm trong dải 09990xxxxx (không cấp cho thuê bao
// thật ở kế hoạch đánh số — dấu hiệu nhận ra dữ liệu tổng hợp); địa chỉ là
// "Ấp Tổng Hợp N" để không trùng địa chỉ thật.

// Họ theo tần suất gần đúng ở Nam Bộ (trọng số cộng dồn).
var familyNames = []struct {
	name string
	w    int
}{
	{"Nguyễn", 31}, {"Trần", 11}, {"Lê", 9}, {"Phạm", 6}, {"Huỳnh", 6}, {"Võ", 5}, {"Phan", 4},
	{"Trương", 3}, {"Bùi", 3}, {"Đặng", 3}, {"Đỗ", 2}, {"Ngô", 2}, {"Hồ", 2}, {"Dương", 2},
	{"Lý", 2}, {"Đoàn", 2}, {"Châu", 1}, {"Lâm", 1}, {"Thái", 1}, {"Tô", 1},
}

var (
	middleMale   = []string{"Văn", "Hữu", "Minh", "Thanh", "Quốc", "Công", "Đức"}
	middleFemale = []string{"Thị", "Ngọc", "Thanh", "Kim", "Mỹ", "Bích"}
	givenMale    = []string{"Hùng", "Dũng", "Phúc", "Tài", "Lộc", "Sang", "Tâm", "Bình", "Hải", "Khang", "Nghĩa", "Trí", "Hiếu", "Toàn", "Thắng", "Vinh", "Lợi", "Nhân"}
	givenFemale  = []string{"Lan", "Hoa", "Mai", "Hằng", "Thảo", "Trang", "Nga", "Tuyết", "Hạnh", "Diễm", "Nhung", "Phượng", "Yến", "Loan", "Xuân", "Hồng"}
)

// fakeName sinh họ tên tổng hợp.
func fakeName(r *rand.Rand) string {
	total := 0
	for _, f := range familyNames {
		total += f.w
	}
	n := r.IntN(total)
	fam := familyNames[0].name
	for _, f := range familyNames {
		if n < f.w {
			fam = f.name
			break
		}
		n -= f.w
	}
	if r.IntN(100) < 62 { // chủ hộ nam chiếm đa số
		return fam + " " + middleMale[r.IntN(len(middleMale))] + " " + givenMale[r.IntN(len(givenMale))]
	}
	return fam + " " + middleFemale[r.IntN(len(middleFemale))] + " " + givenFemale[r.IntN(len(givenFemale))]
}

// syntheticPhonePrefix là dải SĐT tổng hợp (10 chữ số, 09990 + 5 chữ số).
const syntheticPhonePrefix = "09990"

// fakePhone trả SĐT tổng hợp thứ i (duy nhất theo i trong một lần chạy).
func fakePhone(i int) string { return fmt.Sprintf("%s%05d", syntheticPhonePrefix, i%100000) }

// fakeAddress trả địa chỉ tổng hợp trong xã.
func fakeAddress(r *rand.Rand, communeName string) string {
	return fmt.Sprintf("Ấp Tổng Hợp %d, %s", 1+r.IntN(12), communeName)
}

// LatLon là toạ độ WGS84.
type LatLon struct{ Lat, Lon float64 }

// Tâm gần đúng của một số xã/phường của TP Cần Thơ sau sắp xếp 2025 (mã chính
// thức QĐ 19/2025/QĐ-TTg) — toạ độ công khai của trung tâm huyện/thị cũ cùng tên,
// làm tròn 2 chữ số; chỉ dùng để đặt thửa tổng hợp, không phải địa giới.
var communeCenters = map[string]LatLon{
	"31135": {10.03, 105.77}, // Phường Ninh Kiều
	"31168": {10.07, 105.73}, // Phường Bình Thủy
	"31186": {9.99, 105.78},  // Phường Cái Răng
	"31153": {10.11, 105.62}, // Phường Ô Môn
	"31207": {10.27, 105.53}, // Phường Thốt Nốt
	"31261": {10.10, 105.43}, // Xã Cờ Đỏ
	"31299": {9.99, 105.66},  // Xã Phong Điền
	"31258": {10.06, 105.56}, // Xã Thới Lai
	"31232": {10.20, 105.40}, // Xã Vĩnh Thạnh
	"31507": {9.60, 105.97},  // Phường Sóc Trăng
	"31528": {9.77, 105.98},  // Xã Kế Sách
	"31639": {9.61, 106.12},  // Xã Long Phú
	"31567": {9.64, 105.81},  // Xã Mỹ Tú
	"31684": {9.55, 105.98},  // Phường Mỹ Xuyên
	"31732": {9.56, 105.60},  // Phường Ngã Năm
	"31673": {9.52, 106.20},  // Xã Trần Đề
	"31633": {9.62, 106.20},  // Xã Cù Lao Dung
	"31783": {9.33, 105.98},  // Phường Vĩnh Châu
	"31321": {9.78, 105.47},  // Phường Vị Thanh
	"31441": {9.80, 105.56},  // Xã Vị Thủy
	"31471": {9.68, 105.57},  // Phường Long Mỹ
	"31420": {9.80, 105.73},  // Xã Phụng Hiệp
	"31366": {9.92, 105.80},  // Xã Châu Thành (Hậu Giang cũ)
	"31340": {9.82, 105.82},  // Phường Ngã Bảy
}

// Tâm vùng trồng lúa gần đúng của các tỉnh sau sắp xếp 2025 (mã TCTK).
var provinceCenters = map[string]LatLon{
	"91": {10.35, 105.25}, // An Giang (gồm Kiên Giang cũ)
	"82": {10.40, 105.95}, // Đồng Tháp (gồm Tiền Giang cũ)
	"86": {10.05, 106.10}, // Vĩnh Long (gồm Bến Tre, Trà Vinh cũ)
	"92": {9.85, 105.75},  // Cần Thơ (gồm Sóc Trăng, Hậu Giang cũ)
	"96": {9.25, 105.35},  // Cà Mau (gồm Bạc Liêu cũ)
	"80": {10.70, 106.20}, // Tây Ninh (gồm Long An cũ)
}

// communeCenter trả tâm xã: bảng trên, hoặc tâm tỉnh lệch tất định theo mã xã
// (±0,2°) cho xã chưa có trong bảng.
func communeCenter(code, province string) LatLon {
	if c, ok := communeCenters[code]; ok {
		return c
	}
	p, ok := provinceCenters[province]
	if !ok {
		p = LatLon{10.0, 105.8} // giữa ĐBSCL
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(code))
	v := h.Sum64()
	dx := (float64(v&0xffff)/65535.0 - 0.5) * 0.4
	dy := (float64((v>>16)&0xffff)/65535.0 - 0.5) * 0.4
	return LatLon{p.Lat + dy, p.Lon + dx}
}

// Lưới đặt thửa: mỗi thửa một ô 400 m × 400 m quanh tâm xã, thửa nằm trọn trong
// ô (cạnh dài ≤ 350 m) nên các thửa sinh ra không bao giờ chồng lấn nhau.
const (
	cellMeters   = 400.0
	gridCols     = 12
	metersPerDeg = 111_320.0
)

// plotArea trả diện tích thửa (ha) theo phân phối lệch phải 0,3–3 ha, trung vị ~1 ha
// (quy mô nông hộ ĐBSCL).
func plotArea(r *rand.Rand) float64 {
	a := math.Exp(r.NormFloat64()*0.55) * 1.0
	return math.Min(3.0, math.Max(0.3, a))
}

// plotPolygon trả vòng ngoài GeoJSON ([lon, lat], khép kín) của một hình chữ
// nhật dài (tỷ lệ 1,5–4, như thửa ruộng ven kênh), xoay ngẫu nhiên, có rung
// góc nhẹ, đặt ở ô thứ idx quanh tâm xã.
func plotPolygon(r *rand.Rand, center LatLon, idx int, areaHa float64) [][2]float64 {
	col, row := idx%gridCols, idx/gridCols
	// Ô (col,row) tính từ góc tây nam của lưới, lưới căn giữa tâm xã.
	ox := (float64(col) - gridCols/2 + 0.5) * cellMeters
	oy := (float64(row) - 2 + 0.5) * cellMeters
	ratio := 1.5 + r.Float64()*2.5
	long := math.Sqrt(areaHa * 10_000 * ratio)
	short := areaHa * 10_000 / long
	if long > 350 {
		long, short = 350, areaHa*10_000/350
	}
	theta := r.Float64() * math.Pi
	cosT, sinT := math.Cos(theta), math.Sin(theta)
	corners := [][2]float64{{-long / 2, -short / 2}, {long / 2, -short / 2}, {long / 2, short / 2}, {-long / 2, short / 2}}
	latRad := center.Lat * math.Pi / 180
	ring := make([][2]float64, 0, 5)
	for _, c := range corners {
		jx, jy := (r.Float64()-0.5)*2, (r.Float64()-0.5)*2 // rung ±1 m
		x := c[0]*cosT - c[1]*sinT + ox + jx
		y := c[0]*sinT + c[1]*cosT + oy + jy
		lon := center.Lon + x/(metersPerDeg*math.Cos(latRad))
		lat := center.Lat + y/metersPerDeg
		ring = append(ring, [2]float64{round7(lon), round7(lat)})
	}
	return append(ring, ring[0])
}

func round7(v float64) float64 { return math.Round(v*1e7) / 1e7 }

// asciiSlug bỏ dấu tiếng Việt cho mã hiển thị (vd tên HTX → mã ngắn).
func asciiSlug(s string) string {
	repl := strings.NewReplacer(
		"à", "a", "á", "a", "ả", "a", "ã", "a", "ạ", "a", "ă", "a", "ằ", "a", "ắ", "a", "ẳ", "a", "ẵ", "a", "ặ", "a",
		"â", "a", "ầ", "a", "ấ", "a", "ẩ", "a", "ẫ", "a", "ậ", "a", "đ", "d", "è", "e", "é", "e", "ẻ", "e", "ẽ", "e",
		"ẹ", "e", "ê", "e", "ề", "e", "ế", "e", "ể", "e", "ễ", "e", "ệ", "e", "ì", "i", "í", "i", "ỉ", "i", "ĩ", "i",
		"ị", "i", "ò", "o", "ó", "o", "ỏ", "o", "õ", "o", "ọ", "o", "ô", "o", "ồ", "o", "ố", "o", "ổ", "o", "ỗ", "o",
		"ộ", "o", "ơ", "o", "ờ", "o", "ớ", "o", "ở", "o", "ỡ", "o", "ợ", "o", "ù", "u", "ú", "u", "ủ", "u", "ũ", "u",
		"ụ", "u", "ư", "u", "ừ", "u", "ứ", "u", "ử", "u", "ữ", "u", "ự", "u", "ỳ", "y", "ý", "y", "ỷ", "y", "ỹ", "y", "ỵ", "y",
	)
	s = repl.Replace(strings.ToLower(s))
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == ' ' || c == '-':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

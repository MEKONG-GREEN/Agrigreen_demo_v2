package migrator

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Hai dạng tên tệp migration (WH-B, docs/dev/parallel-packages.md):
//
//   - SchemeLegacy:    NNNNNN_<tên>.up.sql — số tuần tự mỗi cụm, cấp tập trung. Chỉ còn cho tệp ĐÃ có trên nhánh
//     tích hợp (origin/dev); không cấp thêm.
//   - SchemeTimestamp: YYYYMMDDHHMMSS_<cụm>_<module>_<tên>.up.sql — mốc UTC lúc tạo tệp, không cần cấp số. Phiên
//     bản 14 chữ số luôn lớn hơn mọi số 6 chữ số nên mọi tệp mới xếp sau tệp cũ ở cả golang-migrate (v1, sắp theo
//     số) lẫn bộ chạy v2 (thứ tự topo giữ thứ tự số trong module). Sổ đã áp không đổi.
//
// Mốc thời gian không chống được trường hợp hai gói song song gộp ngược thứ tự mốc: golang-migrate bỏ qua im lặng tệp
// có số nhỏ hơn số đã áp. tools/migratecheck -base chặn việc đó (tệp mới phải lớn hơn số lớn nhất trên nhánh gốc);
// gói gộp sau chỉ đổi tên tệp của mình sang mốc mới (`go run ./tools/migratecheck -new <cụm>_<module>_<tên>`).
type Scheme int

// Các dạng tên.
const (
	SchemeLegacy Scheme = iota + 1
	SchemeTimestamp
)

// TimestampLayout là định dạng phiên bản của SchemeTimestamp (UTC).
const TimestampLayout = "20060102150405"

var fileNameRe = regexp.MustCompile(`^(\d{6}|\d{14})_([a-z0-9_]+)\.up\.sql$`)

// Name là tên tệp migration đã tách.
type Name struct {
	Version int64
	Stem    string // phần sau số, không có .up.sql
	Scheme  Scheme
}

// ParseName tách tên tệp (chỉ phần base). ok=false nếu không đúng một trong hai dạng.
func ParseName(base string) (Name, bool) {
	m := fileNameRe.FindStringSubmatch(base)
	if m == nil {
		return Name{}, false
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return Name{}, false
	}
	s := SchemeLegacy
	if len(m[1]) == 14 {
		s = SchemeTimestamp
	}
	return Name{Version: v, Stem: m[2], Scheme: s}, true
}

// Time trả mốc UTC của tên SchemeTimestamp; lỗi nếu mốc không phải ngày giờ hợp lệ (vd tháng 13).
func (n Name) Time() (time.Time, error) {
	if n.Scheme != SchemeTimestamp {
		return time.Time{}, fmt.Errorf("không phải tên dạng mốc thời gian")
	}
	s := strconv.FormatInt(n.Version, 10)
	t, err := time.ParseInLocation(TimestampLayout, s, time.UTC)
	if err != nil || t.Format(TimestampLayout) != s {
		return time.Time{}, fmt.Errorf("mốc %s không phải ngày giờ UTC hợp lệ (YYYYMMDDHHMMSS)", s)
	}
	return t, nil
}

// NewFileName trả tên tệp mới dạng SchemeTimestamp cho stem (vd "pf_price_approval") tại thời điểm now (đổi sang UTC).
func NewFileName(now time.Time, stem string) string {
	return now.UTC().Format(TimestampLayout) + "_" + stem + ".up.sql"
}

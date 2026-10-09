package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Stale: quan sát quá hạn dùng tại now — now > asOf + maxAge (đúng mốc asOf + maxAge vẫn còn tươi). maxAge ≤ 0 coi
// như đã quá hạn (không có hạn dùng thì không được dùng như dữ liệu tươi).
func Stale(asOf time.Time, maxAge time.Duration, now time.Time) bool {
	if maxAge <= 0 {
		return true
	}
	return now.After(asOf.Add(maxAge))
}

// EffectiveAsOf: feed là dòng trạng thái (upsert/retract theo entity_key) — bản ghi chưa bị thay hay rút vẫn đúng tới
// as_of của lần đọc tới đầu feed gần nhất (feedAsOf, nil = chưa đọc tới đầu / đã thu hồi). Trả mốc muộn hơn.
func EffectiveAsOf(asOf time.Time, feedAsOf *time.Time) time.Time {
	if feedAsOf != nil && feedAsOf.After(asOf) {
		return *feedAsOf
	}
	return asOf
}

var isoDuration = regexp.MustCompile(`^P(?:([0-9]+)D)?(?:T(?:([0-9]+)H)?(?:([0-9]+)M)?(?:([0-9]+)S)?)?$`)

// ParseMaxAge đọc freshness.max_age của hợp đồng tích hợp (ISO 8601 dạng PnD / PTnHnMnS — không năm, tháng).
func ParseMaxAge(s string) (time.Duration, error) {
	m := isoDuration.FindStringSubmatch(s)
	if m == nil || s == "P" || s == "PT" {
		return 0, fmt.Errorf("max_age %q không hỗ trợ", s)
	}
	var d time.Duration
	for i, unit := range []time.Duration{24 * time.Hour, time.Hour, time.Minute, time.Second} {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.ParseInt(m[i+1], 10, 64)
		if err != nil || n > 1<<20 {
			return 0, fmt.Errorf("max_age %q quá lớn", s)
		}
		d += time.Duration(n) * unit
	}
	if d <= 0 {
		return 0, fmt.Errorf("max_age %q phải > 0", s)
	}
	return d, nil
}

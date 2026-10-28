package domain

// BF-CN-VACC (#168, CN-09): bảng BM-07 (hàng = hộ, cột = vaccine × lần) và đồng bộ ngày tiêm ghi ở nhật ký NC-11 sang lịch
// tiêm (quy tắc liên biểu số 7). Hàm thuần: không đọc đồng hồ, không hạ tầng.

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// MaxMatrixRows giới hạn số lứa (hàng) của một bảng/tệp BM-07 và một lần đọc danh sách lứa.
const MaxMatrixRows = 500

// MatrixColumn là một cột vaccine × lần của bảng BM-07.
type MatrixColumn struct {
	Vaccine string
	Shot    int
	AgeDays int // ngày tuổi nhỏ nhất của mũi trong các lứa (thứ tự cột)
}

// Key là "<vaccine>/<lần>".
func (c MatrixColumn) Key() string { return fmt.Sprintf("%s/%d", c.Vaccine, c.Shot) }

// MatrixColumns gom các mũi của mọi lứa thành cột: mỗi vaccine một nhóm (thứ tự theo ngày tuổi sớm nhất của vaccine), trong
// nhóm theo lần. Lứa cùng loại mô hình nhưng khác phiên bản quy trình vẫn chung một bảng.
func MatrixColumns(batches [][]Dose) []MatrixColumn {
	cols := map[string]*MatrixColumn{}
	first := map[string]int{} // vaccine → ngày tuổi sớm nhất
	for _, ds := range batches {
		for _, d := range ds {
			k := d.Key()
			if c, ok := cols[k]; !ok {
				cols[k] = &MatrixColumn{Vaccine: d.Vaccine, Shot: d.Shot, AgeDays: d.AgeDays}
			} else if d.AgeDays < c.AgeDays {
				c.AgeDays = d.AgeDays
			}
			if a, ok := first[d.Vaccine]; !ok || d.AgeDays < a {
				first[d.Vaccine] = d.AgeDays
			}
		}
	}
	out := make([]MatrixColumn, 0, len(cols))
	for _, c := range cols {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Vaccine != b.Vaccine {
			if first[a.Vaccine] != first[b.Vaccine] {
				return first[a.Vaccine] < first[b.Vaccine]
			}
			return a.Vaccine < b.Vaccine
		}
		return a.Shot < b.Shot
	})
	return out
}

// Align trả mũi của lứa theo thứ tự cột (nil = lứa không có mũi đó, vd đã qua trước ngày nhận giống).
func Align(cols []MatrixColumn, ds []Dose) []*Dose {
	idx := map[string]int{}
	for i, d := range ds {
		idx[d.Key()] = i
	}
	out := make([]*Dose, len(cols))
	for i, c := range cols {
		if j, ok := idx[c.Key()]; ok {
			d := ds[j]
			out[i] = &d
		}
	}
	return out
}

// DiaryDose là một dòng NC-11 (mục 5 sổ nhật ký): tên vaccine như người ghi gõ, lần (0 = không ghi lần), ngày tiêm.
type DiaryDose struct {
	Vaccine string
	Shot    int
	Date    time.Time
}

// MaxDiaryDoses giới hạn số dòng NC-11 xét trong một lần ghi.
const MaxDiaryDoses = 60

func normName(s string) string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return unicode.IsSpace(r) })
	return strings.Join(f, " ")
}

// MatchDiaryDoses tìm mũi của lịch ứng với các dòng NC-11: vaccine khớp mã hoặc tên danh mục (không phân biệt hoa thường,
// khoảng trắng); lần > 0 khớp đúng lần, lần 0 lấy mũi chưa tiêm sớm nhất của vaccine đó. Chỉ điền mũi CHƯA có ngày thực tế
// (không ghi đè ngày Cấp Xã/nông hộ đã ghi); ngày ngoài [ngày nhận giống, hôm nay] bỏ qua. names: mã vaccine → tên.
func MatchDiaryDoses(cur []Dose, in []DiaryDose, names map[string]string, received, today time.Time) []DoseUpdate {
	if len(in) > MaxDiaryDoses {
		in = in[:MaxDiaryDoses]
	}
	byName := map[string]string{}
	for _, d := range cur {
		byName[normName(d.Vaccine)] = d.Vaccine
		if n := normName(names[d.Vaccine]); n != "" {
			if _, taken := byName[n]; !taken {
				byName[n] = d.Vaccine
			}
		}
	}
	used := map[string]bool{}
	var out []DoseUpdate
	for _, x := range in {
		code, ok := byName[normName(x.Vaccine)]
		if !ok || x.Date.IsZero() || x.Date.Before(received) || x.Date.After(today) {
			continue
		}
		pick := -1
		for i, d := range cur {
			if d.Vaccine != code || d.Actual != nil || used[d.Key()] {
				continue
			}
			if x.Shot > 0 && d.Shot != x.Shot {
				continue
			}
			if pick < 0 || d.Expected.Before(cur[pick].Expected) {
				pick = i
			}
		}
		if pick < 0 {
			continue
		}
		d := cur[pick]
		used[d.Key()] = true
		date := x.Date
		out = append(out, DoseUpdate{Vaccine: d.Vaccine, Shot: d.Shot, Actual: &date})
	}
	return out
}

// DiaryRecord là yêu cầu ghi ngày tiêm từ một lần ghi nhật ký đã gửi sang lịch tiêm của lứa.
type DiaryRecord struct {
	Batch         uuid.UUID
	Household     uuid.UUID
	Entry         uuid.UUID
	EntrySeq      int64
	Actor         uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	At            time.Time
	Today         time.Time
	Doses         []DiaryDose
	Names         map[string]string // mã vaccine → tên (danh mục, đọc ngoài giao dịch)
}

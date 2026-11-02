package domain

// BF-CN-VACC (#168, CN-09): bảng BM-07 (hàng = hộ, cột = vaccine × lần) và đồng bộ ngày tiêm ghi ở nhật ký NC-11 sang lịch
// tiêm (quy tắc liên biểu số 7). Hàm thuần: không đọc đồng hồ, không hạ tầng.

import (
	"fmt"
	"slices"
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
	return MatchDiaryDosesOwned(cur, in, names, received, today, nil)
}

// MatchDiaryDosesOwned như MatchDiaryDoses nhưng giữ ánh xạ dòng NC-11 → mũi ỔN ĐỊNH qua các phiên bản của cùng lần ghi
// (Codex #168: điều chỉnh/gửi lại không được biến cùng một dòng thành mũi mới). Ba lượt, mỗi mũi dùng tối đa một lần:
//  1. dòng trùng vaccine (và lần, nếu > 0) với một mũi ĐÃ có đúng ngày đó → mũi ấy đã ghi, không điền thêm;
//  2. dòng còn lại dùng trước các mũi mà phiên bản trước của CHÍNH lần ghi này đã điền (owned: khóa mũi) — dòng đổi ngày
//     không điền mũi mới, cũng không ghi đè ngày đã có;
//  3. dòng còn lại mới điền mũi chưa tiêm (lần 0: mũi chưa tiêm sớm nhất).
func MatchDiaryDosesOwned(cur []Dose, in []DiaryDose, names map[string]string, received, today time.Time,
	owned map[string]bool) []DoseUpdate {
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
	type row struct {
		code string
		x    DiaryDose
	}
	var rows []row
	for _, x := range in {
		code, ok := byName[normName(x.Vaccine)]
		if !ok || x.Date.IsZero() || x.Date.Before(received) || x.Date.After(today) {
			continue
		}
		rows = append(rows, row{code: code, x: x})
	}
	used := map[string]bool{}
	done := make([]bool, len(rows))
	// pick chọn mũi chưa dùng khớp dòng r thỏa ok, ngày dự kiến sớm nhất.
	pick := func(r row, ok func(Dose) bool) int {
		p := -1
		for i, d := range cur {
			if d.Vaccine != r.code || used[d.Key()] || (r.x.Shot > 0 && d.Shot != r.x.Shot) || !ok(d) {
				continue
			}
			if p < 0 || d.Expected.Before(cur[p].Expected) {
				p = i
			}
		}
		return p
	}
	passes := []func(Dose, DiaryDose) bool{
		func(d Dose, x DiaryDose) bool { return d.Actual != nil && d.Actual.Equal(x.Date) },
		func(d Dose, _ DiaryDose) bool { return d.Actual != nil && owned[d.Key()] },
	}
	for _, ok := range passes {
		for k, r := range rows {
			if done[k] {
				continue
			}
			if p := pick(r, func(d Dose) bool { return ok(d, r.x) }); p >= 0 {
				used[cur[p].Key()], done[k] = true, true
			}
		}
	}
	var out []DoseUpdate
	for k, r := range rows {
		if done[k] {
			continue
		}
		p := pick(r, func(d Dose) bool { return d.Actual == nil })
		if p < 0 {
			continue
		}
		d := cur[p]
		used[d.Key()] = true
		date := r.x.Date
		out = append(out, DoseUpdate{Vaccine: d.Vaccine, Shot: d.Shot, Actual: &date})
	}
	return out
}

// FilledBy trả khóa các mũi mà phiên bản next điền ngày thực tế so với phiên bản prev (prev chưa có ngày).
func FilledBy(prev, next []Dose) map[string]bool {
	had := map[string]bool{}
	for _, d := range prev {
		if d.Actual != nil {
			had[d.Key()] = true
		}
	}
	out := map[string]bool{}
	for _, d := range next {
		if d.Actual != nil && !had[d.Key()] {
			out[d.Key()] = true
		}
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
	// Phạm vi (Codex #168 [P1]): xã của hộ của lần ghi, loại mô hình của điểm của hộ, phạm vi xã của người ghi.
	Commune   string
	ModelType string
	ScopeAll  bool
	Scope     []string
}

// Covers: lịch (xã commune, hộ household, loại mô hình modelType) được đồng bộ từ lần ghi r — lịch của CHÍNH hộ của lần ghi,
// hoặc lịch theo địa bàn cùng xã, cùng loại mô hình với hộ; và xã của lịch trong phạm vi người ghi. Ngoài các trường hợp này
// (lứa nhật ký gắn nhầm/cố ý mã lịch của xã khác) → không ghi.
func (r DiaryRecord) Covers(commune string, household *uuid.UUID, modelType string) bool {
	if !r.ScopeAll && !slices.Contains(r.Scope, commune) {
		return false
	}
	if household != nil {
		return *household == r.Household
	}
	return commune != "" && commune == r.Commune && modelType != "" && modelType == r.ModelType
}

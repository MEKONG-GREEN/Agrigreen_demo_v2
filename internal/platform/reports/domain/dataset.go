package domain

import (
	"github.com/shopspring/decimal"
)

// Value là một ô dữ liệu: văn bản hoặc số decimal (kể cả số nguyên). Null = không có số liệu.
type Value struct {
	Text string
	Dec  *decimal.Decimal
}

// Null cho biết ô rỗng.
func (v Value) Null() bool { return v.Dec == nil && v.Text == "" }

// Txt tạo ô văn bản.
func Txt(s string) Value { return Value{Text: s} }

// Dec tạo ô số từ decimal (nil → rỗng).
func Dec(d *decimal.Decimal) Value {
	if d == nil {
		return Value{}
	}
	x := *d
	return Value{Dec: &x}
}

// D tạo ô số từ decimal.
func D(d decimal.Decimal) Value { return Value{Dec: &d} }

// Int tạo ô số nguyên.
func Int(n int64) Value { d := decimal.NewFromInt(n); return Value{Dec: &d} }

// IntP tạo ô số nguyên từ con trỏ (nil → rỗng).
func IntP(n *int) Value {
	if n == nil {
		return Value{}
	}
	return Int(int64(*n))
}

// Row là một dòng theo khóa cột nguồn.
type Row map[string]Value

// DataColumn là một cột của tệp xuất.
type DataColumn struct {
	Key   string
	Title string
	Kind  Kind
	Scale int32
}

// Meta là một dòng đầu báo cáo (Ngày xuất, Kỳ, Vụ, bộ lọc…).
type Meta struct {
	Label string
	Value string
}

// Dataset là nội dung một tệp xuất — Excel và PDF dựng từ CÙNG tập này, nên hai
// định dạng khớp nhau (FN-10 luồng phụ B4).
type Dataset struct {
	Title       string
	Meta        []Meta
	Columns     []DataColumn
	Rows        [][]Value
	Total       []Value // dòng "Tổng cộng" (nil = không có)
	Provisional bool    // "Số liệu tạm tính"
	Notes       []string
	Empty       string // thông báo khi không có dòng nào
}

// ProvisionalLabel là nhãn chân trang khi số liệu chưa đủ (US-DASH-02 AC-1).
const ProvisionalLabel = "Số liệu tạm tính"

// EmptyMessage là thông báo bảng rỗng (FN-10 luồng ngoại lệ).
const EmptyMessage = "Không có dữ liệu phù hợp với bộ lọc đã chọn"

// Display trả chuỗi hiển thị của ô theo quy ước Việt Nam.
func Display(c DataColumn, v Value) string {
	if v.Dec != nil {
		scale := c.Scale
		if c.Kind == KindInt {
			scale = 0
		}
		return FormatVN(*v.Dec, scale)
	}
	return v.Text
}

// Build dựng Dataset từ các dòng nguồn theo cột của mẫu ở cấp tổng hợp level.
func Build(def Definition, level string, rows []Row, total Row) Dataset {
	return BuildWithSources(def, level, rows, total, Sources)
}
func BuildWithSources(def Definition, level string, rows []Row, total Row, sources map[string]SourceSpec) Dataset {
	src := sources[def.Source]
	ds := Dataset{Title: def.Title, Empty: EmptyMessage}
	for _, c := range def.Columns {
		spec, ok := src.Column(c.Key)
		if !ok || !ColumnVisible(spec, level) {
			continue
		}
		ds.Columns = append(ds.Columns, DataColumn{Key: c.Key, Title: c.Title, Kind: spec.Kind, Scale: spec.Scale})
	}
	pick := func(r Row) []Value {
		out := make([]Value, len(ds.Columns))
		for i, c := range ds.Columns {
			out[i] = r[c.Key]
		}
		return out
	}
	for _, r := range rows {
		ds.Rows = append(ds.Rows, pick(r))
	}
	if total != nil && len(rows) > 0 {
		ds.Total = pick(total)
		if len(ds.Columns) > 0 && ds.Total[0].Dec == nil {
			ds.Total[0] = Txt("Tổng cộng")
		}
	}
	if def.Provisional && def.FormNote != "" {
		ds.Notes = append(ds.Notes, def.FormNote)
	}
	return ds
}

package domain

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// Cấp tổng hợp (TW → Tỉnh → Xã → HTX).
const (
	LevelNational    = "national"
	LevelProvince    = "province"
	LevelCommune     = "commune"
	LevelCooperative = "cooperative"
)

var levelRank = map[string]int{LevelNational: 0, LevelProvince: 1, LevelCommune: 2, LevelCooperative: 3}

// Định dạng tệp.
const (
	FormatXLSX = "xlsx"
	FormatPDF  = "pdf"
)

// Trạng thái phiên bản mẫu.
const (
	TemplateActive  = "active"
	TemplateRetired = "retired"
)

// Kind là kiểu dữ liệu của cột.
type Kind string

// Các kiểu cột.
const (
	KindText    Kind = "text"
	KindInt     Kind = "int"
	KindDecimal Kind = "decimal" // diện tích (ha), số máy cần
	KindPercent Kind = "percent" // giá trị đã là phần trăm (60 = 60%)
)

// ColumnSpec là một cột nguồn dữ liệu cung cấp.
type ColumnSpec struct {
	Key      string
	Title    string // tiêu đề mặc định (theo BRD)
	Kind     Kind
	Scale    int32  // số chữ số thập phân hiển thị
	MinLevel string // cột chỉ có nghĩa từ cấp này trở xuống (vd tên xã: commune)
	MaxLevel string // cột chỉ có nghĩa từ cấp này trở lên (vd mục tiêu tỉnh: province)
}

// SourceSpec mô tả một nguồn dữ liệu của mẫu biểu.
type SourceSpec struct {
	Key          string
	Module       string // extension | mechanization | cropcycle
	Levels       []string
	DefaultLevel string
	NeedsPeriod  bool // kỳ tháng/quý/năm (FR-05)
	UsesSeason   bool // vụ (FR-03, CGH)
	NeedsCompare bool // Vụ A + Vụ B (FN-10 tab 3)
	Columns      []ColumnSpec
}

// Column tìm cột theo khóa.
func (s SourceSpec) Column(key string) (ColumnSpec, bool) {
	for _, c := range s.Columns {
		if c.Key == key {
			return c, true
		}
	}
	return ColumnSpec{}, false
}

// Sources là các nguồn dữ liệu được hỗ trợ.
var nativeSourceSpecs = map[string]SourceSpec{
	// CGH FN-10 — cột đúng "Cấu trúc file xuất Excel/PDF" của BRD v1.3.
	port.SourceCGHSummary: {Key: port.SourceCGHSummary, Module: "mechanization", Levels: []string{LevelProvince, LevelCommune},
		DefaultLevel: LevelProvince, UsesSeason: true,
		Columns: []ColumnSpec{
			{Key: "province_name", Title: "Tỉnh", Kind: KindText, MinLevel: LevelProvince},
			{Key: "commune_name", Title: "Xã/Phường", Kind: KindText, MinLevel: LevelCommune},
			{Key: "farm_stage", Title: "Khâu sản xuất", Kind: KindText},
			{Key: "machine_type", Title: "Chủng loại máy", Kind: KindText},
			{Key: "required", Title: "Nhu cầu (số máy cần)", Kind: KindDecimal, Scale: 2},
			{Key: "capacity", Title: "Năng lực (số máy Hoạt động)", Kind: KindInt},
			{Key: "coverage_pct", Title: "Mức đáp ứng (%)", Kind: KindPercent, Scale: 2},
			{Key: "status", Title: "Trạng thái", Kind: KindText},
		}},
	port.SourceCGHShortage: {Key: port.SourceCGHShortage, Module: "mechanization", Levels: []string{LevelCooperative},
		DefaultLevel: LevelCooperative, UsesSeason: true,
		Columns: []ColumnSpec{
			{Key: "cooperative_code", Title: "Mã HTX", Kind: KindText},
			{Key: "cooperative_name", Title: "Tên HTX", Kind: KindText},
			{Key: "province_name", Title: "Tỉnh", Kind: KindText},
			{Key: "commune_name", Title: "Xã/Phường", Kind: KindText},
			{Key: "farm_stage", Title: "Khâu sản xuất", Kind: KindText},
			{Key: "machine_type", Title: "Chủng loại máy", Kind: KindText},
			{Key: "capacity", Title: "Số máy hiện có (Hoạt động)", Kind: KindInt},
			{Key: "required_whole", Title: "Nhu cầu", Kind: KindInt},
			{Key: "shortage", Title: "Số lượng thiếu", Kind: KindInt},
			{Key: "coverage_pct", Title: "Mức đáp ứng (%)", Kind: KindPercent, Scale: 2},
			{Key: "area_source", Title: "Nguồn diện tích", Kind: KindText},
		}},
	port.SourceCGHCompare: {Key: port.SourceCGHCompare, Module: "mechanization", Levels: []string{LevelProvince, LevelCommune},
		DefaultLevel: LevelProvince, UsesSeason: true, NeedsCompare: true,
		Columns: []ColumnSpec{
			{Key: "unit_name", Title: "Vùng", Kind: KindText},
			{Key: "farm_stage", Title: "Khâu sản xuất", Kind: KindText},
			{Key: "machine_type", Title: "Chủng loại máy", Kind: KindText},
			{Key: "required_a", Title: "Nhu cầu Vụ A", Kind: KindDecimal, Scale: 2},
			{Key: "capacity_a", Title: "Năng lực Vụ A", Kind: KindInt},
			{Key: "coverage_a", Title: "Mức đáp ứng Vụ A (%)", Kind: KindPercent, Scale: 2},
			{Key: "required_b", Title: "Nhu cầu Vụ B", Kind: KindDecimal, Scale: 2},
			{Key: "capacity_b", Title: "Năng lực Vụ B", Kind: KindInt},
			{Key: "coverage_b", Title: "Mức đáp ứng Vụ B (%)", Kind: KindPercent, Scale: 2},
			{Key: "capacity_diff", Title: "Chênh lệch Năng lực (B−A)", Kind: KindInt},
			{Key: "coverage_diff", Title: "Chênh lệch Mức đáp ứng (B−A, điểm %)", Kind: KindPercent, Scale: 2},
		}},
	// HTX US-DASH-02: sản lượng theo HTX × vụ × giống — cùng số liệu và công thức với bảng
	// điều hành (US-DASH-01). Cột chiều (HTX, vụ, giống) có trong mẫu thì tách dòng theo
	// chiều đó, không có thì gộp (HasColumn); số hộ đếm phân biệt trong từng dòng.
	port.SourceHTXProduction: {Key: port.SourceHTXProduction, Module: "cropcycle", Levels: []string{LevelCooperative},
		DefaultLevel: LevelCooperative, UsesSeason: true,
		Columns: []ColumnSpec{
			{Key: "cooperative_code", Title: "Mã HTX", Kind: KindText},
			{Key: "cooperative_name", Title: "HTX", Kind: KindText},
			{Key: "season", Title: "Vụ", Kind: KindText},
			{Key: "variety_name", Title: "Giống lúa", Kind: KindText},
			{Key: "seasons", Title: "Số lô (vụ)", Kind: KindInt},
			{Key: "open_seasons", Title: "Đang canh tác", Kind: KindInt},
			{Key: "completed_seasons", Title: "Đã thu hoạch", Kind: KindInt},
			{Key: "households", Title: "Số nông hộ", Kind: KindInt},
			{Key: "area_ha", Title: "Diện tích (ha)", Kind: KindDecimal, Scale: 2},
			{Key: "harvested_area_ha", Title: "Diện tích đã khai sản lượng (ha)", Kind: KindDecimal, Scale: 2},
			{Key: "expected_t", Title: "Sản lượng dự kiến (tấn)", Kind: KindDecimal, Scale: 3},
			{Key: "actual_t", Title: "Sản lượng thực tế (tấn)", Kind: KindDecimal, Scale: 3},
			{Key: "actual_yield_t_per_ha", Title: "Năng suất thực tế (tấn/ha)", Kind: KindDecimal, Scale: 2},
			{Key: "sop_progress_pct", Title: "Tiến độ SOP (%)", Kind: KindPercent, Scale: 1},
		}},
}

// Sources contains only native capability defaults; applications register their catalog.
var Sources = NativeSources()

// HasColumn cho biết mẫu có cột key.
func (d Definition) HasColumn(key string) bool {
	return slices.ContainsFunc(d.Columns, func(c Column) bool { return c.Key == key })
}

// Column là một cột của mẫu (khóa cột nguồn + tiêu đề hiển thị).
type Column struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// Definition là nội dung một phiên bản mẫu (lưu jsonb, ảnh chụp trên export_job).
type Definition struct {
	Title       string   `json:"title"`
	Source      string   `json:"source"`
	Grouping    string   `json:"grouping"`
	Columns     []Column `json:"columns"`
	Formats     []string `json:"formats"`
	Provisional bool     `json:"provisional_layout"` // bố cục tạm — BRD chưa có mẫu biểu chính thức
	FormNote    string   `json:"form_note"`
}

// Template là một phiên bản mẫu biểu. Version 0 = mẫu hệ thống (trong mã).
type Template struct {
	Key       string
	Version   int
	Status    string
	Def       Definition
	Reason    *string
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

// Builtin cho biết mẫu hệ thống.
func (t Template) Builtin() bool { return t.Version == 0 }

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{2,63}$`)

// Lỗi kiểm mẫu.
var ErrTemplate = errors.New("mẫu biểu không hợp lệ")

// FieldError là lỗi theo trường.
type FieldError struct {
	Field string
	Msg   string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

// ValidKey kiểm mã mẫu.
func ValidKey(k string) bool { return keyRe.MatchString(k) }

// Validate kiểm định nghĩa mẫu: nguồn có hỗ trợ, cấp tổng hợp hợp lệ, cột thuộc nguồn, định dạng.
func (d *Definition) Validate() error { return d.ValidateWithSources(Sources) }

// ValidateWithSources validates against the application's immutable catalog.
func (d *Definition) ValidateWithSources(sources map[string]SourceSpec) error {
	d.Title, d.FormNote = strings.TrimSpace(d.Title), strings.TrimSpace(d.FormNote)
	src, ok := sources[d.Source]
	switch {
	case len([]rune(d.Title)) < 5 || len([]rune(d.Title)) > 200:
		return FieldError{"title", "5–200 ký tự"}
	case !ok:
		return FieldError{"source", "nguồn dữ liệu không được hỗ trợ"}
	case len([]rune(d.FormNote)) > 500:
		return FieldError{"form_note", "tối đa 500 ký tự"}
	case len(d.Columns) == 0 || len(d.Columns) > 40:
		return FieldError{"columns", "1–40 cột"}
	case len(d.Formats) == 0:
		return FieldError{"formats", "chọn xlsx và/hoặc pdf"}
	}
	if d.Grouping == "" {
		d.Grouping = src.DefaultLevel
	}
	if !slices.Contains(src.Levels, d.Grouping) {
		return FieldError{"grouping", "nguồn này chỉ hỗ trợ cấp " + strings.Join(src.Levels, ", ")}
	}
	seen := map[string]bool{}
	for i, c := range d.Columns {
		spec, ok := src.Column(c.Key)
		if !ok {
			return FieldError{fmt.Sprintf("columns[%d].key", i), "cột " + c.Key + " không có trong nguồn " + d.Source}
		}
		if seen[c.Key] {
			return FieldError{fmt.Sprintf("columns[%d].key", i), "trùng cột"}
		}
		seen[c.Key] = true
		d.Columns[i].Title = strings.TrimSpace(c.Title)
		if d.Columns[i].Title == "" {
			d.Columns[i].Title = spec.Title
		}
	}
	for _, f := range d.Formats {
		if f != FormatXLSX && f != FormatPDF {
			return FieldError{"formats", "chỉ xlsx, pdf"}
		}
	}
	return nil
}

// LevelAllowed cho biết cấp có dùng được với nguồn.
func LevelAllowed(source, level string) bool { return LevelAllowedWithSources(source, level, Sources) }
func LevelAllowedWithSources(source, level string, sources map[string]SourceSpec) bool {
	return slices.Contains(sources[source].Levels, level)
}

// ColumnVisible: cột có MinLevel chỉ hiện khi cấp tổng hợp đủ chi tiết.
func ColumnVisible(spec ColumnSpec, level string) bool {
	return (spec.MinLevel == "" || levelRank[level] >= levelRank[spec.MinLevel]) &&
		(spec.MaxLevel == "" || levelRank[level] <= levelRank[spec.MaxLevel])
}

func defCols(source string, keys ...string) []Column {
	src := Sources[source]
	out := make([]Column, 0, len(keys))
	for _, k := range keys {
		c, _ := src.Column(k)
		out = append(out, Column{Key: k, Title: c.Title})
	}
	return out
}

func both() []string { return []string{FormatXLSX, FormatPDF} }

// HTXProductionKey là mã mẫu hệ thống báo cáo sản lượng HTX (HTX US-DASH-02, US-DASH-03).
const HTXProductionKey = "htx.production"

// Builtins là mẫu hệ thống (phiên bản 0) — dùng khi tenant chưa có phiên bản riêng.
func Builtins() []Template {
	mk := func(key string, d Definition) Template {
		return Template{Key: key, Version: 0, Status: TemplateActive, Def: d}
	}
	return []Template{
		mk("cgh.summary", Definition{Title: "Tổng hợp theo khu vực", Source: port.SourceCGHSummary, Grouping: LevelProvince,
			Columns: defCols(port.SourceCGHSummary, "province_name", "commune_name", "farm_stage", "machine_type", "required", "capacity",
				"coverage_pct", "status"),
			Formats: both()}),
		mk("cgh.shortage_by_coop", Definition{Title: "Thiếu hụt theo HTX", Source: port.SourceCGHShortage, Grouping: LevelCooperative,
			Columns: defCols(port.SourceCGHShortage, "cooperative_code", "cooperative_name", "province_name", "commune_name", "farm_stage",
				"machine_type", "capacity", "required_whole", "shortage", "coverage_pct", "area_source"),
			Formats: both()}),
		mk(HTXProductionKey, Definition{Title: "Báo cáo sản lượng HTX (dự kiến – thực tế)", Source: port.SourceHTXProduction,
			Grouping: LevelCooperative,
			Columns: defCols(port.SourceHTXProduction, "cooperative_name", "season", "variety_name", "seasons", "households", "area_ha",
				"harvested_area_ha", "expected_t", "actual_t", "actual_yield_t_per_ha", "sop_progress_pct"),
			Formats: both()}),
		mk("cgh.season_compare", Definition{Title: "Đối chiếu giữa các vụ", Source: port.SourceCGHCompare, Grouping: LevelProvince,
			Columns: defCols(port.SourceCGHCompare, "unit_name", "farm_stage", "machine_type", "required_a", "capacity_a", "coverage_a",
				"required_b", "capacity_b", "coverage_b", "capacity_diff", "coverage_diff"),
			Formats: both()}),
	}
}

// Builtin tìm mẫu hệ thống theo mã.
func Builtin(key string) (Template, bool) {
	for _, t := range Builtins() {
		if t.Key == key {
			return t, true
		}
	}
	return Template{}, false
}

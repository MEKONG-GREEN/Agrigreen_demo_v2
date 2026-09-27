// Package domain: quy tắc của pf/mrv — danh mục trường có phiên bản, kiểm
// minh chứng theo ĐÚNG phiên bản danh mục đã ghim, duyệt và ảnh chụp dữ liệu
// của gói xuất. Không phụ thuộc hạ tầng; không gọi time.Now() (R-12).
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// RuleVersion ghi vào cột rule_version của các sổ trong module.
const RuleVersion = "pf.mrv/g1-1"

// DataType là kiểu dữ liệu của một trường — tập đóng; mọi thứ khác là dữ liệu.
type DataType string

// Các kiểu dữ liệu.
const (
	TypeDecimal  DataType = "decimal"   // chuỗi số thập phân (R-10), có đơn vị, min/max, số chữ số lẻ tối đa
	TypeInteger  DataType = "integer"   // số nguyên (số JSON hoặc chuỗi chữ số), min/max
	TypeBoolean  DataType = "boolean"   // true/false
	TypeEnum     DataType = "enum"      // một trong enum_values
	TypeDate     DataType = "date"      // YYYY-MM-DD
	TypePhoto    DataType = "photo"     // danh sách file_ref (ảnh đã mã hóa phong bì); min/max = số ảnh
	TypeGeoPoint DataType = "geo_point" // chỉ cho khóa gps — lưu ở cột geometry riêng
	TypeText     DataType = "text"      // chuỗi; min/max = độ dài ký tự
)

var dataTypes = []DataType{TypeDecimal, TypeInteger, TypeBoolean, TypeEnum, TypeDate, TypePhoto, TypeGeoPoint, TypeText}

// Khóa dành riêng: giá trị KHÔNG nằm trong values mà lấy từ cột riêng của bản
// ghi. Danh mục vẫn quyết định chúng có bắt buộc với từng loại minh chứng hay không.
const (
	KeyPlotRef = "plot_ref" // cột plot_ref (UUID thửa của pf/geo)
	KeySeason  = "season"   // cột season_code (mã vụ trong danh mục season)
	KeyGPS     = "gps"      // cột gps (geometry Point 4326) — dữ liệu cá nhân khi gắn với hộ
	KeyPhoto   = "photo"    // cột file_refs — mọi ảnh của bản ghi
)

var reservedTypes = map[string]DataType{KeyPlotRef: TypeText, KeySeason: TypeText, KeyGPS: TypeGeoPoint, KeyPhoto: TypePhoto}

// reservedColumn là tên trường trong API tương ứng với khóa dành riêng.
var reservedColumn = map[string]string{KeyPlotRef: "plot_ref", KeySeason: "season_code", KeyGPS: "gps", KeyPhoto: "file_refs"}

// IsReserved cho biết khóa lấy giá trị từ cột riêng.
func IsReserved(key string) bool { _, ok := reservedTypes[key]; return ok }

// FieldDef là định nghĩa một trường trong một phiên bản danh mục.
type FieldDef struct {
	Key        string
	LabelVi    string
	Type       DataType
	Unit       string // chỉ decimal/integer, vd "cm", "ngày"
	Required   bool
	Min, Max   *decimal.Decimal
	MaxScale   *int // decimal: số chữ số sau dấu phẩy tối đa
	EnumValues []string
	HelpVi     string
	Position   int
}

// CatalogStatus là trạng thái của phiên bản danh mục trong quy trình duyệt.
type CatalogStatus string

// Các trạng thái.
const (
	CatalogDraft     CatalogStatus = "draft"
	CatalogSubmitted CatalogStatus = "submitted"
	CatalogApproved  CatalogStatus = "approved"
	CatalogRejected  CatalogStatus = "rejected"
)

// CatalogVersion là một phiên bản danh mục trường của một loại minh chứng.
type CatalogVersion struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	EvidenceType string
	Version      int
	Status       CatalogStatus
	Provisional  bool // danh mục tạm, chờ chốt C-1
	SourceNote   string
	Fields       []FieldDef
	SetterRef    uuid.UUID
	ApproverRef  *uuid.UUID
	SubmittedAt  *time.Time
	DecidedAt    *time.Time
	DecisionNote *string
	IdemKey      string
	CreatedAt    time.Time
}

// Lỗi chuyển trạng thái.
var (
	ErrNotSetter  = errors.New("chỉ người đặt được gửi duyệt phiên bản của mình")
	ErrTransition = errors.New("trạng thái phiên bản không cho phép thao tác này")
)

// CanSubmit kiểm gửi duyệt: chỉ nháp, chỉ người đặt.
func (c CatalogVersion) CanSubmit(actor uuid.UUID) error {
	if c.SetterRef != actor {
		return ErrNotSetter
	}
	if c.Status != CatalogDraft {
		return fmt.Errorf("%w: đang %s", ErrTransition, c.Status)
	}
	return nil
}

// CanDecide kiểm duyệt/từ chối: chỉ phiên bản đã gửi duyệt.
func (c CatalogVersion) CanDecide() error {
	if c.Status != CatalogSubmitted {
		return fmt.Errorf("%w: đang %s", ErrTransition, c.Status)
	}
	return nil
}

var (
	evidenceTypeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,39}$`)
	fieldKeyRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)
	enumValueRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,62}$`)
	decimalRe      = regexp.MustCompile(`^-?[0-9]{1,16}(\.[0-9]{1,8})?$`)
	integerRe      = regexp.MustCompile(`^-?[0-9]{1,15}$`)
)

// ValidEvidenceType kiểm mã loại minh chứng.
func ValidEvidenceType(s string) bool { return evidenceTypeRe.MatchString(s) }

// Giới hạn danh mục.
const (
	MaxFields     = 100
	maxEnumValues = 100
	maxTextLen    = 2000
)

// ValidateDefinition kiểm một phiên bản danh mục đề xuất. Trả lỗi theo trường
// (fields[i].<thuộc tính>), rỗng nếu hợp lệ.
func (c CatalogVersion) ValidateDefinition() []problem.FieldError {
	var errs []problem.FieldError
	add := func(f, r string) { errs = append(errs, problem.FieldError{Field: f, Reason: r}) }
	if !evidenceTypeRe.MatchString(c.EvidenceType) {
		add("evidence_type", "3–40 ký tự a-z, 0-9, _; bắt đầu bằng chữ")
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(c.SourceNote)); n < 1 || n > 500 {
		add("source_note", "bắt buộc, tối đa 500 ký tự (nguồn: văn bản, biên bản C-1…)")
	}
	if len(c.Fields) == 0 || len(c.Fields) > MaxFields {
		add("fields", fmt.Sprintf("phải có 1–%d trường", MaxFields))
	}
	seen := map[string]bool{}
	for i, f := range c.Fields {
		p := fmt.Sprintf("fields[%d].", i)
		if !fieldKeyRe.MatchString(f.Key) {
			add(p+"key", "2–63 ký tự a-z, 0-9, _; bắt đầu bằng chữ")
		} else if seen[f.Key] {
			add(p+"key", "trùng khóa "+f.Key)
		}
		seen[f.Key] = true
		if n := utf8.RuneCountInString(strings.TrimSpace(f.LabelVi)); n < 1 || n > 200 {
			add(p+"label_vi", "bắt buộc, tối đa 200 ký tự")
		}
		if !slices.Contains(dataTypes, f.Type) {
			add(p+"data_type", "phải là decimal, integer, boolean, enum, date, photo, geo_point hoặc text")
			continue
		}
		if want, ok := reservedTypes[f.Key]; ok && f.Type != want {
			add(p+"data_type", fmt.Sprintf("khóa dành riêng %s phải có kiểu %s", f.Key, want))
		}
		if f.Type == TypeGeoPoint && f.Key != KeyGPS {
			add(p+"data_type", "geo_point chỉ dùng cho khóa gps (toạ độ lưu ở cột riêng, không nằm trong values)")
		}
		if f.Unit != "" && f.Type != TypeDecimal && f.Type != TypeInteger {
			add(p+"unit", "chỉ decimal và integer có đơn vị")
		} else if utf8.RuneCountInString(f.Unit) > 20 {
			add(p+"unit", "tối đa 20 ký tự")
		}
		errs = append(errs, validateBounds(p, f)...)
		if f.Type == TypeEnum {
			if len(f.EnumValues) == 0 || len(f.EnumValues) > maxEnumValues {
				add(p+"enum_values", fmt.Sprintf("enum phải có 1–%d giá trị", maxEnumValues))
			}
			ev := map[string]bool{}
			for _, v := range f.EnumValues {
				if !enumValueRe.MatchString(v) || ev[v] {
					add(p+"enum_values", "giá trị sai dạng hoặc trùng: "+v)
				}
				ev[v] = true
			}
		} else if len(f.EnumValues) > 0 {
			add(p+"enum_values", "chỉ kiểu enum có enum_values")
		}
		if utf8.RuneCountInString(f.HelpVi) > 500 {
			add(p+"help_vi", "tối đa 500 ký tự")
		}
	}
	return errs
}

func validateBounds(p string, f FieldDef) []problem.FieldError {
	var errs []problem.FieldError
	add := func(fl, r string) { errs = append(errs, problem.FieldError{Field: p + fl, Reason: r}) }
	hasBounds := f.Min != nil || f.Max != nil
	switch f.Type {
	case TypeDecimal:
	case TypeInteger, TypePhoto, TypeText:
		for name, b := range map[string]*decimal.Decimal{"min": f.Min, "max": f.Max} {
			if b != nil && !b.IsInteger() {
				add(name, "phải là số nguyên với kiểu "+string(f.Type))
			}
			if b != nil && f.Type != TypeInteger && b.IsNegative() {
				add(name, "không được âm")
			}
		}
	default:
		if hasBounds {
			add("min", "kiểu "+string(f.Type)+" không có min/max")
		}
	}
	if f.Min != nil && f.Max != nil && f.Min.GreaterThan(*f.Max) {
		add("min", "min phải ≤ max")
	}
	if f.MaxScale != nil {
		if f.Type != TypeDecimal {
			add("max_scale", "chỉ kiểu decimal có max_scale")
		} else if *f.MaxScale < 0 || *f.MaxScale > 8 {
			add("max_scale", "phải trong 0–8")
		}
	}
	sort.Slice(errs, func(i, j int) bool { return errs[i].Field < errs[j].Field })
	return errs
}

// Field trả định nghĩa trường theo khóa.
func (c CatalogVersion) Field(key string) (FieldDef, bool) {
	for _, f := range c.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return FieldDef{}, false
}

// Subject là phần dữ liệu của một bản ghi minh chứng được kiểm theo danh mục.
type Subject struct {
	Values     map[string]json.RawMessage
	PlotRef    *uuid.UUID
	SeasonCode *string
	GPS        *GeoPoint
	FileRefs   []FileRef
}

// ValidateRecord kiểm một bản ghi theo ĐÚNG phiên bản danh mục này (phiên bản
// đã ghim trên bản ghi). Trả lỗi theo trường, rỗng nếu hợp lệ.
func (c CatalogVersion) ValidateRecord(s Subject) []problem.FieldError {
	var errs []problem.FieldError
	add := func(f, r string) { errs = append(errs, problem.FieldError{Field: f, Reason: r}) }

	unknown := make([]string, 0)
	for k := range s.Values {
		if IsReserved(k) {
			add("values."+k, "khóa dành riêng — gửi ở trường "+reservedColumn[k])
			continue
		}
		if _, ok := c.Field(k); !ok {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		add("values."+k, fmt.Sprintf("không có trong danh mục %s phiên bản %d", c.EvidenceType, c.Version))
	}

	fileSet := map[uuid.UUID]bool{}
	for _, f := range s.FileRefs {
		fileSet[f.FileRef] = true
	}
	for _, f := range c.Fields {
		if IsReserved(f.Key) {
			errs = append(errs, checkReserved(f, s)...)
			continue
		}
		raw, present := s.Values[f.Key]
		if present && strings.TrimSpace(string(raw)) == "null" {
			present = false
		}
		if !present {
			if f.Required {
				add("values."+f.Key, "bắt buộc ("+f.LabelVi+")")
			}
			continue
		}
		if reason := checkValue(f, raw, fileSet); reason != "" {
			add("values."+f.Key, reason)
		}
	}
	return errs
}

func checkReserved(f FieldDef, s Subject) []problem.FieldError {
	col := reservedColumn[f.Key]
	present := false
	switch f.Key {
	case KeyPlotRef:
		present = s.PlotRef != nil
	case KeySeason:
		present = s.SeasonCode != nil
	case KeyGPS:
		present = s.GPS != nil
	case KeyPhoto:
		present = len(s.FileRefs) > 0
	}
	if !present {
		if f.Required {
			return []problem.FieldError{{Field: col, Reason: "bắt buộc (" + f.LabelVi + ")"}}
		}
		return nil
	}
	if f.Key == KeyPhoto {
		if r := checkCount(f, len(s.FileRefs), "ảnh"); r != "" {
			return []problem.FieldError{{Field: col, Reason: r}}
		}
	}
	return nil
}

func checkCount(f FieldDef, n int, what string) string {
	d := decimal.NewFromInt(int64(n))
	if f.Min != nil && d.LessThan(*f.Min) {
		return fmt.Sprintf("cần tối thiểu %s %s", f.Min.String(), what)
	}
	if f.Max != nil && d.GreaterThan(*f.Max) {
		return fmt.Sprintf("tối đa %s %s", f.Max.String(), what)
	}
	return ""
}

func checkRange(f FieldDef, d decimal.Decimal) string {
	unit := ""
	if f.Unit != "" {
		unit = " " + f.Unit
	}
	if f.Min != nil && d.LessThan(*f.Min) {
		return "phải ≥ " + f.Min.String() + unit
	}
	if f.Max != nil && d.GreaterThan(*f.Max) {
		return "phải ≤ " + f.Max.String() + unit
	}
	return ""
}

func jsonString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// checkValue trả lý do sai, rỗng nếu hợp lệ.
func checkValue(f FieldDef, raw json.RawMessage, files map[uuid.UUID]bool) string {
	switch f.Type {
	case TypeDecimal:
		s, ok := jsonString(raw)
		if !ok {
			return "số thập phân phải gửi dạng chuỗi, vd \"12.5\" (không dùng số thực — R-10)"
		}
		if !decimalRe.MatchString(s) {
			return "không phải số thập phân hợp lệ (vd \"-3.25\")"
		}
		d, err := decimal.NewFromString(s)
		if err != nil {
			return "không phải số thập phân hợp lệ"
		}
		if f.MaxScale != nil && -d.Exponent() > int32(*f.MaxScale) {
			return fmt.Sprintf("tối đa %d chữ số sau dấu phẩy", *f.MaxScale)
		}
		return checkRange(f, d)
	case TypeInteger:
		s := strings.TrimSpace(string(raw))
		if str, ok := jsonString(raw); ok {
			s = str
		}
		if !integerRe.MatchString(s) {
			return "phải là số nguyên"
		}
		d, _ := decimal.NewFromString(s)
		return checkRange(f, d)
	case TypeBoolean:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return "phải là true hoặc false"
		}
	case TypeEnum:
		s, ok := jsonString(raw)
		if !ok || !slices.Contains(f.EnumValues, s) {
			return "phải là một trong: " + strings.Join(f.EnumValues, ", ")
		}
	case TypeDate:
		s, ok := jsonString(raw)
		if !ok {
			return "phải là ngày YYYY-MM-DD"
		}
		if _, err := time.Parse(time.DateOnly, s); err != nil {
			return "phải là ngày YYYY-MM-DD"
		}
	case TypeText:
		s, ok := jsonString(raw)
		if !ok {
			return "phải là chuỗi"
		}
		n := utf8.RuneCountInString(s)
		if n > maxTextLen {
			return fmt.Sprintf("tối đa %d ký tự", maxTextLen)
		}
		return checkCount(f, n, "ký tự")
	case TypePhoto:
		var refs []string
		if json.Unmarshal(raw, &refs) != nil {
			return "phải là danh sách file_ref"
		}
		seen := map[string]bool{}
		for _, r := range refs {
			id, err := uuid.Parse(r)
			if err != nil || !files[id] || seen[r] {
				return "file_ref phải có trong file_refs của bản ghi và không trùng: " + r
			}
			seen[r] = true
		}
		if len(refs) == 0 && f.Required {
			return "bắt buộc (" + f.LabelVi + ")"
		}
		return checkCount(f, len(refs), "ảnh")
	case TypeGeoPoint:
		return "geo_point chỉ dùng cho khóa gps"
	}
	return ""
}

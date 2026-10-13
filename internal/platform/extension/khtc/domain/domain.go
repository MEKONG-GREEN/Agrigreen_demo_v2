// Package domain: quy tắc thuần của Danh mục KHTC + danh sách CT/DA + Kế hoạch năm (W11.61, D4 KH1, BRD KHTC v2.2
// FN-AD-04.01, FN-TI-02.02, BR-DL-13, BR-DL-21). Không I/O. Số lượng, kinh phí là decimal (R-CODE-01).
package domain

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RuleVersion ghi vào rule_version của mọi dòng sổ KHTC.
const RuleVersion = "kn.khtc/v1"

// Loại danh mục KHTC (FN-AD-04.01).
const (
	KindReportType    = "report_type"    // Loại báo cáo
	KindUnit          = "unit"           // ĐVT
	KindField         = "field"          // Lĩnh vực
	KindFundingSource = "funding_source" // Nguồn kinh phí
	KindSigner        = "signer"         // Người ký (họ tên, chức vụ, thẩm quyền ký, mặc định cho mẫu)
	KindRecipient     = "recipient"      // Nơi nhận (mặc định cho mẫu)
	KindBasis         = "basis"          // Căn cứ (mặc định cho mẫu)
)

// CatalogKinds theo thứ tự cột trái màn Danh mục KHTC.
var CatalogKinds = []string{KindReportType, KindUnit, KindField, KindFundingSource, KindSigner, KindRecipient, KindBasis}

// KindLabel là nhãn tiếng Việt của loại danh mục.
var KindLabel = map[string]string{
	KindReportType: "Loại báo cáo", KindUnit: "Đơn vị tính", KindField: "Lĩnh vực", KindFundingSource: "Nguồn kinh phí",
	KindSigner: "Người ký", KindRecipient: "Nơi nhận", KindBasis: "Căn cứ",
}

// Templates là 5 mẫu báo cáo KHTC (Mẫu 1 tuần, 2 tháng, 3 quý/6/9 tháng, 4 năm, 5 tiến độ các CT).
var Templates = []string{"mau_1", "mau_2", "mau_3", "mau_4", "mau_5"}

// templatedKinds: loại có cột "Mặc định cho mẫu".
var templatedKinds = map[string]bool{KindSigner: true, KindRecipient: true, KindBasis: true}

// Status của mục danh mục / CT-DA.
const (
	StatusActive  = "active"
	StatusRetired = "retired"
)

// Loại CT/DA (danh sách chủ — D20). Ánh xạ tự nhiên sang tab kế hoạch năm.
const (
	ProgramKindProgram        = "program"         // Chương trình (CT KN tỉnh)
	ProgramKindProject        = "project"         // Dự án (KN tỉnh)
	ProgramKindCentralProject = "central_project" // Dự án Khuyến nông Trung ương
	ProgramKindCooperation    = "cooperation"     // CT, DA hợp tác
	ProgramKindResearch       = "research"        // Đề tài KHCN
	ProgramKindAdvisory       = "advisory"        // Dịch vụ tư vấn
)

// ProgramKinds là mọi loại CT/DA hợp lệ.
var ProgramKinds = []string{ProgramKindProgram, ProgramKindProject, ProgramKindCentralProject, ProgramKindCooperation,
	ProgramKindResearch, ProgramKindAdvisory}

// 6 tab của Kế hoạch năm (FN-TI-02.02 B1).
const (
	TabPrograms       = "programs"        // Các chương trình
	TabPrevYear       = "prev_year"       // CT, DA năm trước còn theo dõi
	TabCentralProject = "central_project" // Dự án TW
	TabCooperation    = "cooperation"     // Hợp tác
	TabResearch       = "research"        // Đề tài
	TabAdvisory       = "advisory"        // Dịch vụ tư vấn
)

// Tabs theo thứ tự thanh tab.
var Tabs = []string{TabPrograms, TabPrevYear, TabCentralProject, TabCooperation, TabResearch, TabAdvisory}

// TabLabel là nhãn tab.
var TabLabel = map[string]string{
	TabPrograms: "Các chương trình", TabPrevYear: "CT, DA năm trước", TabCentralProject: "Dự án TW",
	TabCooperation: "Hợp tác", TabResearch: "Đề tài", TabAdvisory: "Dịch vụ tư vấn",
}

// Cấp của dòng kế hoạch.
const (
	EntryProgram = "program" // CT/DA (tham chiếu danh sách chủ)
	EntryGroup   = "group"   // nhóm
	EntryItem    = "item"    // hạng mục / nội dung theo dõi
)

// Nguồn số liệu của hạng mục (BR-DL-10).
const (
	SourceForm   = "form"   // phòng phụ trách nhập ở phiếu
	SourceModule = "module" // hệ thống điền từ module nguồn (source_key)
)

// Trạng thái theo dõi.
const (
	TrackingOn      = "tracking"
	TrackingStopped = "stopped"
)

// Basis của phiên bản kế hoạch.
const (
	BasisInitial = "initial" // phiên bản 1 — lập kế hoạch
	BasisAdjust  = "adjust"  // "Điều chỉnh theo QĐ"
)

// MaxEntries là số dòng tối đa của một phiên bản kế hoạch.
const MaxEntries = 5000

var (
	catalogCodeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)
	programCodeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)
	deptRe        = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
	sourceKeyRe   = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{1,99}$`)
	communeRe     = regexp.MustCompile(`^[0-9]{2,10}$`)
	piiRe         = regexp.MustCompile(`(^|\D)(0\d{9}|\d{12})(\D|$)`)
)

// FieldError là lỗi của một trường (field = đường dẫn JSON).
type FieldError struct{ Field, Reason string }

func (e FieldError) Error() string { return e.Field + ": " + e.Reason }

// Errors là tập lỗi trường.
type Errors []FieldError

func (es Errors) Error() string {
	parts := make([]string, 0, len(es))
	for _, e := range es {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}

func (es *Errors) add(field, reason string) { *es = append(*es, FieldError{field, reason}) }

func textOK(s string, max int) bool { n := utf8.RuneCountInString(s); return n >= 1 && n <= max }

// ---------------------------------------------------------------------------
// Danh mục KHTC
// ---------------------------------------------------------------------------

// CatalogItem là nội dung một phiên bản mục danh mục.
type CatalogItem struct {
	Kind       string
	Code       string
	Name       string
	SortOrder  int
	Status     string
	DefaultFor []string
	Attrs      map[string]string
}

// attrKeys: thuộc tính cho phép theo loại.
var attrKeys = map[string][]string{
	KindSigner:    {"title", "authority"},  // chức vụ, thẩm quyền ký (vd "KT. GIÁM ĐỐC")
	KindUnit:      {"symbol"},              // ký hiệu (ha, con, tấn …)
	KindBasis:     {"document_no", "date"}, // số văn bản, ngày
	KindRecipient: {"address"},
}

// NormalizeCatalog chuẩn hóa và kiểm một mục danh mục.
func NormalizeCatalog(c *CatalogItem) Errors {
	var es Errors
	c.Code = strings.ToLower(strings.TrimSpace(c.Code))
	c.Name = strings.TrimSpace(c.Name)
	if !slices.Contains(CatalogKinds, c.Kind) {
		es.add("kind", "loại danh mục không hợp lệ")
	}
	if !catalogCodeRe.MatchString(c.Code) {
		es.add("code", "1–40 ký tự a-z, 0-9, '.', '_', '-'")
	}
	if !textOK(c.Name, 500) {
		es.add("name", "bắt buộc, tối đa 500 ký tự")
	}
	if c.SortOrder < 0 || c.SortOrder > 100000 {
		es.add("sort_order", "0–100000")
	}
	if c.Status == "" {
		c.Status = StatusActive
	}
	if c.Status != StatusActive && c.Status != StatusRetired {
		es.add("status", "active hoặc retired")
	}
	df := []string{}
	for _, t := range c.DefaultFor {
		t = strings.TrimSpace(t)
		if t == "" || slices.Contains(df, t) {
			continue
		}
		if !slices.Contains(Templates, t) {
			es.add("default_for", "mẫu không hợp lệ: "+t)
			continue
		}
		df = append(df, t)
	}
	sort.Strings(df)
	if len(df) > 0 && !templatedKinds[c.Kind] {
		es.add("default_for", "chỉ người ký, nơi nhận, căn cứ có mặc định cho mẫu")
	}
	c.DefaultFor = df
	attrs := map[string]string{}
	for k, v := range c.Attrs {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !slices.Contains(attrKeys[c.Kind], k) {
			es.add("attrs."+k, "thuộc tính không dùng cho loại này")
			continue
		}
		if utf8.RuneCountInString(v) > 300 {
			es.add("attrs."+k, "tối đa 300 ký tự")
			continue
		}
		attrs[k] = v
	}
	if c.Kind == KindSigner && attrs["title"] == "" {
		es.add("attrs.title", "người ký bắt buộc chức vụ")
	}
	c.Attrs = attrs
	return es
}

// ---------------------------------------------------------------------------
// Danh sách CT/DA chủ (D20)
// ---------------------------------------------------------------------------

// Program là nội dung một phiên bản CT/DA.
type Program struct {
	Code            string
	Name            string
	Kind            string
	FieldCode       string
	OwnerDepartment string
	StartYear       int
	EndYear         int
	Status          string
	Note            string
}

// NormalizeProgram chuẩn hóa và kiểm một CT/DA.
func NormalizeProgram(p *Program) Errors {
	var es Errors
	p.Code = strings.TrimSpace(p.Code)
	p.Name = strings.TrimSpace(p.Name)
	p.FieldCode = strings.ToLower(strings.TrimSpace(p.FieldCode))
	p.OwnerDepartment = strings.TrimSpace(p.OwnerDepartment)
	p.Note = strings.TrimSpace(p.Note)
	if !programCodeRe.MatchString(p.Code) {
		es.add("code", "1–40 ký tự chữ, số, '.', '_', '-'")
	}
	if !textOK(p.Name, 500) {
		es.add("name", "bắt buộc, tối đa 500 ký tự")
	}
	if !slices.Contains(ProgramKinds, p.Kind) {
		es.add("kind", "loại CT/DA không hợp lệ")
	}
	if p.OwnerDepartment != "" && !deptRe.MatchString(p.OwnerDepartment) {
		es.add("owner_department", "mã phòng ban không hợp lệ")
	}
	for _, y := range []struct {
		f string
		v int
	}{{"start_year", p.StartYear}, {"end_year", p.EndYear}} {
		if y.v != 0 && (y.v < 2000 || y.v > 2100) {
			es.add(y.f, "năm 2000–2100")
		}
	}
	if p.StartYear != 0 && p.EndYear != 0 && p.EndYear < p.StartYear {
		es.add("end_year", "không trước năm bắt đầu")
	}
	if p.Status == "" {
		p.Status = StatusActive
	}
	if p.Status != StatusActive && p.Status != StatusRetired {
		es.add("status", "active hoặc retired")
	}
	if utf8.RuneCountInString(p.Note) > 1000 {
		es.add("note", "tối đa 1000 ký tự")
	}
	return es
}

// ---------------------------------------------------------------------------
// Kế hoạch năm
// ---------------------------------------------------------------------------

// Entry là một dòng của cây kế hoạch (một phiên bản).
type Entry struct {
	Ref          uuid.UUID
	Tab          string
	Kind         string
	ParentRef    *uuid.UUID
	ProgramRef   *uuid.UUID // dòng program: CT/DA chủ; dòng con: CT/DA của tổ tiên (tự điền)
	SortOrder    int
	Code         string
	Name         string
	UnitCode     string
	PlannedQty   *decimal.Decimal
	ScaleNote    string
	Budget       *decimal.Decimal
	FundingCode  string
	FieldCode    string
	Department   string
	Communes     []string
	PointsCount  *int
	DataSource   string
	SourceKey    string
	Tracking     string
	StoppedFrom  *time.Time
	StopDecision string
	Note         string
}

// Refs dùng khi kiểm tham chiếu danh mục (app tra trước rồi truyền vào).
type Refs struct {
	Units, Funding, Fields map[string]bool    // mã đang dùng
	Programs               map[uuid.UUID]bool // CT/DA dùng được (đang dùng, hoặc đã có ở phiên bản trước)
}

func dec4(d decimal.Decimal) bool { return d.Equal(d.Round(4)) }

// NormalizePlan chuẩn hóa và kiểm cả cây một phiên bản kế hoạch năm: cấp (program → group → item), cha trước con,
// cùng tab với cha, hạng mục có đúng một phòng phụ trách và nguồn số liệu, ngừng theo dõi có ngày, ngày trong năm.
// Dòng con nhận program_ref của tổ tiên. Thứ tự trả về: cha trước con theo thứ tự gửi.
func NormalizePlan(year int, es []Entry, refs Refs) Errors {
	var errs Errors
	if len(es) > MaxEntries {
		errs.add("entries", fmt.Sprintf("tối đa %d dòng", MaxEntries))
		return errs
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	seen := map[uuid.UUID]int{}
	progInTab := map[string]bool{}
	for i := range es {
		e := &es[i]
		f := func(n string) string { return fmt.Sprintf("entries.%d.%s", i, n) }
		if e.Ref == uuid.Nil {
			errs.add(f("ref"), "bắt buộc")
			continue
		}
		if _, dup := seen[e.Ref]; dup {
			errs.add(f("ref"), "trùng trong phiên bản")
			continue
		}
		seen[e.Ref] = i
		e.Name = strings.TrimSpace(e.Name)
		e.Code = strings.TrimSpace(e.Code)
		e.UnitCode = strings.ToLower(strings.TrimSpace(e.UnitCode))
		e.FundingCode = strings.ToLower(strings.TrimSpace(e.FundingCode))
		e.FieldCode = strings.ToLower(strings.TrimSpace(e.FieldCode))
		e.Department = strings.TrimSpace(e.Department)
		e.ScaleNote = strings.TrimSpace(e.ScaleNote)
		e.SourceKey = strings.TrimSpace(e.SourceKey)
		e.StopDecision = strings.TrimSpace(e.StopDecision)
		e.Note = strings.TrimSpace(e.Note)
		if !slices.Contains(Tabs, e.Tab) {
			errs.add(f("tab"), "tab không hợp lệ")
		}
		if !textOK(e.Name, 1000) {
			errs.add(f("name"), "bắt buộc, tối đa 1000 ký tự")
		}
		if e.Code != "" && utf8.RuneCountInString(e.Code) > 40 {
			errs.add(f("code"), "tối đa 40 ký tự")
		}
		if e.SortOrder < 0 || e.SortOrder > 100000 {
			errs.add(f("sort_order"), "0–100000")
		}
		switch e.Kind {
		case EntryProgram:
			if e.ParentRef != nil {
				errs.add(f("parent_ref"), "CT/DA là cấp đầu, không có cha")
			}
			if e.ProgramRef == nil {
				errs.add(f("program_ref"), "bắt buộc — chọn CT/DA trong danh sách chủ")
			} else {
				if refs.Programs != nil && !refs.Programs[*e.ProgramRef] {
					errs.add(f("program_ref"), "CT/DA không có hoặc đã ngừng trong danh sách chủ")
				}
				k := e.Tab + "/" + e.ProgramRef.String()
				if progInTab[k] {
					errs.add(f("program_ref"), "CT/DA đã có trong tab này")
				}
				progInTab[k] = true
			}
		case EntryGroup, EntryItem:
			if e.ParentRef == nil {
				errs.add(f("parent_ref"), "bắt buộc — nhóm, hạng mục nằm dưới CT/DA")
				break
			}
			pi, ok := seen[*e.ParentRef]
			if !ok || pi == i {
				errs.add(f("parent_ref"), "cha phải đứng trước trong cùng phiên bản")
				break
			}
			p := es[pi]
			if p.Tab != e.Tab {
				errs.add(f("tab"), "cùng tab với dòng cha")
			}
			if p.Kind == EntryItem || (e.Kind == EntryGroup && p.Kind != EntryProgram) {
				errs.add(f("parent_ref"), "cấp không hợp lệ (CT/DA → nhóm → hạng mục)")
			}
			e.ProgramRef = p.ProgramRef
			if p.Tracking == TrackingStopped && e.Tracking != TrackingStopped {
				// Con của dòng đã ngừng cũng ngừng từ cùng ngày.
				e.Tracking, e.StoppedFrom = TrackingStopped, p.StoppedFrom
			}
		default:
			errs.add(f("kind"), "program, group hoặc item")
		}
		if e.Kind == EntryItem {
			if !deptRe.MatchString(e.Department) {
				errs.add(f("department_code"), "hạng mục có đúng một phòng phụ trách")
			}
			if e.DataSource == "" {
				e.DataSource = SourceForm
			}
		} else if e.Department != "" && !deptRe.MatchString(e.Department) {
			errs.add(f("department_code"), "mã phòng ban không hợp lệ")
		}
		switch e.DataSource {
		case "", SourceForm:
			e.SourceKey = ""
		case SourceModule:
			if !sourceKeyRe.MatchString(e.SourceKey) {
				errs.add(f("source_key"), "nguồn module bắt buộc khóa nguồn (BR-DL-10)")
			}
		default:
			errs.add(f("data_source"), "form hoặc module")
		}
		if e.Kind != EntryItem && e.DataSource != "" {
			e.DataSource, e.SourceKey = "", ""
		}
		if e.PlannedQty != nil && (e.PlannedQty.IsNegative() || !dec4(*e.PlannedQty) || e.PlannedQty.GreaterThanOrEqual(decimal.New(1, 16))) {
			errs.add(f("planned_qty"), "≥ 0, tối đa 4 chữ số thập phân")
		}
		if e.Budget != nil && (e.Budget.IsNegative() || !e.Budget.Equal(e.Budget.Round(2)) || e.Budget.GreaterThanOrEqual(decimal.New(1, 18))) {
			errs.add(f("budget"), "≥ 0, tối đa 2 chữ số thập phân")
		}
		if e.PointsCount != nil && (*e.PointsCount < 0 || *e.PointsCount > 100000) {
			errs.add(f("points_count"), "0–100000")
		}
		if e.UnitCode != "" && refs.Units != nil && !refs.Units[e.UnitCode] {
			errs.add(f("unit_code"), "ĐVT không có hoặc đã ngừng trong Danh mục KHTC")
		}
		if e.FundingCode != "" && refs.Funding != nil && !refs.Funding[e.FundingCode] {
			errs.add(f("funding_code"), "nguồn kinh phí không có hoặc đã ngừng trong Danh mục KHTC")
		}
		if e.FieldCode != "" && refs.Fields != nil && !refs.Fields[e.FieldCode] {
			errs.add(f("field_code"), "lĩnh vực không có hoặc đã ngừng trong Danh mục KHTC")
		}
		cs := []string{}
		for _, c := range e.Communes {
			c = strings.TrimSpace(c)
			if c == "" || slices.Contains(cs, c) {
				continue
			}
			if !communeRe.MatchString(c) {
				errs.add(f("communes"), "mã xã không hợp lệ: "+c)
				continue
			}
			cs = append(cs, c)
		}
		if len(cs) > 500 {
			errs.add(f("communes"), "tối đa 500 xã/điểm")
		}
		e.Communes = cs
		if e.Tracking == "" {
			e.Tracking = TrackingOn
		}
		switch e.Tracking {
		case TrackingOn:
			e.StoppedFrom, e.StopDecision = nil, ""
		case TrackingStopped:
			if e.StoppedFrom == nil {
				errs.add(f("stopped_from"), "ngừng theo dõi bắt buộc ngày hiệu lực")
			} else if e.StoppedFrom.Before(start) || e.StoppedFrom.After(end) {
				errs.add(f("stopped_from"), fmt.Sprintf("trong năm kế hoạch %d", year))
			}
		default:
			errs.add(f("tracking"), "tracking hoặc stopped")
		}
		for _, t := range []struct {
			n   string
			v   string
			max int
		}{{"scale_note", e.ScaleNote, 1000}, {"stop_decision", e.StopDecision, 100}, {"note", e.Note, 2000}} {
			if utf8.RuneCountInString(t.v) > t.max {
				errs.add(f(t.n), fmt.Sprintf("tối đa %d ký tự", t.max))
			}
		}
		if piiRe.MatchString(e.Note) || piiRe.MatchString(e.ScaleNote) {
			errs.add(f("note"), "không ghi SĐT hoặc số CCCD")
		}
	}
	return errs
}

// ActiveAt cho biết dòng còn xuất hiện ở ngày at (BR-DL-21: không xuất hiện ở kỳ có ngày chốt sau ngày hiệu lực
// của quyết định ngừng — tức ngừng từ ngày d thì kỳ chốt ≥ d không còn).
func (e Entry) ActiveAt(at time.Time) bool {
	return e.Tracking != TrackingStopped || e.StoppedFrom == nil || at.Before(*e.StoppedFrom)
}

// Roman đổi 1..3999 sang số La Mã.
func Roman(n int) string {
	if n <= 0 || n >= 4000 {
		return ""
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// RomanNumbers đánh số La Mã cho CT/DA của tab "Các chương trình" theo sort_order (rồi thứ tự gửi), bỏ CT đã ngừng
// trước ngày at (BR-DL-21). at = nil: đánh số mọi CT.
func RomanNumbers(es []Entry, at *time.Time) map[uuid.UUID]string {
	type p struct {
		ref   uuid.UUID
		order int
		pos   int
	}
	var ps []p
	for i, e := range es {
		if e.Kind != EntryProgram || e.Tab != TabPrograms || (at != nil && !e.ActiveAt(*at)) {
			continue
		}
		ps = append(ps, p{e.Ref, e.SortOrder, i})
	}
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].order != ps[j].order {
			return ps[i].order < ps[j].order
		}
		return ps[i].pos < ps[j].pos
	})
	out := map[uuid.UUID]string{}
	for i, x := range ps {
		out[x.ref] = Roman(i + 1)
	}
	return out
}

type hashEntry struct {
	Ref          string   `json:"ref"`
	Tab          string   `json:"tab"`
	Kind         string   `json:"kind"`
	Parent       string   `json:"parent,omitempty"`
	Program      string   `json:"program,omitempty"`
	Sort         int      `json:"sort"`
	Code         string   `json:"code,omitempty"`
	Name         string   `json:"name"`
	Unit         string   `json:"unit,omitempty"`
	Qty          string   `json:"qty,omitempty"`
	Scale        string   `json:"scale,omitempty"`
	Budget       string   `json:"budget,omitempty"`
	Funding      string   `json:"funding,omitempty"`
	Field        string   `json:"field,omitempty"`
	Dept         string   `json:"dept,omitempty"`
	Communes     []string `json:"communes,omitempty"`
	Points       *int     `json:"points,omitempty"`
	Source       string   `json:"source,omitempty"`
	SourceKey    string   `json:"source_key,omitempty"`
	Tracking     string   `json:"tracking"`
	StoppedFrom  string   `json:"stopped_from,omitempty"`
	StopDecision string   `json:"stop_decision,omitempty"`
	Note         string   `json:"note,omitempty"`
}

func refStr(u *uuid.UUID) string {
	if u == nil {
		return ""
	}
	return u.String()
}

func decStr(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.String()
}

// EntriesHash là sha256 của nội dung chuẩn hóa các dòng (theo thứ tự lưu) — ghi vào khtc_plan_version.entries_hash.
func EntriesHash(es []Entry) []byte {
	hs := make([]hashEntry, 0, len(es))
	for _, e := range es {
		h := hashEntry{Ref: e.Ref.String(), Tab: e.Tab, Kind: e.Kind, Parent: refStr(e.ParentRef), Program: refStr(e.ProgramRef),
			Sort: e.SortOrder, Code: e.Code, Name: e.Name, Unit: e.UnitCode, Qty: decStr(e.PlannedQty), Scale: e.ScaleNote,
			Budget: decStr(e.Budget), Funding: e.FundingCode, Field: e.FieldCode, Dept: e.Department, Communes: e.Communes,
			Points: e.PointsCount, Source: e.DataSource, SourceKey: e.SourceKey, Tracking: e.Tracking, StopDecision: e.StopDecision,
			Note: e.Note}
		if e.StoppedFrom != nil {
			h.StoppedFrom = e.StoppedFrom.Format(time.DateOnly)
		}
		hs = append(hs, h)
	}
	b, _ := json.Marshal(hs)
	s := sha256.Sum256(b)
	return s[:]
}

// VersionHead là phần đầu một phiên bản kế hoạch (cho chọn phiên bản theo ngày).
type VersionHead struct {
	Version       int
	EffectiveFrom time.Time
}

// PickAt chọn phiên bản dùng cho ngày at (BR-DL-13: ngày hiệu lực ≤ at, gần nhất; cùng ngày → phiên bản sau). 0 = chưa
// có phiên bản nào hiệu lực.
func PickAt(vs []VersionHead, at time.Time) int {
	best := VersionHead{}
	for _, v := range vs {
		if v.EffectiveFrom.After(at) {
			continue
		}
		if best.Version == 0 || v.EffectiveFrom.After(best.EffectiveFrom) ||
			(v.EffectiveFrom.Equal(best.EffectiveFrom) && v.Version > best.Version) {
			best = v
		}
	}
	return best.Version
}

// CopyForYear dựng bản nháp năm mới từ cây năm trước ("Sao chép từ năm trước" — FN-TI-02.02 luồng phụ): giữ các dòng
// còn theo dõi (bỏ dòng đã ngừng và con của nó), cấp entry_ref mới, giữ program_ref (danh sách chủ bền qua các năm).
// newRef cấp định danh mới (UUIDv7 ở app).
func CopyForYear(es []Entry, newRef func() uuid.UUID) []Entry {
	m := map[uuid.UUID]uuid.UUID{}
	out := make([]Entry, 0, len(es))
	for _, e := range es {
		if e.Tracking == TrackingStopped {
			continue
		}
		var parent *uuid.UUID
		if e.ParentRef != nil {
			np, ok := m[*e.ParentRef]
			if !ok {
				continue // cha đã bị bỏ
			}
			parent = &np
		}
		n := e
		n.Ref = newRef()
		m[e.Ref] = n.Ref
		n.ParentRef = parent
		n.Communes = append([]string(nil), e.Communes...)
		n.StoppedFrom, n.StopDecision = nil, ""
		out = append(out, n)
	}
	return out
}

// ParseDate đọc YYYY-MM-DD (UTC, chỉ ngày).
func ParseDate(s string) (time.Time, error) { return time.Parse(time.DateOnly, strings.TrimSpace(s)) }

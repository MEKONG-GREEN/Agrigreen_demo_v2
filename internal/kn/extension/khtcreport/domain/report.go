// Package domain contains the fixed G1 KHTC report layouts and exact calculations.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	form "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	perioddomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	source "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const RuleVersion = "khtc-report/g1-v1"

var ErrInput = errors.New("nội dung báo cáo không hợp lệ")

// Period stores the reporting calendar selected when the period was opened.
type Period struct {
	No          *int      `json:"no"`
	Scope       *string   `json:"scope"`
	ID          uuid.UUID `json:"id"`
	Type        string    `json:"type"`
	Year        int       `json:"year"`
	Name        string    `json:"name"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Cutoff      time.Time `json:"cutoff"`
	Departments []string  `json:"departments"`
}

// Submitted identifies a sealed sheet actually referenced by the latest cap/forms submission.
// Drafts and unreferenced sealed sheets are never inputs to Compose.
type Submitted struct {
	Decision     string     `json:"decision"`
	PeriodID     uuid.UUID  `json:"period_id"`
	Department   string     `json:"department"`
	Type         string     `json:"type"`
	Year         int        `json:"year"`
	From         time.Time  `json:"from"`
	To           time.Time  `json:"to"`
	SubmissionID uuid.UUID  `json:"submission_id"`
	Attempt      int64      `json:"attempt"`
	SheetID      uuid.UUID  `json:"sheet_id"`
	Hash         string     `json:"hash"`
	Sheet        form.Sheet `json:"-"`
}

// SourceTrace contains provenance only, never a copy of another module's raw cells.
type SourceTrace struct {
	TargetKey   string     `json:"target_key"`
	Module      string     `json:"module"`
	Calendar    string     `json:"calendar"`
	NativeKey   string     `json:"native_key"`
	From        *time.Time `json:"from"`
	To          *time.Time `json:"to"`
	Closed      bool       `json:"closed"`
	FiguresHash *string    `json:"figures_hash"`
}

type SourceMetric struct {
	Text       string        `json:"text,omitempty"`
	Trace      []SourceTrace `json:"trace,omitempty"`
	Unit       string        `json:"unit"`
	Key        string        `json:"key"`
	Department string        `json:"department"`
	Form       string        `json:"form"`
	Label      string        `json:"label"`
	Stock      bool          `json:"stock"`
	Value      source.Value  `json:"value"`
}

type SourceRef struct {
	ID            uuid.UUID  `json:"id"`
	Version       int64      `json:"version"`
	PlanVersionID *uuid.UUID `json:"plan_version_id"`
	FiguresHash   string     `json:"figures_hash"`
	StatusHash    string     `json:"status_hash"`
	Closed        bool       `json:"closed"`
	At            time.Time  `json:"at"`
}

type ProgramRow struct {
	Ref        uuid.UUID  `json:"ref"`
	ParentRef  *uuid.UUID `json:"parent_ref"`
	ProgramRef *uuid.UUID `json:"program_ref"`
	Kind       string     `json:"kind"`
	Tab        string     `json:"tab"`
	Roman      string     `json:"roman"`
	Name       string     `json:"name"`
	Unit       string     `json:"unit"`
	Department string     `json:"department"`
	Planned    *string    `json:"planned"`
	Scale      string     `json:"scale"`
	InPeriod   string     `json:"in_period"`
	Adjustment string     `json:"adjustment"`
	YearToDate string     `json:"year_to_date"`
	Percent    *string    `json:"percent"`
	Level      string     `json:"level"`
	Status     string     `json:"status"`
	Content    string     `json:"content"`
	People     string     `json:"people"`
	Budget     string     `json:"budget"`
	Missing    bool       `json:"missing"`
	SourceKey  string     `json:"source_key"`
	Communes   []string   `json:"communes"`
}

type Section struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Editable bool   `json:"editable"`
}

// ExternalEvidence là tệp minh chứng của một số liệu nguồn ngoài (chỉ siêu dữ liệu).
type ExternalEvidence struct {
	ID       uuid.UUID `json:"id"`
	FileName string    `json:"file_name"`
}

// ExternalFigure là một ô "Nhập ngoài" (W11.67, BR-DL-19): giá trị hiệu lực của chỉ tiêu nguồn ngoài trong kỳ, khóa như ô
// số trên bản soạn; "Nguồn dữ liệu" hiện cơ quan, văn bản/kênh, ngày, tệp minh chứng. Chỉ sửa tại Số liệu nguồn ngoài.
type ExternalFigure struct {
	EntryID     uuid.UUID          `json:"entry_id"`
	Version     int64              `json:"version"`
	IndicatorID uuid.UUID          `json:"indicator_id"`
	Code        string             `json:"code"`
	Name        string             `json:"name"`
	Group       string             `json:"group"`
	Value       string             `json:"value"`
	Unit        string             `json:"unit"`
	Agency      string             `json:"agency"`
	Document    string             `json:"document"`
	SourceDate  string             `json:"source_date"`
	Evidence    []ExternalEvidence `json:"evidence"`
	At          time.Time          `json:"at"`
}

// Supplement pins the request lifecycle in the immutable report input.
type Supplement struct {
	ID                  uuid.UUID  `json:"id"`
	Department          string     `json:"department"`
	Instruction         string     `json:"instruction"`
	State               string     `json:"state"`
	DueAt               time.Time  `json:"due_at"`
	NativeSubmissionRef *uuid.UUID `json:"native_submission_ref"`
	ReturnApplied       *bool      `json:"return_applied"`
}
type Data struct {
	Supplements []Supplement                 `json:"supplements,omitempty"`
	Period      Period                       `json:"period"`
	Template    string                       `json:"template"`
	Plan        port.KHTCPlan                `json:"plan"`
	Sources     *SourceRef                   `json:"sources"`
	Metrics     []SourceMetric               `json:"metrics"`
	Submissions []Submitted                  `json:"submissions"`
	Rows        []ProgramRow                 `json:"rows"`
	FormTexts   map[string]map[string]string `json:"form_texts"`
	Provisional []string                     `json:"provisional"`
	InputHash   string                       `json:"input_hash"`
	CapturedAt  time.Time                    `json:"captured_at"`
	// External: ô Nhập ngoài của kỳ (W11.67). Rỗng thì bỏ khỏi JSON và khỏi băm — bản soạn cũ không đổi băm.
	External []ExternalFigure `json:"external,omitempty"`
}

// WithExternal gắn số liệu nguồn ngoài của kỳ vào dữ liệu đã dựng và đưa chúng vào băm đầu vào: đổi giá trị, nguồn hay
// minh chứng sau khi xuất → "Nguồn đã thay đổi" (BR-DL-04). Không có số liệu → giữ nguyên băm của Compose.
func WithExternal(data Data, figures []ExternalFigure) (Data, error) {
	if len(figures) == 0 {
		data.External = nil
		return data, nil
	}
	data.External = append([]ExternalFigure{}, figures...)
	sort.SliceStable(data.External, func(i, j int) bool {
		a, b := data.External[i], data.External[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Code < b.Code
	})
	var err error
	data.InputHash, err = Hash(struct {
		Base     string
		External []ExternalFigure
	}{data.InputHash, data.External})
	return data, err
}

func Template(kind string) string {
	switch kind {
	case "adhoc":
		return "adhoc"
	case "week":
		return "mau_1"
	case "month":
		return "mau_2"
	case "quarter":
		return "mau_3"
	case "year":
		return "mau_4"
	case "program":
		return "mau_5"
	}
	return ""
}

func Sections(template string) []Section {
	switch template {
	case "adhoc":
		return []Section{{"noi_dung", "Nội dung báo cáo", true}, {"chi_tieu", "Chỉ tiêu được chọn", false}}
	case "mau_1":
		return []Section{
			{"A", "KẾT QUẢ ĐẠT ĐƯỢC", false},
			{"A.I.1", "Trồng trọt", false}, {"A.I.2", "Chăn nuôi", false}, {"A.I.3", "Thủy sản", false},
			{"A.II.1", "Chương trình, dự án còn theo dõi năm trước", false},
			{"A.II.2", "Chương trình triển khai trong năm", false},
			{"A.II.3.1", "Công tác báo cáo", true}, {"A.II.3.2", "Công tác khác", false},
			{"A.III", "Thông tin giá cả thị trường", false}, {"A.IV", "Công tác CCHC, tổ chức cán bộ", false},
			{"B", "Đánh giá hạn chế, vướng mắc", true}, {"C.1", "Nhiệm vụ kỳ tới: Sản xuất NLTS", true},
			{"C.2", "Nhiệm vụ kỳ tới: Công tác khuyến nông", true}, {"C.3", "Nhiệm vụ kỳ tới: Giá cả", true},
			{"C.4", "Nhiệm vụ kỳ tới: CCHC", true}, {"D", "Đề xuất, kiến nghị", true},
		}
	case "mau_2", "mau_3", "mau_4":
		return []Section{
			{"I", "Kết quả thực hiện nhiệm vụ công tác khuyến nông", false},
			{"I.1", "Chương trình, dự án còn theo dõi năm trước", false}, {"I.2", "Chương trình triển khai trong năm", false},
			{"I.3.1", "Công tác báo cáo", true}, {"I.3.2", "Công tác khác", false},
			{"II.1", "Công tác cán bộ", false}, {"II.2", "Công tác cải cách hành chính", false},
			{"III.1", "Thuận lợi", true}, {"III.2", "Khó khăn", true}, {"III.3", "Nguyên nhân", true},
			{"IV", "Phương hướng, nhiệm vụ kỳ tới", true}, {"V", "Đề xuất và kiến nghị", true},
		}
	case "mau_5":
		return []Section{{"I", "Tiến độ thực hiện các chương trình", true}, {"II", "Kế hoạch thực hiện kỳ tới", true}, {"phu_luc", "Tiến độ thực hiện chương trình khuyến nông", false}}
	}
	return nil
}

var AdminKeys = []string{"document_no", "place", "report_date", "recipient", "basis", "recipients", "authority", "signer_title", "signer"}

// Edits contains only fields explicitly editable in G1. Numeric/system values have no write representation.
type Edits struct {
	Adhoc *AdhocSelection    `json:"adhoc,omitempty"`
	Admin map[string]*string `json:"admin"`
	Texts map[string]string  `json:"texts"`
}

func (e Edits) Validate(template string) error {
	if e.Adhoc != nil {
		if template != "adhoc" {
			return ErrInput
		}
		if err := e.Adhoc.Validate(); err != nil {
			return err
		}
	}
	for key, value := range e.Admin {
		if !slices.Contains(AdminKeys, key) {
			return fmt.Errorf("%w: ô hành chính %s", ErrInput, key)
		}
		if value != nil && (utf8.RuneCountInString(*value) > 2000 || strings.ContainsRune(*value, 0)) {
			return fmt.Errorf("%w: %s tối đa 2000 ký tự", ErrInput, key)
		}
		if key == "report_date" && value != nil && *value != "" {
			if _, err := time.Parse(time.DateOnly, *value); err != nil {
				return fmt.Errorf("%w: ngày báo cáo", ErrInput)
			}
		}
	}
	allowed := map[string]bool{}
	for _, section := range Sections(template) {
		allowed[section.Key] = section.Editable
	}
	for key, value := range e.Texts {
		if !allowed[key] || utf8.RuneCountInString(value) > 12000 || strings.ContainsRune(value, 0) {
			return fmt.Errorf("%w: mục lời văn %s", ErrInput, key)
		}
	}
	return nil
}

type AdminField struct {
	System   string     `json:"system"`
	Override *string    `json:"override"`
	Actor    *uuid.UUID `json:"actor"`
	At       *time.Time `json:"at"`
	Changed  bool       `json:"system_changed"`
}

func (f AdminField) Value() string {
	if f.Override != nil {
		return *f.Override
	}
	return f.System
}

type Draft struct {
	Adhoc *AdhocSelection       `json:"adhoc,omitempty"`
	Data  Data                  `json:"data"`
	Admin map[string]AdminField `json:"admin"`
	Texts map[string]string     `json:"texts"`
	// Optional for historical payloads: nil retains the original fixed renderer.
	TemplateSnapshot *port.KHTCTemplateSnapshot `json:"template_snapshot,omitempty"`
	TemplateTerminal *string                    `json:"template_terminal,omitempty"`
}

func Edit(d Draft, in Edits, actor uuid.UUID, at time.Time) (Draft, error) {
	if err := in.Validate(d.Data.Template); err != nil {
		return Draft{}, err
	}
	// Copy through JSON so callers cannot mutate a historical payload through shared maps/pointers.
	raw, err := json.Marshal(d)
	if err != nil {
		return Draft{}, err
	}
	var out Draft
	if err := json.Unmarshal(raw, &out); err != nil {
		return Draft{}, err
	}
	if out.Admin == nil {
		out.Admin = map[string]AdminField{}
	}
	if out.Texts == nil {
		out.Texts = map[string]string{}
	}
	if in.Adhoc != nil {
		if err := in.Adhoc.ValidateData(out.Data); err != nil {
			return Draft{}, err
		}
		out.Adhoc = in.Adhoc.Clone()
	}
	for key, value := range in.Admin {
		field := out.Admin[key]
		field.Override, field.Changed = nil, false
		if value != nil {
			copy := *value
			field.Override = &copy
		}
		if value == nil {
			field.Actor, field.At = nil, nil
		} else {
			who, when := actor, at
			field.Actor, field.At = &who, &when
		}
		out.Admin[key] = field
	}
	for key, value := range in.Texts {
		out.Texts[key] = strings.TrimSpace(value)
	}
	if out.Data.Template == "adhoc" {
		out.Data.Provisional = AdhocData(out).Provisional
	}
	return out, nil
}

func Refresh(d Draft, data Data, defaults map[string]string) Draft {
	out := Draft{Data: data, Adhoc: d.Adhoc.Clone(), Admin: map[string]AdminField{}, Texts: map[string]string{}}
	if d.TemplateSnapshot != nil {
		copy := *d.TemplateSnapshot
		copy.Definition = append(json.RawMessage(nil), copy.Definition...)
		if copy.LegacyUntil != nil {
			until := *copy.LegacyUntil
			copy.LegacyUntil = &until
		}
		out.TemplateSnapshot = &copy
	}
	if d.TemplateTerminal != nil {
		term := *d.TemplateTerminal
		out.TemplateTerminal = &term
	}
	for key, value := range d.Texts {
		out.Texts[key] = value
	}
	// Seed narrative sections once from submitted text. An explicitly saved blank stays blank.
	defaultsText := map[string]string{"D": "Không có."}
	depts := make([]string, 0, len(data.FormTexts))
	for dept := range data.FormTexts {
		depts = append(depts, dept)
	}
	sort.Strings(depts)
	appendText := func(key, dept, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if defaultsText[key] != "" {
			defaultsText[key] += "\n"
		}
		defaultsText[key] += dept + ": " + value
	}
	for _, dept := range depts {
		fields := data.FormTexts[dept]
		switch data.Template {
		case "mau_1":
			appendText("B", dept, fields["kho_khan"])
			section := "C.2"
			if dept == "trong_trot" || dept == "chan_nuoi_thuy_san" {
				section = "C.1"
			}
			if dept == "thong_tin_truyen_thong" {
				section = "C.3"
			}
			if dept == "hanh_chinh_tong_hop" {
				section = "C.4"
			}
			appendText(section, dept, fields["nhiem_vu_ky_toi"])
			if section == "C.4" {
				appendText(section, dept, fields["nhiem_vu_cchc"])
			}
		case "mau_5":
			appendText("II", dept, fields["nhiem_vu_ky_toi"])
		default:
			appendText("III.2", dept, fields["kho_khan"])
			appendText("IV", dept, fields["nhiem_vu_ky_toi"])
			appendText("IV", dept, fields["nhiem_vu_cchc"])
		}
	}
	for _, section := range Sections(data.Template) {
		if section.Editable {
			if _, exists := out.Texts[section.Key]; !exists {
				out.Texts[section.Key] = defaultsText[section.Key]
			}
		}
	}
	for _, key := range AdminKeys {
		previous := d.Admin[key]
		next := AdminField{System: defaults[key], Override: previous.Override, Actor: previous.Actor, At: previous.At}
		next.Changed = previous.Override != nil && (previous.Changed || previous.System != next.System)
		out.Admin[key] = next
	}
	if out.Data.Template == "adhoc" {
		out.Data.Provisional = AdhocData(out).Provisional
	}
	return out
}

func Percent(value decimal.Decimal, planned *decimal.Decimal) *string {
	if planned == nil || !planned.IsPositive() {
		return nil
	}
	s := value.Div(*planned).Mul(decimal.NewFromInt(100)).Round(1).String()
	return &s
}
func Level(percent *string) string {
	if percent == nil {
		return "qualitative"
	}
	value, err := decimal.NewFromString(*percent)
	if err != nil {
		return "qualitative"
	}
	if value.GreaterThanOrEqual(decimal.NewFromInt(100)) {
		return "achieved"
	}
	if value.GreaterThanOrEqual(decimal.NewFromInt(30)) {
		return "progressing"
	}
	return "slow"
}

// Attainment uses the exact ratio, independently of the rounded display percentage.
func Attainment(value decimal.Decimal, planned *decimal.Decimal) string {
	if planned == nil || !planned.IsPositive() {
		return "qualitative"
	}
	if value.GreaterThanOrEqual(*planned) {
		return "achieved"
	}
	if value.Mul(decimal.NewFromInt(100)).GreaterThanOrEqual(planned.Mul(decimal.NewFromInt(30))) {
		return "progressing"
	}
	return "slow"
}

func Hash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Compose uses exactly one latest submitted sheet per period/department. The adapter must never pass drafts.
func Compose(period Period, plan port.KHTCPlan, submitted []Submitted, snapshot *SourceRef, metrics []SourceMetric, at time.Time) (Data, error) {
	data := Data{Period: period, Template: Template(period.Type), Plan: plan, Sources: snapshot, Metrics: metrics, Rows: []ProgramRow{},
		Submissions: []Submitted{}, FormTexts: map[string]map[string]string{}, Provisional: []string{}, CapturedAt: at}
	if data.Template == "" {
		return Data{}, ErrInput
	}
	missing := func(reason string) {
		if !slices.Contains(data.Provisional, reason) {
			data.Provisional = append(data.Provisional, reason)
		}
	}
	if plan.VersionID == uuid.Nil {
		missing("Chưa có Kế hoạch năm hiệu lực")
	}
	if snapshot == nil {
		missing("Chưa cập nhật số liệu nguồn")
	} else {
		if !snapshot.Closed {
			missing("Nguồn module chưa chốt đầy đủ")
		}
		if snapshot.PlanVersionID != nil && *snapshot.PlanVersionID != plan.VersionID {
			missing("Phiên bản Kế hoạch năm của ảnh chụp đã thay đổi")
		}
	}
	latest := map[string]Submitted{}
	for _, sheet := range submitted {
		key := sheet.PeriodID.String() + ":" + sheet.Department
		previous, ok := latest[key]
		if !ok || sheet.Attempt > previous.Attempt {
			latest[key] = sheet
		}
		if ok && sheet.Attempt == previous.Attempt && sheet.SubmissionID != previous.SubmissionID {
			return Data{}, fmt.Errorf("%w: lần gửi không duy nhất", ErrInput)
		}
	}
	current := map[string]Submitted{}
	for _, sheet := range latest {
		isCurrentProgram := period.Type == perioddomain.TypeProgram && sheet.PeriodID == period.ID
		if sheet.Year != period.Year || (sheet.To.After(perioddomain.QuantityEnd(period.Type, period.To, period.Cutoff)) && !isCurrentProgram) {
			continue
		}
		if sheet.Type == "week" || sheet.PeriodID == period.ID {
			data.Submissions = append(data.Submissions, sheet)
		}
		if sheet.PeriodID == period.ID {
			current[sheet.Department] = sheet
			data.FormTexts[sheet.Department] = sheet.Sheet.Texts
		}
	}
	sort.Slice(data.Submissions, func(i, j int) bool {
		a, b := data.Submissions[i], data.Submissions[j]
		if !a.To.Equal(b.To) {
			return a.To.Before(b.To)
		}
		if a.Department != b.Department {
			return a.Department < b.Department
		}
		return a.PeriodID.String() < b.PeriodID.String()
	})
	for department, sheet := range current {
		if sheet.Decision == "returned" {
			missing("Phòng " + department + " đang được yêu cầu bổ sung")
		}
	}
	for _, department := range period.Departments {
		if _, ok := current[department]; !ok && period.Type != "adhoc" {
			missing("Phòng " + department + " chưa gửi số liệu")
		}
	}
	byKey := map[string]SourceMetric{}
	for _, metric := range metrics {
		byKey[metric.Key] = metric
	}
	entries := map[uuid.UUID]port.KHTCPlanEntry{}
	for _, entry := range plan.Entries {
		entries[entry.Ref] = entry
	}
	trackedAt := period.From
	if period.Type == "adhoc" && trackedAt.Year() < plan.Year {
		trackedAt = time.Date(plan.Year, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	tracked := func(entry port.KHTCPlanEntry) bool {
		for depth := 0; depth < 16; depth++ {
			if !form.TrackedIn(entry, trackedAt) {
				return false
			}
			if entry.ParentRef == nil {
				return true
			}
			parent, ok := entries[*entry.ParentRef]
			if !ok {
				return false
			}
			entry = parent
		}
		return false
	}
	for _, entry := range plan.Entries {
		if !tracked(entry) {
			continue
		}
		row := ProgramRow{Ref: entry.Ref, ParentRef: entry.ParentRef, ProgramRef: entry.ProgramRef, Kind: entry.Kind, Tab: entry.Tab,
			Roman: entry.Roman, Name: entry.Name, Unit: entry.UnitCode, Department: entry.Department, Scale: entry.ScaleNote,
			InPeriod: "0", Adjustment: "0", YearToDate: "0", People: "0", Budget: "0", SourceKey: entry.SourceKey, Communes: append([]string{}, entry.Communes...)}
		if entry.PlannedQty != nil {
			s := entry.PlannedQty.String()
			row.Planned = &s
		}
		if entry.Kind != "item" {
			data.Rows = append(data.Rows, row)
			continue
		}
		done, adjustment, total, people, budget := decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
		hasRangeSheet := false
		for _, sheet := range data.Submissions {
			if sheet.Department != entry.Department || sheet.Type != "week" {
				continue
			}
			cell := sheet.Sheet.Cells[entry.Ref.String()]
			if cell.Status != "" {
				row.Status = cell.Status
			}
			parse := func(raw string) (decimal.Decimal, error) {
				if strings.TrimSpace(raw) == "" {
					return decimal.Zero, nil
				}
				n, ok := form.ParseNumber(raw)
				if !ok {
					return decimal.Zero, fmt.Errorf("%w: số liệu phiếu", ErrInput)
				}
				return n, nil
			}
			n, err := parse(cell.Done)
			if err != nil {
				return Data{}, err
			}
			total = total.Add(n)
			if !sheet.To.Before(period.From) {
				if _, exists := sheet.Sheet.Cells[entry.Ref.String()]; exists {
					hasRangeSheet = true
				}
				done = done.Add(n)
				p, err := parse(cell.People)
				if err != nil {
					return Data{}, err
				}
				people = people.Add(p)
				b, err := parse(cell.Budget)
				if err != nil {
					return Data{}, err
				}
				budget = budget.Add(b)
			}
			for _, adj := range sheet.Sheet.Adjustments {
				if adj.EntryRef != entry.Ref.String() || adj.PeriodYear != period.Year {
					continue
				}
				amount, err := parse(adj.Amount)
				if err != nil {
					return Data{}, err
				}
				total = total.Add(amount)
				if !sheet.To.Before(period.From) {
					adjustment = adjustment.Add(amount)
				}
			}
		}
		if entry.DataSource == "module" {
			metric, ok := byKey[entry.SourceKey]
			if !ok || metric.Value.Provisional() {
				row.Missing = true
				missing("Hạng mục chưa có nguồn đã chốt: " + entry.Name)
			}
			if ok {
				v := metric.Value
				if v.Increment != nil {
					done = *v.Increment
				} else if v.Sum != nil {
					done = *v.Sum
				}
				if v.YearToDate != nil {
					total = *v.YearToDate
				} else if v.Sum != nil {
					total = *v.Sum
				}
				adjustment = decimal.Zero
			}
		} else if _, ok := current[entry.Department]; !ok && (period.Type != "adhoc" || !hasRangeSheet) {
			row.Missing = true
			missing("Phòng " + entry.Department + " chưa gửi số liệu")
		}
		row.InPeriod, row.Adjustment, row.YearToDate, row.People, row.Budget = done.String(), adjustment.String(), total.String(), people.String(), budget.String()
		row.Percent = Percent(total, entry.PlannedQty)
		row.Level = Attainment(total, entry.PlannedQty)
		if sheet, ok := current[entry.Department]; ok {
			cell := sheet.Sheet.Cells[entry.Ref.String()]
			if cell.Status != "" {
				row.Status = cell.Status
			}
			row.Content = cell.Content
		}
		data.Rows = append(data.Rows, row)
	}
	sort.Strings(data.Provisional)
	fingerprint := struct {
		Plan        port.KHTCPlan
		Sources     *SourceRef
		Metrics     []SourceMetric
		Submissions []Submitted
	}{plan, snapshot, metrics, data.Submissions}
	var err error
	data.InputHash, err = Hash(fingerprint)
	return data, err
}

type Filter struct {
	Level      string `json:"level"`
	Department string `json:"department"`
	Program    string `json:"program"`
	Commune    string `json:"commune"`
}

func (f Filter) Validate() error {
	if !slices.Contains([]string{"", "achieved", "progressing", "slow", "qualitative"}, f.Level) {
		return ErrInput
	}
	return nil
}

// SelectRows applies only display/export filters; it does not mutate the frozen report data.
func SelectRows(rows []ProgramRow, filter Filter) []ProgramRow {
	if filter.Department == "" && filter.Program == "" && filter.Commune == "" && filter.Level == "" {
		return append([]ProgramRow{}, rows...)
	}
	byRef := map[uuid.UUID]ProgramRow{}
	for _, row := range rows {
		byRef[row.Ref] = row
	}
	keep := map[uuid.UUID]bool{}
	for _, row := range rows {
		if row.Kind != "item" {
			continue
		}
		if filter.Level != "" && row.Level != filter.Level {
			continue
		}
		if filter.Department != "" && row.Department != filter.Department {
			continue
		}
		if filter.Commune != "" && !slices.Contains(row.Communes, filter.Commune) {
			continue
		}
		program := filter.Program == "" || row.Ref.String() == filter.Program || (row.ProgramRef != nil && row.ProgramRef.String() == filter.Program)
		parents := []uuid.UUID{row.Ref}
		parent := row.ParentRef
		for depth := 0; parent != nil && depth < 16; depth++ {
			parents = append(parents, *parent)
			if parent.String() == filter.Program {
				program = true
			}
			ancestor, ok := byRef[*parent]
			if !ok {
				break
			}
			parent = ancestor.ParentRef
		}
		if program {
			for _, id := range parents {
				keep[id] = true
			}
		}
	}
	out := []ProgramRow{}
	for _, row := range rows {
		if keep[row.Ref] {
			out = append(out, row)
		}
	}
	return out
}

// WithYields derives provincial yields from total output / total harvested area.
// Averaging commune yields or adding weekly ratios would bias the result.
func WithYields(metrics []SourceMetric) []SourceMetric {
	out := append([]SourceMetric{}, metrics...)
	byKey := map[string]SourceMetric{}
	for _, metric := range metrics {
		byKey[metric.Key] = metric
	}
	for _, area := range metrics {
		if area.Department != "trong_trot" || area.Value.Cumulative == nil {
			continue
		}
		outputKey := ""
		for _, pair := range [][2]string{{":dt_th_luy_ke", ":san_luong_luy_ke"}, {":dt_th", ":san_luong"}} {
			if strings.HasSuffix(area.Key, pair[0]) {
				outputKey = strings.TrimSuffix(area.Key, pair[0]) + pair[1]
				break
			}
		}
		output, ok := byKey[outputKey]
		if !ok || output.Value.Cumulative == nil {
			continue
		}
		var value *decimal.Decimal
		if area.Value.Cumulative.IsPositive() {
			n := output.Value.Cumulative.Div(*area.Value.Cumulative).Round(2)
			value = &n
		}
		key := "yield:" + area.Key
		out = append(out, SourceMetric{Key: key, Department: area.Department, Form: area.Form, Label: "Năng suất — " + strings.TrimPrefix(area.Label, "Diện tích thu hoạch"), Unit: "tấn/ha", Stock: true, Trace: append(append([]SourceTrace{}, area.Trace...), output.Trace...), Value: source.Value{SourceKey: key, Mode: "yield", Cumulative: value, Closed: area.Value.Closed && output.Value.Closed, Unavailable: area.Value.Unavailable || output.Value.Unavailable, NoSource: area.Value.NoSource && output.Value.NoSource, SourceKeys: append(append([]string{}, area.Value.SourceKeys...), output.Value.SourceKeys...), BaseRef: "sản lượng / diện tích thu hoạch"}})
	}
	return out
}

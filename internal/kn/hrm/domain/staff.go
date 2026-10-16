package domain

// W11.22 (pf 000150) — HỒ SƠ NHÂN SỰ (BRD HCTH v3.0 FN-07, FN-08): loại nhân sự, mã hồ sơ, quá trình có hiệu lực (sự
// kiện), phép chiếu tại một ngày (as-of: trạng thái, đơn vị, chức vụ, ngạch – bậc). Dữ liệu cá nhân KHÔNG ở đây: họ tên,
// SĐT, địa chỉ, ngày sinh, giới tính, email, các trường TK-01..06 nằm ở kho định danh (RB-3) — gói này chỉ giữ mã tham
// chiếu chủ thể và tên trường đã đổi.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// StaffType là loại nhân sự (HS-15).
type StaffType string

// Ba loại nhân sự của FN-07 B2.
const (
	StaffVienChuc StaffType = "vien_chuc" // Viên chức
	StaffHopDong  StaffType = "hop_dong"  // Hợp đồng lao động
	StaffCanBoXa  StaffType = "can_bo_xa" // Cán bộ khuyến nông cấp xã
)

// StaffTypes theo thứ tự hiển thị.
var StaffTypes = []StaffType{StaffVienChuc, StaffHopDong, StaffCanBoXa}

// ParseStaffType kiểm loại nhân sự.
func ParseStaffType(s string) (StaffType, bool) {
	for _, t := range StaffTypes {
		if string(t) == s {
			return t, true
		}
	}
	return "", false
}

// CodePrefix là tiền tố mã hồ sơ theo loại (HS-01: "sinh theo quy tắc, không trùng"). BRD ví dụ "VC-HCTH-011" gắn mã
// phòng — đơn vị đổi theo quá trình nên mã hồ sơ KHÔNG chứa đơn vị (mã không đổi suốt đời hồ sơ): VC-00011.
func (t StaffType) CodePrefix() string {
	switch t {
	case StaffHopDong:
		return "HD"
	case StaffCanBoXa:
		return "XA"
	}
	return "VC"
}

// StaffCode sinh mã hồ sơ từ số thứ tự trong loại.
func StaffCode(t StaffType, n int) string { return fmt.Sprintf("%s-%05d", t.CodePrefix(), n) }

// HasSensitiveTab: thẻ Thống kê BC Sở (TK-01..06) chỉ áp dụng viên chức, hợp đồng lao động.
func (t StaffType) HasSensitiveTab() bool { return t != StaffCanBoXa }

// Precision là mức chính xác của ngày (BR-05: "ngày cho phép chỉ năm hoặc tháng/năm, lưu kèm mức chính xác").
type Precision string

// Mức chính xác.
const (
	PrecisionDay   Precision = "day"
	PrecisionMonth Precision = "month"
	PrecisionYear  Precision = "year"
)

// ParseDate đọc "YYYY-MM-DD", "YYYY-MM" hoặc "YYYY" → ngày đầu kỳ (00:00 UTC) và mức chính xác.
func ParseDate(s string) (time.Time, Precision, error) {
	for _, f := range []struct {
		layout string
		p      Precision
	}{{"2006-01-02", PrecisionDay}, {"2006-01", PrecisionMonth}, {"2006", PrecisionYear}} {
		if len(s) == len(f.layout) {
			if t, err := time.Parse(f.layout, s); err == nil && t.Year() >= 1900 && t.Year() <= 2200 {
				return t, f.p, nil
			}
		}
	}
	return time.Time{}, "", invalid("ngày %q phải là YYYY-MM-DD, YYYY-MM hoặc YYYY", s)
}

// FormatDate in ngày theo mức chính xác.
func FormatDate(t time.Time, p Precision) string {
	switch p {
	case PrecisionYear:
		return t.Format("2006")
	case PrecisionMonth:
		return t.Format("2006-01")
	}
	return t.Format("2006-01-02")
}

// EventKind là loại sự kiện quá trình (cột kind của pf_hrm.staff_event).
type EventKind string

// Loại sự kiện. Mỗi loại có hiệu lực từ valid_from; "đến ngày" là valid_to (tăng cường, kỷ luật) hoặc sự kiện cùng
// loại kế tiếp (đơn vị, chức vụ, ngạch – bậc).
const (
	EventWorkStart  EventKind = "work_start" // FN-08: ngày vào làm
	EventWorkEnd    EventKind = "work_end"   // FN-08: ngày nghỉ việc + lý do (nghỉ hưu, chuyển công tác, thôi việc)
	EventUnit       EventKind = "unit"       // HS-16 đơn vị công tác (cán bộ xã: xã + đơn vị quản lý HS-37)
	EventPosition   EventKind = "position"   // HS-17..20 chức vụ, tên chức vụ ghi rõ, hệ số PCCV, bổ nhiệm
	EventSecondment EventKind = "secondment" // HS-21 tăng cường (từ – đến)
	EventGrade      EventKind = "grade"      // HS-23 ngạch + NB-04 bậc (xếp ngạch, chuyển ngạch)
	EventStep       EventKind = "step"       // NB-04 bậc (nâng bậc — FN-10 W11.25 ghi cùng loại)
	EventDegree     EventKind = "degree"     // HS-24 văn bằng chuyên môn
	EventTraining   EventKind = "training"   // HS-26..31 LLCT, QLNN, ngoại ngữ, tin học, bồi dưỡng
	EventAward      EventKind = "award"      // HS-36 khen thưởng (không số tiền)
	EventDiscipline EventKind = "discipline" // kỷ luật — cờ nhập tay cho nâng bậc (Q-075), không quản lý quy trình
)

// EventKinds theo thứ tự thẻ.
var EventKinds = []EventKind{EventWorkStart, EventWorkEnd, EventUnit, EventPosition, EventSecondment, EventGrade, EventStep,
	EventDegree, EventTraining, EventAward, EventDiscipline}

// ParseEventKind kiểm loại sự kiện.
func ParseEventKind(s string) (EventKind, bool) {
	for _, k := range EventKinds {
		if string(k) == s {
			return k, true
		}
	}
	return "", false
}

// Loại bồi dưỡng/trình độ của sự kiện training (HS-26..31).
const (
	TrainingPoliticalTheory = "political_theory" // HS-26, danh mục political_theory
	TrainingStateMgmt       = "state_mgmt"       // HS-27, danh mục state_mgmt
	TrainingForeignLanguage = "foreign_language" // HS-28, danh mục foreign_language + language_level
	TrainingIT              = "it_level"         // HS-29, danh mục it_level
	TrainingLeadership      = "leadership"       // HS-30: cap_phong | cap_so
	TrainingExtension       = "extension_cdnn"   // HS-31: bồi dưỡng CDNN chuyên ngành khuyến nông
)

// Ref là một tham chiếu tới danh mục nhân sự mà app phải kiểm "đang dùng".
type Ref struct {
	Kind Kind
	Code string
}

// fieldSpec mô tả một trường của payload.
type fieldSpec struct {
	required bool
	max      int  // độ dài tối đa (rune); 0 = theo loại
	catalog  Kind // khác rỗng: mã danh mục
	date     bool // ngày (YYYY-MM-DD | YYYY-MM | YYYY)
	decimal  bool // số thập phân ≥ 0, 2 chữ số
	integer  bool // số nguyên 1..99
	enum     []string
}

var eventFields = map[EventKind]map[string]fieldSpec{
	EventWorkStart: {"decision_no": {max: 50}},
	EventWorkEnd:   {"reason": {required: true, catalog: KindLeaveReason}, "decision_no": {max: 50}},
	EventUnit:      {"unit": {max: 40}, "commune": {max: 20}, "mgmt_unit": {max: 40}},
	EventPosition: {"position": {required: true, catalog: KindPosition}, "title": {max: 200}, "allowance": {decimal: true},
		"appointed_on": {date: true}, "decision_no": {max: 50}},
	EventSecondment: {"to_org": {required: true, max: 200}, "decision_no": {max: 50}},
	EventGrade: {"grade": {required: true, catalog: KindGrade}, "step": {required: true, integer: true}, "step_since": {date: true},
		"reserved_coeff": {decimal: true}, "decision_no": {max: 50}},
	EventStep:   {"step": {required: true, integer: true}, "step_since": {date: true}, "reserved_coeff": {decimal: true}, "decision_no": {max: 50}},
	EventDegree: {"level": {required: true, catalog: KindEducationLevel}, "major": {catalog: KindMajor}, "form": {enum: []string{"chinh_quy", "tai_chuc"}}, "institution": {max: 200}},
	EventTraining: {"type": {required: true, enum: []string{TrainingPoliticalTheory, TrainingStateMgmt, TrainingForeignLanguage, TrainingIT,
		TrainingLeadership, TrainingExtension}}, "code": {max: 40}, "level": {catalog: KindLanguageLevel}, "cert": {enum: []string{"degree", "certificate"}}},
	EventAward:      {"form": {required: true, catalog: KindAwardForm}, "title": {max: 200}, "level": {max: 100}, "decision_no": {max: 50}},
	EventDiscipline: {"form": {required: true, catalog: KindDisciplineForm}, "decision_no": {max: 50}},
}

var reUnitCode = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,39}$`)
var reCommune = regexp.MustCompile(`^[0-9]{5}$`)

// EventInput là một sự kiện quá trình cần ghi (đã đọc từ HTTP).
type EventInput struct {
	Kind      EventKind
	ValidFrom string // YYYY-MM-DD | YYYY-MM | YYYY
	ValidTo   string // tùy chọn (tăng cường, kỷ luật)
	Payload   map[string]string
}

// Event là một sự kiện quá trình đã chuẩn hóa.
type Event struct {
	ID         uuid.UUID
	Kind       EventKind
	ValidFrom  time.Time
	Precision  Precision
	ValidTo    *time.Time
	Payload    map[string]string
	Void       bool       // dòng hủy (action = 'void')
	Supersedes *uuid.UUID // dòng bị thay/hủy
	Reason     string
	Seq        int64
	RecordedAt time.Time
	Actor      uuid.UUID
}

// NormalizeEvent kiểm và chuẩn hóa một sự kiện theo loại nhân sự; trả các tham chiếu danh mục cần kiểm "đang dùng".
// Không kiểm số bậc so với bảng hệ số (app tra bảng tại ngày hiệu lực).
func NormalizeEvent(t StaffType, in EventInput) (Event, []Ref, error) {
	spec, ok := eventFields[in.Kind]
	if !ok {
		return Event{}, nil, invalid("loại sự kiện %q", in.Kind)
	}
	from, prec, err := ParseDate(in.ValidFrom)
	if err != nil {
		return Event{}, nil, invalid("valid_from: %v", err)
	}
	ev := Event{Kind: in.Kind, ValidFrom: from, Precision: prec, Payload: map[string]string{}}
	if in.ValidTo != "" {
		if in.Kind != EventSecondment && in.Kind != EventDiscipline {
			return Event{}, nil, invalid("valid_to chỉ dùng cho tăng cường, kỷ luật")
		}
		to, _, err := ParseDate(in.ValidTo)
		if err != nil {
			return Event{}, nil, invalid("valid_to: %v", err)
		}
		if to.Before(from) {
			return Event{}, nil, invalid("valid_to trước valid_from")
		}
		ev.ValidTo = &to
	}
	var refs []Ref
	for k, v := range in.Payload {
		f, ok := spec[k]
		if !ok {
			return Event{}, nil, invalid("trường %q không thuộc sự kiện %s", k, in.Kind)
		}
		if v == "" {
			continue
		}
		switch {
		case f.catalog != "":
			v = NormalizeCode(f.catalog, v)
			if !ValidCode(f.catalog, v) {
				return Event{}, nil, invalid("%s: mã %q", k, v)
			}
			refs = append(refs, Ref{f.catalog, v})
		case f.date:
			d, p, err := ParseDate(v)
			if err != nil {
				return Event{}, nil, invalid("%s: %v", k, err)
			}
			v = FormatDate(d, p)
		case f.decimal:
			d, err := decimal.NewFromString(v)
			if err != nil || d.IsNegative() || !d.Equal(d.Round(2)) || d.GreaterThan(decimal.RequireFromString("99.99")) {
				return Event{}, nil, invalid("%s: số thập phân 0–99,99, tối đa 2 chữ số", k)
			}
			v = d.StringFixed(2)
		case f.integer:
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 99 {
				return Event{}, nil, invalid("%s: số nguyên 1–99", k)
			}
			v = strconv.Itoa(n)
		case len(f.enum) > 0:
			if !contains(f.enum, v) {
				return Event{}, nil, invalid("%s: %q không thuộc %v", k, v, f.enum)
			}
		default:
			if utf8.RuneCountInString(v) > f.max {
				return Event{}, nil, invalid("%s: tối đa %d ký tự", k, f.max)
			}
		}
		ev.Payload[k] = v
	}
	for k, f := range spec {
		if f.required && ev.Payload[k] == "" {
			return Event{}, nil, invalid("thiếu %s", k)
		}
	}
	switch in.Kind {
	case EventUnit:
		// BR-07: cán bộ xã gắn một xã (ĐVHC) và một đơn vị quản lý (HS-37); nhân sự khác gắn đơn vị nội bộ (HS-16).
		if t == StaffCanBoXa {
			if !reCommune.MatchString(ev.Payload["commune"]) || !reUnitCode.MatchString(ev.Payload["mgmt_unit"]) {
				return Event{}, nil, invalid("cán bộ khuyến nông cấp xã phải chọn xã (mã ĐVHC 5 số) và đơn vị quản lý")
			}
			delete(ev.Payload, "unit")
		} else {
			if !reUnitCode.MatchString(ev.Payload["unit"]) {
				return Event{}, nil, invalid("thiếu đơn vị công tác")
			}
			delete(ev.Payload, "commune")
			delete(ev.Payload, "mgmt_unit")
		}
	case EventTraining:
		typ := ev.Payload["type"]
		catalogOf := map[string]Kind{TrainingPoliticalTheory: KindPoliticalTheory, TrainingStateMgmt: KindStateMgmt,
			TrainingForeignLanguage: KindForeignLanguage, TrainingIT: KindITLevel}
		if k, ok := catalogOf[typ]; ok {
			c := NormalizeCode(k, ev.Payload["code"])
			if !ValidCode(k, c) {
				return Event{}, nil, invalid("%s: thiếu mã danh mục", typ)
			}
			ev.Payload["code"] = c
			refs = append(refs, Ref{k, c})
		} else if typ == TrainingLeadership {
			if c := ev.Payload["code"]; c != "cap_phong" && c != "cap_so" {
				return Event{}, nil, invalid("bồi dưỡng lãnh đạo: cap_phong | cap_so")
			}
		} else {
			delete(ev.Payload, "code")
		}
		if typ != TrainingForeignLanguage {
			if ev.Payload["level"] != "" || ev.Payload["cert"] != "" {
				return Event{}, nil, invalid("trình độ, loại văn bằng chỉ dùng cho ngoại ngữ")
			}
		}
	case EventGrade, EventStep:
		if ev.Payload["step_since"] == "" {
			ev.Payload["step_since"] = FormatDate(from, prec) // Q-069: ngày xếp bậc mặc định = ngày hiệu lực
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].Kind < refs[j].Kind || refs[i].Kind == refs[j].Kind && refs[i].Code < refs[j].Code
	})
	return ev, refs, nil
}

// Effective trả các sự kiện còn hiệu lực ghi sổ: bỏ dòng hủy và mọi dòng đã bị thay/hủy bởi dòng sau. Thứ tự: valid_from
// rồi seq.
func Effective(evs []Event) []Event {
	dead := map[uuid.UUID]bool{}
	for _, e := range evs {
		if e.Supersedes != nil {
			dead[*e.Supersedes] = true
		}
	}
	out := make([]Event, 0, len(evs))
	for _, e := range evs {
		if !e.Void && !dead[e.ID] {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].ValidFrom.Equal(out[j].ValidFrom) {
			return out[i].ValidFrom.Before(out[j].ValidFrom)
		}
		return out[i].Seq < out[j].Seq
	})
	return out
}

// Snapshot là phép chiếu của hồ sơ tại một ngày (as-of). Rỗng = chưa có sự kiện hiệu lực tới ngày đó.
type Snapshot struct {
	At          time.Time
	Status      string     // "active" (Đang công tác) | "left" (Đã nghỉ việc) | "none" (chưa vào làm)
	StartedOn   *time.Time // ngày vào làm của lần làm việc hiện hành
	EndedOn     *time.Time
	LeaveReason string
	Unit        string // HS-16
	Commune     string // cán bộ xã
	MgmtUnit    string // HS-37
	Position    string
	Title       string
	Allowance   string
	AppointedOn string
	Grade       string
	Step        int
	StepSince   string // Q-069: ngày xếp bậc hiện tại
	Reserved    string // Q-069: hệ số bảo lưu
	Secondments []Event
	Disciplined bool // đang thi hành kỷ luật tại ngày (cờ — Q-075)
	TopDegree   string
}

// Status của hồ sơ.
const (
	StatusActive = "active"
	StatusLeft   = "left"
	StatusNone   = "none"
)

// Project chiếu các sự kiện (đã Effective hoặc thô) tại ngày at. Bậc là phép chiếu: sự kiện ngạch đặt ngạch + bậc, sự
// kiện bậc sau đó thay bậc. Trạng thái (FN-08 BR-01): có ngày vào làm ≤ at và không có ngày nghỉ việc trong (vào làm;
// at] → Đang công tác; ngày nghỉ việc ≤ at → Đã nghỉ việc từ ngày đó.
func Project(evs []Event, at time.Time, degreeRank func(level string) int) Snapshot {
	s := Snapshot{At: at, Status: StatusNone}
	var grade *Event
	for _, e := range Effective(evs) {
		if e.ValidFrom.After(at) {
			continue
		}
		p := e.Payload
		switch e.Kind {
		case EventWorkStart:
			d := e.ValidFrom
			s.Status, s.StartedOn, s.EndedOn, s.LeaveReason = StatusActive, &d, nil, ""
		case EventWorkEnd:
			if s.StartedOn != nil && !e.ValidFrom.Before(*s.StartedOn) {
				d := e.ValidFrom
				s.Status, s.EndedOn, s.LeaveReason = StatusLeft, &d, p["reason"]
			}
		case EventUnit:
			s.Unit, s.Commune, s.MgmtUnit = p["unit"], p["commune"], p["mgmt_unit"]
		case EventPosition:
			s.Position, s.Title, s.Allowance, s.AppointedOn = p["position"], p["title"], p["allowance"], p["appointed_on"]
		case EventGrade:
			g := e
			grade = &g
			s.Grade, s.StepSince, s.Reserved = p["grade"], p["step_since"], p["reserved_coeff"]
			s.Step, _ = strconv.Atoi(p["step"])
		case EventStep:
			if grade != nil {
				s.Step, _ = strconv.Atoi(p["step"])
				s.StepSince, s.Reserved = p["step_since"], p["reserved_coeff"]
			}
		case EventSecondment:
			if e.ValidTo == nil || !e.ValidTo.Before(at) {
				s.Secondments = append(s.Secondments, e)
			}
		case EventDiscipline:
			if e.ValidTo == nil || !e.ValidTo.Before(at) {
				s.Disciplined = true
			}
		case EventDegree:
			if degreeRank == nil || degreeRank(p["level"]) >= degreeRank(s.TopDegree) {
				s.TopDegree = p["level"]
			}
		}
	}
	return s
}

// ValidateWork kiểm FN-08: ngày nghỉ việc không trước ngày vào làm của lần làm việc đó; ngày vào làm mới không trước
// ngày nghỉ việc trước đó. evs là các sự kiện hiệu lực đã có (kể cả ev sau khi thêm).
func ValidateWork(evs []Event) error {
	var start *time.Time
	ended := true
	for _, e := range Effective(evs) {
		switch e.Kind {
		case EventWorkStart:
			if !ended {
				return invalid("đã có ngày vào làm %s chưa có ngày nghỉ việc", start.Format("2006-01-02"))
			}
			d := e.ValidFrom
			start, ended = &d, false
		case EventWorkEnd:
			if start == nil || ended {
				return invalid("ngày nghỉ việc %s không có ngày vào làm trước đó", e.ValidFrom.Format("2006-01-02"))
			}
			if e.ValidFrom.Before(*start) {
				return invalid("ngày nghỉ việc trước ngày vào làm")
			}
			ended = true
		}
	}
	return nil
}

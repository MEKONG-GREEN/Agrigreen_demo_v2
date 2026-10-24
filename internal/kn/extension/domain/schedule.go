package domain

// KN v7.7 gói P5 — lịch công tác tuần (FN-XA-03.08, FN-TI-02.07). Quy tắc thuần, không phụ thuộc hạ tầng,
// không time.Now() (R-12): "bây giờ" và múi giờ truyền vào.

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Trạng thái lịch (FN-XA-03.08: Nháp → Chờ duyệt → Đã duyệt / Yêu cầu chỉnh sửa).
const (
	ScheduleDraft            = "draft"
	SchedulePending          = "pending"
	ScheduleApproved         = "approved"
	ScheduleChangesRequested = "changes_requested"
)

// Sự việc của lịch (sổ work_schedule_event).
const (
	ScheduleActDraftSaved      = "draft_saved"
	ScheduleActSubmitted       = "submitted"
	ScheduleActAmended         = "amended"
	ScheduleActApproved        = "approved"
	ScheduleActChangeRequested = "change_requested"
	ScheduleActLocked          = "locked"
)

// Thao tác lưu của người đăng ký.
const (
	SaveDraft  = "draft"
	SaveSubmit = "submit"
)

// Buổi.
const (
	SessionMorning   = "morning"
	SessionAfternoon = "afternoon"
)

// Giới hạn.
const (
	MaxScheduleItems = 100
	// MaxWeeksAhead: đăng ký trước tối đa bao nhiêu tuần (chặn nhập nhầm năm).
	MaxWeeksAhead = 8
)

// Lỗi quy tắc lịch.
var (
	ErrWeekNotMonday     = errors.New("phải là Thứ Hai đầu tuần (YYYY-MM-DD)")
	ErrWeekTooFar        = errors.New("chỉ đăng ký tối đa 8 tuần tới")
	ErrItemOutsideWeek   = errors.New("ngày phải thuộc tuần đã chọn")
	ErrItemSession       = errors.New("buổi phải là morning (Sáng) hoặc afternoon (Chiều)")
	ErrItemContent       = errors.New("nội dung bắt buộc, 3–500 ký tự")
	ErrItemTime          = errors.New("giờ dạng HH:MM, giờ kết thúc sau giờ bắt đầu")
	ErrItemTooLong       = errors.New("quá dài")
	ErrItemDuplicateID   = errors.New("mã mục bị trùng")
	ErrTooManyItems      = errors.New("tối đa 100 mục một tuần")
	ErrSubmitEmpty       = errors.New("lịch gửi duyệt phải có ít nhất một mục")
	ErrItemCommune       = errors.New("xã không thuộc các xã phụ trách")
	ErrItemCommuneReq    = errors.New("xã/phường bắt buộc")
	ErrItemLocationReq   = errors.New("địa điểm bắt buộc (nhập địa điểm hoặc chọn vị trí)")
	ErrScheduleReasonLen = errors.New("bắt buộc, 10–1000 ký tự")
)

// ScheduleItem là một mục công tác của một phiên bản lịch.
type ScheduleItem struct {
	RowID          uuid.UUID // dòng sổ của phiên bản
	ItemID         uuid.UUID // mục logic, giữ qua các phiên bản
	Version        int
	Position       int
	Day            time.Time // 00:00 UTC của ngày nghiệp vụ
	Session        string
	Start, End     string // "HH:MM" hoặc rỗng
	Content        string
	CommuneCode    string
	CooperativeRef uuid.UUID
	Location       string
	Participants   string
	Note           string
	SupersedesID   *uuid.UUID
	// Place: địa điểm công tác chọn trên bản đồ (CNTS CV-06, W11.46) — sổ phụ work_schedule_item_place. nil = không có.
	Place *GeoPoint
}

// DayString là ngày dạng YYYY-MM-DD.
func (it ScheduleItem) DayString() string { return it.Day.Format(time.DateOnly) }

// sameContent cho biết hai mục giống nhau về nội dung hiển thị (bỏ qua dòng, phiên bản, vị trí).
func (it ScheduleItem) sameContent(o ScheduleItem) bool {
	return it.DayString() == o.DayString() && it.Session == o.Session && it.Start == o.Start && it.End == o.End &&
		it.Content == o.Content && it.CommuneCode == o.CommuneCode && it.CooperativeRef == o.CooperativeRef &&
		it.Location == o.Location && it.Participants == o.Participants && it.Note == o.Note && it.Place.Equal(o.Place)
}

// ParseDate đọc ngày nghiệp vụ YYYY-MM-DD thành 00:00 UTC (kiểu date của DB).
func ParseDate(s string) (time.Time, error) {
	return time.Parse(time.DateOnly, strings.TrimSpace(s))
}

// civil trả ngày nghiệp vụ (00:00 UTC) của thời điểm t theo múi giờ zone.
func civil(t time.Time, zone *time.Location) time.Time {
	l := t.In(zone)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}

// WeekStartOf trả Thứ Hai của tuần chứa t (giờ địa phương zone).
func WeekStartOf(t time.Time, zone *time.Location) time.Time {
	d := civil(t, zone)
	off := (int(d.Weekday()) + 6) % 7 // Thứ Hai = 0
	return d.AddDate(0, 0, -off)
}

// NextWeekStart là tuần mặc định khi đăng ký (FN-XA-03.08: "mặc định tuần kế tiếp").
func NextWeekStart(now time.Time, zone *time.Location) time.Time {
	return WeekStartOf(now, zone).AddDate(0, 0, 7)
}

// ValidWeekStart kiểm ngày là Thứ Hai.
func ValidWeekStart(d time.Time) bool { return d.Weekday() == time.Monday }

// WeekInstant là 00:00 giờ địa phương của ngày nghiệp vụ d.
func WeekInstant(d time.Time, zone *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, zone)
}

// WeekLocked: tuần đã kết thúc (sau 24:00 Chủ Nhật giờ VN) bị khóa — chỉ xem (FN-TI-02.07, FN-XA-03.08).
func WeekLocked(weekStart, now time.Time, zone *time.Location) bool {
	return !now.Before(WeekInstant(weekStart, zone).AddDate(0, 0, 7))
}

// ScheduleDueAt = 00:00 Thứ Hai của tuần (giờ địa phương) + offsetHours.
func ScheduleDueAt(weekStart time.Time, offsetHours int, zone *time.Location) time.Time {
	return WeekInstant(weekStart, zone).Add(time.Duration(offsetHours) * time.Hour).UTC()
}

// CheckWeek kiểm tuần đăng ký: Thứ Hai, không quá MaxWeeksAhead tuần tới.
func CheckWeek(weekStart, now time.Time, zone *time.Location) error {
	if !ValidWeekStart(weekStart) {
		return FieldError{Field: "week_start", Err: ErrWeekNotMonday}
	}
	if weekStart.After(WeekStartOf(now, zone).AddDate(0, 0, 7*MaxWeeksAhead)) {
		return FieldError{Field: "week_start", Err: ErrWeekTooFar}
	}
	return nil
}

// ParseClock đọc "HH:MM" (rỗng = không có giờ).
func ParseClock(s string) (string, int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", -1, nil
	}
	if len(s) == 8 && strings.HasSuffix(s, ":00") { // dạng time của DB "08:30:00"
		s = s[:5]
	}
	if len(s) != 5 || s[2] != ':' {
		return "", 0, ErrItemTime
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return "", 0, ErrItemTime
	}
	return s, h*60 + m, nil
}

// NormalizeItems kiểm và chuẩn hóa mục của một phiên bản: ngày trong tuần, buổi, giờ, độ dài, địa điểm bắt buộc (chữ hoặc vị trí), xã bắt buộc và thuộc
// allowedCommunes (nil = không giới hạn phạm vi, vẫn bắt buộc có xã), không SĐT/CCCD trong văn bản tự do (RB-3, gap §4.2). Mục thiếu
// ItemID được gán newID(). Thứ tự sau chuẩn hóa: ngày, buổi, giờ bắt đầu, vị trí gửi lên.
func NormalizeItems(weekStart time.Time, items []ScheduleItem, allowedCommunes []string, newID func() uuid.UUID) ([]ScheduleItem, error) {
	if len(items) > MaxScheduleItems {
		return nil, FieldError{Field: "items", Err: ErrTooManyItems}
	}
	end := weekStart.AddDate(0, 0, 7)
	seen := map[uuid.UUID]bool{}
	out := make([]ScheduleItem, 0, len(items))
	for i, it := range items {
		f := func(name string) string { return fmt.Sprintf("items[%d].%s", i, name) }
		if it.ItemID == uuid.Nil {
			it.ItemID = newID()
		}
		if seen[it.ItemID] {
			return nil, FieldError{Field: f("id"), Err: ErrItemDuplicateID}
		}
		seen[it.ItemID] = true
		if it.Day.Before(weekStart) || !it.Day.Before(end) {
			return nil, FieldError{Field: f("day"), Err: ErrItemOutsideWeek}
		}
		if it.Session != SessionMorning && it.Session != SessionAfternoon {
			return nil, FieldError{Field: f("session"), Err: ErrItemSession}
		}
		var sm, em int
		var err error
		if it.Start, sm, err = ParseClock(it.Start); err != nil {
			return nil, FieldError{Field: f("start_time"), Err: err}
		}
		if it.End, em, err = ParseClock(it.End); err != nil {
			return nil, FieldError{Field: f("end_time"), Err: err}
		}
		if sm >= 0 && em >= 0 && em <= sm {
			return nil, FieldError{Field: f("end_time"), Err: ErrItemTime}
		}
		it.Content = strings.TrimSpace(it.Content)
		if n := len([]rune(it.Content)); n < 3 || n > 500 {
			return nil, FieldError{Field: f("content"), Err: ErrItemContent}
		}
		it.CommuneCode = strings.TrimSpace(it.CommuneCode)
		// Xã/phường bắt buộc với mọi người đăng ký (log bug TT-05/CN-04; quyết định PO 25/10/2026: cả cán bộ tỉnh).
		if it.CommuneCode == "" {
			return nil, FieldError{Field: f("commune_code"), Err: ErrItemCommuneReq}
		}
		if it.CommuneCode != "" && allowedCommunes != nil && !contains(allowedCommunes, it.CommuneCode) {
			return nil, FieldError{Field: f("commune_code"), Err: ErrItemCommune}
		}
		it.Location, it.Participants, it.Note = strings.TrimSpace(it.Location), strings.TrimSpace(it.Participants), strings.TrimSpace(it.Note)
		// Địa điểm bắt buộc: chữ (vd "UBND xã") hoặc vị trí trên bản đồ (dùng để check-in).
		if it.Location == "" && it.Place == nil {
			return nil, FieldError{Field: f("location"), Err: ErrItemLocationReq}
		}
		if it.Place != nil {
			p, err := it.Place.Normalize()
			if err != nil {
				return nil, FieldError{Field: f("place"), Err: err}
			}
			it.Place = &p
		}
		for _, t := range []struct {
			name string
			v    string
			max  int
		}{{"location", it.Location, 200}, {"participants", it.Participants, 500}, {"note", it.Note, 1000}, {"content", it.Content, 500}} {
			if len([]rune(t.v)) > t.max {
				return nil, FieldError{Field: f(t.name), Err: ErrItemTooLong}
			}
			if ContainsPII(t.v) {
				return nil, FieldError{Field: f(t.name), Err: ErrPIIInText}
			}
		}
		it.Position = i
		out = append(out, it)
	}
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if !x.Day.Equal(y.Day) {
			return x.Day.Before(y.Day)
		}
		if x.Session != y.Session {
			return x.Session == SessionMorning
		}
		return x.Start < y.Start
	})
	for i := range out {
		out[i].Position = i
	}
	return out, nil
}

// Khung giờ mặc định của buổi khi mục không ghi giờ (dùng chung với web-ops và field-app).
const (
	morningFrom, morningTo     = 7 * 60, 11*60 + 30
	afternoonFrom, afternoonTo = 13 * 60, 17 * 60
)

// interval trả khoảng [bắt đầu, kết thúc) theo phút: không ghi giờ → cả buổi; có giờ bắt đầu mà không có giờ kết
// thúc → 1 giờ.
func interval(it ScheduleItem) (int, int) {
	from, to := morningFrom, morningTo
	if it.Session == SessionAfternoon {
		from, to = afternoonFrom, afternoonTo
	}
	_, s, err := ParseClock(it.Start)
	if err != nil || s < 0 {
		return from, to
	}
	_, e, err := ParseClock(it.End)
	if err != nil || e <= s {
		e = s + 60
	}
	return s, e
}

// Conflicts trả các mục trùng thời gian (FN-XA-03.08: "trùng thời gian → cảnh báo"; Hình 12: mục trùng giờ tô
// vàng): hai mục cùng ngày có khoảng giờ giao nhau (interval). Chỉ là cảnh báo, không chặn lưu.
func Conflicts(items []ScheduleItem) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			a, b := items[i], items[j]
			if a.DayString() != b.DayString() {
				continue
			}
			as, ae := interval(a)
			bs, be := interval(b)
			if as < be && bs < ae {
				out[a.ItemID], out[b.ItemID] = true, true
			}
		}
	}
	return out
}

// ChangedSince đánh dấu mục khác bản đã duyệt (mục mới hoặc đổi nội dung) — "phần điều chỉnh chờ duyệt lại".
func ChangedSince(items, approved []ScheduleItem) map[uuid.UUID]bool {
	prev := map[uuid.UUID]ScheduleItem{}
	for _, it := range approved {
		prev[it.ItemID] = it
	}
	out := map[uuid.UUID]bool{}
	for _, it := range items {
		if p, ok := prev[it.ItemID]; !ok || !p.sameContent(it) {
			out[it.ItemID] = true
		}
	}
	return out
}

// FieldChange là một dòng "trường: cũ → mới" của khung Log (kế hoạch §1.5).
type FieldChange struct {
	Field string
	Old   *string
	New   *string
}

var weekdayVI = [...]string{"CN", "T2", "T3", "T4", "T5", "T6", "T7"}

// ItemLabel là nhãn ngắn của mục trong Log, vd "T2 21/09 Sáng 08:00".
func ItemLabel(it ScheduleItem) string {
	s := "Sáng"
	if it.Session == SessionAfternoon {
		s = "Chiều"
	}
	l := fmt.Sprintf("%s %s %s", weekdayVI[it.Day.Weekday()], it.Day.Format("02/01"), s)
	if it.Start != "" {
		l += " " + it.Start
	}
	return l
}

func itemSummary(it ScheduleItem) string {
	parts := []string{ItemLabel(it)}
	if it.End != "" {
		parts[0] += "–" + it.End
	}
	parts = append(parts, it.Content)
	if it.Location != "" {
		parts = append(parts, it.Location)
	}
	return strings.Join(parts, " · ")
}

func strp(s string) *string { return &s }

// DiffItems so hai phiên bản (cũ → mới) thành các dòng Log: thêm mục, bỏ mục, đổi từng trường của mục.
func DiffItems(old, cur []ScheduleItem) []FieldChange {
	prev := map[uuid.UUID]ScheduleItem{}
	for _, it := range old {
		prev[it.ItemID] = it
	}
	var out []FieldChange
	seen := map[uuid.UUID]bool{}
	for _, it := range cur {
		seen[it.ItemID] = true
		p, ok := prev[it.ItemID]
		if !ok {
			out = append(out, FieldChange{Field: "Thêm mục", New: strp(itemSummary(it))})
			continue
		}
		label := ItemLabel(p)
		cmp := func(name, a, b string) {
			if a != b {
				var o, n *string
				if a != "" {
					o = strp(a)
				}
				if b != "" {
					n = strp(b)
				}
				out = append(out, FieldChange{Field: label + " · " + name, Old: o, New: n})
			}
		}
		cmp("Ngày", p.DayString(), it.DayString())
		cmp("Buổi", sessionVI(p.Session), sessionVI(it.Session))
		cmp("Giờ bắt đầu", p.Start, it.Start)
		cmp("Giờ kết thúc", p.End, it.End)
		cmp("Nội dung", p.Content, it.Content)
		cmp("Xã", p.CommuneCode, it.CommuneCode)
		cmp("HTX", refString(p.CooperativeRef), refString(it.CooperativeRef))
		cmp("Địa điểm", p.Location, it.Location)
		cmp("Thành phần phối hợp", p.Participants, it.Participants)
		cmp("Ghi chú", p.Note, it.Note)
	}
	for _, it := range old {
		if !seen[it.ItemID] {
			out = append(out, FieldChange{Field: "Bỏ mục", Old: strp(itemSummary(it))})
		}
	}
	return out
}

func sessionVI(s string) string {
	switch s {
	case SessionMorning:
		return "Sáng"
	case SessionAfternoon:
		return "Chiều"
	}
	return s
}

func refString(u uuid.UUID) string {
	if u == uuid.Nil {
		return ""
	}
	return u.String()
}

// StatusVI là nhãn tiếng Việt của trạng thái.
func StatusVI(s string) string {
	switch s {
	case ScheduleDraft:
		return "Nháp"
	case SchedulePending:
		return "Chờ duyệt"
	case ScheduleApproved:
		return "Đã duyệt"
	case ScheduleChangesRequested:
		return "Yêu cầu chỉnh sửa"
	}
	return s
}

// Lỗi chuyển trạng thái.
var (
	ErrScheduleDraftAfterSubmit = errors.New("lịch đang chờ duyệt hoặc đã duyệt — điều chỉnh bằng Gửi duyệt")
	ErrScheduleNotPending       = errors.New("lịch không ở trạng thái Chờ duyệt")
)

// NextOnSave trả sự việc và trạng thái sau khi người đăng ký lưu (action draft|submit) lịch đang ở status
// (rỗng = chưa có). approvedVersion > 0 nghĩa là lịch đã có bản được duyệt → gửi là "điều chỉnh".
//
//   - Nháp: chỉ khi chưa gửi (chưa có, Nháp) hoặc bị Yêu cầu chỉnh sửa.
//   - Gửi duyệt: từ mọi trạng thái; từ Chờ duyệt là thay bản chờ duyệt; có bản đã duyệt → amended.
func NextOnSave(status, action string, approvedVersion int) (act, after string, err error) {
	switch action {
	case SaveDraft:
		if status == SchedulePending || status == ScheduleApproved {
			return "", "", ErrScheduleDraftAfterSubmit
		}
		return ScheduleActDraftSaved, ScheduleDraft, nil
	case SaveSubmit:
		if approvedVersion > 0 {
			return ScheduleActAmended, SchedulePending, nil
		}
		return ScheduleActSubmitted, SchedulePending, nil
	}
	return "", "", FieldError{Field: "action", Err: errors.New("draft hoặc submit")}
}

// ValidScheduleReason kiểm lý do yêu cầu chỉnh sửa: 10–1000 ký tự, không SĐT/CCCD.
func ValidScheduleReason(reason string) error {
	r := strings.TrimSpace(reason)
	if n := len([]rune(r)); n < 10 || n > 1000 {
		return FieldError{Field: "reason", Err: ErrScheduleReasonLen}
	}
	if ContainsPII(r) {
		return FieldError{Field: "reason", Err: ErrPIIInText}
	}
	return nil
}

// ScheduleStreamKey là khóa chuỗi băm của một lịch trong hai sổ.
func ScheduleStreamKey(id uuid.UUID) string { return "wsched:" + id.String() }

// ApproverLevelAllowed: cấp quản lý level được duyệt theo danh sách levels (Q-05). level rỗng (chưa có P1, không
// biết cấp quản lý) → được (tạm, chờ khách).
func ApproverLevelAllowed(level string, levels []string) bool {
	if level == "" {
		return true
	}
	return contains(levels, strings.ToLower(strings.TrimSpace(level)))
}

// ParseLevels đọc danh sách cấp quản lý "truong_phong, pho_truong_phong".
func ParseLevels(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Schedule là trạng thái hiện hành của một lịch tuần (bảng chiếu work_schedule_current).
type Schedule struct {
	ID              uuid.UUID
	OwnerRef        uuid.UUID
	OwnerLevel      string
	Department      string
	ProvinceCode    string
	Communes        []string
	WeekStart       time.Time
	Status          string
	Version         int
	ApprovedVersion int // 0 = chưa có bản được duyệt
	Amendment       bool
	Late            bool
	ItemCount       int
	ConflictCount   int
	FirstSubmitted  *time.Time
	SubmittedAt     *time.Time
	ReviewedAt      *time.Time
	ReviewerRef     uuid.UUID
	LastReason      string
	LockedAt        *time.Time
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	UpdatedBy       uuid.UUID
	UpdatedAt       time.Time
	LastEventID     uuid.UUID
	LastSeq         int64
}

// ScheduleEvent là một dòng sổ work_schedule_event.
type ScheduleEvent struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	ScheduleID  uuid.UUID
	Version     int
	Action      string
	StatusAfter string
	OwnerRef    uuid.UUID
	OwnerLevel  string
	Department  string
	Province    string
	Communes    []string
	WeekStart   time.Time
	ItemCount   int
	Late        bool
	Reason      string
	Note        string
	EventTime   time.Time
	RecordedAt  time.Time
	ActorRef    uuid.UUID
	DeviceID    *string
	IdemKey     string
	Seq         int64
}

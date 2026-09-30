package domain

// Quy tắc nghiệm thu G3 lấy từ prototype mekong-green src/erp/field/service.ts — chép
// QUY TẮC, không chép mã:
//   - FM-01: không cuộn rơm trước ngày gặt (số cuộn công đoạn bale có ngày < ngày gặt → từ
//     chối). Chưa biết ngày gặt → vẫn cho ghi nhưng GẮN CỜ, không bao giờ im lặng;
//   - FM-02: rơm phải cuộn xong trong N ngày sau gặt (N = baling_deadline_days, tham số có
//     phiên bản, mặc định 3 = MAX_DAYS_AFTER_HARVEST của prototype — tạm, chờ khách); quá hạn
//     KHÔNG chặn (rơm vẫn phải thu) nhưng gắn cờ;
//   - FM-06: phân công không vượt năng lực/ngày của tổ; vượt thì từ chối, trừ khi giám sát
//     ghi lý do (phân công tay được vượt nhưng phải thấy cảnh báo);
//   - FM-07: chỉ tổ đang hoạt động nhận việc;
//   - FM-08: gợi ý phân công tất định (cùng dữ liệu → cùng thứ tự), không bao giờ vượt năng
//     lực; giám sát xác nhận; xác nhận lặp không sinh phân công trùng.

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Phiên bản quy tắc (cột rule_version).
const (
	RuleCrew           = "fieldops.crew.v1"
	RuleAssignment     = "fieldops.crew_assignment.v1" // FM-06/07/08
	RuleHarvestCheck   = "fieldops.harvest_check.v1"   // FM-01
	RuleBalingDeadline = "fieldops.baling_deadline.v1" // FM-02
	RuleParam          = "fieldops.param.v1"
)

// Trạng thái / sự kiện tổ gom.
const (
	CrewActive      = "active"
	CrewInactive    = "inactive"
	CrewCreated     = "created"
	CrewRevised     = "revised"
	CrewDeactivated = "deactivated"
	CrewReactivated = "reactivated"
)

// Đơn vị năng lực của tổ.
const (
	UnitBales = "bales" // cuộn/ngày
	UnitHa    = "ha"    // ha/ngày
)

// Cách giao tổ.
const (
	ModeManual    = "manual"
	ModeSuggested = "suggested"
)

// Lỗi nghiệp vụ tổ gom / phân công / ngày gặt; app/ quy về problem.
var (
	ErrBeforeHarvest = errors.New("fieldops: cuộn rơm trước ngày gặt (FM-01)")
	ErrCrewInactive  = errors.New("fieldops: tổ gom đã ngừng hoạt động (FM-07)")
)

var (
	crewCodeRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,31}$`)
	communeRe  = regexp.MustCompile(`^[0-9]{5}$`)
)

// CrewSpec là nội dung một phiên bản tổ gom.
type CrewSpec struct {
	Code            string
	Name            string
	MemberRefs      []uuid.UUID
	CapacityUnit    string
	CapacityPerDay  decimal.Decimal
	ServiceCommunes []string
}

// NormalizeCrew kiểm và chuẩn hóa tổ gom (mã in hoa, thành viên/xã không trùng, giữ thứ tự).
func NormalizeCrew(c CrewSpec) (CrewSpec, error) {
	c.Code = strings.ToUpper(strings.TrimSpace(c.Code))
	c.Name = strings.TrimSpace(c.Name)
	if !crewCodeRe.MatchString(c.Code) {
		return c, fieldErr("code", "2–32 ký tự A-Z, 0-9, '-', '_'")
	}
	if n := len([]rune(c.Name)); n < 2 || n > 200 {
		return c, fieldErr("name", "2–200 ký tự")
	}
	seen := map[uuid.UUID]bool{}
	members := make([]uuid.UUID, 0, len(c.MemberRefs))
	for _, m := range c.MemberRefs {
		if m == uuid.Nil {
			return c, fieldErr("member_refs", "UUID subject tài khoản MKG")
		}
		if !seen[m] {
			seen[m] = true
			members = append(members, m)
		}
	}
	if len(members) == 0 || len(members) > 100 {
		return c, fieldErr("member_refs", "1..100 thành viên")
	}
	c.MemberRefs = members
	if c.CapacityUnit != UnitBales && c.CapacityUnit != UnitHa {
		return c, fieldErr("capacity_unit", "bales | ha")
	}
	if c.CapacityPerDay.Sign() <= 0 || c.CapacityPerDay.GreaterThan(decimal.NewFromInt(1_000_000)) || c.CapacityPerDay.Exponent() < -3 {
		return c, fieldErr("capacity_per_day", "> 0, tối đa 3 chữ số thập phân")
	}
	if c.CapacityUnit == UnitBales && !c.CapacityPerDay.Equal(c.CapacityPerDay.Truncate(0)) {
		return c, fieldErr("capacity_per_day", "số cuộn/ngày là số nguyên")
	}
	cs := map[string]bool{}
	communes := make([]string, 0, len(c.ServiceCommunes))
	for _, s := range c.ServiceCommunes {
		s = strings.TrimSpace(s)
		if !communeRe.MatchString(s) {
			return c, fieldErr("service_communes", "mã xã 5 chữ số")
		}
		if !cs[s] {
			cs[s] = true
			communes = append(communes, s)
		}
	}
	if len(communes) > 50 {
		return c, fieldErr("service_communes", "tối đa 50 xã")
	}
	c.ServiceCommunes = communes
	return c, nil
}

// ---------------------------------------------------------------------------
// Ngày nghiệp vụ
// ---------------------------------------------------------------------------

// Day trả ngày nghiệp vụ (giờ Việt Nam) của t, biểu diễn 00:00 UTC — cùng dạng với ngày
// của khung lệnh gom (time.Parse "2006-01-02").
func Day(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// AddDays cộng n ngày vào một ngày nghiệp vụ.
func AddDays(d time.Time, n int) time.Time { return d.AddDate(0, 0, n) }

// DaysBetween = to − from (ngày).
func DaysBetween(from, to time.Time) int { return int(to.Sub(from).Hours() / 24) }

// ---------------------------------------------------------------------------
// FM-01: ngày gặt
// ---------------------------------------------------------------------------

// Kết quả kiểm ngày gặt.
const (
	HarvestOK          = "ok"          // biết ngày gặt, cuộn không trước ngày gặt
	HarvestUnknown     = "unknown"     // nền tảng không có / chưa chia sẻ ngày gặt (RB-2) → cho ghi, gắn cờ
	HarvestUnavailable = "unavailable" // không gọi được nền tảng, không có bảng đệm → cho ghi, gắn cờ
)

// Season là ngày gặt của một vụ trên thửa, chuẩn hóa từ port.SeasonDates (ngày nghiệp vụ, 00:00 UTC).
type Season struct {
	SeasonRef *uuid.UUID
	Expected  time.Time  // ngày gặt dự kiến (expected_harvest_date) — gốc hạn cuộn FM-02 (như harvest_date của prototype)
	Open      *time.Time // ngày mở cửa sổ gặt (harvest_open_date, theo SOP) — mốc FM-01; nil = dùng Expected
}

// SeasonsFrom chuẩn hóa các vụ đọc qua port.HarvestCalendar: bỏ vụ đã hủy và vụ không có ngày
// gặt dự kiến hợp lệ; ngày mở gặt sai dạng hoặc sau ngày dự kiến bị bỏ qua.
func SeasonsFrom(xs []port.SeasonDates) []Season {
	out := make([]Season, 0, len(xs))
	for _, x := range xs {
		if x.Status == "cancelled" {
			continue
		}
		exp, err := time.Parse(time.DateOnly, x.ExpectedHarvestDate)
		if err != nil {
			continue
		}
		s := Season{Expected: exp}
		if o, err := time.Parse(time.DateOnly, x.HarvestOpenDate); err == nil && !o.After(exp) {
			s.Open = &o
		}
		if x.SeasonRef != uuid.Nil {
			r := x.SeasonRef
			s.SeasonRef = &r
		}
		out = append(out, s)
	}
	return out
}

// EarliestHarvest là mốc FM-01: ngày mở cửa sổ gặt nếu có, không thì ngày gặt dự kiến.
func EarliestHarvest(expected time.Time, open *time.Time) time.Time {
	if open != nil && open.Before(expected) {
		return *open
	}
	return expected
}

// SelectSeason chọn vụ của thửa ứng với lệnh gom: vụ có ngày gặt dự kiến GẦN NGÀY BẮT ĐẦU
// khung lệnh gom nhất (hòa → vụ sau, rồi season_ref). Tất định với cùng dữ liệu. nil khi
// không có vụ nào.
func SelectSeason(seasons []Season, anchor time.Time) *Season {
	if len(seasons) == 0 {
		return nil
	}
	xs := append([]Season(nil), seasons...)
	dist := func(s Season) int {
		d := DaysBetween(anchor, s.Expected)
		if d < 0 {
			return -d
		}
		return d
	}
	sort.SliceStable(xs, func(i, j int) bool {
		a, b := xs[i], xs[j]
		if da, db := dist(a), dist(b); da != db {
			return da < db
		}
		if !a.Expected.Equal(b.Expected) {
			return a.Expected.After(b.Expected)
		}
		return seasonKey(a) < seasonKey(b)
	})
	return &xs[0]
}

func seasonKey(s Season) string {
	if s.SeasonRef == nil {
		return ""
	}
	return s.SeasonRef.String()
}

// CheckBaleDate áp FM-01: ngày cuộn (ngày nghiệp vụ của event_time) không được trước ngày gặt.
func CheckBaleDate(baleDay, harvestDay time.Time) error {
	if baleDay.Before(harvestDay) {
		return ErrBeforeHarvest
	}
	return nil
}

// ---------------------------------------------------------------------------
// FM-02: hạn cuộn sau gặt
// ---------------------------------------------------------------------------

// DefaultBalingDeadlineDays là N mặc định (prototype MAX_DAYS_AFTER_HARVEST = 3) — tạm, chờ khách.
const DefaultBalingDeadlineDays = 3

// Trạng thái hạn cuộn của một thửa trong lệnh gom.
const (
	BalingHarvestUnknown = "harvest_unknown" // chưa biết ngày gặt (không tính được hạn — gắn cờ riêng)
	BalingNotHarvested   = "not_harvested"   // chưa tới ngày gặt
	BalingPending        = "pending"         // đã gặt, chưa cuộn, còn trong hạn
	BalingOverdue        = "overdue"         // đã gặt, chưa cuộn, QUÁ hạn (rơm nằm ruộng — rủi ro ẩm mục)
	BalingOnTime         = "baled_on_time"   // đã cuộn trong hạn
	BalingLate           = "baled_late"      // đã cuộn nhưng sau hạn
)

// Baling là kết quả tính hạn cuộn.
type Baling struct {
	Status      string
	Deadline    *time.Time // ngày gặt + N
	DaysOnField *int       // số ngày rơm nằm ruộng (tới hôm nay, hoặc tới ngày cuộn)
}

// Flagged cho biết thửa bị gắn cờ FM-02 (quá hạn chưa cuộn, hoặc cuộn trễ).
func (b Baling) Flagged() bool { return b.Status == BalingOverdue || b.Status == BalingLate }

// BalingState tính trạng thái FM-02. harvest nil = chưa biết ngày gặt; baled nil = chưa cuộn.
func BalingState(harvest, baled *time.Time, today time.Time, days int) Baling {
	if harvest == nil {
		return Baling{Status: BalingHarvestUnknown}
	}
	deadline := AddDays(*harvest, days)
	b := Baling{Deadline: &deadline}
	if baled != nil {
		n := DaysBetween(*harvest, *baled)
		b.DaysOnField = &n
		if baled.After(deadline) {
			b.Status = BalingLate
		} else {
			b.Status = BalingOnTime
		}
		return b
	}
	if today.Before(*harvest) {
		b.Status = BalingNotHarvested
		return b
	}
	n := DaysBetween(*harvest, today)
	b.DaysOnField = &n
	if today.After(deadline) {
		b.Status = BalingOverdue
	} else {
		b.Status = BalingPending
	}
	return b
}

// ---------------------------------------------------------------------------
// FM-06: năng lực tổ
// ---------------------------------------------------------------------------

// Workload là việc giao cho tổ: khoảng ngày dự kiến và khối lượng theo đơn vị năng lực.
type Workload struct {
	From, To    time.Time
	ExpectedQty decimal.Decimal
}

// Days là số ngày của khoảng (≥ 1).
func (w Workload) Days() int { return DaysBetween(w.From, w.To) + 1 }

// DailyLoad chia đều khối lượng cho các ngày (làm tròn 3 chữ số, tối thiểu 0,001).
func (w Workload) DailyLoad() decimal.Decimal {
	d := w.ExpectedQty.Div(decimal.NewFromInt(int64(w.Days()))).Round(3)
	if d.Sign() <= 0 {
		d = decimal.New(1, -3)
	}
	return d
}

// Booking là một phân công đang giữ năng lực của tổ (lệnh gom đang ở trạng thái assigned).
type Booking struct {
	OrderID   uuid.UUID
	From, To  time.Time
	DailyLoad decimal.Decimal
}

// Capacity là kết quả kiểm năng lực của tổ cho một việc.
type Capacity struct {
	Fits     bool
	PeakLoad decimal.Decimal // tải cao nhất/ngày trong khoảng SAU khi thêm việc
	// OverDay: ngày đầu tiên vượt năng lực (nil khi vừa).
	OverDay  *time.Time
	OverLoad decimal.Decimal // tải của OverDay sau khi thêm việc
}

// CheckCapacity áp FM-06: mỗi ngày của w, Σ tải các phân công khác + tải việc mới ≤ năng lực.
func CheckCapacity(bookings []Booking, w Workload, capacity decimal.Decimal) Capacity {
	daily := w.DailyLoad()
	res := Capacity{Fits: true}
	for d := w.From; !d.After(w.To); d = AddDays(d, 1) {
		load := daily
		for _, b := range bookings {
			if !d.Before(b.From) && !d.After(b.To) {
				load = load.Add(b.DailyLoad)
			}
		}
		if load.GreaterThan(res.PeakLoad) {
			res.PeakLoad = load
		}
		if res.Fits && load.GreaterThan(capacity) {
			day := d
			res.Fits, res.OverDay, res.OverLoad = false, &day, load
		}
	}
	return res
}

// ---------------------------------------------------------------------------
// FM-08: gợi ý phân công
// ---------------------------------------------------------------------------

// Lý do tổ không được gợi ý.
const (
	UnfitUnit     = "unit_mismatch" // việc không có khối lượng theo đơn vị năng lực của tổ
	UnfitCapacity = "over_capacity" // vượt năng lực ít nhất một ngày
)

// Candidate là một tổ đang hoạt động được xét gợi ý.
type Candidate struct {
	CrewID          uuid.UUID
	Code            string
	CapacityPerDay  decimal.Decimal
	ServiceCommunes []string
	// Workload của việc theo đơn vị của tổ; nil = không có khối lượng theo đơn vị này.
	Workload *Workload
	Bookings []Booking
}

// Suggestion là một tổ trong kết quả gợi ý.
type Suggestion struct {
	CrewID uuid.UUID
	Code   string
	Rank   int  // 1 = gợi ý hàng đầu (chỉ tổ vừa năng lực)
	Local  bool // xã của lệnh gom thuộc service_communes của tổ ("gần nhất")
	// LoadRatio = tải cao nhất/ngày sau khi giao ÷ năng lực (ít tải nhất trước).
	LoadRatio decimal.Decimal
	Capacity  Capacity
	Unfit     string // "" khi vừa; UnfitUnit | UnfitCapacity
}

// Suggest xếp hạng tổ cho một lệnh gom ở xã commune (FM-08):
//  1. chỉ tổ ĐANG HOẠT ĐỘNG (người gọi lọc, FM-07) và VỪA năng lực mọi ngày (FM-06);
//  2. gần nhất: xã của lệnh gom thuộc service_communes của tổ trước;
//  3. ít tải nhất: tỷ lệ tải cao nhất/ngày sau khi giao ÷ năng lực tăng dần;
//  4. hòa → mã tổ tăng dần.
//
// Tất định: cùng dữ liệu → cùng thứ tự. Tổ không vừa trả kèm lý do, không có hạng.
func Suggest(cands []Candidate, commune string) (fit, unfit []Suggestion) {
	for _, c := range cands {
		s := Suggestion{CrewID: c.CrewID, Code: c.Code}
		for _, x := range c.ServiceCommunes {
			if x == commune {
				s.Local = true
				break
			}
		}
		if c.Workload == nil {
			s.Unfit = UnfitUnit
			unfit = append(unfit, s)
			continue
		}
		s.Capacity = CheckCapacity(c.Bookings, *c.Workload, c.CapacityPerDay)
		s.LoadRatio = s.Capacity.PeakLoad.Div(c.CapacityPerDay).Round(4)
		if !s.Capacity.Fits {
			s.Unfit = UnfitCapacity
			unfit = append(unfit, s)
			continue
		}
		fit = append(fit, s)
	}
	sort.SliceStable(fit, func(i, j int) bool {
		a, b := fit[i], fit[j]
		if a.Local != b.Local {
			return a.Local
		}
		if c := a.LoadRatio.Cmp(b.LoadRatio); c != 0 {
			return c < 0
		}
		return a.Code < b.Code
	})
	for i := range fit {
		fit[i].Rank = i + 1
	}
	sort.SliceStable(unfit, func(i, j int) bool { return unfit[i].Code < unfit[j].Code })
	return fit, unfit
}

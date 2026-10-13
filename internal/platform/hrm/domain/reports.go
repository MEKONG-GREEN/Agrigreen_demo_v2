package domain

// W11.28 — BÁO CÁO SỞ NS01–NS06 (BRD HCTH v3.0 FN-12) và DASHBOARD NHÂN SỰ (FN-06): phép dựng THUẦN từ phép chiếu as-of
// của hồ sơ (Project) + trường đọc từ kho định danh (chỉ trong bộ nhớ) + danh mục FN-19. Không I/O ở đây.
//
// Quy tắc kiểm tra dùng chung (Mục V.2.0): R1 tổng nhóm ngạch, trình độ, độ tuổi = tổng số (NS06, chặn); R2 tổng NS05 = số
// người NS01 cùng ngày chốt (chặn); R3 NS01 tháng này = tháng trước + tiếp nhận − nghỉ (cảnh báo); R6 tuổi nghỉ hưu (cảnh
// báo); TK thiếu dữ liệu thẻ Thống kê BC Sở (cảnh báo, liệt kê người thiếu).

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ReportCode là mã biểu báo cáo Sở.
type ReportCode string

// Sáu biểu của FN-12.
const (
	NS01 ReportCode = "NS01" // Danh sách viên chức hiện có mặt (hằng tháng)
	NS02 ReportCode = "NS02" // Danh sách viên chức nghỉ việc trong tháng
	NS03 ReportCode = "NS03" // Danh sách hợp đồng lao động
	NS04 ReportCode = "NS04" // Trích ngang viên chức (36 cột, có trường nhạy cảm)
	NS05 ReportCode = "NS05" // Thống kê trình độ, chuyên ngành theo đơn vị
	NS06 ReportCode = "NS06" // Báo cáo chất lượng CCVC, HĐLĐ (Phụ lục 2, 39 cột)
)

// Kỳ của biểu.
const (
	PeriodMonth   = "month"
	PeriodQuarter = "quarter"
	PeriodAdHoc   = "ad_hoc" // khi Sở yêu cầu — kỳ là ngày chốt
)

// ReportMeta mô tả một biểu.
type ReportMeta struct {
	Code      ReportCode
	Name      string
	Period    string
	Aggregate bool     // biểu thống kê (NS05, NS06): nhấn ô xem người được đếm
	Sensitive []string // trường thẻ Thống kê BC Sở (TK) biểu dùng — xuất cần quyền sensitive.view, ghi nhật ký trường
	Source    string   // nguồn mẫu hiện hành (BRD Mục III)
}

// Reports trả sáu biểu theo thứ tự.
func Reports() []ReportMeta {
	return []ReportMeta{
		{NS01, "Danh sách viên chức hiện có mặt", PeriodMonth, false, []string{"ethnicity", "hometown", "revolution_date", "party_date"},
			"Thống kê CCVCNLĐ — sheet Tháng (19 cột)"},
		{NS02, "Danh sách viên chức nghỉ việc", PeriodMonth, false, []string{"ethnicity", "hometown", "revolution_date", "party_date"},
			"Thống kê CCVCNLĐ — sheet NGHỈ"},
		{NS03, "Danh sách hợp đồng lao động", PeriodAdHoc, false, nil, "Thống kê CCVCNLĐ — sheet HĐLĐ-ĐVSN"},
		{NS04, "Danh sách trích ngang viên chức", PeriodAdHoc, false,
			[]string{"ethnicity", "religion", "revolution_date", "party_date", "birthplace", "hometown"}, "Biểu mẫu nhân sự — sheet Trích ngang (36 cột)"},
		{NS05, "Thống kê nhân sự theo trình độ, chuyên ngành", PeriodMonth, true, []string{"ethnicity"}, "Biểu mẫu nhân sự — sheet Tổng hợp"},
		{NS06, "Báo cáo chất lượng CCVC, HĐLĐ (Phụ lục 2)", PeriodQuarter, true, []string{"ethnicity", "religion", "party_date"},
			"Biểu mẫu nhân sự — Sheet1 Phụ lục 2 (39 cột)"},
	}
}

// ReportOf trả mô tả biểu; ok = false nếu mã lạ.
func ReportOf(code string) (ReportMeta, bool) {
	for _, r := range Reports() {
		if string(r.Code) == code {
			return r, true
		}
	}
	return ReportMeta{}, false
}

// PeriodLabel sinh nhãn kỳ cho tiêu đề (FN-12 AC-03: tiêu đề tự đổi theo kỳ): "THÁNG 7/2026", "QUÝ III/2026", "NGÀY 15/07/2026".
func PeriodLabel(period string, asOf time.Time) string {
	switch period {
	case PeriodMonth:
		return fmt.Sprintf("THÁNG %d/%d", int(asOf.Month()), asOf.Year())
	case PeriodQuarter:
		return fmt.Sprintf("QUÝ %s/%d", []string{"I", "II", "III", "IV"}[(int(asOf.Month())-1)/3], asOf.Year())
	}
	return "NGÀY " + asOf.Format("02/01/2006")
}

// PeriodStart trả ngày đầu kỳ chứa asOf (tháng/quý; biểu theo yêu cầu: chính asOf).
func PeriodStart(period string, asOf time.Time) time.Time {
	switch period {
	case PeriodMonth:
		return time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, time.UTC)
	case PeriodQuarter:
		return time.Date(asOf.Year(), time.Month((int(asOf.Month())-1)/3*3+1), 1, 0, 0, 0, 0, time.UTC)
	}
	return asOf
}

// Degree là một văn bằng hiệu lực tại ngày chốt.
type Degree struct {
	Level, Major, Form string
	Year               int
}

// Training là một bồi dưỡng/trình độ (HS-26..31) hiệu lực tại ngày chốt.
type Training struct {
	Type, Code, Level, Cert string
	Year                    int
}

// Person là MỘT nhân sự đã ghép: phép chiếu as-of + trường kho (CHỈ trong bộ nhớ, R-14 — không log) + quá trình đã lọc.
type Person struct {
	StaffID   uuid.UUID
	Code      string
	Type      StaffType
	Snap      Snapshot
	FullName  string `pii:"true"`
	Phone     string `pii:"true"`
	Address   string `pii:"true"`
	Gender    string // nam | nu | "" (chưa đọc được)
	Birth     *time.Time
	BirthPrec Precision
	// Thẻ Thống kê BC Sở — chỉ có khi biểu cần và người gọi có quyền (Sensitive = true).
	Sensitive      bool
	Ethnicity      string `pii:"true"`
	Religion       string `pii:"true"`
	PartyDate      string `pii:"true"`
	Hometown       string `pii:"true"`
	Birthplace     string `pii:"true"`
	RevolutionDate string `pii:"true"`
	Degrees        []Degree
	Trainings      []Training
}

// CollectQualifications trả văn bằng và bồi dưỡng hiệu lực tới ngày at (sự kiện degree, training).
func CollectQualifications(evs []Event, at time.Time) ([]Degree, []Training) {
	var ds []Degree
	var ts []Training
	for _, e := range Effective(evs) {
		if e.ValidFrom.After(at) {
			continue
		}
		p := e.Payload
		switch e.Kind {
		case EventDegree:
			ds = append(ds, Degree{Level: p["level"], Major: p["major"], Form: p["form"], Year: e.ValidFrom.Year()})
		case EventTraining:
			ts = append(ts, Training{Type: p["type"], Code: p["code"], Level: p["level"], Cert: p["cert"], Year: e.ValidFrom.Year()})
		}
	}
	return ds, ts
}

// CatItem là mục danh mục dùng cho báo cáo (phiên bản đọc được tại thời điểm biết).
type CatItem struct {
	Name      string
	SortOrder int
	Attrs     Attrs
}

// Catalog là danh mục nhân sự + đơn vị nội bộ cho một lần dựng báo cáo.
type Catalog struct {
	Items map[Kind]map[string]CatItem
	Units []UnitRef // thứ tự hiển thị (Ban Giám đốc, các phòng theo danh mục, các trạm/trại)
}

// UnitRef là một đơn vị nội bộ.
type UnitRef struct{ Code, Name string }

// Name trả tên mục (mã lạ → chính mã).
func (c Catalog) Name(k Kind, code string) string {
	if code == "" {
		return ""
	}
	if it, ok := c.Items[k][code]; ok {
		return it.Name
	}
	return code
}

func (c Catalog) item(k Kind, code string) (CatItem, bool) {
	it, ok := c.Items[k][code]
	return it, ok
}

// sortOf: thứ tự mục; mã lạ xếp cuối.
func (c Catalog) sortOf(k Kind, code string) int {
	if it, ok := c.Items[k][code]; ok {
		return it.SortOrder
	}
	return 1 << 30
}

// sortedCodes trả mã của loại k theo thứ tự hiển thị, lọc theo f.
func (c Catalog) sortedCodes(k Kind, f func(CatItem) bool) []string {
	var out []string
	for code, it := range c.Items[k] {
		if f == nil || f(it) {
			out = append(out, code)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := c.Items[k][out[i]], c.Items[k][out[j]]
		return a.SortOrder < b.SortOrder || a.SortOrder == b.SortOrder && out[i] < out[j]
	})
	return out
}

// UnitName trả tên đơn vị (mã lạ → mã; rỗng → "Chưa xếp đơn vị").
func (c Catalog) UnitName(code string) string {
	if code == "" {
		return "Chưa xếp đơn vị"
	}
	for _, u := range c.Units {
		if u.Code == code && u.Name != "" {
			return u.Name
		}
	}
	return code
}

// TopDegree trả văn bằng cao nhất (thứ hạng của trình độ; cùng hạng lấy bằng sau).
func (c Catalog) TopDegree(ds []Degree) (Degree, bool) {
	best, ok := Degree{}, false
	for _, d := range ds {
		if !ok || c.Items[KindEducationLevel][d.Level].Attrs.Rank >= c.Items[KindEducationLevel][best.Level].Attrs.Rank {
			best, ok = d, true
		}
	}
	return best, ok
}

// bestTraining trả bồi dưỡng loại typ có thứ tự danh mục nhỏ nhất (cao nhất) — kind là danh mục của mã.
func (c Catalog) bestTraining(ts []Training, typ string, kind Kind) (Training, bool) {
	best, ok := Training{}, false
	for _, t := range ts {
		if t.Type != typ {
			continue
		}
		if !ok || kind != "" && c.sortOf(kind, t.Code) < c.sortOf(kind, best.Code) || kind == "" && t.Code == "cap_so" {
			best, ok = t, true
		}
	}
	return best, ok
}

// Cột của biểu.
type Column struct {
	Key       string
	Group     string // nhãn nhóm (hàng tiêu đề 1); rỗng = cột gộp hai hàng
	Label     string
	Sensitive bool // lấy từ thẻ Thống kê BC Sở
	Numeric   bool
}

// Loại dòng.
const (
	RowGroup  = "group"  // "I. TRUNG TÂM KHUYẾN NÔNG"
	RowUnit   = "unit"   // tiêu đề đơn vị (biểu danh sách) hoặc dòng số liệu của đơn vị (biểu thống kê)
	RowPerson = "person" // một người
	RowTotal  = "total"  // Tổng cộng / Trung tâm
)

// Row là một dòng biểu.
type Row struct {
	Kind    string
	Key     string // mã đơn vị (dòng thống kê) — khóa nhấn ô
	Cells   []string
	StaffID uuid.UUID // dòng người
}

// Mức của quy tắc kiểm tra.
const (
	LevelBlock = "block"
	LevelWarn  = "warn"
)

// Check là kết quả của một quy tắc kiểm tra.
type Check struct {
	Rule     string // R1, R2, R3, R6, TK, CLASS
	Level    string
	OK       bool
	Message  string
	StaffIDs []uuid.UUID
}

// Table là biểu đã dựng.
type Table struct {
	Code     ReportCode
	Title    string
	Period   string // nhãn kỳ (PeriodLabel)
	AsOf     time.Time
	Columns  []Column
	Rows     []Row
	Cells    map[string][]uuid.UUID // "rowKey|colKey" → người được đếm (biểu thống kê)
	People   []uuid.UUID            // mọi người có trong biểu (nhật ký xuất)
	Checks   []Check
	Footnote string
}

// Blocked cho biết biểu có quy tắc mức chặn không đạt.
func (t Table) Blocked() bool {
	for _, c := range t.Checks {
		if !c.OK && c.Level == LevelBlock {
			return true
		}
	}
	return false
}

// Status trả trạng thái kiểm tra của biểu (FN-12 B1): ready | warning | blocked.
func (t Table) Status() string {
	st := "ready"
	for _, c := range t.Checks {
		if !c.OK {
			if c.Level == LevelBlock {
				return "blocked"
			}
			st = "warning"
		}
	}
	return st
}

// CellKey là khóa của một ô thống kê.
func CellKey(row, col string) string { return row + "|" + col }

// BuildInput là dữ liệu của một lần dựng biểu.
type BuildInput struct {
	AsOf   time.Time
	Period string // kỳ của biểu (PeriodMonth …)
	Cat    Catalog
	People []Person // dân số đã chiếu tại AsOf (mọi loại, mọi trạng thái) — builder tự lọc
	// R3 (NS01): số viên chức có mặt cuối tháng trước, tiếp nhận và nghỉ trong (cuối tháng trước; AsOf].
	Prev       []uuid.UUID
	Joined     []uuid.UUID
	LeftIDs    []uuid.UUID
	PrevAsOf   time.Time
	Retirement RetirementAges // R6
	// NS05 R2: số dòng NS01 cùng ngày chốt (builder tự tính từ People nếu < 0).
}

// RetirementAges là tuổi nghỉ hưu (tháng) và cửa sổ nhắc (tháng) — tham số promotion (W11.21).
type RetirementAges struct {
	MaleMonths, FemaleMonths, RemindMonths int
}

// Build dựng biểu code.
func Build(code ReportCode, in BuildInput) (Table, error) {
	meta, ok := ReportOf(string(code))
	if !ok {
		return Table{}, invalid("biểu %q", code)
	}
	t := Table{Code: code, Title: strings.ToUpper(meta.Name), Period: PeriodLabel(meta.Period, in.AsOf), AsOf: in.AsOf,
		Cells: map[string][]uuid.UUID{}}
	switch code {
	case NS01:
		buildList(&t, in, ns01Columns(), activeOf(in.People, StaffVienChuc), "VC", ns01Row)
		t.Checks = append(t.Checks, checkR3(in, len(t.People)), checkTK(in.People, t.People))
	case NS02:
		from := PeriodStart(PeriodMonth, in.AsOf)
		var left []Person
		for _, p := range in.People {
			if p.Type == StaffVienChuc && p.Snap.Status == StatusLeft && p.Snap.EndedOn != nil && !p.Snap.EndedOn.Before(from) &&
				!p.Snap.EndedOn.After(in.AsOf) {
				left = append(left, p)
			}
		}
		buildList(&t, in, ns01Columns(), left, "VC", ns02Row)
		t.Checks = append(t.Checks, checkR3(in, len(activeOf(in.People, StaffVienChuc))))
	case NS03:
		buildList(&t, in, ns03Columns(), activeOf(in.People, StaffHopDong), "HĐ", ns03Row)
	case NS04:
		people := activeOf(in.People, StaffVienChuc)
		buildList(&t, in, ns04Columns(), people, "", ns04Row)
		t.Rows = append(t.Rows, Row{Kind: RowTotal, Cells: totalCells(len(t.Columns), "Tổng cộng: "+strconv.Itoa(len(people))+" người")})
		t.Checks = append(t.Checks, checkTK(in.People, t.People))
	case NS05:
		buildNS05(&t, in)
	case NS06:
		buildNS06(&t, in)
	}
	return t, nil
}

func totalCells(n int, label string) []string {
	c := make([]string, n)
	if n > 1 {
		c[1] = label
	}
	return c
}

// activeOf lọc người Đang công tác tại ngày chốt thuộc các loại types.
func activeOf(ps []Person, types ...StaffType) []Person {
	var out []Person
	for _, p := range ps {
		if p.Snap.Status == StatusActive && contains(typeStrings(types), string(p.Type)) {
			out = append(out, p)
		}
	}
	return out
}

func typeStrings(ts []StaffType) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t)
	}
	return out
}

// seconded cho biết người đang tăng cường tại ngày chốt (HS-21).
func seconded(p Person) (Event, bool) {
	if len(p.Snap.Secondments) == 0 {
		return Event{}, false
	}
	return p.Snap.Secondments[len(p.Snap.Secondments)-1], true
}

// orderPeople sắp người theo đơn vị (thứ tự danh mục đơn vị; mã lạ sau, theo mã; chưa xếp cuối), trong đơn vị: lãnh đạo
// theo thứ tự chức vụ (Trưởng, Phó …), rồi viên chức; cùng bậc theo tên rồi mã hồ sơ (NS01 "Thứ tự").
func orderPeople(ps []Person, cat Catalog) []Person {
	unitIdx := map[string]int{}
	for i, u := range cat.Units {
		unitIdx[u.Code] = i
	}
	rank := func(p Person) (int, string) {
		if i, ok := unitIdx[p.Snap.Unit]; ok {
			return i, ""
		}
		if p.Snap.Unit == "" {
			return 1 << 30, ""
		}
		return 1 << 29, p.Snap.Unit
	}
	out := append([]Person(nil), ps...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ua, sa := rank(a)
		ub, sb := rank(b)
		if ua != ub || sa != sb {
			return ua < ub || ua == ub && sa < sb
		}
		pa, pb := positionRank(a, cat), positionRank(b, cat)
		if pa != pb {
			return pa < pb
		}
		na, nb := strings.ToLower(a.FullName), strings.ToLower(b.FullName)
		if na != nb {
			return na < nb
		}
		return a.Code < b.Code
	})
	return out
}

func positionRank(p Person, cat Catalog) int {
	it, ok := cat.item(KindPosition, p.Snap.Position)
	if !ok {
		return 1 << 30
	}
	if !it.Attrs.Leader {
		return 1<<29 + it.SortOrder
	}
	return it.SortOrder
}

// buildList dựng biểu danh sách: nhóm "I. TRUNG TÂM KHUYẾN NÔNG", dòng tiêu đề đơn vị kèm số người (unitSuffix: "VC",
// "HĐ"; rỗng = không có dòng đơn vị), dòng người đánh STT liên tục.
func buildList(t *Table, in BuildInput, cols []Column, ps []Person, unitSuffix string, row func(Person, Catalog, int) []string) {
	t.Columns = cols
	t.Rows = append(t.Rows, Row{Kind: RowGroup, Cells: totalCells(len(cols), "I. TRUNG TÂM KHUYẾN NÔNG")})
	ordered := orderPeople(ps, in.Cat)
	count := map[string]int{}
	for _, p := range ordered {
		count[p.Snap.Unit]++
	}
	cur := "\x00"
	for i, p := range ordered {
		if unitSuffix != "" && p.Snap.Unit != cur {
			cur = p.Snap.Unit
			t.Rows = append(t.Rows, Row{Kind: RowUnit, Key: cur,
				Cells: totalCells(len(cols), fmt.Sprintf("%s (%d %s)", in.Cat.UnitName(cur), count[cur], unitSuffix))})
		}
		t.Rows = append(t.Rows, Row{Kind: RowPerson, StaffID: p.StaffID, Cells: row(p, in.Cat, i+1)})
		t.People = append(t.People, p.StaffID)
	}
}

// FormatBirth in ngày sinh theo mức chính xác (dd/mm/yyyy, mm/yyyy, yyyy).
func FormatBirth(t *time.Time, p Precision) string {
	if t == nil {
		return ""
	}
	switch p {
	case PrecisionYear:
		return t.Format("2006")
	case PrecisionMonth:
		return t.Format("01/2006")
	}
	return t.Format("02/01/2006")
}

// fmtDateField in trường ngày dạng YYYY[-MM[-DD]] theo kiểu Việt Nam.
func fmtDateField(s string) string {
	d, p, err := ParseDate(s)
	if err != nil {
		return s
	}
	return FormatBirth(&d, p)
}

func genderSplit(p Person, v string) (string, string) {
	switch p.Gender {
	case "nam":
		return v, ""
	case "nu":
		return "", v
	}
	return v, ""
}

func positionText(p Person, cat Catalog) string {
	if p.Snap.Title != "" {
		return p.Snap.Title
	}
	return cat.Name(KindPosition, p.Snap.Position)
}

func degreeText(p Person, cat Catalog) string {
	d, ok := cat.TopDegree(p.Degrees)
	if !ok {
		return ""
	}
	s := cat.Name(KindEducationLevel, d.Level)
	if d.Major != "" {
		s += " " + cat.Name(KindMajor, d.Major)
	}
	return s
}

func trainingName(p Person, cat Catalog, typ string, kind Kind) string {
	t, ok := cat.bestTraining(p.Trainings, typ, kind)
	if !ok {
		return ""
	}
	return cat.Name(kind, t.Code)
}

func languagesText(p Person, cat Catalog, filter func(Training) bool) string {
	var out []string
	for _, t := range p.Trainings {
		if t.Type != TrainingForeignLanguage || filter != nil && !filter(t) {
			continue
		}
		s := cat.Name(KindForeignLanguage, t.Code)
		if t.Level != "" {
			s += " " + cat.Name(KindLanguageLevel, t.Level)
		}
		out = append(out, s)
	}
	return strings.Join(out, "; ")
}

func leadershipText(p Person, cat Catalog) string {
	t, ok := cat.bestTraining(p.Trainings, TrainingLeadership, "")
	if !ok {
		return ""
	}
	if t.Code == "cap_so" {
		return "Cấp sở và tương đương"
	}
	return "Cấp phòng"
}

func noteText(p Person) string {
	var notes []string
	if s, ok := seconded(p); ok {
		notes = append(notes, "Tăng cường "+s.Payload["to_org"])
	}
	return strings.Join(notes, "; ")
}

func ns01Columns() []Column {
	return []Column{
		{Key: "stt", Label: "STT"}, {Key: "ho_ten", Label: "Họ và tên"},
		{Key: "ns_nam", Group: "Ngày sinh", Label: "Nam"}, {Key: "ns_nu", Group: "Ngày sinh", Label: "Nữ"},
		{Key: "chuc_vu", Label: "Chức vụ, chức danh"}, {Key: "phong_ban", Label: "Phòng, ban, đơn vị công tác"},
		{Key: "dan_toc", Label: "Dân tộc", Sensitive: true}, {Key: "que_quan", Label: "Quê quán", Sensitive: true},
		{Key: "tgcm", Label: "Ngày TGCM", Sensitive: true}, {Key: "vao_dang", Label: "Ngày vào Đảng", Sensitive: true},
		{Key: "td_cm", Group: "Trình độ", Label: "Chuyên môn"}, {Key: "td_ct", Group: "Trình độ", Label: "Chính trị"},
		{Key: "td_qlnn", Group: "Trình độ", Label: "QLNN"}, {Key: "td_nn", Group: "Trình độ", Label: "Ngoại ngữ"},
		{Key: "td_th", Group: "Trình độ", Label: "Tin học"},
		{Key: "bd_ldql", Label: "Bồi dưỡng LĐQL"}, {Key: "ngach", Label: "Ngạch/CDNN"}, {Key: "sdt", Label: "Số điện thoại"},
		{Key: "ghi_chu", Label: "Ghi chú"},
	}
}

func ns01Row(p Person, cat Catalog, n int) []string {
	nam, nu := genderSplit(p, FormatBirth(p.Birth, p.BirthPrec))
	return []string{strconv.Itoa(n), p.FullName, nam, nu, positionText(p, cat), cat.UnitName(p.Snap.Unit),
		cat.Name(KindEthnicity, p.Ethnicity), p.Hometown, fmtDateField(p.RevolutionDate), fmtDateField(p.PartyDate),
		degreeText(p, cat), trainingName(p, cat, TrainingPoliticalTheory, KindPoliticalTheory), trainingName(p, cat, TrainingStateMgmt, KindStateMgmt),
		languagesText(p, cat, nil), trainingName(p, cat, TrainingIT, KindITLevel), leadershipText(p, cat), cat.Name(KindGrade, p.Snap.Grade),
		p.Phone, noteText(p)}
}

func ns02Row(p Person, cat Catalog, n int) []string {
	r := ns01Row(p, cat, n)
	note := cat.Name(KindLeaveReason, p.Snap.LeaveReason)
	if p.Snap.EndedOn != nil {
		note = strings.TrimSpace(note + " từ " + p.Snap.EndedOn.Format("02/01/2006"))
	}
	r[len(r)-1] = note
	return r
}

func ns03Columns() []Column {
	return []Column{
		{Key: "stt", Label: "STT"}, {Key: "ho_ten", Label: "Họ và tên"},
		{Key: "nam_nam", Group: "Năm sinh", Label: "Nam"}, {Key: "nam_nu", Group: "Năm sinh", Label: "Nữ"},
		{Key: "ngach", Label: "Ngạch/CDNN"}, {Key: "chuc_vu", Label: "Chức vụ/chức danh"}, {Key: "phong_ban", Label: "Phòng, ban, bộ phận"},
		{Key: "ghi_chu", Label: "Ghi chú"},
	}
}

func ns03Row(p Person, cat Catalog, n int) []string {
	y := ""
	if p.Birth != nil {
		y = p.Birth.Format("2006")
	}
	nam, nu := genderSplit(p, y)
	return []string{strconv.Itoa(n), p.FullName, nam, nu, cat.Name(KindGrade, p.Snap.Grade), positionText(p, cat), cat.UnitName(p.Snap.Unit), noteText(p)}
}

// Cột trình độ của NS04 theo mã trình độ.
var ns04Levels = []struct{ key, level, label string }{
	{"td_ts", "tien_si", "TS"}, {"td_ths", "thac_si", "ThS"}, {"td_dh", "dai_hoc", "ĐH"}, {"td_cd", "cao_dang", "CĐ"}, {"td_tc", "trung_cap", "TC"},
}

func ns04Columns() []Column {
	cols := []Column{
		{Key: "stt", Label: "STT"}, {Key: "ho_ten", Label: "Họ và tên"},
		{Key: "ns_nam", Group: "Năm sinh", Label: "Nam"}, {Key: "ns_nu", Group: "Năm sinh", Label: "Nữ"},
		{Key: "dan_toc", Label: "Dân tộc", Sensitive: true}, {Key: "ton_giao", Label: "Tôn giáo", Sensitive: true},
		{Key: "tgcm", Label: "Ngày TGCM", Sensitive: true}, {Key: "vao_dang", Label: "Ngày vào Đảng", Sensitive: true},
		{Key: "chuc_vu", Label: "Chức vụ"}, {Key: "phong_ban", Label: "Phòng, ban"},
		{Key: "cv_ten", Group: "Chức vụ", Label: "Tên chức vụ ghi rõ"}, {Key: "cv_pccv", Group: "Chức vụ", Label: "Hệ số PCCV"},
		{Key: "cv_bn", Group: "Chức vụ", Label: "Ngày bổ nhiệm"},
	}
	for _, l := range ns04Levels {
		cols = append(cols, Column{Key: l.key, Group: "Trình độ chuyên môn (chuyên ngành)", Label: l.label})
	}
	return append(cols,
		Column{Key: "hinh_thuc", Group: "Trình độ chuyên môn (chuyên ngành)", Label: "Chính quy/Tại chức"},
		Column{Key: "nam_tn", Group: "Trình độ chuyên môn (chuyên ngành)", Label: "Năm tốt nghiệp"},
		Column{Key: "llct", Label: "Lý luận chính trị"},
		Column{Key: "nn_anh", Group: "Ngoại ngữ", Label: "Tiếng Anh"}, Column{Key: "nn_dtts", Group: "Ngoại ngữ", Label: "Tiếng DTTS"},
		Column{Key: "nn_khac", Group: "Ngoại ngữ", Label: "Khác"},
		Column{Key: "tin_hoc", Label: "Tin học"},
		Column{Key: "bd_qlnn", Group: "Bồi dưỡng", Label: "QLNN"}, Column{Key: "bd_ldql", Group: "Bồi dưỡng", Label: "LĐQL cấp phòng/sở"},
		Column{Key: "bd_cdnn", Group: "Bồi dưỡng", Label: "CDNN Khuyến nông"},
		Column{Key: "ngach", Label: "Ngạch/CDNN"}, Column{Key: "noi_sinh", Label: "Nơi sinh", Sensitive: true},
		Column{Key: "que_quan", Label: "Quê quán", Sensitive: true}, Column{Key: "noi_o", Label: "Nơi ở hiện nay"},
		Column{Key: "bhxh", Label: "Số BHXH"}, Column{Key: "cccd", Label: "Số CCCD"},
		Column{Key: "dien_thoai", Label: "Điện thoại"}, Column{Key: "ghi_chu", Label: "Ghi chú"},
	)
}

func ns04Row(p Person, cat Catalog, n int) []string {
	y := ""
	if p.Birth != nil {
		y = p.Birth.Format("2006")
	}
	nam, nu := genderSplit(p, y)
	r := []string{strconv.Itoa(n), p.FullName, nam, nu, cat.Name(KindEthnicity, p.Ethnicity), cat.Name(KindReligion, p.Religion),
		fmtDateField(p.RevolutionDate), fmtDateField(p.PartyDate), cat.Name(KindPosition, p.Snap.Position), cat.UnitName(p.Snap.Unit),
		p.Snap.Title, p.Snap.Allowance, fmtDateField(p.Snap.AppointedOn)}
	for _, l := range ns04Levels {
		var majors []string
		for _, d := range p.Degrees {
			if d.Level == l.level {
				majors = append(majors, cat.Name(KindMajor, d.Major))
			}
		}
		r = append(r, strings.Join(majors, "; "))
	}
	form, year := "", ""
	if d, ok := cat.TopDegree(p.Degrees); ok {
		form = map[string]string{"chinh_quy": "Chính quy", "tai_chuc": "Tại chức"}[d.Form]
		year = strconv.Itoa(d.Year)
	}
	ext := ""
	for _, t := range p.Trainings {
		if t.Type == TrainingExtension {
			ext = "Có (" + strconv.Itoa(t.Year) + ")"
		}
	}
	other := func(code string) func(Training) bool { return func(t Training) bool { return t.Code == code } }
	return append(r, form, year, trainingName(p, cat, TrainingPoliticalTheory, KindPoliticalTheory),
		languagesText(p, cat, other("tieng_anh")), languagesText(p, cat, other("tieng_dtts")), languagesText(p, cat, other("ngoai_ngu_khac")),
		trainingName(p, cat, TrainingIT, KindITLevel), trainingName(p, cat, TrainingStateMgmt, KindStateMgmt), leadershipText(p, cat), ext,
		cat.Name(KindGrade, p.Snap.Grade), p.Birthplace, p.Hometown, p.Address, "", "", p.Phone, noteText(p))
}

// --- NS05: thống kê trình độ, chuyên ngành theo đơn vị. ---

// RowKeyCenter, RowKeySecondment: khóa dòng "Trung tâm" và "Tăng cường" của NS05/NS06.
const (
	RowKeyCenter     = "_center"
	RowKeySecondment = "_secondment"
)

// statsRows trả các dòng thống kê theo đơn vị (danh mục đơn vị + đơn vị lạ có người) và hàm chọn dòng của một người.
func statsRows(ps []Person, cat Catalog, secondmentRow bool) ([]UnitRef, func(Person) string) {
	rows := append([]UnitRef(nil), cat.Units...)
	known := map[string]bool{}
	for _, u := range rows {
		known[u.Code] = true
	}
	var extra []string
	for _, p := range ps {
		if !known[p.Snap.Unit] {
			known[p.Snap.Unit] = true
			extra = append(extra, p.Snap.Unit)
		}
	}
	sort.Strings(extra)
	for _, u := range extra {
		rows = append(rows, UnitRef{Code: u, Name: cat.UnitName(u)})
	}
	if secondmentRow {
		rows = append(rows, UnitRef{Code: RowKeySecondment, Name: "TĂNG CƯỜNG"})
	}
	return rows, func(p Person) string {
		if _, ok := seconded(p); ok && secondmentRow {
			return RowKeySecondment
		}
		return p.Snap.Unit
	}
}

func buildNS05(t *Table, in BuildInput) {
	people := activeOf(in.People, StaffVienChuc)
	cat := in.Cat
	cols := []Column{{Key: "stt", Label: "STT"}, {Key: "don_vi", Label: "Đơn vị"},
		{Key: "nu", Label: "Nữ", Numeric: true}, {Key: "dtts", Label: "DT thiểu số", Numeric: true, Sensitive: true}}
	levels := cat.sortedCodes(KindEducationLevel, func(it CatItem) bool { return true })
	var counted []string // trình độ có nhóm chuyên ngành (TS … TC)
	for _, lv := range levels {
		groups := cat.sortedCodes(KindMajorGroup, func(it CatItem) bool { return it.Attrs.Level == lv })
		if len(groups) == 0 {
			continue
		}
		counted = append(counted, lv)
		for _, g := range groups {
			cols = append(cols, Column{Key: "g:" + g, Group: cat.Name(KindEducationLevel, lv), Label: cat.Name(KindMajorGroup, g), Numeric: true})
		}
		cols = append(cols, Column{Key: "lv:" + lv, Group: cat.Name(KindEducationLevel, lv), Label: "Cộng", Numeric: true})
	}
	cols = append(cols, Column{Key: "khac", Label: "Khác", Numeric: true}, Column{Key: "tong", Label: "Tổng số", Numeric: true})
	t.Columns = cols
	units, rowOf := statsRows(people, cat, true)
	var unclassified []uuid.UUID
	add := func(row, col string, id uuid.UUID) {
		k := CellKey(row, col)
		t.Cells[k] = append(t.Cells[k], id)
	}
	for _, p := range people {
		row := rowOf(p)
		t.People = append(t.People, p.StaffID)
		cells := []string{"tong"}
		if p.Gender == "nu" {
			cells = append(cells, "nu")
		}
		if minority(p, cat) {
			cells = append(cells, "dtts")
		}
		d, ok := cat.TopDegree(p.Degrees)
		switch {
		case !ok || !contains(counted, d.Level):
			cells = append(cells, "khac")
		default:
			cells = append(cells, "lv:"+d.Level)
			mj, _ := cat.item(KindMajor, d.Major)
			if g := mj.Attrs.Groups[d.Level]; g != "" {
				cells = append(cells, "g:"+g)
			} else {
				unclassified = append(unclassified, p.StaffID)
			}
		}
		for _, c := range cells {
			add(row, c, p.StaffID)
			add(RowKeyCenter, c, p.StaffID)
		}
	}
	emitStats(t, append([]UnitRef{{Code: RowKeyCenter, Name: "TRUNG TÂM"}}, units...))
	n01 := len(people)
	total := len(t.Cells[CellKey(RowKeyCenter, "tong")])
	t.Checks = append(t.Checks,
		Check{Rule: "R2", Level: LevelBlock, OK: total == n01,
			Message: fmt.Sprintf("Tổng số NS05 (%d) = số người NS01 cùng ngày chốt (%d)", total, n01)},
		Check{Rule: "CLASS", Level: LevelWarn, OK: len(unclassified) == 0, StaffIDs: unclassified,
			Message: fmt.Sprintf("%d người có văn bằng chưa phân loại được vào nhóm chuyên ngành (FN-19 BR-03)", len(unclassified))},
		checkTK(in.People, t.People))
}

func minority(p Person, cat Catalog) bool {
	if p.Ethnicity == "" {
		return false
	}
	it, ok := cat.item(KindEthnicity, p.Ethnicity)
	return ok && it.Attrs.Minority || !ok && p.Ethnicity != "kinh"
}

// emitStats ghi dòng thống kê từ Cells (dòng không người vẫn hiện số 0 — giữ bố cục mẫu Sở).
func emitStats(t *Table, rows []UnitRef) {
	n := 0
	for _, u := range rows {
		cells := make([]string, len(t.Columns))
		kind := RowUnit
		if u.Code == RowKeyCenter {
			kind = RowTotal
		} else {
			n++
			cells[0] = strconv.Itoa(n)
		}
		cells[1] = u.Name
		for i, c := range t.Columns {
			if c.Numeric {
				cells[i] = strconv.Itoa(len(t.Cells[CellKey(u.Code, c.Key)]))
			}
		}
		t.Rows = append(t.Rows, Row{Kind: kind, Key: u.Code, Cells: cells})
	}
}

// --- NS06: Phụ lục 2. ---

// Nhóm tuổi NS06 (cột 33–39).
const (
	AgeUnder30 = "age_le30"
	Age31to40  = "age_31_40"
	Age41to50  = "age_41_50"
	AgeFrom51  = "age_ge51"
)

// AgeMonths trả số tháng tuổi tròn tại ngày at.
func AgeMonths(birth, at time.Time) int {
	m := (at.Year()-birth.Year())*12 + int(at.Month()) - int(birth.Month())
	if at.Day() < birth.Day() {
		m--
	}
	return m
}

// AgeGroup trả nhóm tuổi NS06 theo tuổi tròn năm.
func AgeGroup(years int) string {
	switch {
	case years <= 30:
		return AgeUnder30
	case years <= 40:
		return Age31to40
	case years <= 50:
		return Age41to50
	}
	return AgeFrom51
}

// OverRetirement cho biết người đã quá tuổi nghỉ hưu tại ngày at (false nếu thiếu giới tính/ngày sinh/tham số).
func (r RetirementAges) OverRetirement(p Person, at time.Time) bool {
	lim := r.limit(p)
	return lim > 0 && p.Birth != nil && AgeMonths(*p.Birth, at) >= lim
}

// RetireSoon cho biết người sẽ đến tuổi nghỉ hưu trong cửa sổ nhắc (chưa quá).
func (r RetirementAges) RetireSoon(p Person, at time.Time) bool {
	lim := r.limit(p)
	if lim <= 0 || p.Birth == nil || r.RemindMonths <= 0 {
		return false
	}
	m := AgeMonths(*p.Birth, at)
	return m < lim && m+r.RemindMonths >= lim
}

func (r RetirementAges) limit(p Person) int {
	switch p.Gender {
	case "nam":
		return r.MaleMonths
	case "nu":
		return r.FemaleMonths
	}
	return 0
}

func ns06Columns() []Column {
	c := func(key, group, label string, sens bool) Column {
		return Column{Key: key, Group: group, Label: label, Numeric: true, Sensitive: sens}
	}
	return []Column{
		{Key: "stt", Label: "STT"}, {Key: "don_vi", Label: "Đơn vị"}, c("tong", "", "Tổng số", false),
		c("nu", "Trong đó", "Nữ", false), c("dang_vien", "Trong đó", "Đảng viên", true), c("dtts", "Trong đó", "DTTS", true),
		c("ton_giao", "Trong đó", "Tôn giáo", true),
		c("ng:CVCC", "Ngạch/hạng", "CVCC và tương đương", false), c("ng:CVC", "Ngạch/hạng", "CVC và tương đương", false),
		c("ng:CV", "Ngạch/hạng", "CV và tương đương", false), c("ng:CS", "Ngạch/hạng", "CS và tương đương", false),
		c("ng:CON_LAI", "Ngạch/hạng", "Còn lại", false),
		c("cm:tien_si", "Chuyên môn", "TS", false), c("cm:thac_si", "Chuyên môn", "ThS", false), c("cm:dai_hoc", "Chuyên môn", "ĐH", false),
		c("cm:cao_dang", "Chuyên môn", "CĐ", false), c("cm:trung_cap", "Chuyên môn", "TC", false), c("cm:con_lai", "Chuyên môn", "Còn lại", false),
		c("ct:cu_nhan", "Chính trị", "Cử nhân", false), c("ct:cao_cap", "Chính trị", "Cao cấp", false),
		c("ct:trung_cap", "Chính trị", "Trung cấp", false), c("ct:so_cap", "Chính trị", "Sơ cấp", false),
		c("th:tc", "Tin học", "Trung cấp trở lên", false), c("th:cc", "Tin học", "Chứng chỉ", false),
		c("nn:anh_dh", "Ngoại ngữ — Anh văn", "ĐH trở lên", false), c("nn:anh_cc", "Ngoại ngữ — Anh văn", "Chứng chỉ", false),
		c("nn:khac_dh", "Ngoại ngữ — khác", "ĐH trở lên", false), c("nn:khac_cc", "Ngoại ngữ — khác", "Chứng chỉ", false),
		c("nn:dtts", "", "Tiếng dân tộc", false),
		c("ql:cvcc", "QLNN", "CVCC", false), c("ql:cvc", "QLNN", "CVC", false), c("ql:cv", "QLNN", "CV và tương đương", false),
		c(AgeUnder30, "Độ tuổi", "Dưới 30", false), c(Age31to40, "Độ tuổi", "31–40", false), c(Age41to50, "Độ tuổi", "41–50", false),
		c(AgeFrom51, "Độ tuổi — từ 51", "Tổng", false), c("age_nu_51_60", "Độ tuổi — từ 51", "Nữ 51–60", false),
		c("age_nam_51_62", "Độ tuổi — từ 51", "Nam 51–62", false), c("age_over", "", "Trên tuổi nghỉ hưu", false),
	}
}

func buildNS06(t *Table, in BuildInput) {
	cat := in.Cat
	t.Columns = ns06Columns()
	people := activeOf(in.People, StaffVienChuc, StaffHopDong)
	units, rowOf := statsRows(people, cat, false)
	var noAge, over, soon []uuid.UUID
	add := func(row, col string, id uuid.UUID) {
		for _, r := range []string{row, RowKeyCenter} {
			k := CellKey(r, col)
			t.Cells[k] = append(t.Cells[k], id)
		}
	}
	levelCols := map[string]bool{"tien_si": true, "thac_si": true, "dai_hoc": true, "cao_dang": true, "trung_cap": true}
	for _, p := range people {
		row, id := rowOf(p), p.StaffID
		t.People = append(t.People, id)
		add(row, "tong", id)
		if p.Gender == "nu" {
			add(row, "nu", id)
		}
		if p.PartyDate != "" {
			if d, _, err := ParseDate(p.PartyDate); err == nil && !d.After(in.AsOf) {
				add(row, "dang_vien", id)
			}
		}
		if minority(p, cat) {
			add(row, "dtts", id)
		}
		if p.Religion != "" {
			if it, ok := cat.item(KindReligion, p.Religion); !ok || !it.Attrs.NoReligion {
				add(row, "ton_giao", id)
			}
		}
		eq := EquivConLai
		if it, ok := cat.item(KindGrade, p.Snap.Grade); ok && it.Attrs.EquivGroup != "" {
			eq = it.Attrs.EquivGroup
		}
		add(row, "ng:"+eq, id)
		lv := "con_lai"
		if d, ok := cat.TopDegree(p.Degrees); ok && levelCols[d.Level] {
			lv = d.Level
		}
		add(row, "cm:"+lv, id)
		if tr, ok := cat.bestTraining(p.Trainings, TrainingPoliticalTheory, KindPoliticalTheory); ok {
			add(row, "ct:"+tr.Code, id)
		}
		if tr, ok := cat.bestTraining(p.Trainings, TrainingIT, KindITLevel); ok {
			if tr.Code == "trung_cap_tro_len" {
				add(row, "th:tc", id)
			} else {
				add(row, "th:cc", id)
			}
		}
		lang := map[string]bool{}
		for _, tr := range p.Trainings {
			if tr.Type != TrainingForeignLanguage {
				continue
			}
			kind := "cc"
			if tr.Cert == "degree" {
				kind = "dh"
			}
			switch tr.Code {
			case "tieng_anh":
				lang["nn:anh_"+kind] = true
			case "tieng_dtts":
				lang["nn:dtts"] = true
			default:
				lang["nn:khac_"+kind] = true
			}
		}
		for k := range lang {
			add(row, k, id)
		}
		if tr, ok := cat.bestTraining(p.Trainings, TrainingStateMgmt, KindStateMgmt); ok {
			c := tr.Code
			if c == "can_su" {
				c = "cv"
			}
			add(row, "ql:"+c, id)
		}
		if p.Birth == nil {
			noAge = append(noAge, id)
		} else {
			years := AgeMonths(*p.Birth, in.AsOf) / 12
			g := AgeGroup(years)
			add(row, g, id)
			if g == AgeFrom51 {
				if p.Gender == "nu" && years <= 60 {
					add(row, "age_nu_51_60", id)
				}
				if p.Gender == "nam" && years <= 62 {
					add(row, "age_nam_51_62", id)
				}
			}
			if in.Retirement.OverRetirement(p, in.AsOf) {
				add(row, "age_over", id)
				over = append(over, id)
			} else if in.Retirement.RetireSoon(p, in.AsOf) {
				soon = append(soon, id)
			}
		}
	}
	emitStats(t, append([]UnitRef{{Code: RowKeyCenter, Name: "TỔNG CỘNG"}}, units...))
	// R1 (chặn): tổng cột 8–12, 13–18, 33–36 = cột 3 ở MỌI dòng.
	var bad []string
	for _, u := range append([]UnitRef{{Code: RowKeyCenter, Name: "Tổng cộng"}}, units...) {
		total := len(t.Cells[CellKey(u.Code, "tong")])
		ageKeys := map[string]bool{AgeUnder30: true, Age31to40: true, Age41to50: true, AgeFrom51: true}
		for _, grp := range []struct {
			name string
			in   func(string) bool
		}{{"ngạch/hạng", func(k string) bool { return strings.HasPrefix(k, "ng:") }},
			{"trình độ chuyên môn", func(k string) bool { return strings.HasPrefix(k, "cm:") }},
			{"độ tuổi", func(k string) bool { return ageKeys[k] }}} {
			sum := 0
			for _, c := range t.Columns {
				if grp.in(c.Key) {
					sum += len(t.Cells[CellKey(u.Code, c.Key)])
				}
			}
			if sum != total {
				bad = append(bad, fmt.Sprintf("%s: tổng %s %d ≠ %d", u.Name, grp.name, sum, total))
			}
		}
	}
	msg := "Tổng các nhóm ngạch/hạng, trình độ chuyên môn, độ tuổi = tổng số có mặt ở mọi dòng"
	if len(bad) > 0 {
		msg += " — lệch: " + strings.Join(bad, "; ")
		if len(noAge) > 0 {
			msg += fmt.Sprintf(" (%d người thiếu ngày sinh)", len(noAge))
		}
	}
	t.Checks = append(t.Checks, Check{Rule: "R1", Level: LevelBlock, OK: len(bad) == 0, Message: msg, StaffIDs: noAge},
		Check{Rule: "R6", Level: LevelWarn, OK: len(over) == 0, StaffIDs: over,
			Message: fmt.Sprintf("%d người trên tuổi nghỉ hưu vẫn đang công tác", len(over))},
		Check{Rule: "R6", Level: LevelWarn, OK: true, StaffIDs: soon,
			Message: fmt.Sprintf("Nhắc: %d người đến tuổi nghỉ hưu trong %d tháng tới", len(soon), in.Retirement.RemindMonths)},
		checkTK(in.People, t.People))
}

// checkR3 (cảnh báo): số NS01 tại ngày chốt = số cuối tháng trước + tiếp nhận − nghỉ, chuyển đi.
func checkR3(in BuildInput, now int) Check {
	want := len(in.Prev) + len(in.Joined) - len(in.LeftIDs)
	return Check{Rule: "R3", Level: LevelWarn, OK: now == want,
		Message: fmt.Sprintf("Số NS01 ngày %s (%d) = ngày %s (%d) + tiếp nhận (%d) − nghỉ, chuyển đi (%d) = %d", in.AsOf.Format("02/01/2006"),
			now, in.PrevAsOf.Format("02/01/2006"), len(in.Prev), len(in.Joined), len(in.LeftIDs), want)}
}

// TKRequired là các trường bắt buộc của thẻ Thống kê BC Sở với viên chức, HĐLĐ (TK-01, TK-02, TK-04, TK-06).
var TKRequired = []string{"ethnicity", "religion", "hometown", "revolution_date"}

// checkTK (cảnh báo): người trong biểu thiếu trường TK bắt buộc (chỉ khi đã đọc thẻ).
func checkTK(all []Person, in []uuid.UUID) Check {
	set := map[uuid.UUID]bool{}
	for _, id := range in {
		set[id] = true
	}
	var miss []uuid.UUID
	for _, p := range all {
		if !set[p.StaffID] || !p.Sensitive || !p.Type.HasSensitiveTab() {
			continue
		}
		if p.Ethnicity == "" || p.Religion == "" || p.Hometown == "" || p.RevolutionDate == "" {
			miss = append(miss, p.StaffID)
		}
	}
	return Check{Rule: "TK", Level: LevelWarn, OK: len(miss) == 0, StaffIDs: miss,
		Message: fmt.Sprintf("%d người thiếu dữ liệu thẻ Thống kê BC Sở (dân tộc, tôn giáo, quê quán, ngày TGCM)", len(miss))}
}

// --- FN-06: Dashboard nhân sự. ---

// Dashboard là số liệu tổng quan tại một ngày chốt (BR-01: khớp NS01, NS06 cùng ngày).
type Dashboard struct {
	AsOf    time.Time
	Total   int            // = số dòng NS01 (viên chức Đang công tác) — AC-02
	Staff   int            // dân số NS06 (viên chức + HĐLĐ Đang công tác)
	ByType  map[string]int // loại nhân sự → số Đang công tác
	ByUnit  []Count
	Gender  map[string]int // nam | nu | unknown (dân số NS06)
	Age     map[string]int // nhóm tuổi NS06 + unknown
	Edu     []Count        // trình độ cao nhất (dân số NS06)
	Grade   map[string]int // nhóm tương đương (dân số NS06)
	Joined  int            // vào làm trong tháng của ngày chốt (mọi loại)
	Left    int            // nghỉ việc trong tháng của ngày chốt (mọi loại)
	Soon    int            // đến tuổi nghỉ hưu trong cửa sổ nhắc
	Over    int            // trên tuổi nghỉ hưu vẫn công tác
	Members map[string][]uuid.UUID
	PII     bool // đọc được giới tính/ngày sinh từ kho
}

// Count là một cặp mã – số.
type Count struct {
	Key, Name string
	N         int
}

// BuildDashboard dựng dashboard từ dân số trong phạm vi người xem (ps đã chiếu tại AsOf).
func BuildDashboard(ps []Person, cat Catalog, asOf time.Time, ret RetirementAges, pii bool) Dashboard {
	d := Dashboard{AsOf: asOf, ByType: map[string]int{}, Gender: map[string]int{}, Age: map[string]int{}, Grade: map[string]int{},
		Members: map[string][]uuid.UUID{}, PII: pii}
	put := func(k string, id uuid.UUID) { d.Members[k] = append(d.Members[k], id) }
	from := PeriodStart(PeriodMonth, asOf)
	unit := map[string]int{}
	edu := map[string]int{}
	for _, p := range ps {
		if p.Snap.StartedOn != nil && !p.Snap.StartedOn.Before(from) && !p.Snap.StartedOn.After(asOf) {
			d.Joined++
			put("joined", p.StaffID)
		}
		if p.Snap.Status == StatusLeft && p.Snap.EndedOn != nil && !p.Snap.EndedOn.Before(from) {
			d.Left++
			put("left", p.StaffID)
		}
		if p.Snap.Status != StatusActive {
			continue
		}
		d.ByType[string(p.Type)]++
		put("type:"+string(p.Type), p.StaffID)
		if p.Type == StaffVienChuc {
			d.Total++
			put("total", p.StaffID)
		}
		if p.Type == StaffCanBoXa {
			continue
		}
		d.Staff++
		put("staff", p.StaffID)
		u := p.Snap.Unit
		unit[u]++
		put("unit:"+u, p.StaffID)
		g := p.Gender
		if g == "" {
			g = "unknown"
		}
		d.Gender[g]++
		put("gender:"+g, p.StaffID)
		a := "unknown"
		if p.Birth != nil {
			a = AgeGroup(AgeMonths(*p.Birth, asOf) / 12)
		}
		d.Age[a]++
		put("age:"+a, p.StaffID)
		lv := "none"
		if dg, ok := cat.TopDegree(p.Degrees); ok {
			lv = dg.Level
		}
		edu[lv]++
		put("edu:"+lv, p.StaffID)
		eq := EquivConLai
		if it, ok := cat.item(KindGrade, p.Snap.Grade); ok && it.Attrs.EquivGroup != "" {
			eq = it.Attrs.EquivGroup
		}
		d.Grade[eq]++
		put("grade:"+eq, p.StaffID)
		if ret.OverRetirement(p, asOf) {
			d.Over++
			put("over_retire", p.StaffID)
		} else if ret.RetireSoon(p, asOf) {
			d.Soon++
			put("retire_soon", p.StaffID)
		}
	}
	seen := map[string]bool{}
	for _, u := range cat.Units {
		if unit[u.Code] > 0 {
			d.ByUnit = append(d.ByUnit, Count{u.Code, u.Name, unit[u.Code]})
		}
		seen[u.Code] = true
	}
	var extra []string
	for u := range unit {
		if !seen[u] {
			extra = append(extra, u)
		}
	}
	sort.Strings(extra)
	for _, u := range extra {
		d.ByUnit = append(d.ByUnit, Count{u, cat.UnitName(u), unit[u]})
	}
	for _, lv := range append(cat.sortedCodes(KindEducationLevel, nil), "none") {
		if edu[lv] > 0 {
			name := cat.Name(KindEducationLevel, lv)
			if lv == "none" {
				name = "Chưa có văn bằng"
			}
			d.Edu = append(d.Edu, Count{lv, name, edu[lv]})
		}
	}
	return d
}

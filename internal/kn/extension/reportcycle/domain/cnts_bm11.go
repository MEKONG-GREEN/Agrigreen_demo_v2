package domain

// Báo cáo phòng CNTS BM-11 tuần/tháng/quý/năm (W11.45 — BRD CNTS v3.7 UR-TINH-16, FN-16 BR-01…BR-06, Mục III.5 quy tắc 13 và
// vòng đời "Báo cáo phòng (BM-11)"; D4 CN5). Bố cục theo báo cáo giấy của phòng ("Phòng CNTS BC tuần 39", "Báo cáo tháng 9
// Phòng Chăn nuôi – Thủy sản"): phần TỰ ĐỘNG (thủy sản: khai thác + nuôi trồng, lũy kế; chăn nuôi: tổng đàn, tiêm phòng, hóa
// chất, KSGM — từ BM-04/BM-17/BM-18 đã nộp; kết quả chương trình, dự án) không sửa tay (BR-01); phần VIẾT (hạn chế, phối hợp,
// kế hoạch, đề xuất…) do cán bộ tổng hợp soạn, chủ nhiệm viết đoạn của chương trình mình.
//
// Hàm thuần: app đọc nguồn (FiguresReader, sổ nuôi trồng, CT/DA) rồi gọi các hàm ở đây. Số decimal chính xác (R-10).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// Loại báo cáo BM-11.
const (
	BM11Week    = "week"
	BM11Month   = "month"
	BM11Quarter = "quarter"
	BM11Year    = "year"
)

// BM11Types là các loại báo cáo theo thứ tự màn.
var BM11Types = []string{BM11Week, BM11Month, BM11Quarter, BM11Year}

// Trạng thái báo cáo (sau mỗi bước của sổ cnts_bm11_event). "Đã duyệt" là bước tức thời: duyệt xong tự gửi (BR-03).
const (
	BM11Draft    = "draft"    // Đang soạn
	BM11Pending  = "pending"  // Chờ duyệt
	BM11Returned = "returned" // Trả lại (có ý kiến) — soạn tiếp
	BM11Sent     = "sent"     // Đã gửi (từng nơi nhận bấm Đã nhận)
)

// Bước của sổ.
const (
	BM11EvCreated   = "created"
	BM11EvRefreshed = "refreshed"
	BM11EvEdited    = "edited"
	BM11EvSubmitted = "submitted"
	BM11EvReturned  = "returned"
	BM11EvApproved  = "approved"
	BM11EvReceived  = "received"
)

// Nơi nhận (BRD: tuần — Ban Giám đốc, HCTH; tháng/quý/năm — Ban Giám đốc, KHTC; cấu hình được lúc tạo).
const (
	BM11RecipientBGD  = "bgd"
	BM11RecipientHCTH = "hcth"
	BM11RecipientKHTC = "khtc"
)

// BM11Recipients là nơi nhận hợp lệ (thứ tự in "Nơi nhận").
var BM11Recipients = []Key{{BM11RecipientBGD, "Ban Giám đốc TTKN"}, {BM11RecipientHCTH, "Phòng Hành chính – Tổng hợp"},
	{BM11RecipientKHTC, "Phòng Kế hoạch – Tài chính"}}

// BM11Schema là thẻ dạng nội dung phiên bản; BM11RuleVersion ghi ở sổ (đổi cách tính = phiên bản mới).
const (
	BM11Schema      = "kn.cnts_bm11/v1"
	BM11RuleVersion = "kn.cnts_bm11/v1"
)

// Giới hạn đầu vào người dùng (lesson #107/#109: chặn kích thước do người dùng điều khiển).
const (
	BM11MaxSectionRunes = 8000
	BM11MaxCommentRunes = 2000
	BM11MaxPrograms     = 200
	BM11MaxWeeks        = 60
)

// BM11DefaultRecipients trả nơi nhận mặc định của loại báo cáo.
func BM11DefaultRecipients(t string) []string {
	if t == BM11Week {
		return []string{BM11RecipientBGD, BM11RecipientHCTH}
	}
	return []string{BM11RecipientBGD, BM11RecipientKHTC}
}

// BM11NormalizeRecipients kiểm và sắp nơi nhận (rỗng = mặc định).
func BM11NormalizeRecipients(t string, in []string) ([]string, error) {
	if len(in) == 0 {
		return BM11DefaultRecipients(t), nil
	}
	if len(in) > len(BM11Recipients) {
		return nil, fmt.Errorf("tối đa %d nơi nhận", len(BM11Recipients))
	}
	var out []string
	for _, r := range BM11Recipients {
		if slices.Contains(in, r.Key) {
			out = append(out, r.Key)
		}
	}
	for _, x := range in {
		if !slices.ContainsFunc(BM11Recipients, func(k Key) bool { return k.Key == x }) {
			return nil, fmt.Errorf("nơi nhận %q không hợp lệ (bgd, hcth, khtc)", x)
		}
	}
	return out, nil
}

// BM11RecipientLabel trả tên nơi nhận.
func BM11RecipientLabel(code string) string {
	for _, r := range BM11Recipients {
		if r.Key == code {
			return r.Label
		}
	}
	return code
}

// BM11Next trả trạng thái sau bước ev từ trạng thái cur ("" = chưa có báo cáo); false = không chuyển được (khớp trigger DB).
func BM11Next(cur, ev string) (string, bool) {
	editable := cur == BM11Draft || cur == BM11Returned
	switch {
	case ev == BM11EvCreated && cur == "":
		return BM11Draft, true
	case (ev == BM11EvRefreshed || ev == BM11EvEdited) && editable:
		return BM11Draft, true
	case ev == BM11EvSubmitted && editable:
		return BM11Pending, true
	case ev == BM11EvReturned && cur == BM11Pending:
		return BM11Returned, true
	case ev == BM11EvApproved && cur == BM11Pending:
		return BM11Sent, true
	case ev == BM11EvReceived && cur == BM11Sent:
		return BM11Sent, true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Kỳ
// ---------------------------------------------------------------------------

// BM11Period là kỳ của báo cáo: khoảng ngày dương lịch + các tuần CNTS có NGÀY CUỐI trong khoảng (cùng quy tắc lũy kế
// W11.44 — tuần vắt tháng/quý/năm tính theo ngày cuối tuần). Báo cáo tuần: một tuần CNTS.
type BM11Period struct {
	Type, Key, Label string
	From, To         time.Time
	Weeks            []Span
}

// BM11CalendarRange trả khoảng ngày của kỳ tháng "YYYY-MM", quý "YYYY-Qn", năm "YYYY".
func BM11CalendarRange(t, key string) (time.Time, time.Time, string, error) {
	bad := fmt.Errorf("khóa kỳ %q không hợp lệ với loại %s", key, t)
	switch t {
	case BM11Month:
		d, err := time.Parse("2006-01", key)
		if err != nil {
			return time.Time{}, time.Time{}, "", bad
		}
		from := Date(d.Year(), d.Month(), 1)
		return from, from.AddDate(0, 1, -1), fmt.Sprintf("Tháng %d/%d", int(d.Month()), d.Year()), nil
	case BM11Quarter:
		y, q, ok := strings.Cut(key, "-Q")
		yy, e1 := strconv.Atoi(y)
		qq, e2 := strconv.Atoi(q)
		if !ok || e1 != nil || e2 != nil || len(y) != 4 || qq < 1 || qq > 4 {
			return time.Time{}, time.Time{}, "", bad
		}
		from := Date(yy, time.Month(3*qq-2), 1)
		return from, from.AddDate(0, 3, -1), fmt.Sprintf("Quý %s/%d", []string{"I", "II", "III", "IV"}[qq-1], yy), nil
	case BM11Year:
		yy, err := strconv.Atoi(key)
		if err != nil || len(key) != 4 {
			return time.Time{}, time.Time{}, "", bad
		}
		return Date(yy, 1, 1), Date(yy, 12, 31), fmt.Sprintf("Năm %d", yy), nil
	}
	return time.Time{}, time.Time{}, "", bad
}

// BM11WeeksEnding trả các tuần (quy tắc r) có ngày cuối trong [from, to], cũ trước.
func BM11WeeksEnding(r PeriodRule, from, to time.Time) ([]Span, error) {
	w, err := r.PeriodOf(PeriodWeek, from, nil)
	if err != nil {
		return nil, err
	}
	var out []Span
	for !w.To.After(to) && len(out) <= BM11MaxWeeks {
		if !w.To.Before(from) {
			out = append(out, w)
		}
		if w, err = r.PeriodOf(PeriodWeek, w.To.AddDate(0, 0, 1), nil); err != nil {
			return nil, err
		}
	}
	if len(out) > BM11MaxWeeks {
		return nil, fmt.Errorf("kỳ quá dài (tối đa %d tuần)", BM11MaxWeeks)
	}
	return out, nil
}

// BM11DueAt là mốc được lập báo cáo: hạn nộp của tuần cuối (12:00 ngày sau ngày cuối tuần, giờ loc — BR-06: báo cáo tuần lập
// sau 12:00 Thứ Ba).
func BM11DueAt(last Span, loc *time.Location) time.Time {
	d := last.To.AddDate(0, 0, 1)
	return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, loc)
}

// ---------------------------------------------------------------------------
// Số liệu tự động
// ---------------------------------------------------------------------------

// BM11Line là một chỉ tiêu chăn nuôi của báo cáo (dòng bảng 1.2 của mẫu tuần).
type BM11Line struct {
	Key, Label, Unit string
	Group            string   // tong_dan | tiem_phong | hoa_chat | ksgm
	Stock            bool     // số hiện có (tổng đàn) — lấy tuần cuối của kỳ; còn lại cộng các tuần
	Sum              []string // trường BM-04 cộng thành dòng
	Sub              bool     // dòng con (thụt vào, "- Tả heo Châu Phi")
}

// BM11Groups là nhóm dòng chăn nuôi (thứ tự mẫu).
var BM11Groups = []Key{{"tong_dan", "Tổng đàn"}, {"tiem_phong", "Tiêm phòng"}, {"hoa_chat", "Hóa chất"}, {"ksgm", "Kiểm soát giết mổ"}}

func bm11L(group, key, label, unit string, stock, sub bool, sum ...string) BM11Line {
	if len(sum) == 0 {
		sum = []string{key}
	}
	return BM11Line{Key: key, Label: label, Unit: unit, Group: group, Stock: stock, Sum: sum, Sub: sub}
}

// BM11Lines là các chỉ tiêu chăn nuôi theo mẫu báo cáo tuần của phòng (trường BM-04 W11.42).
var BM11Lines = []BM11Line{
	bm11L("tong_dan", "tong_dan_heo", "Heo", "con", true, false), bm11L("tong_dan", "tong_dan_de", "Dê", "con", true, false),
	bm11L("tong_dan", "tong_dan_trau", "Trâu", "con", true, false), bm11L("tong_dan", "tong_dan_bo", "Bò", "con", true, false),
	bm11L("tong_dan", "tong_dan_vit", "Vịt", "con", true, false), bm11L("tong_dan", "tong_dan_ga", "Gà", "con", true, false),
	bm11L("tong_dan", "tong_dan_yen", "Yến", "nhà", true, false), bm11L("tong_dan", "tong_dan_cho_meo", "Chó, mèo", "con", true, false),
	bm11L("tiem_phong", "tiem_heo", "Heo", "liều", false, false, "tiem_ta_heo_cp", "tiem_dich_ta_tht_heo", "tiem_lmlm_heo", "tiem_tai_xanh_heo"),
	bm11L("tiem_phong", "tiem_ta_heo_cp", "Tả heo Châu Phi", "liều", false, true),
	bm11L("tiem_phong", "tiem_dich_ta_tht_heo", "Dịch tả – Tụ huyết trùng", "liều", false, true),
	bm11L("tiem_phong", "tiem_lmlm_heo", "Lở mồm long móng", "liều", false, true),
	bm11L("tiem_phong", "tiem_tai_xanh_heo", "Heo tai xanh", "liều", false, true),
	bm11L("tiem_phong", "tiem_bo", "Bò", "liều", false, false, "tiem_vdnc_bo", "tiem_tht_bo", "tiem_lmlm_bo"),
	bm11L("tiem_phong", "tiem_vdnc_bo", "VDNC", "liều", false, true),
	bm11L("tiem_phong", "tiem_tht_bo", "Tụ huyết trùng", "liều", false, true),
	bm11L("tiem_phong", "tiem_lmlm_bo", "Lở mồm long móng", "liều", false, true),
	bm11L("tiem_phong", "tiem_cum_gia_cam", "Cúm gia cầm", "liều", false, false),
	bm11L("tiem_phong", "tiem_dai", "Dại chó, mèo", "liều", false, false),
	bm11L("hoa_chat", "hoa_chat_gia_suc", "Gia súc", "lít", false, false),
	bm11L("hoa_chat", "hoa_chat_gia_cam", "Gia cầm", "lít", false, false),
	bm11L("ksgm", "ksgm_gia_suc", "Gia súc", "con", false, false),
	bm11L("ksgm", "ksgm_gia_cam", "Gia cầm", "con", false, false),
}

// BM11Coverage là độ phủ nộp của một biểu nguồn trong kỳ: N = xã có kỳ, n = xã đã nộp MỌI tuần có kỳ (bài nộp Đã nộp/Đã
// xác thực hoặc "không phát sinh"; bỏ bài Yêu cầu điều chỉnh); không ước tính phần thiếu (BR-04).
type BM11Coverage struct {
	Form     string   `json:"form"`
	Expected int      `json:"expected"`
	Reported int      `json:"reported"`
	Missing  []string `json:"missing"` // mã xã thiếu (đã sắp)
	// MissingNames: tên xã thiếu lúc tính (app điền; phụ lục "danh sách xã không nộp" của báo cáo).
	MissingNames []string `json:"missing_names"`
	Periods      int      `json:"periods"`
	Closed       int      `json:"closed"`
}

// Pct là tỷ lệ nộp (%, 0 khi không có xã).
func (c BM11Coverage) Pct() int {
	if c.Expected == 0 {
		return 0
	}
	return c.Reported * 100 / c.Expected
}

// AllClosed: mọi kỳ nguồn đã chốt (có ít nhất một kỳ).
func (c BM11Coverage) AllClosed() bool { return c.Periods > 0 && c.Closed == c.Periods }

func inWeeks(weeks []Span, p SourcePeriod) bool {
	return slices.ContainsFunc(weeks, func(w Span) bool { return w.Key == p.Key || (w.From.Equal(p.From) && w.To.Equal(p.To)) })
}

func communeOf(p SourcePeriod, u string) string {
	if c, ok := p.Parents[u]; ok && c != "" {
		return c
	}
	return u
}

// BM11CoverageOf tính độ phủ của biểu form trên các tuần của kỳ.
func BM11CoverageOf(form string, weeks []Span, ps []SourcePeriod) BM11Coverage {
	c := BM11Coverage{Form: form, Missing: []string{}}
	expected, missing := map[string]bool{}, map[string]bool{}
	for _, p := range ps {
		if !inWeeks(weeks, p) {
			continue
		}
		c.Periods++
		if p.Closed() {
			c.Closed++
		}
		for _, u := range p.Units {
			cm := communeOf(p, u)
			expected[cm] = true
			if _, ok := p.Data[u]; !ok {
				missing[cm] = true
			}
		}
	}
	c.Expected = len(expected)
	for cm := range missing {
		c.Missing = append(c.Missing, cm)
	}
	sort.Strings(c.Missing)
	c.Reported = c.Expected - len(c.Missing)
	return c
}

// BM11Livestock cộng chỉ tiêu chăn nuôi từ BM-04 của các tuần: số hiện có (tổng đàn) = tổng các xã đã nộp TUẦN CUỐI có kỳ;
// số trong kỳ (tiêm phòng, hóa chất, KSGM) = tổng mọi tuần, mọi xã đã nộp. Trường thiếu/không phải số tính 0.
func BM11Livestock(weeks []Span, ps []SourcePeriod) Dec {
	out := Dec{}
	for _, l := range BM11Lines {
		out[l.Key] = decimal.Zero
	}
	var in []SourcePeriod
	for _, p := range ps {
		if inWeeks(weeks, p) {
			in = append(in, p)
		}
	}
	sort.SliceStable(in, func(i, j int) bool { return in[i].To.Before(in[j].To) })
	var lastTo time.Time
	if len(in) > 0 {
		lastTo = in[len(in)-1].To
	}
	val := func(vs map[string]string, k string) decimal.Decimal {
		d, err := decimal.NewFromString(strings.TrimSpace(vs[k]))
		if err != nil {
			return decimal.Zero
		}
		return d
	}
	for _, p := range in {
		for _, u := range p.Units {
			x, ok := p.Data[u]
			if !ok || x.NilReport {
				continue
			}
			for _, l := range BM11Lines {
				if l.Stock && !p.To.Equal(lastTo) {
					continue
				}
				for _, f := range l.Sum {
					out[l.Key] = out[l.Key].Add(val(x.Values, f))
				}
			}
		}
	}
	return out
}

// BM11Catch là số thủy sản của báo cáo (tấn): trong kỳ và lũy kế năm (theo ngày cuối tuần cuối của kỳ).
type BM11Catch struct {
	CatchCur string `json:"catch_cur"` // khai thác trong kỳ
	CatchYTD string `json:"catch_ytd"` // khai thác lũy kế năm
	AquaCur  string `json:"aqua_cur"`  // nuôi trồng trong kỳ (đối tượng tính tấn)
	AquaYTD  string `json:"aqua_ytd"`  // nuôi trồng lũy kế năm
	TotalCur string `json:"total_cur"` // tổng sản lượng = khai thác + nuôi trồng
	TotalYTD string `json:"total_ytd"` // tổng lũy kế năm
	// Groups: khai thác trong kỳ theo nhóm BM-17 (ca, tom, muc, khac).
	Groups map[string]string `json:"groups"`
}

// ---------------------------------------------------------------------------
// Nội dung (một phiên bản)
// ---------------------------------------------------------------------------

// BM11Section là một mục phần viết.
type BM11Section struct {
	Key, Label string
}

// BM11SectionsOf trả các mục phần viết theo loại (bố cục báo cáo giấy của phòng).
func BM11SectionsOf(t string) []BM11Section {
	if t == BM11Week {
		return []BM11Section{{"nhiem_vu_khac", "Một số nhiệm vụ khác có liên quan"}, {"han_che", "Đánh giá hạn chế, vướng mắc"},
			{"phoi_hop", "Công tác phối hợp"}, {"ke_hoach", "Kế hoạch thực hiện nhiệm vụ trong tuần tới"}, {"de_xuat", "Đề xuất, kiến nghị"}}
	}
	return []BM11Section{{"phoi_hop", "Các công tác phối hợp khác"}, {"tien_do", "Tiến độ thực hiện chương trình, dự án"},
		{"thuan_loi", "Những thuận lợi"}, {"kho_khan", "Những khó khăn"}, {"nguyen_nhan", "Nguyên nhân khách quan và chủ quan"},
		{"phuong_huong", "Phương hướng, nhiệm vụ kỳ tới"}, {"de_xuat", "Đề xuất, kiến nghị"}}
}

// BM11Program là kết quả một chương trình/dự án: phần tự động (số điểm, hộ) + đoạn chủ nhiệm viết.
type BM11Program struct {
	Ref        string `json:"ref"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Kind       string `json:"kind"` // tinh | knqg | hop_tac (W11.47)
	ManagerRef string `json:"manager_ref"`
	Sites      int    `json:"sites"`
	Households int    `json:"households"`
	Text       string `json:"text"`
}

// BM11Supplement là một chỉ tiêu của kỳ trước đã gửi có số thay đổi sau khi gửi (BR-02: "Số liệu kỳ trước nộp bổ sung").
type BM11Supplement struct {
	Period string `json:"period"`
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unit   string `json:"unit"`
	Sent   string `json:"sent"`
	Now    string `json:"now"`
}

// BM11Content là nội dung đầy đủ của một phiên bản (đóng băng ở sổ).
type BM11Content struct {
	Schema     string            `json:"schema"`
	Basis      string            `json:"basis"` // submitted | closed (mọi kỳ nguồn đã chốt)
	Weeks      []string          `json:"weeks"`
	Livestock  map[string]string `json:"livestock"`
	Fishery    BM11Catch         `json:"fishery"`
	Coverage   []BM11Coverage    `json:"coverage"`
	Programs   []BM11Program     `json:"programs"`
	Sections   map[string]string `json:"sections"`
	Supplement []BM11Supplement  `json:"supplement"`
	ComputedAt string            `json:"computed_at"`
}

// Hash là SHA-256 (hex) của nội dung dạng JSON (khóa map đã sắp).
func (c BM11Content) Hash() (json.RawMessage, string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

// BM11CheckText kiểm một đoạn viết (độ dài).
func BM11CheckText(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.ValidString(s) && utf8.RuneCountInString(s) <= BM11MaxSectionRunes
}

// BM11Communes trả "Tổng hợp từ n/N xã" của biểu chính (BM-04 chăn nuôi, nếu có kỳ; ngược lại BM-17).
func BM11Communes(cov []BM11Coverage) (BM11Coverage, bool) {
	for _, form := range []string{CNTSBM04, CNTSBM17} {
		for _, c := range cov {
			if c.Form == form && c.Periods > 0 {
				return c, true
			}
		}
	}
	return BM11Coverage{}, false
}

// BM11Diff so số chăn nuôi + thủy sản của nội dung đã gửi với số tính lại hiện tại — các dòng khác nhau (BR-02).
func BM11Diff(period string, sent BM11Content, now Dec, nowCatch BM11Catch) []BM11Supplement {
	var out []BM11Supplement
	cmp := func(key, label, unit, a, b string) {
		da, ea := decimal.NewFromString(orZeroStr(a))
		db, eb := decimal.NewFromString(orZeroStr(b))
		if ea == nil && eb == nil && !da.Equal(db) {
			out = append(out, BM11Supplement{Period: period, Key: key, Label: label, Unit: unit, Sent: da.String(), Now: db.String()})
		}
	}
	for _, l := range BM11Lines {
		if l.Sub {
			continue
		}
		g := ""
		for _, k := range BM11Groups {
			if k.Key == l.Group {
				g = k.Label
			}
		}
		cmp(l.Key, g+" — "+l.Label, l.Unit, sent.Livestock[l.Key], now[l.Key].String())
	}
	cmp("khai_thac", "Khai thác thủy sản", "tấn", sent.Fishery.CatchCur, nowCatch.CatchCur)
	cmp("nuoi_trong", "Nuôi trồng thủy sản", "tấn", sent.Fishery.AquaCur, nowCatch.AquaCur)
	return out
}

func orZeroStr(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}

package app

// Tổng hợp CNTS Cấp Tỉnh (W11.44 — BRD CNTS v3.7 UR-TINH-14, UR-TINH-15, FN-16; D4 CN4): BM-02 KSGM tuần KSGM/tháng (tổng
// BM-01 + dòng điều chỉnh), BM-18 Phụ lục 2 tuần CNTS (khai thác từ BM-17 theo xã + nuôi trồng nhập theo đối tượng, lũy kế
// quý/năm), chốt/mở lại bảng tổng hợp (số công bố cho KHTC qua pf_extension_pub.v1_closed_figures (nhánh cnts_aggregate) — D17, D18), phiên
// bản tệp chia sẻ Chi cục (Excel, có Phụ lục 2). Số tự cộng tính khi đọc từ kỳ native cap/forms (FiguresReader); sổ chỉ
// giữ phần người nhập và phần đóng băng (pf 20261013230931).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// CNTSFacilityCatalog là phần cổng danh mục CNTS (pf/masterdata CNTSCatalogs) mà tổng hợp BM-02 cần: tên, mã, loại,
// mộc dấu của cơ sở giết mổ.
type CNTSFacilityCatalog interface {
	FacilitiesAt(ctx context.Context, on time.Time, communeCodes []string) ([]knport.SlaughterFacility, error)
}

// Mã lỗi (docs/errors/pf-extension-cnts-agg.md).
var (
	ErrAggClosed = problem.New(http.StatusConflict, "extension.cnts_aggregate_closed",
		"Bảng tổng hợp của tuần đã chốt — mở lại trước khi điều chỉnh hoặc nhập nuôi trồng")
	ErrAggSourcesOpen = problem.New(http.StatusConflict, "extension.cnts_aggregate_sources_open",
		"Còn kỳ nguồn của tuần chưa chốt — chốt mọi biểu nguồn của tuần trước khi chốt bảng tổng hợp")
	ErrAggState = problem.New(http.StatusConflict, "extension.cnts_aggregate_state",
		"Bảng tổng hợp đã ở trạng thái này (đã chốt / chưa chốt)")
	ErrShareNotFound = problem.New(http.StatusNotFound, "extension.cnts_share_not_found", "Không tìm thấy phiên bản tệp chia sẻ")
	errNotCNTS       = problem.Forbidden.WithDetail("tổng hợp CNTS chỉ dành cho Phòng Chăn nuôi – Thủy sản (Cấp Tỉnh, Cấp Quốc gia xem) và Admin xem")
	errNotCNTSWriter = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh Phòng Chăn nuôi – Thủy sản ghi điều chỉnh, nuôi trồng, chốt, chia sẻ")
)

// ShareRecipient là nơi nhận mặc định của tệp chia sẻ (BRD CNTS §I: phòng chỉ chia sẻ file KSGM, tiêm phòng cho Chi cục).
const ShareRecipient = "Chi cục Chăn nuôi và Thú y"

// ShareSchema là thẻ dạng nội dung phiên bản tệp chia sẻ.
const ShareSchema = "kn.cnts_share/v1"

var (
	reWeekKey = regexp.MustCompile(`^([0-9]{4})-W([0-9]{2})$`)
	reKSGMKey = regexp.MustCompile(`^([0-9]{4})-(0[1-9]|1[0-2])-K([1-4])$`)
	reMonth   = regexp.MustCompile(`^([0-9]{4})-(0[1-9]|1[0-2])$`)
)

// ---------------------------------------------------------------------------
// Quyền, phạm vi
// ---------------------------------------------------------------------------

func (s *Service) aggReady() error {
	if s.FormsFigures == nil || s.DB == nil {
		return ErrDependency.WithDetail("chưa nối cap/forms (số liệu kỳ native) cho tổng hợp CNTS")
	}
	return nil
}

// aggReader: Admin KN, hoặc Cấp Tỉnh/Cấp Quốc gia phòng CNTS. Tỉnh = tỉnh của Nút; Cấp Quốc gia/Admin truyền province.
func (s *Service) aggReader(ctx context.Context, province string) (actor, string, error) {
	a, p, err := s.aggScope(ctx, province)
	if err == nil && p == "" {
		err = problem.Validation.WithField("province", "Cấp Quốc gia/Admin phải chọn tỉnh")
	}
	return a, p, err
}

// aggScope như aggReader nhưng cho phép tỉnh rỗng (Cấp Quốc gia/Admin không chọn tỉnh — mọi tỉnh của tenant).
func (s *Service) aggScope(ctx context.Context, province string) (actor, string, error) {
	if err := s.aggReady(); err != nil {
		return actor{}, "", err
	}
	a, err := s.knActor(ctx)
	if err != nil {
		return a, "", err
	}
	ok := a.k.Role == knport.SessionRoleAdmin ||
		(a.k.Department == domain.DeptChanNuoiThuySan && (a.k.Role == knport.SessionRoleProvince || a.k.Role == knport.SessionRoleNational))
	if !ok {
		return a, "", errNotCNTS
	}
	p := a.province
	if p == "" {
		p = strings.TrimSpace(province)
	}
	return a, p, nil
}

// aggWriter: Cấp Tỉnh phòng CNTS, không ở chế độ chỉ xem.
func (s *Service) aggWriter(ctx context.Context) (actor, string, error) {
	a, p, err := s.aggReader(ctx, "")
	if err != nil {
		return a, p, err
	}
	if a.k.ReadOnly() {
		return a, p, ErrReadOnly
	}
	if a.k.Role != knport.SessionRoleProvince || a.k.Department != domain.DeptChanNuoiThuySan {
		return a, p, errNotCNTSWriter
	}
	return a, p, nil
}

// aggClassify đổi lỗi DB của sổ tổng hợp sang mã lỗi.
func aggClassify(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "cnts_agg_input_closed":
		return ErrAggClosed.WithCause(err)
	case "cnts_agg_close_state":
		return ErrAggState.WithCause(err)
	}
	if pg.Code == "23505" {
		return problem.Conflict.WithCause(err)
	}
	return err
}

func aggIdem(prefix, idem string) string {
	if idem == "" {
		idem = ids.New().String()
	}
	return "cnts_agg:" + prefix + ":" + idem
}

// ---------------------------------------------------------------------------
// Kỳ
// ---------------------------------------------------------------------------

// KeySpan là một kỳ (khóa, nhãn, khoảng ngày).
type KeySpan struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	From  string `json:"from"`
	To    string `json:"to"`
}

func keySpan(sp domain.Span) KeySpan {
	return KeySpan{Key: sp.Key, Label: sp.Label, From: sp.From.Format(time.DateOnly), To: sp.To.Format(time.DateOnly)}
}

// cntsWeek trả tuần CNTS theo khóa (lịch của phòng tại 01/01 của năm khóa — D8); key rỗng = tuần chứa hôm nay.
func (s *Service) cntsWeek(ctx context.Context, key string) (domain.Span, domain.Calendar, error) {
	if key == "" {
		now := s.Clock.Now().In(clock.Zone())
		today := domain.Date(now.Year(), now.Month(), now.Day())
		cal := s.cntsCalendar(ctx, today)
		sp, err := cal.Rule.PeriodOf(domain.PeriodWeek, today, nil)
		return sp, cal, err
	}
	m := reWeekKey.FindStringSubmatch(key)
	if m == nil {
		return domain.Span{}, domain.Calendar{}, problem.Validation.WithField("week", "dạng YYYY-Www")
	}
	y, _ := strconv.Atoi(m[1])
	cal := s.cntsCalendar(ctx, domain.Date(y, 1, 1))
	sp, err := domain.SpanOfKey(cal.Rule, domain.PeriodWeek, key, nil)
	if err != nil {
		return domain.Span{}, cal, problem.Validation.WithField("week", err.Error())
	}
	return sp, cal, nil
}

// ksgmPeriod trả các tuần KSGM của khóa: tuần KSGM "YYYY-MM-Kn" (một tuần) hoặc tháng "YYYY-MM" (4 tuần); rỗng = tuần
// KSGM chứa hôm nay.
func (s *Service) ksgmPeriod(ctx context.Context, periodType, key string) (string, string, []domain.Span, error) {
	starts := s.ksgmStarts(ctx)
	monthWeeks := func(y int, mo time.Month) ([]domain.Span, error) {
		var st []int
		if starts != nil {
			st = starts(y, mo)
		}
		return domain.MonthKSGMWeeks(y, mo, st)
	}
	if key == "" {
		now := s.Clock.Now().In(clock.Zone())
		ws, err := monthWeeks(now.Year(), now.Month())
		if err != nil {
			return "", "", nil, err
		}
		if periodType == domain.AggMonth {
			return domain.AggMonth, now.Format("2006-01"), ws, nil
		}
		today := domain.Date(now.Year(), now.Month(), now.Day())
		for _, w := range ws {
			if !today.Before(w.From) && !today.After(w.To) {
				return domain.AggKSGMWeek, w.Key, []domain.Span{w}, nil
			}
		}
		return domain.AggKSGMWeek, ws[0].Key, ws[:1], nil
	}
	if m := reKSGMKey.FindStringSubmatch(key); m != nil && periodType != domain.AggMonth {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		ws, err := monthWeeks(y, time.Month(mo))
		if err != nil {
			return "", "", nil, problem.Validation.WithField("key", err.Error())
		}
		for _, w := range ws {
			if w.Key == key {
				return domain.AggKSGMWeek, key, []domain.Span{w}, nil
			}
		}
	}
	if m := reMonth.FindStringSubmatch(key); m != nil && periodType != domain.AggKSGMWeek {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		ws, err := monthWeeks(y, time.Month(mo))
		if err != nil {
			return "", "", nil, problem.Validation.WithField("key", err.Error())
		}
		return domain.AggMonth, key, ws, nil
	}
	return "", "", nil, problem.Validation.WithField("key", "tuần KSGM YYYY-MM-Kn hoặc tháng YYYY-MM")
}

// sources đọc kỳ nguồn và quy về cơ sở số liệu (submitted: bỏ bài "Yêu cầu điều chỉnh"; closed: ảnh chụp mới nhất).
func (s *Service) sources(ctx context.Context, code string, from, to time.Time, basis string, communes map[string]bool) ([]domain.SourcePeriod, error) {
	figs, err := s.FormsFigures.Figures(ctx, formsport.FiguresQuery{Profile: domain.ProfileCNTS, TemplateCode: code, From: from, To: to})
	if err != nil {
		return nil, err
	}
	out := make([]domain.SourcePeriod, 0, len(figs))
	for _, f := range figs {
		// W11.50 (FN-21 BR-02, AC-04): kỳ `adhoc` là đợt điều tra — số liệu riêng của đợt, không cộng vào tổng hợp/lũy kế
		// định kỳ dù đợt dùng cùng mã biểu.
		if f.Kind == domain.PeriodAdhoc {
			continue
		}
		sp := domain.SourcePeriod{ID: f.ID, Key: f.Key, From: f.From, To: f.To, Status: f.Status, ClosedNo: f.ClosedNo, Parents: f.Parents,
			Data: map[string]domain.SourceUnit{}}
		commune := func(u string) string {
			if p, ok := f.Parents[u]; ok {
				return p
			}
			return u
		}
		inProv := func(u string) bool { return communes == nil || communes[commune(u)] }
		for _, u := range f.Units {
			if inProv(u) {
				sp.Units = append(sp.Units, u)
			}
		}
		if len(sp.Units) == 0 {
			continue
		}
		src := f.Submitted
		if basis == domain.BasisClosed {
			src = f.Closed
		}
		for u, x := range src {
			if !inProv(u) || (basis != domain.BasisClosed && x.Decision == "returned") {
				continue
			}
			sp.Data[u] = domain.SourceUnit{Values: x.Values, NilReport: x.NilReport}
		}
		out = append(out, sp)
	}
	return out, nil
}

// provinceCommunes trả tập xã (Nút xã đang hoạt động) của tỉnh; nil = không lọc (chưa nối pf/party).
func provinceCommunes(nb *nameBook) map[string]bool {
	if len(nb.byCommune) == 0 {
		return nil
	}
	out := make(map[string]bool, len(nb.byCommune))
	for c := range nb.byCommune {
		out[c] = true
	}
	return out
}

func decStrings(d domain.Dec, keys []domain.Key) map[string]string {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k.Key] = d[k.Key].String()
	}
	return out
}

// ---------------------------------------------------------------------------
// Trạng thái chốt, phiên bản chia sẻ
// ---------------------------------------------------------------------------

// AggStateView là trạng thái chốt của một bảng tổng hợp (một tuần).
type AggStateView struct {
	Key         string  `json:"key"`
	State       string  `json:"state"` // none | closed | reopened
	CloseCount  int     `json:"close_count"`
	LastAt      *string `json:"last_at"`
	FiguresHash *string `json:"figures_hash"`
	// Changed: số tính lại hiện tại (theo ảnh chụp nguồn) khác số đã chốt — "Nguồn đã thay đổi".
	Changed bool `json:"changed"`
}

// ShareView là một phiên bản tệp chia sẻ.
type ShareView struct {
	ID          string `json:"id"`
	Version     int64  `json:"version"`
	At          string `json:"at"`
	Basis       string `json:"basis"`
	Recipient   string `json:"recipient"`
	Channel     string `json:"channel"`
	Note        string `json:"note"`
	ContentHash string `json:"content_hash"`
	ActorName   string `json:"actor_name"`
}

func shareViews(ctx context.Context, s *Service, rows []store.Share) []ShareView {
	refs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		refs = append(refs, r.ActorRef)
	}
	nm := s.names(ctx, refs)
	out := make([]ShareView, 0, len(rows))
	for _, r := range rows {
		out = append(out, ShareView{ID: r.ID.String(), Version: r.Version, At: r.At.Format(time.RFC3339), Basis: r.Basis, Recipient: r.Recipient,
			Channel: r.Channel, Note: r.Note, ContentHash: r.ContentHash, ActorName: nm[r.ActorRef]})
	}
	return out
}

func aggState(key string, evs []store.AggEvent) AggStateView {
	v := AggStateView{Key: key, State: "none"}
	for _, e := range evs {
		at := e.At.Format(time.RFC3339)
		v.State, v.LastAt = e.Action, &at
		if e.Action == "closed" {
			v.CloseCount++
			h := e.FiguresHash
			v.FiguresHash = &h
		}
	}
	return v
}

// ---------------------------------------------------------------------------
// BM-02
// ---------------------------------------------------------------------------

// BM02RowView là một cơ sở của BM-02.
type BM02RowView struct {
	FacilityRef  string            `json:"facility_ref"`
	FacilityCode string            `json:"facility_code"`
	FacilityName string            `json:"facility_name"`
	FacilityKind string            `json:"facility_kind"` // tap_trung | nho_le | ""
	StampsGS     []string          `json:"stamps_gs"`
	StampsGC     []string          `json:"stamps_gc"`
	CommuneCode  string            `json:"commune_code"`
	CommuneName  string            `json:"commune_name"`
	Auto         map[string]string `json:"auto"`
	Adjust       map[string]string `json:"adjust"`
	Total        map[string]string `json:"total"`
	DaysExpected int               `json:"days_expected"`
	DaysReported int               `json:"days_reported"`
	MissingDays  int               `json:"missing_days"`
	Stale        bool              `json:"stale"`
}

// BM02CommuneView là dòng cộng của một xã.
type BM02CommuneView struct {
	CommuneCode string            `json:"commune_code"`
	CommuneName string            `json:"commune_name"`
	Total       map[string]string `json:"total"`
	Facilities  int               `json:"facilities"`
}

// AdjustmentView là một dòng điều chỉnh.
type AdjustmentView struct {
	ID           string            `json:"id"`
	WeekKey      string            `json:"week_key"`
	FacilityRef  string            `json:"facility_ref"`
	FacilityName string            `json:"facility_name"`
	CommuneCode  string            `json:"commune_code"`
	Deltas       map[string]string `json:"deltas"`
	Reason       string            `json:"reason"`
	ActorName    string            `json:"actor_name"`
	At           string            `json:"at"`
	Stale        bool              `json:"stale"`
}

// SourcesView là tình trạng kỳ nguồn.
type SourcesView struct {
	Count     int  `json:"count"`
	Closed    int  `json:"closed"`
	AllClosed bool `json:"all_closed"`
}

// BM02View là bảng BM-02 (tuần KSGM hoặc tháng).
type BM02View struct {
	Province      string            `json:"province"`
	PeriodType    string            `json:"period_type"`
	PeriodKey     string            `json:"period_key"`
	Label         string            `json:"label"`
	From          string            `json:"from"`
	To            string            `json:"to"`
	PrevKey       string            `json:"prev_key"`
	NextKey       string            `json:"next_key"`
	Basis         string            `json:"basis"`
	Weeks         []KeySpan         `json:"weeks"`
	Species       []domain.Key      `json:"species"`
	Rows          []BM02RowView     `json:"rows"`
	Communes      []BM02CommuneView `json:"communes"`
	Auto          map[string]string `json:"auto"`
	Adjust        map[string]string `json:"adjust"`
	Total         map[string]string `json:"total"`
	Sources       SourcesView       `json:"sources"`
	UnitsTotal    int               `json:"units_total"`
	UnitsReported int               `json:"units_reported"`
	Adjustments   []AdjustmentView  `json:"adjustments"`
	Aggregates    []AggStateView    `json:"aggregates"`
	Shares        []ShareView       `json:"shares"`
}

func monthLabel(key string) string {
	t, err := time.Parse("2006-01", key)
	if err != nil {
		return key
	}
	return fmt.Sprintf("Tháng %d/%d", int(t.Month()), t.Year())
}

func ksgmLabel(w domain.Span) string {
	n := w.Key[len(w.Key)-1:]
	return fmt.Sprintf("Tuần KSGM %s tháng %d/%d (%s – %s)", n, int(w.From.Month()), w.From.Year(), w.From.Format("02/01"), w.To.Format("02/01"))
}

// neighbours trả khóa kỳ trước/sau của BM-02.
func (s *Service) ksgmNeighbours(ctx context.Context, pt string, weeks []domain.Span) (string, string) {
	if pt == domain.AggMonth {
		f := weeks[0].From
		return f.AddDate(0, -1, 0).Format("2006-01"), f.AddDate(0, 1, 0).Format("2006-01")
	}
	w := weeks[0]
	pk, nk := "", ""
	if _, k, ws, err := s.ksgmPeriodAt(ctx, w.From.AddDate(0, 0, -1)); err == nil && len(ws) > 0 {
		pk = k
	}
	if _, k, ws, err := s.ksgmPeriodAt(ctx, w.To.AddDate(0, 0, 1)); err == nil && len(ws) > 0 {
		nk = k
	}
	return pk, nk
}

func (s *Service) ksgmPeriodAt(ctx context.Context, d time.Time) (string, string, []domain.Span, error) {
	var st []int
	if f := s.ksgmStarts(ctx); f != nil {
		st = f(d.Year(), d.Month())
	}
	ws, err := domain.MonthKSGMWeeks(d.Year(), d.Month(), st)
	if err != nil {
		return "", "", nil, err
	}
	for _, w := range ws {
		if !d.Before(w.From) && !d.After(w.To) {
			return domain.AggKSGMWeek, w.Key, []domain.Span{w}, nil
		}
	}
	return "", "", nil, errors.New("ngoài tháng")
}

type bm02Data struct {
	pt, key  string
	weeks    []domain.Span
	res      domain.BM02Result
	adjs     []store.AdjustmentRow
	stale    map[uuid.UUID]bool
	nb       *nameBook
	fac      map[string]knport.SlaughterFacility
	evs      map[string][]store.AggEvent
	shares   []store.Share
	basis    string
	province string
}

func (s *Service) loadBM02(ctx context.Context, a actor, province, periodType, key, basis string) (*bm02Data, error) {
	if basis == "" {
		basis = domain.BasisSubmitted
	}
	if basis != domain.BasisSubmitted && basis != domain.BasisClosed {
		return nil, problem.Validation.WithField("basis", "submitted | closed")
	}
	pt, k, weeks, err := s.ksgmPeriod(ctx, periodType, key)
	if err != nil {
		return nil, err
	}
	nb, err := s.regionNames(ctx, province)
	if err != nil {
		return nil, err
	}
	comm := provinceCommunes(nb)
	from, to := weeks[0].From, weeks[len(weeks)-1].To
	days, err := s.sources(ctx, domain.CNTSBM01, from, to, basis, comm)
	if err != nil {
		return nil, err
	}
	cur := days
	if basis != domain.BasisSubmitted {
		if cur, err = s.sources(ctx, domain.CNTSBM01, from, to, domain.BasisSubmitted, comm); err != nil {
			return nil, err
		}
	}
	auto := domain.AutoByFacilityWeek(weeks, cur)
	d := &bm02Data{pt: pt, key: k, weeks: weeks, nb: nb, basis: basis, province: province, evs: map[string][]store.AggEvent{}}
	wkeys := make([]string, 0, len(weeks))
	for _, w := range weeks {
		wkeys = append(wkeys, w.Key)
	}
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if d.adjs, e = store.Adjustments(ctx, tx, province, wkeys); e != nil {
			return e
		}
		for _, wk := range wkeys {
			if d.evs[wk], e = store.AggEvents(ctx, tx, province, domain.CNTSBM02, domain.AggKSGMWeek, wk); e != nil {
				return e
			}
		}
		d.shares, e = store.Shares(ctx, tx, province, domain.CNTSBM02, pt, k)
		return e
	})
	if err != nil {
		return nil, err
	}
	adjs := make([]domain.BM02Adjustment, 0, len(d.adjs))
	for _, x := range d.adjs {
		adjs = append(adjs, x.BM02Adjustment)
	}
	d.stale = domain.StaleAdjustments(adjs, auto)
	d.res = domain.BM02(weeks, days, adjs, auto)
	d.fac = map[string]knport.SlaughterFacility{}
	if s.CNTSFacilities != nil {
		if fs, err := s.CNTSFacilities.FacilitiesAt(ctx, to, nil); err == nil {
			for _, f := range fs {
				d.fac[f.Ref.String()] = f
			}
		}
	}
	return d, nil
}

func (s *Service) bm02View(ctx context.Context, d *bm02Data) BM02View {
	v := BM02View{Province: d.province, PeriodType: d.pt, PeriodKey: d.key, From: d.weeks[0].From.Format(time.DateOnly),
		To: d.weeks[len(d.weeks)-1].To.Format(time.DateOnly), Basis: d.basis, Species: domain.KSGMSpecies,
		Auto: decStrings(d.res.Auto, domain.KSGMSpecies), Adjust: decStrings(d.res.Adjust, domain.KSGMSpecies),
		Total: decStrings(d.res.Total(), domain.KSGMSpecies), Sources: SourcesView{Count: d.res.SourceCount, Closed: d.res.SourceClosed,
			AllClosed: d.res.AllClosed}, UnitsTotal: d.res.UnitsTotal, UnitsReported: d.res.UnitsReported,
		Rows: []BM02RowView{}, Communes: []BM02CommuneView{}, Adjustments: []AdjustmentView{}, Aggregates: []AggStateView{}}
	if d.pt == domain.AggMonth {
		v.Label = monthLabel(d.key)
	} else {
		v.Label = ksgmLabel(d.weeks[0])
	}
	v.PrevKey, v.NextKey = s.ksgmNeighbours(ctx, d.pt, d.weeks)
	for _, w := range d.weeks {
		ks := keySpan(w)
		ks.Label = ksgmLabel(w)
		v.Weeks = append(v.Weeks, ks)
	}
	facName := func(ref string) string {
		if f, ok := d.fac[ref]; ok {
			return f.Name
		}
		return ref
	}
	var cur *BM02CommuneView
	for _, r := range d.res.Rows {
		f := d.fac[r.Facility]
		commune := r.Commune
		if commune == "" {
			commune = f.CommuneCode
		}
		rv := BM02RowView{FacilityRef: r.Facility, FacilityCode: f.Code, FacilityName: facName(r.Facility), FacilityKind: f.Kind,
			StampsGS: nonNil(f.StampsGS), StampsGC: nonNil(f.StampsGC), CommuneCode: commune, CommuneName: d.nb.commune(ctx, s, commune),
			Auto: decStrings(r.Auto, domain.KSGMSpecies), Adjust: decStrings(r.Adjust, domain.KSGMSpecies),
			Total: decStrings(r.Total(), domain.KSGMSpecies), DaysExpected: r.DaysExpected, DaysReported: r.DaysReported,
			MissingDays: r.MissingDays(), Stale: r.Stale}
		v.Rows = append(v.Rows, rv)
		if cur == nil || cur.CommuneCode != commune {
			v.Communes = append(v.Communes, BM02CommuneView{CommuneCode: commune, CommuneName: rv.CommuneName, Total: map[string]string{}})
			cur = &v.Communes[len(v.Communes)-1]
		}
		cur.Facilities++
		for _, sp := range domain.KSGMSpecies {
			a, _ := decimal.NewFromString(orZero(cur.Total[sp.Key]))
			cur.Total[sp.Key] = a.Add(r.Total()[sp.Key]).String()
		}
	}
	refs := make([]uuid.UUID, 0, len(d.adjs))
	for _, x := range d.adjs {
		refs = append(refs, x.ActorRef)
	}
	nm := s.names(ctx, refs)
	for _, x := range d.adjs {
		v.Adjustments = append(v.Adjustments, AdjustmentView{ID: x.ID.String(), WeekKey: x.WeekKey, FacilityRef: x.Facility,
			FacilityName: facName(x.Facility), CommuneCode: x.Commune, Deltas: decStrings(x.Deltas, domain.KSGMSpecies), Reason: x.Reason,
			ActorName: nm[x.ActorRef], At: x.At.Format(time.RFC3339), Stale: d.stale[x.ID]})
	}
	for _, w := range d.weeks {
		v.Aggregates = append(v.Aggregates, aggState(w.Key, d.evs[w.Key]))
	}
	v.Shares = shareViews(ctx, s, d.shares)
	return v
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// BM02 trả bảng BM-02 (tuần KSGM "YYYY-MM-Kn" hoặc tháng "YYYY-MM").
func (s *Service) BM02(ctx context.Context, province, periodType, key, basis string) (BM02View, error) {
	a, prov, err := s.aggReader(ctx, province)
	if err != nil {
		return BM02View{}, err
	}
	d, err := s.loadBM02(ctx, a, prov, periodType, key, basis)
	if err != nil {
		return BM02View{}, err
	}
	return s.bm02View(ctx, d), nil
}

// AdjustmentInput là một dòng điều chỉnh BM-02.
type AdjustmentInput struct {
	WeekKey     string
	FacilityRef string
	Deltas      map[string]string
	Reason      string
}

// AddAdjustment ghi một dòng điều chỉnh (chỉ ghi thêm). Tuần KSGM đã chốt → 409 extension.cnts_aggregate_closed.
func (s *Service) AddAdjustment(ctx context.Context, in AdjustmentInput, idem string) (AdjustmentView, error) {
	a, prov, err := s.aggWriter(ctx)
	if err != nil {
		return AdjustmentView{}, err
	}
	if !reKSGMKey.MatchString(in.WeekKey) {
		return AdjustmentView{}, problem.Validation.WithField("week_key", "tuần KSGM YYYY-MM-Kn")
	}
	fac, err := uuid.Parse(in.FacilityRef)
	if err != nil {
		return AdjustmentView{}, problem.Validation.WithField("facility_ref", "mã cơ sở (UUID)")
	}
	reason := strings.TrimSpace(in.Reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return AdjustmentView{}, problem.Validation.WithField("reason", "lý do 10–500 ký tự")
	}
	deltas := domain.Dec{}
	nonZero := false
	for k, v := range in.Deltas {
		if !slices.ContainsFunc(domain.KSGMSpecies, func(x domain.Key) bool { return x.Key == k }) {
			return AdjustmentView{}, problem.Validation.WithField("deltas."+k, "loài không có trong BM-01")
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		d, ok := domain.ParseDec(v)
		if !ok || d.Exponent() < -3 {
			return AdjustmentView{}, problem.Validation.WithField("deltas."+k, "số (có thể âm)")
		}
		deltas[k] = d
		nonZero = nonZero || !d.IsZero()
	}
	if !nonZero {
		return AdjustmentView{}, problem.Validation.WithField("deltas", "nhập ít nhất một số khác 0")
	}
	_, _, weeks, err := s.ksgmPeriod(ctx, domain.AggKSGMWeek, in.WeekKey)
	if err != nil {
		return AdjustmentView{}, err
	}
	nb, err := s.regionNames(ctx, prov)
	if err != nil {
		return AdjustmentView{}, err
	}
	days, err := s.sources(ctx, domain.CNTSBM01, weeks[0].From, weeks[0].To, domain.BasisSubmitted, provinceCommunes(nb))
	if err != nil {
		return AdjustmentView{}, err
	}
	base := domain.AutoByFacilityWeek(weeks, days)[fac.String()][in.WeekKey]
	commune := ""
	for _, d := range days {
		if p, ok := d.Parents[fac.String()]; ok {
			commune = p
		}
	}
	if base == nil {
		base = domain.Dec{}
	}
	row := store.AdjustmentRow{BM02Adjustment: domain.BM02Adjustment{ID: ids.New(), Facility: fac.String(), Commune: commune, WeekKey: in.WeekKey,
		Deltas: deltas, Reason: reason, Base: base, ActorRef: a.p.ActorRef, At: s.now()}, Province: prov}
	key := aggIdem("adj", idem)
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		prev, e := store.AdjustmentByIdem(ctx, tx, key)
		if e != nil || prev != nil {
			if prev != nil {
				row = *prev
			}
			return e
		}
		return store.InsertAdjustment(ctx, tx, a.p.TenantID, &row, key)
	})
	if err != nil {
		return AdjustmentView{}, aggClassify(err)
	}
	return AdjustmentView{ID: row.ID.String(), WeekKey: row.WeekKey, FacilityRef: row.Facility, FacilityName: row.Facility, CommuneCode: row.Commune,
		Deltas: decStrings(row.Deltas, domain.KSGMSpecies), Reason: row.Reason, At: row.At.Format(time.RFC3339)}, nil
}

// ---------------------------------------------------------------------------
// BM-18 Phụ lục 2
// ---------------------------------------------------------------------------

// ColsView là bốn cột Phụ lục 2 (chuỗi thập phân).
type ColsView struct {
	Prev string `json:"prev"`
	Cur  string `json:"cur"`
	QTD  string `json:"qtd"`
	YTD  string `json:"ytd"`
}

func colsView(c domain.Cols, key string) ColsView {
	return ColsView{Prev: c.Prev[key].String(), Cur: c.Cur[key].String(), QTD: c.QTD[key].String(), YTD: c.YTD[key].String()}
}

func colsMap(c domain.Cols, keys []domain.Key) map[string]ColsView {
	out := make(map[string]ColsView, len(keys))
	for _, k := range keys {
		out[k.Key] = colsView(c, k.Key)
	}
	return out
}

// BM18CommuneView là một xã (khai thác).
type BM18CommuneView struct {
	CommuneCode  string                       `json:"commune_code"`
	CommuneName  string                       `json:"commune_name"`
	RegionCode   string                       `json:"region_code"`
	RegionName   string                       `json:"region_name"`
	Cells        map[string]ColsView          `json:"cells"` // ca, tom, muc, khac, tong
	Expected     bool                         `json:"expected"`
	Reported     bool                         `json:"reported"`
	NilReport    bool                         `json:"nil_report"`
	MissingWeeks []string                     `json:"missing_weeks"`
	ByWeek       map[string]map[string]string `json:"by_week"` // tuần → nhóm → số (sheet theo tuần)
}

// BM18RegionView là dòng cộng Khu vực.
type BM18RegionView struct {
	RegionCode string              `json:"region_code"`
	RegionName string              `json:"region_name"`
	Sort       int                 `json:"sort"`
	Cells      map[string]ColsView `json:"cells"`
}

// BM18AquaView là một đối tượng nuôi trồng.
type BM18AquaView struct {
	Code       string   `json:"code"`
	Label      string   `json:"label"`
	Group      string   `json:"group"`
	AreaUnit   string   `json:"area_unit"`
	OutputUnit string   `json:"output_unit"`
	InTotal    bool     `json:"in_total"`
	Area       ColsView `json:"area"`
	Output     ColsView `json:"output"`
	// Entered: số nhập của tuần chọn (rỗng = chưa nhập) — ô sửa trên màn.
	EnteredArea   *string `json:"entered_area"`
	EnteredOutput *string `json:"entered_output"`
	Note          string  `json:"note"`
}

// BM18View là bảng Phụ lục 2 của một tuần.
type BM18View struct {
	Province      string              `json:"province"`
	Week          KeySpan             `json:"week"`
	Prev          *KeySpan            `json:"prev"`
	PrevKey       string              `json:"prev_key"`
	NextKey       string              `json:"next_key"`
	Calendar      string              `json:"calendar"`
	Basis         string              `json:"basis"`
	Groups        []domain.Key        `json:"groups"`
	AquaGroups    []domain.Key        `json:"aqua_groups"`
	Weeks         []KeySpan           `json:"weeks"`
	Communes      []BM18CommuneView   `json:"communes"`
	Regions       []BM18RegionView    `json:"regions"`
	Catch         map[string]ColsView `json:"catch"`
	Aqua          []BM18AquaView      `json:"aqua"`
	AquaArea      ColsView            `json:"aqua_area"`
	AquaOutput    ColsView            `json:"aqua_output"`
	Total         ColsView            `json:"total"`
	Sources       SourcesView         `json:"sources"`
	UnitsTotal    int                 `json:"units_total"`
	UnitsReported int                 `json:"units_reported"`
	MissingUnits  []string            `json:"missing_units"`
	Aggregate     AggStateView        `json:"aggregate"`
	Shares        []ShareView         `json:"shares"`
}

type bm18Data struct {
	week     domain.Span
	cal      domain.Calendar
	res      domain.BM18Result
	aqua     []store.AquaRow
	periods  []domain.SourcePeriod
	nb       *nameBook
	evs      []store.AggEvent
	shares   []store.Share
	basis    string
	province string
}

func (s *Service) loadBM18(ctx context.Context, a actor, province, key, basis string) (*bm18Data, error) {
	if basis == "" {
		basis = domain.BasisSubmitted
	}
	if basis != domain.BasisSubmitted && basis != domain.BasisClosed {
		return nil, problem.Validation.WithField("basis", "submitted | closed")
	}
	w, cal, err := s.cntsWeek(ctx, key)
	if err != nil {
		return nil, err
	}
	nb, err := s.regionNames(ctx, province)
	if err != nil {
		return nil, err
	}
	from := domain.Date(w.To.Year(), 1, 1)
	if p := w.From.AddDate(0, 0, -1); p.Before(from) {
		from = p
	}
	ps, err := s.sources(ctx, domain.CNTSBM17, from, w.To, basis, provinceCommunes(nb))
	if err != nil {
		return nil, err
	}
	d := &bm18Data{week: w, cal: cal, nb: nb, basis: basis, province: province, periods: ps}
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if d.aqua, e = store.AquaCurrent(ctx, tx, province, from, w.To); e != nil {
			return e
		}
		if d.evs, e = store.AggEvents(ctx, tx, province, domain.CNTSBM18, domain.AggWeek, w.Key); e != nil {
			return e
		}
		d.shares, e = store.Shares(ctx, tx, province, domain.CNTSBM18, domain.AggWeek, w.Key)
		return e
	})
	if err != nil {
		return nil, err
	}
	aq := make([]domain.AquaEntry, 0, len(d.aqua))
	for _, x := range d.aqua {
		aq = append(aq, domain.AquaEntry{WeekKey: x.WeekKey, WeekTo: x.To, Object: x.Object, Area: x.Area, Output: x.Output})
	}
	regions := map[string]string{}
	for c, r := range nb.byCommune {
		regions[c] = r.RegionCode
	}
	d.res = domain.BM18(domain.BM18Input{Week: w, Periods: ps, Aqua: aq, Regions: regions})
	return d, nil
}

func decPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func (s *Service) bm18View(ctx context.Context, d *bm18Data) BM18View {
	groups := append(slices.Clone(domain.CatchGroups), domain.Key{Key: domain.CatchTotal, Label: "Tổng khai thác"})
	v := BM18View{Province: d.province, Week: keySpan(d.week), Calendar: d.cal.Ref(), Basis: d.basis, Groups: groups,
		AquaGroups: domain.AquaGroups, Communes: []BM18CommuneView{}, Regions: []BM18RegionView{}, Catch: colsMap(d.res.Catch, groups),
		AquaArea: colsView(d.res.AquaArea, domain.AquaKey), AquaOutput: colsView(d.res.AquaOutput, domain.AquaKey),
		Total: colsView(d.res.Total, domain.AquaKey), Sources: SourcesView{Count: d.res.SourceCount, Closed: d.res.SourceClosed,
			AllClosed: d.res.AllClosed}, UnitsTotal: d.res.UnitsTotal, UnitsReported: d.res.UnitsReported,
		MissingUnits: nonNil(d.res.MissingUnits), Aggregate: aggState(d.week.Key, d.evs)}
	v.Week.Label = fmt.Sprintf("Tuần %d/%d (%s – %s)", d.week.WeekNo, d.week.WeekYear, d.week.From.Format("02/01"), d.week.To.Format("02/01/2006"))
	if d.res.Prev != nil {
		p := keySpan(*d.res.Prev)
		v.Prev = &p
	}
	if p, err := d.cal.Rule.PeriodOf(domain.PeriodWeek, d.week.From.AddDate(0, 0, -1), nil); err == nil {
		v.PrevKey = p.Key
	}
	if n, err := d.cal.Rule.PeriodOf(domain.PeriodWeek, d.week.To.AddDate(0, 0, 1), nil); err == nil {
		v.NextKey = n.Key
	}
	for _, w := range d.res.Weeks {
		v.Weeks = append(v.Weeks, keySpan(w))
	}
	sortOf := func(region string) int {
		if r, ok := d.nb.regions[region]; ok {
			return r.SortOrder
		}
		return 1 << 20
	}
	for _, c := range d.res.Communes {
		bw := map[string]map[string]string{}
		for wk, vals := range c.ByWeek {
			bw[wk] = decStrings(vals, groups)
		}
		v.Communes = append(v.Communes, BM18CommuneView{CommuneCode: c.Commune, CommuneName: d.nb.commune(ctx, s, c.Commune),
			RegionCode: c.Region, RegionName: d.nb.region(c.Region), Cells: colsMap(c.Cols, groups), Expected: c.Expected,
			Reported: c.Reported, NilReport: c.NilReport, MissingWeeks: nonNil(c.MissingWeeks), ByWeek: bw})
	}
	sort.SliceStable(v.Communes, func(i, j int) bool {
		a, b := v.Communes[i], v.Communes[j]
		if sa, sb := sortOf(a.RegionCode), sortOf(b.RegionCode); sa != sb {
			return sa < sb
		}
		return a.CommuneName < b.CommuneName
	})
	for _, r := range d.res.Regions {
		v.Regions = append(v.Regions, BM18RegionView{RegionCode: r.Region, RegionName: d.nb.region(r.Region), Sort: sortOf(r.Region),
			Cells: colsMap(r.Cols, groups)})
	}
	sort.SliceStable(v.Regions, func(i, j int) bool { return v.Regions[i].Sort < v.Regions[j].Sort })
	entered := map[string]store.AquaRow{}
	for _, x := range d.aqua {
		if x.WeekKey == d.week.Key {
			entered[x.Object] = x
		}
	}
	for _, r := range d.res.Aqua {
		o := r.Object
		av := BM18AquaView{Code: o.Code, Label: o.Label, Group: o.Group, AreaUnit: o.AreaUnit, OutputUnit: o.OutputUnit, InTotal: o.InTotal(),
			Area: colsView(r.Area, domain.AquaKey), Output: colsView(r.Output, domain.AquaKey)}
		if e, ok := entered[o.Code]; ok {
			av.EnteredArea, av.EnteredOutput, av.Note = decPtr(e.Area), decPtr(e.Output), e.Note
		}
		v.Aqua = append(v.Aqua, av)
	}
	v.Shares = shareViews(ctx, s, d.shares)
	return v
}

// BM18 trả Phụ lục 2 của tuần CNTS "YYYY-Www" (rỗng = tuần hiện tại).
func (s *Service) BM18(ctx context.Context, province, week, basis string) (BM18View, error) {
	a, prov, err := s.aggReader(ctx, province)
	if err != nil {
		return BM18View{}, err
	}
	d, err := s.loadBM18(ctx, a, prov, week, basis)
	if err != nil {
		return BM18View{}, err
	}
	v := s.bm18View(ctx, d)
	if v.Aggregate.State == "closed" {
		v.Aggregate.Changed = s.bm18Changed(ctx, a, prov, d.week.Key, v.Aggregate.FiguresHash)
	}
	return v, nil
}

// bm18Changed: số đã chốt (theo ảnh chụp nguồn hiện tại, tính lại) khác số công bố — "Nguồn đã thay đổi".
func (s *Service) bm18Changed(ctx context.Context, a actor, prov, key string, hash *string) bool {
	if hash == nil {
		return false
	}
	d, err := s.loadBM18(ctx, a, prov, key, domain.BasisClosed)
	if err != nil {
		return false
	}
	p := domain.PubBM18(d.res)
	h, err := domain.FiguresHash(p.Rows, p.Totals)
	return err == nil && h != *hash
}

// AquaInput là số nuôi trồng của một đối tượng trong tuần (rỗng = không đổi ô đó... nil = để trống).
type AquaInput struct {
	Object string
	Area   *string
	Output *string
	Note   string
}

// SaveAqua ghi số nuôi trồng của tuần (mỗi đối tượng một dòng mới, thay dòng trước). Tuần BM-18 đã chốt → 409.
func (s *Service) SaveAqua(ctx context.Context, week string, entries []AquaInput, idem string) (BM18View, error) {
	a, prov, err := s.aggWriter(ctx)
	if err != nil {
		return BM18View{}, err
	}
	if !reWeekKey.MatchString(week) {
		return BM18View{}, problem.Validation.WithField("week", "dạng YYYY-Www")
	}
	w, _, err := s.cntsWeek(ctx, week)
	if err != nil {
		return BM18View{}, err
	}
	if len(entries) == 0 || len(entries) > len(domain.AquaObjects) {
		return BM18View{}, problem.Validation.WithField("entries", fmt.Sprintf("1–%d đối tượng", len(domain.AquaObjects)))
	}
	parse := func(field string, p *string) (*decimal.Decimal, error) {
		if p == nil || strings.TrimSpace(*p) == "" {
			return nil, nil
		}
		d, ok := domain.ParseDec(*p)
		if !ok || d.IsNegative() || d.Exponent() < -4 {
			return nil, problem.Validation.WithField(field, "số không âm, tối đa 4 chữ số thập phân")
		}
		return &d, nil
	}
	rows := make([]store.AquaRow, 0, len(entries))
	seen := map[string]bool{}
	for i, e := range entries {
		f := "entries[" + strconv.Itoa(i) + "]"
		if _, ok := domain.AquaObjectByCode(e.Object); !ok || seen[e.Object] {
			return BM18View{}, problem.Validation.WithField(f+".object", "đối tượng không có hoặc trùng")
		}
		seen[e.Object] = true
		ar, err := parse(f+".area", e.Area)
		if err != nil {
			return BM18View{}, err
		}
		out, err := parse(f+".output", e.Output)
		if err != nil {
			return BM18View{}, err
		}
		if len([]rune(e.Note)) > 500 {
			return BM18View{}, problem.Validation.WithField(f+".note", "tối đa 500 ký tự")
		}
		rows = append(rows, store.AquaRow{ID: ids.New(), Province: prov, WeekKey: w.Key, From: w.From, To: w.To, Object: e.Object, Area: ar,
			Output: out, Note: strings.TrimSpace(e.Note), ActorRef: a.p.ActorRef})
	}
	base := aggIdem("aqua", idem)
	now := s.now()
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if done, e := store.AquaExists(ctx, tx, base+":0"); e != nil || done {
			return e
		}
		for i := range rows {
			if e := store.InsertAqua(ctx, tx, a.p.TenantID, &rows[i], now, base+":"+strconv.Itoa(i)); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return BM18View{}, aggClassify(err)
	}
	return s.BM18(ctx, prov, w.Key, domain.BasisSubmitted)
}

// ---------------------------------------------------------------------------
// Chốt / mở lại bảng tổng hợp
// ---------------------------------------------------------------------------

// aggSource là một kỳ nguồn đã dùng cho lần chốt (truy vết).
type aggSource struct {
	Form     string `json:"form"`
	PeriodID string `json:"period_id"`
	Key      string `json:"key"`
	ClosedNo int64  `json:"closed_no"`
}

func sourcesJSON(form string, ps []domain.SourcePeriod, keep func(domain.SourcePeriod) bool) (json.RawMessage, error) {
	out := []aggSource{}
	for _, p := range ps {
		if keep(p) {
			out = append(out, aggSource{Form: form, PeriodID: p.ID.String(), Key: p.Key, ClosedNo: p.ClosedNo})
		}
	}
	return json.Marshal(out)
}

// CloseAggregate chốt bảng tổng hợp của một tuần (BM-02: tuần KSGM "YYYY-MM-Kn"; BM-18: tuần CNTS "YYYY-Www"): mọi kỳ nguồn
// của tuần phải đã chốt (D17); số công bố tính từ ảnh chụp nguồn (lũy kế của nguồn — D18), đóng băng ở sổ.
func (s *Service) CloseAggregate(ctx context.Context, form, key, idem string) (AggStateView, error) {
	a, prov, err := s.aggWriter(ctx)
	if err != nil {
		return AggStateView{}, err
	}
	ev := store.AggEvent{ID: ids.New(), Province: prov, Form: form, DefVersion: domain.AggDefinitionID(form), Action: "closed",
		ActorRef: a.p.ActorRef, At: s.now()}
	var pub domain.Pub
	switch form {
	case domain.CNTSBM02:
		if !reKSGMKey.MatchString(key) {
			return AggStateView{}, problem.Validation.WithField("period_key", "tuần KSGM YYYY-MM-Kn")
		}
		d, err := s.loadBM02(ctx, a, prov, domain.AggKSGMWeek, key, domain.BasisClosed)
		if err != nil {
			return AggStateView{}, err
		}
		if !d.res.AllClosed {
			return AggStateView{}, ErrAggSourcesOpen.WithDetail(fmt.Sprintf("BM-01: %d/%d kỳ ngày đã chốt", d.res.SourceClosed, d.res.SourceCount))
		}
		w := d.weeks[0]
		pub = domain.PubBM02(d.res)
		ev.PeriodType, ev.PeriodKey, ev.From, ev.To, ev.Label = domain.AggKSGMWeek, w.Key, w.From, w.To, ksgmLabel(w)
		ev.Rule = s.cntsCalendar(ctx, w.From).Rule.Tag()
		days, err := s.sources(ctx, domain.CNTSBM01, w.From, w.To, domain.BasisClosed, provinceCommunes(d.nb))
		if err != nil {
			return AggStateView{}, err
		}
		if ev.Sources, err = sourcesJSON(domain.CNTSBM01, days, func(domain.SourcePeriod) bool { return true }); err != nil {
			return AggStateView{}, err
		}
	case domain.CNTSBM18:
		if !reWeekKey.MatchString(key) {
			return AggStateView{}, problem.Validation.WithField("period_key", "tuần CNTS YYYY-Www")
		}
		d, err := s.loadBM18(ctx, a, prov, key, domain.BasisClosed)
		if err != nil {
			return AggStateView{}, err
		}
		if !d.res.AllClosed {
			return AggStateView{}, ErrAggSourcesOpen.WithDetail(fmt.Sprintf("BM-17: %d/%d kỳ của tuần đã chốt", d.res.SourceClosed, d.res.SourceCount))
		}
		pub = domain.PubBM18(d.res)
		w := d.week
		ev.PeriodType, ev.PeriodKey, ev.From, ev.To = domain.AggWeek, w.Key, w.From, w.To
		ev.Label = fmt.Sprintf("Tuần %d/%d (%s – %s)", w.WeekNo, w.WeekYear, w.From.Format("02/01"), w.To.Format("02/01/2006"))
		ev.Rule = d.cal.Rule.Tag()
		if ev.Sources, err = sourcesJSON(domain.CNTSBM17, d.periods, func(p domain.SourcePeriod) bool { return p.To.Before(w.To.AddDate(0, 0, 1)) }); err != nil {
			return AggStateView{}, err
		}
	default:
		return AggStateView{}, problem.Validation.WithField("form", "CNTS-BM02 | CNTS-BM18")
	}
	ev.AggPeriodID = domain.AggPeriodID(a.p.TenantID, prov, form, ev.PeriodType, ev.PeriodKey)
	ev.Pub = &pub
	if ev.FiguresHash, err = domain.FiguresHash(pub.Rows, pub.Totals); err != nil {
		return AggStateView{}, err
	}
	return s.writeAgg(ctx, a, &ev, aggIdem("close", idem))
}

// ReopenAggregate mở lại bảng tổng hợp đã chốt (lý do 10–500 ký tự) — số đã công bố giữ tới lần chốt lại.
func (s *Service) ReopenAggregate(ctx context.Context, form, key, reason, idem string) (AggStateView, error) {
	a, prov, err := s.aggWriter(ctx)
	if err != nil {
		return AggStateView{}, err
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return AggStateView{}, problem.Validation.WithField("reason", "lý do 10–500 ký tự")
	}
	pt := domain.AggWeek
	switch {
	case form == domain.CNTSBM02 && reKSGMKey.MatchString(key):
		pt = domain.AggKSGMWeek
	case form == domain.CNTSBM18 && reWeekKey.MatchString(key):
	default:
		return AggStateView{}, problem.Validation.WithField("period_key", "CNTS-BM02 + YYYY-MM-Kn hoặc CNTS-BM18 + YYYY-Www")
	}
	var last *store.AggEvent
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		evs, e := store.AggEvents(ctx, tx, prov, form, pt, key)
		if len(evs) > 0 {
			last = &evs[len(evs)-1]
		}
		return e
	})
	if err != nil {
		return AggStateView{}, err
	}
	if last == nil {
		return AggStateView{}, ErrAggState.WithDetail("bảng tổng hợp chưa chốt lần nào")
	}
	ev := *last
	ev.ID, ev.Action, ev.Reason, ev.Pub, ev.FiguresHash, ev.Sources, ev.ActorRef, ev.At = ids.New(), "reopened", reason, nil, "", nil,
		a.p.ActorRef, s.now()
	return s.writeAgg(ctx, a, &ev, aggIdem("reopen", idem))
}

func (s *Service) writeAgg(ctx context.Context, a actor, ev *store.AggEvent, idem string) (AggStateView, error) {
	var evs []store.AggEvent
	err := s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		prev, e := store.AggByIdem(ctx, tx, idem)
		if e != nil {
			return e
		}
		if prev == nil {
			if e := store.InsertAgg(ctx, tx, a.p.TenantID, ev, idem); e != nil {
				return e
			}
		} else {
			ev = prev
		}
		evs, e = store.AggEvents(ctx, tx, ev.Province, ev.Form, ev.PeriodType, ev.PeriodKey)
		return e
	})
	if err != nil {
		return AggStateView{}, aggClassify(err)
	}
	return aggState(ev.PeriodKey, evs), nil
}

// ---------------------------------------------------------------------------
// Phiên bản tệp chia sẻ Chi cục
// ---------------------------------------------------------------------------

// ShareContent là nội dung đóng băng của một phiên bản tệp chia sẻ (Excel dựng lại từ đây).
type ShareContent struct {
	Schema       string    `json:"schema"`
	Form         string    `json:"form"`
	PeriodType   string    `json:"period_type"`
	PeriodKey    string    `json:"period_key"`
	ProvinceName string    `json:"province_name"`
	BM02         *BM02View `json:"bm02,omitempty"`
	BM18         *BM18View `json:"bm18,omitempty"`
}

// ShareResult là kết quả tạo phiên bản (Replayed = số không đổi so với phiên bản cuối — không tạo phiên bản mới).
type ShareResult struct {
	Share    ShareView
	Replayed bool
}

func contentHash(c ShareContent) (json.RawMessage, string, error) {
	// Băm trên số liệu, bỏ phần thay đổi theo thời điểm đọc (danh sách phiên bản, trạng thái chốt, dòng điều chỉnh đã có
	// trong tổng).
	cp := c
	if c.BM02 != nil {
		b := *c.BM02
		b.Shares, b.Aggregates = nil, nil
		cp.BM02 = &b
	}
	if c.BM18 != nil {
		b := *c.BM18
		b.Shares, b.Aggregate = nil, AggStateView{}
		cp.BM18 = &b
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return nil, "", err
	}
	h := sha256.Sum256(raw)
	return raw, hex.EncodeToString(h[:]), nil
}

func (s *Service) provinceName(ctx context.Context, code string) string {
	if s.Units != nil {
		if u, err := s.Units.Get(ctx, code, s.now()); err == nil && u.Name != "" {
			return u.Name
		}
	}
	return code
}

// CreateShare tạo phiên bản tệp chia sẻ Chi cục của một bảng (BM-02 tuần KSGM/tháng, BM-18 tuần). Cơ sở số liệu: số đã
// chốt nếu bảng (mọi tuần KSGM của tháng) đã chốt, ngược lại số đã gửi. Số không đổi so với phiên bản cuối → trả phiên
// bản cuối (không tạo mới); có thay đổi → phiên bản mới ghi chú "Thay đổi so với bản đã gửi ngày …".
func (s *Service) CreateShare(ctx context.Context, form, periodType, key, idem string) (ShareResult, error) {
	a, prov, err := s.aggWriter(ctx)
	if err != nil {
		return ShareResult{}, err
	}
	c := ShareContent{Schema: ShareSchema, Form: form, ProvinceName: s.provinceName(ctx, prov)}
	basis := domain.BasisSubmitted
	switch form {
	case domain.CNTSBM02:
		d, err := s.loadBM02(ctx, a, prov, periodType, key, domain.BasisSubmitted)
		if err != nil {
			return ShareResult{}, err
		}
		all := true
		for _, w := range d.weeks {
			all = all && aggState(w.Key, d.evs[w.Key]).State == "closed"
		}
		if all {
			basis = domain.BasisClosed
			if d, err = s.loadBM02(ctx, a, prov, periodType, key, basis); err != nil {
				return ShareResult{}, err
			}
		}
		v := s.bm02View(ctx, d)
		c.PeriodType, c.PeriodKey, c.BM02 = d.pt, d.key, &v
	case domain.CNTSBM18:
		d, err := s.loadBM18(ctx, a, prov, key, domain.BasisSubmitted)
		if err != nil {
			return ShareResult{}, err
		}
		if aggState(d.week.Key, d.evs).State == "closed" {
			basis = domain.BasisClosed
			if d, err = s.loadBM18(ctx, a, prov, key, basis); err != nil {
				return ShareResult{}, err
			}
		}
		v := s.bm18View(ctx, d)
		c.PeriodType, c.PeriodKey, c.BM18 = domain.AggWeek, d.week.Key, &v
	default:
		return ShareResult{}, problem.Validation.WithField("form", "CNTS-BM02 | CNTS-BM18")
	}
	raw, hash, err := contentHash(c)
	if err != nil {
		return ShareResult{}, err
	}
	row := store.Share{ID: ids.New(), Province: prov, Form: form, PeriodType: c.PeriodType, PeriodKey: c.PeriodKey, Basis: basis,
		Content: raw, ContentHash: hash, Recipient: ShareRecipient, Channel: "download", ActorRef: a.p.ActorRef, At: s.now()}
	idemKey := aggIdem("share", idem)
	replayed := false
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if prev, e := store.ShareByIdem(ctx, tx, idemKey); e != nil || prev != nil {
			if prev != nil {
				row, replayed = *prev, true
			}
			return e
		}
		prevs, e := store.Shares(ctx, tx, prov, form, c.PeriodType, c.PeriodKey)
		if e != nil {
			return e
		}
		if n := len(prevs); n > 0 {
			last := prevs[n-1]
			if last.ContentHash == hash {
				row, replayed = last, true
				return nil
			}
			row.Note = fmt.Sprintf("Thay đổi so với bản đã gửi ngày %s (phiên bản %d)", last.At.In(clock.Zone()).Format("02/01/2006 15:04"), last.Version)
		}
		return store.InsertShare(ctx, tx, a.p.TenantID, &row, idemKey)
	})
	if err != nil {
		return ShareResult{}, aggClassify(err)
	}
	return ShareResult{Share: shareViews(ctx, s, []store.Share{row})[0], Replayed: replayed}, nil
}

// Shares liệt kê phiên bản tệp chia sẻ của một bảng.
func (s *Service) Shares(ctx context.Context, province, form, periodType, key string) ([]ShareView, error) {
	a, prov, err := s.aggReader(ctx, province)
	if err != nil {
		return nil, err
	}
	var rows []store.Share
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.Shares(ctx, tx, prov, form, periodType, key)
		return e
	})
	if err != nil {
		return nil, err
	}
	return shareViews(ctx, s, rows), nil
}

// AggExport là tệp xuất của tổng hợp CNTS.
type AggExport struct {
	Body        []byte
	ContentType string
	FileName    string
}

// ShareFile dựng tệp Excel của một phiên bản đã lưu (BM-02 hoặc BM-18 kèm Phụ lục 2 + 4 sheet khai thác theo xã, tuần).
func (s *Service) ShareFile(ctx context.Context, id uuid.UUID) (AggExport, error) {
	a, prov, err := s.aggScope(ctx, "")
	if err != nil {
		return AggExport{}, err
	}
	var sh *store.Share
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		sh, e = store.ShareByID(ctx, tx, id)
		return e
	})
	if err != nil {
		return AggExport{}, err
	}
	if sh == nil || (prov != "" && sh.Province != prov) {
		return AggExport{}, ErrShareNotFound
	}
	var c ShareContent
	if err := json.Unmarshal(sh.Content, &c); err != nil {
		return AggExport{}, err
	}
	body, err := shareXLSX(c, *sh)
	if err != nil {
		return AggExport{}, err
	}
	name := fmt.Sprintf("%s-%s-%s-v%d.xlsx", strings.ToLower(strings.ReplaceAll(c.Form, "CNTS-", "")), sh.Province, sh.PeriodKey, sh.Version)
	return AggExport{Body: body, ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", FileName: name}, nil
}

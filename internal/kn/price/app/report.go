package app

// Báo cáo giá từ bảng giá ĐÃ DUYỆT (W11.77, pf 20261013164235; BRD Báo giá nông sản v1.3 FN-TW-01.01 Bảng giá tổng hợp,
// FN-TW-01.02 Báo cáo tăng giảm / biến động, FN-TW-01.03 Dashboard theo vùng; Mục II.4 CT-04). Mọi số liệu lấy qua
// port.Boards (Published / PublishedBetween — phiên bản đã duyệt hiện hành của mỗi ngày, ảnh chụp lúc duyệt); không đọc
// phiếu chưa duyệt. Ngày đang mở duyệt vẫn hiện phiên bản cũ, nhãn "Đang cập nhật" (AC-03 FN-TW-01.01).
//
// Quyền xem (Mục V BRD — "Cấp TW chỉ xem"):
//   - Bảng giá tổng hợp, Biến động giá: Cấp TW, Cấp Tỉnh, Cấp Xã, Admin. Xuất Biến động (Excel / PDF): chỉ Cấp Tỉnh, Admin
//     (BR-04 FN-TW-01.02). Xuất Bảng giá tổng hợp: mọi vai xem được (B3 FN-TW-01.01 không giới hạn — giả định W11.77).
//   - Dashboard theo vùng: Cấp TW, Cấp Tỉnh, Admin (Cấp Xã không — Mục IV.1).

import (
	"bytes"
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// ---------------------------------------------------------------------------------------------------------------------
// port.Boards: dòng giá theo xã, khoảng ngày
// ---------------------------------------------------------------------------------------------------------------------

// boardPointRow đổi dòng giá tính vào bảng giá (số liệu hiện tại) sang dòng ảnh chụp theo xã.
func boardPointRow(l port.PriceLine) store.BoardPointRow {
	return store.BoardPointRow{Point: l.PointCode, Region: l.RegionCode, Item: l.ItemCode, Min: l.Min, Max: l.Max, Mid: l.Mid,
		Adjusted: l.Adjusted, Late: l.Late}
}

func pointLineOf(r store.BoardPointRow) port.BoardPointLine {
	return port.BoardPointLine{PointCode: r.Point, RegionCode: r.Region, ItemCode: r.Item, Min: r.Min, Max: r.Max, Mid: r.Mid,
		Adjusted: r.Adjusted, Late: r.Late}
}

func versionKey(day time.Time, version int) string {
	return day.Format(time.DateOnly) + ":v" + strconv.Itoa(version)
}

// reopenedView: ngày đang mở duyệt (hoặc đang duyệt lại sau mở duyệt) — phiên bản đã duyệt vẫn công bố.
func reopenedView(b domain.Board, now time.Time) bool {
	view := b.View(now)
	return view == domain.BoardReopened || (view == domain.BoardApproving && b.BaseStatus == domain.BoardReopened)
}

// rebuildPoints dựng giá theo xã của phiên bản duyệt trước pf 20261013164235 (chưa có ảnh chụp board_point) từ phiếu:
// chỉ khi ngày KHÔNG mở duyệt — bảng giá Đã duyệt chặn mọi sửa phiếu nên số liệu hiện tại = số liệu lúc duyệt.
func (s *TicketService) rebuildPoints(ctx context.Context, tx pgx.Tx, prof port.Profile, day time.Time, b domain.Board,
	now time.Time) ([]port.BoardPointLine, error) {
	if reopenedView(b, now) {
		return nil, nil
	}
	sum, _, err := s.daySummary(ctx, tx, prof, day, b.View(now))
	if err != nil {
		return nil, err
	}
	out := make([]port.BoardPointLine, 0, len(sum.Lines))
	for _, l := range sum.Lines {
		out = append(out, pointLineOf(boardPointRow(l)))
	}
	return out, nil
}

// publishedRange đọc phiên bản đã duyệt hiện hành của mọi ngày đã duyệt trong [from, to] (dòng toàn tỉnh + theo xã).
func (s *TicketService) publishedRange(ctx context.Context, tx pgx.Tx, prof port.Profile, from, to, now time.Time) ([]port.PublishedBoard,
	error) {
	bs, err := store.BoardsBetween(ctx, tx, prof.Name, from, to)
	if err != nil {
		return nil, err
	}
	type cur struct {
		day time.Time
		b   domain.Board
	}
	var days []cur
	for d, b := range bs {
		if b.Published() {
			days = append(days, cur{domain.BusinessDay(d), b})
		}
	}
	if len(days) == 0 {
		return nil, nil
	}
	sort.Slice(days, func(i, j int) bool { return days[i].day.Before(days[j].day) })
	lines, err := store.BoardLinesBetween(ctx, tx, prof.Name, from, to)
	if err != nil {
		return nil, err
	}
	points, err := store.BoardPointsBetween(ctx, tx, prof.Name, from, to)
	if err != nil {
		return nil, err
	}
	weeks, err := store.ApprovedWeeks(ctx, tx, prof.Name, from, to)
	if err != nil {
		return nil, err
	}
	lineBy := map[string][]port.BoardLine{}
	for _, r := range lines {
		k := versionKey(r.Day, r.Version)
		lineBy[k] = append(lineBy[k], port.BoardLine{ItemCode: r.Item, ItemName: r.ItemName, GroupCode: r.GroupCode, UnitCode: r.UnitCode,
			Min: r.Min, Max: r.Max, Avg: r.Avg, Points: r.Points})
	}
	pointBy := map[string][]port.BoardPointLine{}
	for _, r := range points {
		k := versionKey(r.Day, r.Version)
		pointBy[k] = append(pointBy[k], pointLineOf(r))
	}
	out := make([]port.PublishedBoard, 0, len(days))
	for _, d := range days {
		k := versionKey(d.day, d.b.Version)
		pb := port.PublishedBoard{Day: d.day, Version: d.b.Version, Reopened: reopenedView(d.b, now), WeekKey: weeks[k], Lines: lineBy[k],
			PointLines: pointBy[k]}
		if d.b.ApprovedAt != nil {
			pb.ApprovedAt = *d.b.ApprovedAt
		}
		if len(pb.PointLines) == 0 && len(pb.Lines) > 0 {
			if pb.PointLines, err = s.rebuildPoints(ctx, tx, prof, d.day, d.b, now); err != nil {
				return nil, err
			}
		}
		out = append(out, pb)
	}
	return out, nil
}

// PublishedBetween (port.Boards) trả phiên bản đã duyệt hiện hành của mọi ngày đã duyệt trong [from, to] (không kiểm vai).
func (s *TicketService) PublishedBetween(ctx context.Context, profile string, from, to time.Time) ([]port.PublishedBoard, error) {
	prof, ok := s.calendarProfile(profile)
	if !ok {
		return nil, port.ErrProfileNotFound
	}
	var out []port.PublishedBoard
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = s.publishedRange(ctx, tx, prof, domain.BusinessDay(from), domain.BusinessDay(to), s.Clock.Now())
		return err
	})
	return out, err
}

// publishedOne đọc phiên bản đã duyệt hiện hành của đúng ngày day (ok = false nếu chưa duyệt).
func (s *TicketService) publishedOne(ctx context.Context, tx pgx.Tx, prof port.Profile, day, now time.Time) (port.PublishedBoard, bool,
	error) {
	bs, err := s.publishedRange(ctx, tx, prof, day, day, now)
	if err != nil || len(bs) == 0 {
		return port.PublishedBoard{}, false, err
	}
	return bs[0], true, nil
}

// ---------------------------------------------------------------------------------------------------------------------
// Tổng hợp theo phạm vi (CT-04)
// ---------------------------------------------------------------------------------------------------------------------

// scope là phạm vi tổng hợp: nhóm vùng, xã (rỗng = toàn tỉnh), nhóm hàng.
type scope struct {
	Region, Point, Group string
}

// whole cho biết phạm vi là toàn tỉnh (dùng dòng bảng giá đã chụp — AC-02 FN-TW-01.01 trùng bảng giá đã duyệt).
func (sc scope) whole() bool { return sc.Region == "" && sc.Point == "" }

func (sc scope) point(l port.BoardPointLine) bool {
	return (sc.Region == "" || l.RegionCode == sc.Region) && (sc.Point == "" || l.PointCode == sc.Point)
}

// aggregate tổng hợp bảng giá pb trong phạm vi sc theo CT-04: map mặt hàng → dòng (min, max, trung bình P, số xã).
// Không lọc nhóm hàng (bên gọi lọc theo thông tin mặt hàng).
func aggregate(pb port.PublishedBoard, sc scope) map[string]domain.BoardLine {
	out := map[string]domain.BoardLine{}
	if sc.whole() {
		for _, l := range pb.Lines {
			out[l.ItemCode] = domain.BoardLine{Item: l.ItemCode, Min: l.Min, Max: l.Max, Avg: l.Avg, Points: l.Points}
		}
		return out
	}
	var src []domain.BoardSource
	for _, l := range pb.PointLines {
		if sc.point(l) {
			src = append(src, domain.BoardSource{Point: l.PointCode, Item: l.ItemCode, Min: l.Min, Max: l.Max, Mid: l.Mid})
		}
	}
	for _, a := range domain.AggregateBoard(src) {
		out[a.Item] = a
	}
	return out
}

// reportCtx là danh mục dùng để hiển thị báo cáo tại một ngày: tên nhóm hàng, ĐVT, nhóm vùng, xã, loại mặt hàng.
type reportCtx struct {
	cat     *dayCat
	regions map[string]domain.Ver[domain.Entry]
	points  map[string]domain.PointState
	flat    decimal.Decimal
}

func (s *TicketService) loadReportCtx(ctx context.Context, tx pgx.Tx, prof port.Profile, day time.Time) (*reportCtx, error) {
	cat, err := loadDayCat(ctx, tx, day)
	if err != nil {
		return nil, err
	}
	ps, err := loadPointSnap(ctx, tx)
	if err != nil {
		return nil, err
	}
	set, _, _, err := s.thresholdSet(ctx, tx, prof)
	if err != nil {
		return nil, err
	}
	rc := &reportCtx{cat: cat, regions: domain.EffectiveAt(ps.regions, day), points: ps.states(day), flat: decimal.NewFromInt(1)}
	if set.Default.Params != nil && set.Default.Params.FlatPct.IsPositive() {
		rc.flat = set.Default.Params.FlatPct
	}
	return rc, nil
}

func (rc *reportCtx) regionName(code string) string {
	if r, ok := rc.regions[code]; ok {
		return r.V.Name
	}
	return code
}

func (rc *reportCtx) pointName(code string) string {
	if p, ok := rc.points[code]; ok {
		return p.Name
	}
	return code
}

// Ref là mã + tên cho bộ lọc.
type Ref struct {
	Code string
	Name string
}

// regionRefs trả nhóm vùng đang hiệu lực, theo thứ tự.
func (rc *reportCtx) regionRefs() []Ref {
	type r struct {
		ref   Ref
		order int
	}
	var rs []r
	for code, v := range rc.regions {
		rs = append(rs, r{Ref{code, v.V.Name}, v.V.SortOrder})
	}
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].order != rs[j].order {
			return rs[i].order < rs[j].order
		}
		return rs[i].ref.Code < rs[j].ref.Code
	})
	out := make([]Ref, len(rs))
	for i, x := range rs {
		out[i] = x.ref
	}
	return out
}

// groupRefs trả nhóm hàng đang hiệu lực, theo thứ tự.
func (rc *reportCtx) groupRefs() []Ref {
	out := make([]Ref, 0, len(rc.cat.groups))
	for code, name := range rc.cat.groups {
		out = append(out, Ref{code, name})
	}
	sort.Slice(out, func(i, j int) bool {
		oi, oj := rank(rc.cat.order, out[i].Code), rank(rc.cat.order, out[j].Code)
		if oi != oj {
			return oi < oj
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// ItemInfo là thông tin hiển thị của một mặt hàng trong báo cáo (theo dòng bảng giá đã chụp).
type ItemInfo struct {
	ItemCode  string
	ItemName  string
	GroupCode string
	GroupName string
	UnitCode  string
	UnitName  string
	Basic     bool // mặt hàng Cơ bản (BR-03 FN-TW-01.03 — hiển thị mặc định ở dashboard)
}

func (rc *reportCtx) info(l port.BoardLine) ItemInfo {
	it := rc.cat.states[l.ItemCode]
	return ItemInfo{ItemCode: l.ItemCode, ItemName: l.ItemName, GroupCode: l.GroupCode, GroupName: rc.cat.groupName(l.GroupCode),
		UnitCode: l.UnitCode, UnitName: rc.cat.unitName(l.UnitCode), Basic: it.Kind == domain.ItemBasic}
}

// sortInfos sắp theo thứ tự nhóm hàng rồi mã mặt hàng.
func (rc *reportCtx) sortInfos(n int, at func(int) ItemInfo, swap func(i, j int)) {
	sort.Sort(infoSorter{n: n, less: func(i, j int) bool {
		a, b := at(i), at(j)
		if oa, ob := rank(rc.cat.order, a.GroupCode), rank(rc.cat.order, b.GroupCode); oa != ob {
			return oa < ob
		}
		return a.ItemCode < b.ItemCode
	}, swap: swap})
}

type infoSorter struct {
	n    int
	less func(i, j int) bool
	swap func(i, j int)
}

func (x infoSorter) Len() int           { return x.n }
func (x infoSorter) Less(i, j int) bool { return x.less(i, j) }
func (x infoSorter) Swap(i, j int)      { x.swap(i, j) }

// Move là biến động của một giá so với kỳ trước (CT-04).
type Move struct {
	Prev      *decimal.Decimal
	ChangePct *decimal.Decimal
	Trend     string // up | down | flat | "" (kỳ trước không có giá)
}

func moveOf(prev *decimal.Decimal, cur, flat decimal.Decimal) Move {
	if prev == nil {
		return Move{}
	}
	t, pct, ok := domain.Trend(*prev, cur, flat)
	if !ok {
		return Move{Prev: prev}
	}
	return Move{Prev: prev, ChangePct: &pct, Trend: t}
}

// reportViewer kiểm vai xem báo cáo: levels là các cấp được xem (vai quản trị luôn xem được).
func reportViewer(c caller, levels ...string) error {
	if c.manage {
		return nil
	}
	for _, l := range levels {
		if c.level == l {
			return nil
		}
	}
	return problem.Forbidden.WithDetail("vai của bạn không xem báo cáo này")
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TW-01.01 — Bảng giá tổng hợp chung
// ---------------------------------------------------------------------------------------------------------------------

// SummaryFilter là bộ lọc Bảng giá tổng hợp (B1).
type SummaryFilter struct {
	Day     time.Time // ngày báo giá (zero = hôm nay → ngày công bố gần nhất)
	Region  string    // nhóm vùng (rỗng = Toàn tỉnh)
	Group   string    // nhóm hàng
	ByPoint bool      // cách xem theo Xã
	Item    string    // luồng phụ: giá từng xã của một mặt hàng
}

// SummaryLine là một mặt hàng của Bảng giá tổng hợp.
type SummaryLine struct {
	ItemInfo
	Min, Max, Avg decimal.Decimal
	Points        int
	Move
}

// PointPrice là giá một mặt hàng của một xã (cách xem theo Xã, chi tiết mặt hàng, xã trong vùng).
type PointPrice struct {
	ItemInfo
	PointCode  string
	PointName  string
	RegionCode string
	RegionName string
	Min, Max   decimal.Decimal
	Mid        decimal.Decimal
	Late       bool
}

// SummaryView là Bảng giá tổng hợp của một ngày.
type SummaryView struct {
	Requested  time.Time
	Day        time.Time // ngày của bảng đang hiển thị (zero = Chưa có dữ liệu)
	Version    int
	ApprovedAt time.Time
	NotToday   bool // ngày chọn chưa có bảng giá công bố → bảng gần nhất ("Chưa công bố hôm nay")
	Updating   bool // ngày đang mở duyệt → phiên bản cũ ("Đang cập nhật")
	PrevDay    *time.Time
	FlatPct    decimal.Decimal
	Region     string
	Lines      []SummaryLine
	Points     []PointPrice
	Regions    []Ref
	Groups     []Ref
	ReadOnly   bool // vai không có quyền sửa bảng giá ("Chỉ xem")
}

// boardBefore đọc bảng giá đã duyệt gần nhất ≤ day (inclusive) hoặc < day.
func (s *TicketService) boardBefore(ctx context.Context, tx pgx.Tx, prof port.Profile, day, now time.Time, inclusive bool) (port.PublishedBoard,
	bool, error) {
	d, ok, err := store.LatestPublished(ctx, tx, prof.Name, day, inclusive)
	if err != nil || !ok {
		return port.PublishedBoard{}, false, err
	}
	return s.publishedOne(ctx, tx, prof, domain.BusinessDay(d), now)
}

// pointPrices dựng giá theo xã của pb trong phạm vi sc (lọc nhóm hàng, mặt hàng), theo nhóm vùng, xã, mặt hàng.
func (rc *reportCtx) pointPrices(pb port.PublishedBoard, sc scope, item string) []PointPrice {
	infos := map[string]ItemInfo{}
	for _, l := range pb.Lines {
		infos[l.ItemCode] = rc.info(l)
	}
	var out []PointPrice
	for _, l := range pb.PointLines {
		inf, ok := infos[l.ItemCode]
		if !ok || !sc.point(l) || (sc.Group != "" && inf.GroupCode != sc.Group) || (item != "" && l.ItemCode != item) {
			continue
		}
		out = append(out, PointPrice{ItemInfo: inf, PointCode: l.PointCode, PointName: rc.pointName(l.PointCode), RegionCode: l.RegionCode,
			RegionName: rc.regionName(l.RegionCode), Min: l.Min, Max: l.Max, Mid: l.Mid, Late: l.Late})
	}
	regionOrder := map[string]int{}
	for i, r := range rc.regionRefs() {
		regionOrder[r.Code] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := rank(regionOrder, a.RegionCode), rank(regionOrder, b.RegionCode); ra != rb {
			return ra < rb
		}
		if a.PointCode != b.PointCode {
			return a.PointCode < b.PointCode
		}
		if oa, ob := rank(rc.cat.order, a.GroupCode), rank(rc.cat.order, b.GroupCode); oa != ob {
			return oa < ob
		}
		return a.ItemCode < b.ItemCode
	})
	return out
}

// summaryLines tổng hợp pb trong phạm vi sc và so với prev (nil = không có kỳ trước).
func (rc *reportCtx) summaryLines(pb port.PublishedBoard, prev *port.PublishedBoard, sc scope) []SummaryLine {
	cur := aggregate(pb, sc)
	var before map[string]domain.BoardLine
	if prev != nil {
		before = aggregate(*prev, sc)
	}
	var out []SummaryLine
	for _, l := range pb.Lines {
		a, ok := cur[l.ItemCode]
		inf := rc.info(l)
		if !ok || (sc.Group != "" && inf.GroupCode != sc.Group) {
			continue
		}
		var p *decimal.Decimal
		if b, ok := before[l.ItemCode]; ok {
			v := b.Avg
			p = &v
		}
		out = append(out, SummaryLine{ItemInfo: inf, Min: a.Min, Max: a.Max, Avg: a.Avg, Points: a.Points, Move: moveOf(p, a.Avg, rc.flat)})
	}
	rc.sortInfos(len(out), func(i int) ItemInfo { return out[i].ItemInfo }, func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// Summary đọc Bảng giá tổng hợp (FN-TW-01.01): phiên bản đã duyệt mới nhất của ngày chọn (BR-01), CT-04 theo nhóm vùng
// (BR-02), so với bảng giá công bố gần nhất trước đó; ngày chưa công bố → bảng gần nhất trước ngày chọn; chưa có bảng
// nào → Day zero ("Chưa có dữ liệu").
func (s *TicketService) Summary(ctx context.Context, profile string, f SummaryFilter) (SummaryView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return SummaryView{}, err
	}
	if err := reportViewer(c, "TW", "TINH", "XA"); err != nil {
		return SummaryView{}, err
	}
	now := s.Clock.Now()
	v := SummaryView{Requested: s.dayOrToday(f.Day), Region: f.Region, ReadOnly: !reviewer(c)}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		pb, ok, err := s.boardBefore(ctx, tx, prof, v.Requested, now, true)
		if err != nil {
			return err
		}
		rcDay := v.Requested
		if ok {
			rcDay = pb.Day
		}
		rc, err := s.loadReportCtx(ctx, tx, prof, rcDay)
		if err != nil {
			return err
		}
		v.Regions, v.Groups, v.FlatPct = rc.regionRefs(), rc.groupRefs(), rc.flat
		if !ok {
			return nil
		}
		v.Day, v.Version, v.ApprovedAt, v.Updating, v.NotToday = pb.Day, pb.Version, pb.ApprovedAt, pb.Reopened, !pb.Day.Equal(v.Requested)
		prev, okPrev, err := s.boardBefore(ctx, tx, prof, pb.Day, now, false)
		if err != nil {
			return err
		}
		var pp *port.PublishedBoard
		if okPrev {
			pp, v.PrevDay = &prev, &prev.Day
		}
		sc := scope{Region: f.Region, Group: f.Group}
		v.Lines = rc.summaryLines(pb, pp, sc)
		if f.ByPoint || f.Item != "" {
			v.Points = rc.pointPrices(pb, sc, f.Item)
		}
		return nil
	})
	return v, err
}

var summaryHeader = []string{"STT", "Nhóm hàng", "Mặt hàng", "ĐVT", "Giá thấp nhất", "Giá cao nhất", "Giá trung bình", "Kỳ trước",
	"Tăng/giảm (%)", "Xu hướng", "Số xã có giá"}

var trendVI = map[string]string{domain.TrendUp: "Tăng", domain.TrendDown: "Giảm", domain.TrendFlat: "Đi ngang"}

func decStr(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.String()
}

func pctStr(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return signedPct(*d)
}

// exportSummaryFile dựng tệp Excel Bảng giá tổng hợp theo bộ lọc.
func (s *TicketService) exportSummaryFile(ctx context.Context, profile string, f SummaryFilter) (Export, error) {
	v, err := s.Summary(ctx, profile, f)
	if err != nil {
		return Export{}, err
	}
	if v.Day.IsZero() {
		return Export{}, port.ErrReportEmpty
	}
	var rows [][]string
	if f.ByPoint {
		header := []string{"STT", "Nhóm vùng", "Xã báo giá", "Nhóm hàng", "Mặt hàng", "ĐVT", "Giá thấp nhất", "Giá cao nhất", "Giá P", "Nộp muộn"}
		for i, p := range v.Points {
			late := ""
			if p.Late {
				late = "Nộp muộn"
			}
			rows = append(rows, []string{strconv.Itoa(i + 1), p.RegionName, p.PointName, p.GroupName, p.ItemName, p.UnitName, p.Min.String(),
				p.Max.String(), p.Mid.String(), late})
		}
		return xlsxExport("Bảng giá theo xã", header, rows, "bang-gia-theo-xa-"+v.Day.Format(time.DateOnly)+".xlsx")
	}
	for i, l := range v.Lines {
		rows = append(rows, []string{strconv.Itoa(i + 1), l.GroupName, l.ItemName, l.UnitName, l.Min.String(), l.Max.String(), l.Avg.String(),
			decStr(l.Prev), pctStr(l.ChangePct), trendVI[l.Trend], strconv.Itoa(l.Points)})
	}
	return xlsxExport("Bảng giá tổng hợp", summaryHeader, rows, "bang-gia-tong-hop-"+v.Day.Format(time.DateOnly)+".xlsx")
}

// Sản phẩm của lần xuất báo cáo giá trong sổ egress_event (pf/metering).
const (
	ProductSummaryExport   = "price_summary_board"
	ProductMovementExport  = "price_movement"
	ProductDashboardExport = "price_region_dashboard"
	ProductProgressExport  = "price_progress"
	ProductStatsExport     = "price_ticket_stats"
)

// observe đưa lần xuất báo cáo qua điểm kiểm soát dùng chung (ADR-0024, A5): kênh report_export, bên nhận = chính cán bộ
// xuất, Internal → luôn observe (ghi, đếm; không bao giờ từ chối). Chưa nối checkpoint → không ghi.
func (s *TicketService) observe(ctx context.Context, product string, x Export, err error) (Export, error) {
	if err != nil || s.Egress == nil {
		return x, err
	}
	p, ok := authz.FromContext(ctx)
	if !ok {
		return x, nil
	}
	_ = meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: p.TenantID,
		CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: p.ActorRef, Product: product,
		Records: int64(x.Rows), Bytes: int64(len(x.Body)), OccurredAt: s.Clock.Now().UTC(), ActorRef: p.ActorRef,
		TraceID: telemetry.TraceID(ctx), Internal: true,
	}, s.EgressLog)
	return x, nil
}

// ExportSummary xuất Excel Bảng giá tổng hợp theo bộ lọc (B3 FN-TW-01.01) — qua điểm kiểm soát xuất.
func (s *TicketService) ExportSummary(ctx context.Context, profile string, f SummaryFilter) (Export, error) {
	x, err := s.exportSummaryFile(ctx, profile, f)
	return s.observe(ctx, ProductSummaryExport, x, err)
}

// ExportMovement xuất Báo cáo biến động (Excel / PDF; Cấp Tỉnh, Admin) — qua điểm kiểm soát xuất.
func (s *TicketService) ExportMovement(ctx context.Context, profile string, f MovementFilter, format string) (Export, error) {
	x, err := s.exportMovementFile(ctx, profile, f, format)
	return s.observe(ctx, ProductMovementExport, x, err)
}

// ExportDashboard xuất số liệu Dashboard theo vùng ra Excel — qua điểm kiểm soát xuất.
func (s *TicketService) ExportDashboard(ctx context.Context, profile string, f DashboardFilter) (Export, error) {
	x, err := s.exportDashboardFile(ctx, profile, f)
	return s.observe(ctx, ProductDashboardExport, x, err)
}

// ExportProgress xuất danh sách Tiến độ nhập giá (luồng phụ FN-TI-02.01) — qua điểm kiểm soát xuất.
func (s *TicketService) ExportProgress(ctx context.Context, profile string, f ProgressFilter) (Export, error) {
	x, err := s.exportProgressFile(ctx, profile, f)
	return s.observe(ctx, ProductProgressExport, x, err)
}

// ExportStats xuất Thống kê phiếu báo giá (B3 FN-TI-02.08) — qua điểm kiểm soát xuất.
func (s *TicketService) ExportStats(ctx context.Context, profile string, f StatsFilter) (Export, error) {
	x, err := s.exportStatsFile(ctx, profile, f)
	return s.observe(ctx, ProductStatsExport, x, err)
}

func xlsxExport(sheet string, header []string, rows [][]string, name string) (Export, error) {
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, sheet, header, rows); err != nil {
		return Export{}, err
	}
	return Export{Body: buf.Bytes(), ContentType: tabular.ContentTypeXLSX, FileName: name, Rows: len(rows)}, nil
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TW-01.02 — Báo cáo tăng giảm, biến động giá
// ---------------------------------------------------------------------------------------------------------------------

// Kỳ so sánh.
const (
	PeriodDay   = "day"
	PeriodWeek  = "week"
	PeriodMonth = "month"
)

// MovementFilter là bộ lọc Báo cáo biến động (B1).
type MovementFilter struct {
	Period string    // day | week | month
	Day    time.Time // ngày thuộc kỳ hiện tại (zero = hôm nay)
	Region string
	Point  string
	Group  string
	Item   string // mặt hàng cho xu hướng / chi tiết theo xã (rỗng = mặt hàng đầu tiên)
}

// Period là một kỳ so sánh.
type Period struct {
	From, To time.Time
	Label    string
	Days     int // số ngày công bố trong kỳ
}

// MovementLine là biến động của một mặt hàng giữa hai kỳ.
type MovementLine struct {
	ItemInfo
	Cur *decimal.Decimal
	Move
}

// TrendPoint là giá trung bình của mặt hàng trong một kỳ (xu hướng).
type TrendPoint struct {
	Period Period
	Avg    *decimal.Decimal
}

// PointMove là biến động của mặt hàng tại một xã (luồng phụ — chi tiết theo xã).
type PointMove struct {
	PointCode  string
	PointName  string
	RegionName string
	Cur        *decimal.Decimal
	Move
}

// MovementCards là hàng thẻ chỉ số (B2).
type MovementCards struct {
	Up, Down, Flat int
	Exceeded       int // số lần vượt ngưỡng: cảnh báo đã chốt trong kỳ (BR-03)
}

// MovementView là Báo cáo biến động giá.
type MovementView struct {
	Period       string
	Cur, Prev    Period
	Insufficient bool // "Chưa đủ dữ liệu so sánh" — chưa có 2 kỳ công bố
	FlatPct      decimal.Decimal
	Cards        MovementCards
	Lines        []MovementLine
	Item         string
	Trend        []TrendPoint
	Points       []PointMove
	Regions      []Ref
	Groups       []Ref
	CanExport    bool
}

// trendPeriods là số kỳ của biểu đồ xu hướng.
const trendPeriods = 8

// periodOf trả kỳ chứa day: ngày (= day), tuần báo cáo (lịch kỳ hệ thống W11.03 / QĐ-6), tháng dương lịch.
func (s *TicketService) periodOf(ctx context.Context, kind string, day time.Time) (Period, error) {
	switch kind {
	case PeriodWeek:
		w, err := s.weekOf(ctx, day)
		if err != nil {
			return Period{}, err
		}
		return Period{From: w.From, To: w.To, Label: w.Key}, nil
	case PeriodMonth:
		from, to := domain.MonthOf(day)
		return Period{From: from, To: to, Label: from.Format("01/2006")}, nil
	}
	return Period{From: day, To: day, Label: day.Format("02/01/2006")}, nil
}

// periodAvg là giá trung bình của mỗi mặt hàng trong kỳ p: trung bình Giá trung bình (CT-04, phạm vi sc) các ngày công
// bố trong kỳ (BR-02). Trả số ngày công bố.
func periodAvg(boards []port.PublishedBoard, p Period, sc scope) (map[string]decimal.Decimal, int) {
	vals := map[string][]decimal.Decimal{}
	days := 0
	for _, pb := range boards {
		if pb.Day.Before(p.From) || pb.Day.After(p.To) {
			continue
		}
		days++
		for item, a := range aggregate(pb, sc) {
			vals[item] = append(vals[item], a.Avg)
		}
	}
	out := map[string]decimal.Decimal{}
	for item, vs := range vals {
		if m, ok := domain.Mean(vs); ok {
			out[item] = m
		}
	}
	return out, days
}

// Movement đọc Báo cáo tăng giảm, biến động giá (FN-TW-01.02): kỳ hiện tại chứa ngày chọn (kỳ ngày: ngày công bố gần
// nhất ≤ ngày chọn) so với kỳ liền trước (kỳ ngày: ngày công bố liền trước — bỏ qua ngày nghỉ), thẻ chỉ số, xu hướng
// trendPeriods kỳ của một mặt hàng, chi tiết theo xã.
func (s *TicketService) Movement(ctx context.Context, profile string, f MovementFilter) (MovementView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return MovementView{}, err
	}
	if err := reportViewer(c, "TW", "TINH", "XA"); err != nil {
		return MovementView{}, err
	}
	switch f.Period {
	case "":
		f.Period = PeriodDay
	case PeriodDay, PeriodWeek, PeriodMonth:
	default:
		return MovementView{}, problem.Validation.WithField("period", "day | week | month")
	}
	now := s.Clock.Now()
	day := s.dayOrToday(f.Day)
	v := MovementView{Period: f.Period, CanExport: reviewer(c)}
	sc := scope{Region: f.Region, Point: f.Point, Group: f.Group}
	// P0-POOL-KN-2 (#139): kỳ tuần tra lịch kỳ hệ thống (Weeks — pf/extension qua gốc lắp ráp, giao dịch riêng trên CHUNG pool)
	// NGOÀI giao dịch đọc báo cáo. Kỳ chỉ phụ thuộc ngày và loại kỳ.
	var trend []Period
	if f.Period != PeriodDay {
		p, err := s.periodOf(ctx, f.Period, day)
		if err != nil {
			return MovementView{}, err
		}
		for i := 0; i < trendPeriods; i++ {
			trend = append([]Period{p}, trend...)
			if p, err = s.periodOf(ctx, f.Period, p.From.AddDate(0, 0, -1)); err != nil {
				return MovementView{}, err
			}
		}
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		// Kỳ hiện tại và các kỳ trước (xu hướng).
		periods := slices.Clone(trend)
		if f.Period == PeriodDay {
			bs, err := s.lastPublished(ctx, tx, prof, day, trendPeriods)
			if err != nil {
				return err
			}
			for _, d := range bs {
				periods = append(periods, Period{From: d, To: d, Label: d.Format("02/01/2006")})
			}
		}
		rcDay := day
		if len(periods) > 0 {
			rcDay = periods[len(periods)-1].To
			if rcDay.After(day) {
				rcDay = day
			}
		}
		rc, err := s.loadReportCtx(ctx, tx, prof, rcDay)
		if err != nil {
			return err
		}
		v.Regions, v.Groups, v.FlatPct = rc.regionRefs(), rc.groupRefs(), rc.flat
		if len(periods) == 0 {
			v.Insufficient = true
			return nil
		}
		boards, err := s.publishedRange(ctx, tx, prof, periods[0].From, periods[len(periods)-1].To, now)
		if err != nil {
			return err
		}
		avgs := make([]map[string]decimal.Decimal, len(periods))
		for i := range periods {
			avgs[i], periods[i].Days = periodAvg(boards, periods[i], sc)
		}
		last := len(periods) - 1
		v.Cur = periods[last]
		if last >= 1 {
			v.Prev = periods[last-1]
		}
		if last < 1 || v.Cur.Days == 0 || v.Prev.Days == 0 {
			v.Insufficient = true
		}
		// Thông tin mặt hàng: dòng bảng giá đã chụp (mới nhất trước) trong khoảng.
		infos := map[string]ItemInfo{}
		for i := len(boards) - 1; i >= 0; i-- {
			for _, l := range boards[i].Lines {
				if _, ok := infos[l.ItemCode]; !ok {
					infos[l.ItemCode] = rc.info(l)
				}
			}
		}
		for item, inf := range infos {
			if sc.Group != "" && inf.GroupCode != sc.Group {
				continue
			}
			cur, okCur := avgs[last][item]
			var prev *decimal.Decimal
			if last >= 1 {
				if p, ok := avgs[last-1][item]; ok {
					prev = &p
				}
			}
			if !okCur && prev == nil {
				continue
			}
			ml := MovementLine{ItemInfo: inf}
			if okCur {
				cv := cur
				ml.Cur = &cv
				ml.Move = moveOf(prev, cur, rc.flat)
			} else {
				ml.Prev = prev
			}
			switch ml.Trend {
			case domain.TrendUp:
				v.Cards.Up++
			case domain.TrendDown:
				v.Cards.Down++
			case domain.TrendFlat:
				v.Cards.Flat++
			}
			v.Lines = append(v.Lines, ml)
		}
		rc.sortInfos(len(v.Lines), func(i int) ItemInfo { return v.Lines[i].ItemInfo }, func(i, j int) {
			v.Lines[i], v.Lines[j] = v.Lines[j], v.Lines[i]
		})
		v.Item = f.Item
		if v.Item == "" && len(v.Lines) > 0 {
			v.Item = v.Lines[0].ItemCode
		}
		for i, p := range periods {
			tp := TrendPoint{Period: p}
			if a, ok := avgs[i][v.Item]; ok {
				av := a
				tp.Avg = &av
			}
			v.Trend = append(v.Trend, tp)
		}
		if v.Item != "" {
			v.Points = movementPoints(rc, boards, v.Cur, v.Prev, last >= 1, sc, v.Item)
		}
		v.Cards.Exceeded, err = s.exceeded(ctx, tx, v.Cur, sc, infos)
		return err
	})
	return v, err
}

// lastPublished trả tối đa n ngày công bố gần nhất ≤ day, tăng dần.
func (s *TicketService) lastPublished(ctx context.Context, tx pgx.Tx, prof port.Profile, day time.Time, n int) ([]time.Time, error) {
	var out []time.Time
	d, inclusive := day, true
	for len(out) < n {
		x, ok, err := store.LatestPublished(ctx, tx, prof.Name, d, inclusive)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		x = domain.BusinessDay(x)
		out = append([]time.Time{x}, out...)
		d, inclusive = x, false
	}
	return out, nil
}

// movementPoints là biến động của item tại từng xã trong phạm vi sc.
func movementPoints(rc *reportCtx, boards []port.PublishedBoard, cur, prev Period, hasPrev bool, sc scope, item string) []PointMove {
	perPoint := func(p Period) map[string]decimal.Decimal {
		vals := map[string][]decimal.Decimal{}
		for _, pb := range boards {
			if pb.Day.Before(p.From) || pb.Day.After(p.To) {
				continue
			}
			for _, l := range pb.PointLines {
				if l.ItemCode == item && sc.point(l) {
					vals[l.PointCode] = append(vals[l.PointCode], l.Mid)
				}
			}
		}
		out := map[string]decimal.Decimal{}
		for pt, vs := range vals {
			if m, ok := domain.Mean(vs); ok {
				out[pt] = m
			}
		}
		return out
	}
	c := perPoint(cur)
	var pr map[string]decimal.Decimal
	if hasPrev {
		pr = perPoint(prev)
	}
	codes := map[string]bool{}
	for k := range c {
		codes[k] = true
	}
	for k := range pr {
		codes[k] = true
	}
	var out []PointMove
	for code := range codes {
		pm := PointMove{PointCode: code, PointName: rc.pointName(code)}
		if st, ok := rc.points[code]; ok {
			pm.RegionName = rc.regionName(st.RegionCode)
		}
		var pp *decimal.Decimal
		if v, ok := pr[code]; ok {
			pp = &v
		}
		if v, ok := c[code]; ok {
			cv := v
			pm.Cur = &cv
			pm.Move = moveOf(pp, v, rc.flat)
		} else {
			pm.Prev = pp
		}
		out = append(out, pm)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PointCode < out[j].PointCode })
	return out
}

// exceeded đếm cảnh báo đã chốt (Đã hiệu chỉnh, Giữ nguyên, Đã loại) của phiếu còn hiệu lực có ngày báo giá trong kỳ p,
// trong phạm vi sc (BR-03 FN-TW-01.02 — không gồm cảnh báo Tạm tính đã tự hủy).
func (s *TicketService) exceeded(ctx context.Context, tx pgx.Tx, p Period, sc scope, infos map[string]ItemInfo) (int, error) {
	if p.From.IsZero() {
		return 0, nil
	}
	ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{From: p.From, To: p.To, Live: true})
	if err != nil {
		return 0, err
	}
	ps, err := loadPointSnap(ctx, tx)
	if err != nil {
		return 0, err
	}
	idList := make([]uuid.UUID, 0, len(ts))
	for _, t := range ts {
		if !domain.Sent(t.Status) {
			continue
		}
		if !sc.whole() {
			st, ok := ps.states(t.Day)[t.PointCode]
			if !ok || !sc.point(port.BoardPointLine{PointCode: t.PointCode, RegionCode: st.RegionCode}) {
				continue
			}
		}
		idList = append(idList, t.ID)
	}
	as, err := store.LoadAlerts(ctx, tx, idList)
	if err != nil {
		return 0, err
	}
	n := 0
	for k, a := range bookAlerts(as).latest {
		if a.Status == domain.AlertCancelled || !domain.AlertResolved(a.Status) {
			continue
		}
		if sc.Group != "" {
			if inf, ok := infos[k.item]; !ok || inf.GroupCode != sc.Group {
				continue
			}
		}
		n++
	}
	return n, nil
}

var movementHeader = []string{"STT", "Nhóm hàng", "Mặt hàng", "ĐVT", "Giá kỳ này", "Giá kỳ trước", "Tăng/giảm (%)", "Xu hướng"}

// ExportMovement xuất Báo cáo biến động theo bộ lọc đang chọn — Excel hoặc PDF (format = "pdf"); chỉ Cấp Tỉnh, Admin
// (BR-04, AC-03 FN-TW-01.02).
func (s *TicketService) exportMovementFile(ctx context.Context, profile string, f MovementFilter, format string) (Export, error) {
	v, err := s.Movement(ctx, profile, f)
	if err != nil {
		return Export{}, err
	}
	if !v.CanExport {
		return Export{}, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh, Admin xuất báo cáo biến động giá")
	}
	if len(v.Lines) == 0 {
		return Export{}, port.ErrReportEmpty
	}
	rows := make([][]string, 0, len(v.Lines))
	for i, l := range v.Lines {
		rows = append(rows, []string{strconv.Itoa(i + 1), l.GroupName, l.ItemName, l.UnitName, decStr(l.Cur), decStr(l.Prev),
			pctStr(l.ChangePct), trendVI[l.Trend]})
	}
	name := "bien-dong-gia-" + v.Period + "-" + v.Cur.From.Format(time.DateOnly)
	if format == "pdf" {
		if s.PDF == nil {
			return Export{}, port.ErrPDFUnavailable
		}
		meta := [][2]string{{"Kỳ so sánh", periodVI[v.Period]}, {"Kỳ này", v.Cur.Label}, {"Kỳ trước", v.Prev.Label},
			{"Nhóm vùng", orAll(f.Region)}, {"Xã", orAll(f.Point)}, {"Nhóm hàng", orAll(f.Group)},
			{"Tăng / Giảm / Đi ngang", strconv.Itoa(v.Cards.Up) + " / " + strconv.Itoa(v.Cards.Down) + " / " + strconv.Itoa(v.Cards.Flat)},
			{"Số lần vượt ngưỡng", strconv.Itoa(v.Cards.Exceeded)}}
		b, err := s.PDF.RenderTablePDF("Báo cáo tăng giảm, biến động giá", meta, movementHeader, rows, s.Clock.Now())
		if err != nil {
			return Export{}, err
		}
		return Export{Body: b, ContentType: "application/pdf", FileName: name + ".pdf", Rows: len(rows)}, nil
	}
	return xlsxExport("Biến động giá", movementHeader, rows, name+".xlsx")
}

var periodVI = map[string]string{PeriodDay: "Ngày", PeriodWeek: "Tuần", PeriodMonth: "Tháng"}

func orAll(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Tất cả"
	}
	return s
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TW-01.03 — Dashboard tổng hợp giá nông sản theo vùng
// ---------------------------------------------------------------------------------------------------------------------

// DashboardFilter là bộ lọc Dashboard (B2).
type DashboardFilter struct {
	Day    time.Time
	Group  string
	Item   string // mặt hàng của biểu đồ xu hướng
	Region string // vùng đang bấm — giá từng xã trong vùng (B4)
	All    bool   // mọi mặt hàng (mặc định chỉ mặt hàng Cơ bản — BR-03)
}

// RegionAvg là giá trung bình của một mặt hàng trong một nhóm vùng (CT-04, phạm vi nhóm vùng).
type RegionAvg struct {
	Region string
	Avg    *decimal.Decimal
	Points int
}

// DashLine là một mặt hàng của bảng giá trung bình theo vùng.
type DashLine struct {
	ItemInfo
	Avg     decimal.Decimal // toàn tỉnh
	Move                    // toàn tỉnh so kỳ trước
	Regions []RegionAvg     // theo thứ tự Regions của view
}

// GroupMove là số mặt hàng tăng / giảm / đi ngang của một nhóm hàng (biểu đồ thanh biến động).
type GroupMove struct {
	Group          Ref
	Up, Down, Flat int
}

// DashRegion là một nhóm vùng trên dashboard.
type DashRegion struct {
	Ref
	Points int // số xã có báo giá trong ngày
}

// DashTrend là giá trung bình của mặt hàng theo vùng ở một ngày công bố.
type DashTrend struct {
	Day     time.Time
	Avg     *decimal.Decimal
	Regions []RegionAvg
}

// DashboardCards là hàng thẻ chỉ số (B3).
type DashboardCards struct {
	Up, Down, Flat int
	Points         int // số xã có báo giá
}

// DashboardView là Dashboard theo vùng của một ngày công bố.
type DashboardView struct {
	Requested  time.Time
	Day        time.Time
	Version    int
	ApprovedAt time.Time
	NotToday   bool
	Updating   bool
	PrevDay    *time.Time
	Cards      DashboardCards
	Regions    []DashRegion
	Lines      []DashLine
	Groups     []GroupMove
	Item       string
	Trend      []DashTrend
	Region     string
	Drill      []PointPrice
}

// dashTrendDays là số ngày công bố của biểu đồ xu hướng.
const dashTrendDays = 30

func regionAvgs(pb port.PublishedBoard, regions []Ref, item string) []RegionAvg {
	out := make([]RegionAvg, 0, len(regions))
	for _, r := range regions {
		ra := RegionAvg{Region: r.Code}
		if a, ok := aggregate(pb, scope{Region: r.Code})[item]; ok {
			av := a.Avg
			ra.Avg, ra.Points = &av, a.Points
		}
		out = append(out, ra)
	}
	return out
}

// Dashboard đọc Dashboard tổng hợp giá theo vùng (FN-TW-01.03): số liệu trùng Bảng giá tổng hợp cùng ngày, cùng nhóm
// vùng (AC-01); bấm vùng → giá từng xã của nhóm vùng có giá trong ngày (AC-02).
func (s *TicketService) Dashboard(ctx context.Context, profile string, f DashboardFilter) (DashboardView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return DashboardView{}, err
	}
	if err := reportViewer(c, "TW", "TINH"); err != nil {
		return DashboardView{}, err
	}
	now := s.Clock.Now()
	v := DashboardView{Requested: s.dayOrToday(f.Day), Region: f.Region}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		pb, ok, err := s.boardBefore(ctx, tx, prof, v.Requested, now, true)
		if err != nil || !ok {
			return err
		}
		rc, err := s.loadReportCtx(ctx, tx, prof, pb.Day)
		if err != nil {
			return err
		}
		v.Day, v.Version, v.ApprovedAt, v.Updating, v.NotToday = pb.Day, pb.Version, pb.ApprovedAt, pb.Reopened, !pb.Day.Equal(v.Requested)
		prev, okPrev, err := s.boardBefore(ctx, tx, prof, pb.Day, now, false)
		if err != nil {
			return err
		}
		var pp *port.PublishedBoard
		if okPrev {
			pp, v.PrevDay = &prev, &prev.Day
		}
		regions := rc.regionRefs()
		perRegion := map[string]map[string]bool{}
		allPoints := map[string]bool{}
		for _, l := range pb.PointLines {
			if perRegion[l.RegionCode] == nil {
				perRegion[l.RegionCode] = map[string]bool{}
			}
			perRegion[l.RegionCode][l.PointCode] = true
			allPoints[l.PointCode] = true
		}
		for _, r := range regions {
			v.Regions = append(v.Regions, DashRegion{Ref: r, Points: len(perRegion[r.Code])})
		}
		v.Cards.Points = len(allPoints)
		groupMoves := map[string]*GroupMove{}
		for _, sl := range rc.summaryLines(pb, pp, scope{Group: f.Group}) {
			switch sl.Trend {
			case domain.TrendUp:
				v.Cards.Up++
			case domain.TrendDown:
				v.Cards.Down++
			case domain.TrendFlat:
				v.Cards.Flat++
			}
			gm, ok := groupMoves[sl.GroupCode]
			if !ok {
				gm = &GroupMove{Group: Ref{sl.GroupCode, sl.GroupName}}
				groupMoves[sl.GroupCode] = gm
			}
			switch sl.Trend {
			case domain.TrendUp:
				gm.Up++
			case domain.TrendDown:
				gm.Down++
			case domain.TrendFlat:
				gm.Flat++
			}
			if !f.All && !sl.Basic && sl.ItemCode != f.Item {
				continue
			}
			v.Lines = append(v.Lines, DashLine{ItemInfo: sl.ItemInfo, Avg: sl.Avg, Move: sl.Move, Regions: regionAvgs(pb, regions, sl.ItemCode)})
		}
		for _, g := range rc.groupRefs() {
			if gm, ok := groupMoves[g.Code]; ok {
				v.Groups = append(v.Groups, *gm)
			}
		}
		v.Item = f.Item
		if v.Item == "" && len(v.Lines) > 0 {
			v.Item = v.Lines[0].ItemCode
		}
		if f.Region != "" {
			v.Drill = rc.pointPrices(pb, scope{Region: f.Region, Group: f.Group}, "")
		}
		if v.Item == "" {
			return nil
		}
		days, err := s.lastPublished(ctx, tx, prof, pb.Day, dashTrendDays)
		if err != nil || len(days) == 0 {
			return err
		}
		boards, err := s.publishedRange(ctx, tx, prof, days[0], days[len(days)-1], now)
		if err != nil {
			return err
		}
		for _, b := range boards {
			dt := DashTrend{Day: b.Day, Regions: regionAvgs(b, regions, v.Item)}
			if a, ok := aggregate(b, scope{})[v.Item]; ok {
				av := a.Avg
				dt.Avg = &av
			}
			v.Trend = append(v.Trend, dt)
		}
		return nil
	})
	return v, err
}

var dashboardHeader = []string{"STT", "Nhóm hàng", "Mặt hàng", "ĐVT", "Giá TB toàn tỉnh", "Kỳ trước", "Tăng/giảm (%)", "Xu hướng"}

// ExportDashboard xuất số liệu dashboard ra Excel (luồng phụ FN-TW-01.03): giá trung bình toàn tỉnh và từng nhóm vùng,
// hoặc giá từng xã của vùng đang bấm.
func (s *TicketService) exportDashboardFile(ctx context.Context, profile string, f DashboardFilter) (Export, error) {
	v, err := s.Dashboard(ctx, profile, f)
	if err != nil {
		return Export{}, err
	}
	if v.Day.IsZero() {
		return Export{}, port.ErrReportEmpty
	}
	header := append([]string(nil), dashboardHeader...)
	for _, r := range v.Regions {
		header = append(header, "Giá TB "+r.Name, "Số xã "+r.Name)
	}
	rows := make([][]string, 0, len(v.Lines))
	for i, l := range v.Lines {
		row := []string{strconv.Itoa(i + 1), l.GroupName, l.ItemName, l.UnitName, l.Avg.String(), decStr(l.Prev), pctStr(l.ChangePct),
			trendVI[l.Trend]}
		for _, ra := range l.Regions {
			row = append(row, decStr(ra.Avg), strconv.Itoa(ra.Points))
		}
		rows = append(rows, row)
	}
	return xlsxExport("Dashboard theo vùng", header, rows, "dashboard-gia-theo-vung-"+v.Day.Format(time.DateOnly)+".xlsx")
}

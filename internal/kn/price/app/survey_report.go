package app

// Báo cáo giá thành sản xuất theo vụ (W11.78; BRD Báo giá v1.3 FN-TW-01.04, CT-05):
//   - BR-01: chỉ phiếu Đã duyệt của đợt Đã chốt (phiếu Loại, Đã xóa không tính — AC-02).
//   - BR-02: bình quân = trung bình cộng các phiếu; thêm trung vị cho giá bán và giá thành (AC-01: 5.274, 5.580, 5.384 →
//     bình quân 5.413, trung vị 5.384). Mặt hàng dưới 3 phiếu gắn nhãn "Mẫu nhỏ" (Q-083 — giả định tạm của prototype; ngưỡng
//     k = 10 khi số liệu rời tenant thuộc cổng công bố, không áp ở đây).
//   - BR-03: chỉ số tổng hợp — không đọc kho định danh, không có họ tên / SĐT nông hộ (AC-03); Cấp TW, Cấp Tỉnh, Admin xem.
//   - BR-04: chỉ Cấp Tỉnh, Admin xuất Excel (qua điểm kiểm soát xuất dùng chung).
//   - Nhóm vùng của xã = nhóm vùng của xã báo giá cùng mã ĐVHC tại ngày kết thúc thu thập (W11.72); không có → "Khác".

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SmallSample: mặt hàng dưới SmallSample phiếu đã duyệt gắn nhãn "Mẫu nhỏ" (BRD; Q-083).
const SmallSample = 3

// CostFilter là bộ lọc báo cáo giá thành (B1).
type CostFilter struct {
	Round   uuid.UUID // rỗng → đợt đã chốt gần nhất
	Compare uuid.UUID // đợt so sánh (luồng phụ)
	Crop    string
	Item    string
	Region  string
	Commune string
}

// CostRow là một dòng tổng hợp theo mặt hàng (B2). Tiền làm tròn đồng, % hai số lẻ.
type CostRow struct {
	Item, ItemName, Crop string
	N                    int
	SmallSample          bool
	AvgCosts             map[string]decimal.Decimal
	AvgTotalCost         decimal.Decimal
	AvgYieldHa           decimal.Decimal
	AvgPrice             decimal.Decimal
	MedianPrice          decimal.Decimal
	AvgCostPerKg         decimal.Decimal
	MedianCostPerKg      decimal.Decimal
	AvgProfit            decimal.Decimal
	AvgProfitPerHa       decimal.Decimal
	AvgMarginPct         *decimal.Decimal
}

// CostRegion là giá thành – giá bán theo vùng của một mặt hàng (B3).
type CostRegion struct {
	Item, ItemName, Region, RegionName string
	N                                  int
	AvgCostPerKg, AvgPrice             decimal.Decimal
}

// CostCommune là phân bố giá bán, giá thành theo xã của một mặt hàng (luồng phụ).
type CostCommune struct {
	Commune, CommuneName                     string
	N                                        int
	MinPrice, MaxPrice, MedianPrice, AvgCost decimal.Decimal
	MedianCostPerKg                          decimal.Decimal
}

// CostKPIs là hàng thẻ chỉ số.
type CostKPIs struct {
	Forms          int
	AvgCostPerKg   *decimal.Decimal
	AvgPrice       *decimal.Decimal
	AvgProfitPerHa *decimal.Decimal
}

// CostBlock là kết quả của một đợt.
type CostBlock struct {
	Round    RoundSummary
	KPIs     CostKPIs
	Rows     []CostRow
	Regions  []CostRegion
	Communes []CostCommune
}

// CostReport là màn Báo cáo giá thành sản xuất.
type CostReport struct {
	Rounds    []RoundSummary // đợt đã chốt (chọn đợt / so sánh)
	Main      *CostBlock
	Compare   *CostBlock
	Regions   []Ref
	CanExport bool
}

type costCtx struct {
	rounds  []store.SurveyRound
	forms   map[uuid.UUID][]store.SurveyForm
	states  map[string]domain.ItemState
	names   map[string]string
	regions map[string]string // mã nhóm vùng → tên
	point   func(day time.Time) map[string]string
}

func money(d decimal.Decimal) decimal.Decimal { return d.Round(0) }

// Cost trả báo cáo giá thành (Cấp TW, Cấp Tỉnh, Admin).
func (s *SurveyService) Cost(ctx context.Context, profile string, f CostFilter) (CostReport, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return CostReport{}, err
	}
	if !a.read && a.c.level != "TW" {
		return CostReport{}, problem.Forbidden.WithDetail("chỉ Cấp TW, Cấp Tỉnh, Admin xem báo cáo giá thành")
	}
	out := CostReport{CanExport: a.read}
	err = s.ctxTx(ctx, func(tx pgx.Tx) error {
		cc, err := s.loadCost(ctx, tx, a.prof.Name)
		if err != nil {
			return err
		}
		for _, r := range cc.rounds {
			out.Rounds = append(out.Rounds, RoundSummary{ID: r.RoundID, Name: r.Name, Season: r.Season, Year: r.Year, From: r.CollectFrom,
				To: r.CollectTo, Crops: r.Crops, Status: r.Status, Forms: approvedCount(cc.forms[r.RoundID]), Seq: r.Seq})
		}
		for code, name := range cc.regions {
			out.Regions = append(out.Regions, Ref{Code: code, Name: name})
		}
		slices.SortFunc(out.Regions, func(x, y Ref) int { return strings.Compare(x.Name, y.Name) })
		if len(cc.rounds) == 0 {
			return nil
		}
		main := cc.rounds[0]
		if f.Round != uuid.Nil {
			i := slices.IndexFunc(cc.rounds, func(r store.SurveyRound) bool { return r.RoundID == f.Round })
			if i < 0 {
				return problem.Validation.WithField("round", "chỉ chọn đợt đã chốt")
			}
			main = cc.rounds[i]
		}
		b := cc.block(main, f)
		out.Main = &b
		if f.Compare != uuid.Nil && f.Compare != main.RoundID {
			i := slices.IndexFunc(cc.rounds, func(r store.SurveyRound) bool { return r.RoundID == f.Compare })
			if i < 0 {
				return problem.Validation.WithField("compare", "chỉ so sánh với đợt đã chốt")
			}
			c := cc.block(cc.rounds[i], f)
			out.Compare = &c
		}
		return nil
	})
	return out, err
}

func approvedCount(fs []store.SurveyForm) int {
	n := 0
	for _, f := range fs {
		if f.Status == domain.FormApproved {
			n++
		}
	}
	return n
}

func (s *SurveyService) loadCost(ctx context.Context, tx pgx.Tx, profile string) (*costCtx, error) {
	rs, err := store.SurveyRounds(ctx, tx, profile)
	if err != nil {
		return nil, err
	}
	cc := &costCtx{forms: map[uuid.UUID][]store.SurveyForm{}, regions: map[string]string{}}
	var ids []uuid.UUID
	for _, r := range rs {
		if r.Status == domain.RoundClosed {
			cc.rounds = append(cc.rounds, r)
			ids = append(ids, r.RoundID)
		}
	}
	// Đợt chốt gần nhất trước: theo năm, ngày kết thúc thu thập.
	slices.SortStableFunc(cc.rounds, func(x, y store.SurveyRound) int { return y.CollectTo.Compare(x.CollectTo) })
	if len(ids) > 0 {
		fs, err := store.SurveyForms(ctx, tx, ids)
		if err != nil {
			return nil, err
		}
		for _, f := range fs {
			cc.forms[f.RoundID] = append(cc.forms[f.RoundID], f)
		}
	}
	isn, err := loadSnap(ctx, tx)
	if err != nil {
		return nil, err
	}
	cc.states = allItemStates(isn)
	if cc.names, err = communeNames(ctx, tx); err != nil {
		return nil, err
	}
	ps, err := loadPointSnap(ctx, tx)
	if err != nil {
		return nil, err
	}
	regs, err := store.Effective(ctx, tx, domain.KindRegion, s.today())
	if err != nil {
		return nil, err
	}
	for _, r := range regs {
		cc.regions[r.Code] = r.Name
	}
	cc.point = func(day time.Time) map[string]string {
		out := map[string]string{}
		for _, st := range ps.states(day) {
			if st.AdminUnitCode != "" {
				out[st.AdminUnitCode] = st.RegionCode
			}
		}
		return out
	}
	return cc, nil
}

type acc struct {
	n                                       int
	costs                                   map[string][]decimal.Decimal
	total, yield, price, perKg, profit, pha []decimal.Decimal
	margin                                  []decimal.Decimal
}

func (x *acc) add(f store.SurveyForm) {
	c := domain.CT05(figures(f))
	if x.costs == nil {
		x.costs = map[string][]decimal.Decimal{}
	}
	x.n++
	for _, k := range domain.CostKeys {
		x.costs[k] = append(x.costs[k], f.Costs[k])
	}
	x.total = append(x.total, c.TotalCost)
	x.yield = append(x.yield, c.YieldPerHa)
	x.price = append(x.price, effPrice(f))
	x.perKg = append(x.perKg, c.CostPerKg)
	x.profit = append(x.profit, c.Profit)
	x.pha = append(x.pha, c.ProfitPerHa)
	if c.MarginPct != nil {
		x.margin = append(x.margin, *c.MarginPct)
	}
}

func mean(vs []decimal.Decimal) decimal.Decimal {
	m, _ := domain.Mean(vs)
	return m
}

func (cc *costCtx) block(r store.SurveyRound, f CostFilter) CostBlock {
	b := CostBlock{Round: RoundSummary{ID: r.RoundID, Name: r.Name, Season: r.Season, Year: r.Year, From: r.CollectFrom, To: r.CollectTo,
		Crops: r.Crops, Status: r.Status, Seq: r.Seq}}
	region := cc.point(r.CollectTo)
	byItem, byRegion, byCommune := map[string]*acc{}, map[[2]string]*acc{}, map[string]*acc{}
	all := &acc{}
	for _, fm := range cc.forms[r.RoundID] {
		if fm.Status != domain.FormApproved {
			continue
		}
		reg := region[fm.Commune]
		if (f.Crop != "" && fm.Crop != f.Crop) || (f.Item != "" && fm.Item != f.Item) || (f.Commune != "" && fm.Commune != f.Commune) ||
			(f.Region != "" && reg != f.Region) {
			continue
		}
		for _, x := range []*acc{accOf(byItem, fm.Item), accOf(byRegion, [2]string{fm.Item, reg}), accOf(byCommune, fm.Commune)} {
			x.add(fm)
		}
		all.add(fm)
		b.Round.Forms++
	}
	b.KPIs.Forms = all.n
	if all.n > 0 {
		cpk, pr, pha := money(mean(all.perKg)), money(mean(all.price)), money(mean(all.pha))
		b.KPIs.AvgCostPerKg, b.KPIs.AvgPrice, b.KPIs.AvgProfitPerHa = &cpk, &pr, &pha
	}
	crop := map[string]string{}
	for _, fm := range cc.forms[r.RoundID] {
		crop[fm.Item] = fm.Crop
	}
	for item, x := range byItem {
		row := CostRow{Item: item, ItemName: itemLabel(cc.states[item]), Crop: crop[item], N: x.n, SmallSample: x.n < SmallSample,
			AvgCosts: map[string]decimal.Decimal{}, AvgTotalCost: money(mean(x.total)), AvgYieldHa: mean(x.yield).Round(2),
			AvgPrice: money(mean(x.price)), MedianPrice: money(*domain.Median(x.price)), AvgCostPerKg: money(mean(x.perKg)),
			MedianCostPerKg: money(*domain.Median(x.perKg)), AvgProfit: money(mean(x.profit)), AvgProfitPerHa: money(mean(x.pha))}
		for _, k := range domain.CostKeys {
			row.AvgCosts[k] = money(mean(x.costs[k]))
		}
		if len(x.margin) > 0 {
			m := mean(x.margin).Round(2)
			row.AvgMarginPct = &m
		}
		b.Rows = append(b.Rows, row)
	}
	slices.SortFunc(b.Rows, func(x, y CostRow) int {
		if c := slices.Index(domain.Crops, x.Crop) - slices.Index(domain.Crops, y.Crop); c != 0 {
			return c
		}
		return strings.Compare(x.ItemName, y.ItemName)
	})
	for k, x := range byRegion {
		name := cc.regions[k[1]]
		if k[1] == "" || name == "" {
			name = "Khác"
		}
		b.Regions = append(b.Regions, CostRegion{Item: k[0], ItemName: itemLabel(cc.states[k[0]]), Region: k[1], RegionName: name, N: x.n,
			AvgCostPerKg: money(mean(x.perKg)), AvgPrice: money(mean(x.price))})
	}
	slices.SortFunc(b.Regions, func(x, y CostRegion) int {
		if c := strings.Compare(x.ItemName, y.ItemName); c != 0 {
			return c
		}
		return strings.Compare(x.RegionName, y.RegionName)
	})
	if f.Item != "" {
		for c, x := range byCommune {
			mn, mx := x.price[0], x.price[0]
			for _, p := range x.price {
				mn, mx = decimal.Min(mn, p), decimal.Max(mx, p)
			}
			b.Communes = append(b.Communes, CostCommune{Commune: c, CommuneName: nameOr(cc.names, c), N: x.n, MinPrice: money(mn), MaxPrice: money(mx),
				MedianPrice: money(*domain.Median(x.price)), AvgCost: money(mean(x.perKg)), MedianCostPerKg: money(*domain.Median(x.perKg))})
		}
		slices.SortFunc(b.Communes, func(x, y CostCommune) int { return strings.Compare(x.CommuneName, y.CommuneName) })
	}
	return b
}

func accOf[K comparable](m map[K]*acc, k K) *acc {
	if m[k] == nil {
		m[k] = &acc{}
	}
	return m[k]
}

// ExportCost xuất báo cáo giá thành ra Excel (Cấp Tỉnh, Admin — BR-04; không có dữ liệu cá nhân).
func (s *SurveyService) ExportCost(ctx context.Context, profile string, f CostFilter) (Export, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return Export{}, err
	}
	if !a.read {
		return Export{}, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh, Admin xuất báo cáo giá thành")
	}
	v, err := s.Cost(ctx, profile, f)
	if err != nil {
		return Export{}, err
	}
	if v.Main == nil {
		return Export{}, problem.Validation.WithDetail("Chưa có đợt điều tra đã chốt")
	}
	header := []string{"STT", "Nhóm cây", "Mặt hàng", "Số phiếu"}
	for _, k := range domain.CostKeys {
		header = append(header, domain.CostNames[k])
	}
	header = append(header, "Tổng chi phí", "Năng suất (kg/ha)", "Giá bán BQ", "Giá bán trung vị", "Giá thành / kg BQ", "Giá thành / kg trung vị",
		"Lợi nhuận BQ", "Lợi nhuận / ha", "Tỷ suất lợi nhuận (%)", "Ghi chú")
	var rows [][]string
	for i, r := range v.Main.Rows {
		row := []string{strconv.Itoa(i + 1), domain.CropNames[r.Crop], r.ItemName, strconv.Itoa(r.N)}
		for _, k := range domain.CostKeys {
			row = append(row, r.AvgCosts[k].String())
		}
		note := ""
		if r.SmallSample {
			note = "Mẫu nhỏ"
		}
		margin := ""
		if r.AvgMarginPct != nil {
			margin = r.AvgMarginPct.StringFixed(2)
		}
		row = append(row, r.AvgTotalCost.String(), r.AvgYieldHa.String(), r.AvgPrice.String(), r.MedianPrice.String(), r.AvgCostPerKg.String(),
			r.MedianCostPerKg.String(), r.AvgProfit.String(), r.AvgProfitPerHa.String(), margin, note)
		rows = append(rows, row)
	}
	x, err := xlsxExport("Giá thành sản xuất", header, rows, "gia-thanh-san-xuat-"+v.Main.Round.ID.String()[:8]+".xlsx")
	return s.observe(ctx, ProductSurveyCostExport, x, err)
}

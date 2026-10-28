package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// UnitRow là tiến độ của một xã trong đợt.
type UnitRow struct {
	Commune, CommuneName string
	Region, RegionName   string
	Status               string // trạng thái hiển thị của cap/forms (not_submitted, overdue, submitted, accepted, returned, missed…)
	Versions             int64
	Late                 bool
	LastSubmittedAt      *time.Time
}

// RegionRow là tiến độ gom theo khu vực (AC-02).
type RegionRow struct {
	Code, Name                                 string
	Total, Submitted, Pending, Overdue, Missed int
}

// Progress là tiến độ của đợt đã phát hành.
type Progress struct {
	PeriodStatus                                         string // open | closed | reopened | cancelled
	Total, Submitted, Pending, Overdue, Missed, Returned int
	Regions                                              []RegionRow
	Units                                                []UnitRow // chỉ ở chi tiết
}

type nameBook struct {
	regions  map[string]string
	byUnit   map[string]partyport.CommuneRegion
	communes map[string]string
}

func (s *Service) names(ctx context.Context, province string, units []string, communes bool) nameBook {
	nb := nameBook{regions: map[string]string{}, byUnit: map[string]partyport.CommuneRegion{}, communes: map[string]string{}}
	if rg := s.regions(); rg != nil && province != "" {
		if rs, err := rg.ProvinceRegions(ctx, province, true); err == nil {
			for _, r := range rs {
				nb.regions[r.Code] = r.Name
			}
		}
		if m, err := rg.CommuneRegions(ctx, province); err == nil {
			nb.byUnit = m
		}
	}
	if communes && s.Units != nil {
		for _, u := range units {
			if au, err := s.Units.Get(ctx, u, s.now()); err == nil && au.Name != "" {
				nb.communes[u] = au.Name
			}
		}
	}
	return nb
}

func (nb nameBook) region(unit string) (string, string) {
	code := nb.byUnit[unit].RegionCode
	if code == "" {
		return "", "Chưa gán khu vực"
	}
	if n, ok := nb.regions[code]; ok {
		return code, n
	}
	if n := nb.byUnit[unit].RegionName; n != "" {
		return code, n
	}
	return code, code
}

func (nb nameBook) commune(unit string) string {
	if n, ok := nb.communes[unit]; ok {
		return n
	}
	return unit
}

func province(c Campaign) string {
	if c.Version.Province != nil {
		return *c.Version.Province
	}
	return ""
}

// progress đọc tiến độ kỳ native của đợt (người gọi phải thấy kỳ — kiểm ở cap/forms) và gom theo khu vực.
func (s *Service) progress(ctx context.Context, _ caller, cur Campaign, detail bool) (Progress, error) {
	p, err := s.Control.Progress(ctx, domain.Profile, *cur.Period.FormsPeriodRef)
	if err != nil {
		return Progress{}, err
	}
	units := make([]string, 0, len(p.Units))
	for _, u := range p.Units {
		units = append(units, u.Unit)
	}
	nb := s.names(ctx, province(cur), units, detail)
	out := Progress{PeriodStatus: p.Status}
	byRegion := map[string]*RegionRow{}
	var order []string
	for _, u := range p.Units {
		code, name := nb.region(u.Unit)
		r := byRegion[code]
		if r == nil {
			r = &RegionRow{Code: code, Name: name}
			byRegion[code] = r
			order = append(order, code)
		}
		r.Total++
		out.Total++
		switch {
		case u.Versions > 0:
			r.Submitted++
			out.Submitted++
		case u.Status == "missed":
			r.Missed++
			out.Missed++
		default:
			r.Pending++
			out.Pending++
		}
		if u.Status == "overdue" {
			r.Overdue++
			out.Overdue++
		}
		if u.Decision == "returned" {
			out.Returned++
		}
		if detail {
			out.Units = append(out.Units, UnitRow{Commune: u.Unit, CommuneName: nb.commune(u.Unit), Region: code, RegionName: name, Status: u.Status,
				Versions: u.Versions, Late: u.Late, LastSubmittedAt: u.LastSubmittedAt})
		}
	}
	sort.Strings(order)
	for _, code := range order {
		out.Regions = append(out.Regions, *byRegion[code])
	}
	sort.SliceStable(out.Units, func(i, j int) bool {
		if out.Units[i].Region != out.Units[j].Region {
			return out.Units[i].Region < out.Units[j].Region
		}
		return out.Units[i].Commune < out.Units[j].Commune
	})
	return out, nil
}

// ---------------------------------------------------------------------------
// Kết quả tổng hợp (B5)
// ---------------------------------------------------------------------------

// ResultField là một chỉ tiêu số của biểu điều tra.
type ResultField struct {
	Key, Label, Unit string
}

// ResultRow là số liệu của một xã.
type ResultRow struct {
	Commune, CommuneName, RegionName string
	Values                           map[string]string
	NilReport                        bool
}

// Results là bảng kết quả của đợt: cộng các chỉ tiêu số của các xã (số liệu riêng của đợt — không vào lũy kế định kỳ).
type Results struct {
	Campaign Campaign
	// Basis: closed = ảnh chụp lúc đóng đợt; submitted = bài đã gửi (bỏ bài đang "Yêu cầu điều chỉnh").
	Basis    string
	Fields   []ResultField
	Rows     []ResultRow
	Total    map[string]string
	Units    int
	Reported int
}

type fieldDef struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Unit  string `json:"unit"`
}

// Results đọc kết quả của đợt đã phát hành.
func (s *Service) Results(ctx context.Context, cid uuid.UUID) (Results, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Results{}, err
	}
	cur, err := s.load(ctx, c, cid)
	if err != nil {
		return Results{}, err
	}
	if cur.Period == nil {
		return Results{}, ErrState.WithDetail("đợt chưa phát hành — chưa có kết quả")
	}
	if s.Figures == nil {
		return Results{}, ErrDependency.WithDetail("chưa nối số liệu kỳ native (cap/forms)")
	}
	v := cur.Version
	ref, err := s.Forms.LatestTemplate(ctx, domain.Profile, v.TemplateCode)
	if err != nil {
		return Results{}, err
	}
	var defs []fieldDef
	if err := json.Unmarshal(ref.Fields, &defs); err != nil {
		return Results{}, err
	}
	out := Results{Campaign: cur, Basis: "submitted", Total: map[string]string{}}
	for _, d := range defs {
		if d.Type == "integer" || d.Type == "decimal" {
			out.Fields = append(out.Fields, ResultField{Key: d.Key, Label: d.Label, Unit: d.Unit})
		}
	}
	zone := clock.Zone()
	from := time.Date(v.From.Year(), v.From.Month(), v.From.Day(), 0, 0, 0, 0, zone)
	to := time.Date(v.To.Year(), v.To.Month(), v.To.Day(), 0, 0, 0, 0, zone)
	figs, err := s.Figures.Figures(ctx, formsport.FiguresQuery{Profile: domain.Profile, TemplateCode: v.TemplateCode, From: from, To: to})
	if err != nil {
		return Results{}, err
	}
	var pf *formsport.PeriodFigures
	for i := range figs {
		if figs[i].ID == *cur.Period.FormsPeriodRef {
			pf = &figs[i]
		}
	}
	if pf == nil {
		return Results{}, ErrNotFound.WithDetail("không đọc được kỳ native của đợt")
	}
	src := pf.Submitted
	if cur.State == domain.StateClosed && pf.ClosedNo > 0 {
		out.Basis, src = "closed", pf.Closed
	}
	nb := s.names(ctx, province(cur), pf.Units, true)
	sum := map[string]decimal.Decimal{}
	out.Units = len(pf.Units)
	for _, u := range pf.Units {
		x, ok := src[u]
		_, rn := nb.region(u)
		row := ResultRow{Commune: u, CommuneName: nb.commune(u), RegionName: rn, Values: map[string]string{}}
		if ok && (out.Basis == "closed" || x.Decision != "returned") {
			out.Reported++
			row.NilReport = x.NilReport
			for _, f := range out.Fields {
				val, has := x.Values[f.Key]
				if !has {
					continue
				}
				row.Values[f.Key] = val
				if d, err := decimal.NewFromString(val); err == nil {
					sum[f.Key] = sum[f.Key].Add(d)
				}
			}
		}
		out.Rows = append(out.Rows, row)
	}
	for _, f := range out.Fields {
		out.Total[f.Key] = sum[f.Key].String()
	}
	return out, nil
}

// ResultsXLSX dựng tệp Excel kết quả (B5 "xuất Excel"): một sheet, dòng xã + dòng Tổng.
func (s *Service) ResultsXLSX(ctx context.Context, id uuid.UUID) ([]byte, string, error) {
	r, err := s.Results(ctx, id)
	if err != nil {
		return nil, "", err
	}
	x := excelize.NewFile()
	defer func() { _ = x.Close() }()
	sh := "KetQua"
	if err := x.SetSheetName("Sheet1", sh); err != nil {
		return nil, "", err
	}
	set := func(col, row int, v any) error {
		cell, err := excelize.CoordinatesToCellName(col, row)
		if err != nil {
			return err
		}
		return x.SetCellValue(sh, cell, v)
	}
	v := r.Campaign.Version
	if err := set(1, 1, "Đợt điều tra: "+v.Name); err != nil {
		return nil, "", err
	}
	basis := "Số đã gửi (chưa đóng đợt)"
	if r.Basis == "closed" {
		basis = "Số lúc đóng đợt"
	}
	if err := set(1, 2, fmt.Sprintf("Thời gian %s – %s · %s · Đã nộp %d/%d xã", v.From.Format("02/01/2006"), v.To.Format("02/01/2006"), basis,
		r.Reported, r.Units)); err != nil {
		return nil, "", err
	}
	head := []any{"Mã xã", "Xã", "Khu vực"}
	for _, f := range r.Fields {
		label := f.Label
		if f.Unit != "" {
			label += " (" + f.Unit + ")"
		}
		head = append(head, label)
	}
	for i, h := range head {
		if err := set(i+1, 4, h); err != nil {
			return nil, "", err
		}
	}
	row := 5
	for _, rr := range r.Rows {
		vals := []any{rr.Commune, rr.CommuneName, rr.RegionName}
		for _, f := range r.Fields {
			vals = append(vals, numOrBlank(rr.Values[f.Key]))
		}
		for i, val := range vals {
			if err := set(i+1, row, val); err != nil {
				return nil, "", err
			}
		}
		row++
	}
	if err := set(1, row, "TỔNG"); err != nil {
		return nil, "", err
	}
	for i, f := range r.Fields {
		if err := set(i+4, row, numOrBlank(r.Total[f.Key])); err != nil {
			return nil, "", err
		}
	}
	buf, err := x.WriteToBuffer()
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "dot-dieu-tra-" + domain.PeriodKey(v.CampaignID, v.From) + ".xlsx", nil
}

func numOrBlank(s string) any {
	if s == "" {
		return ""
	}
	if d, err := decimal.NewFromString(s); err == nil {
		f, _ := d.Float64()
		return f
	}
	return s
}
